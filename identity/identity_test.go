package identity

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/identity/fakeidp"
	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/internal/pgtest"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type Probe struct{ manifest.ObjectMeta }

func TestStandaloneTenancyAndRegisteredVisibility(t *testing.T) {
	db := pgtest.Open(t)
	check(t, Register(db, Extensions{Kinds: map[string]Kind{"Probe": {Table: "probes", Scope: "project", MachineRead: true}}, GrantRoles: []e.RoleType{"runner"}, CredentialPurposes: map[string]string{"test-lease": "probes"}}))
	check(t, Migrate(db))
	check(t, db.AutoMigrate(&Probe{}))
	s := NewService(db)
	ctx := context.Background()
	check(t, s.ProvisionUser(ctx, "owner@example.test", "correct horse battery", false))
	var u user
	check(t, db.Where("email = ?", "owner@example.test").First(&u).Error)
	owner := WithPrincipal(ctx, e.Principal{Type: "user", UserID: u.ID})
	a, _, err := s.Accounts().CreateOrUpdate(owner, e.Account{Resource: e.Resource{Name: "one"}})
	check(t, err)
	owner = WithPrincipal(ctx, e.Principal{Type: "user", UserID: u.ID, AccountID: e.AccountID(a.ID), Scope: e.ScopeAccount})
	project, err := s.Projects().Create(owner, e.Project{Resource: e.Resource{Name: "first", AccountID: e.AccountID(a.ID)}})
	check(t, err)
	probe := Probe{ObjectMeta: manifest.ObjectMeta{Account: manifest.ResourceID(a.ID), Project: manifest.ResourceID(project.ID)}}
	visibility := Visibility{DB: db}
	check(t, visibility.Admit(owner, &probe))
	check(t, Authorize(owner, db, &probe, true))
	probe.UID = "b9b5b5a4-ed13-401d-a5cd-7847ac5c7170"
	check(t, db.Create(&probe).Error)
	check(t, s.ProvisionUser(ctx, "admin@example.test", "correct horse battery", false))
	var admin user
	check(t, db.Where("email = ?", "admin@example.test").First(&admin).Error)
	_, err = s.AccountMemberships().Create(owner, e.AccountID(a.ID), e.AccountMembership{UserID: admin.ID, Role: "admin"})
	check(t, err)
	adminctx := WithPrincipal(ctx, e.Principal{Type: "user", UserID: admin.ID, AccountID: e.AccountID(a.ID), Scope: e.ScopeAccount})
	if visibility.Admit(adminctx, &probe) == nil {
		t.Fatal("account administrator acquired project authority")
	}
	machine, err := s.MachineIdentities().Create(owner, e.AccountID(a.ID), e.MachineIdentity{Resource: e.Resource{Name: "runner"}})
	check(t, err)
	tok, err := s.MachineTokens().Create(owner, e.MachineIdentityID(machine.ID), e.MachineToken{})
	check(t, err)
	p, err := s.Authenticate(ctx, tok.Token)
	check(t, err)
	machinectx := WithPrincipal(ctx, p)
	query, err := visibility.Filter(machinectx, db.Model(&Probe{}), &Probe{})
	check(t, err)
	var count int64
	check(t, query.Count(&count).Error)
	if count != 0 {
		t.Fatal("ungranted machine can see project")
	}
	grant, err := s.MachineGrants().Create(owner, e.ProjectID(project.ID), e.MachineGrant{AgentID: e.MachineIdentityID(machine.ID), Roles: []e.RoleType{"runner"}})
	check(t, err)
	if !agentGrant(machinectx, db, e.ProjectID(project.ID), "runner") {
		t.Fatal("grant unavailable")
	}
	query, err = visibility.Filter(machinectx, db.Model(&Probe{}), &Probe{})
	check(t, err)
	check(t, query.Count(&count).Error)
	if count != 1 {
		t.Fatal("granted machine cannot see project resource")
	}
	check(t, Authorize(machinectx, db, &probe, false))
	if Authorize(machinectx, db, &probe, true) == nil {
		t.Fatal("machine read grant permits writes")
	}
	grant.Status = "revoked"
	_, _, err = s.MachineGrants().CreateOrUpdate(WithRequest(owner, Request{IfMatch: ETag(grant.Revision)}), grant)
	check(t, err)
	if agentGrant(machinectx, db, e.ProjectID(project.ID), "runner") {
		t.Fatal("revoked grant remains authoritative")
	}
	query, err = visibility.Filter(machinectx, db.Model(&Probe{}), &Probe{})
	check(t, err)
	check(t, query.Count(&count).Error)
	if count != 0 {
		t.Fatal("revoked machine can see project resource")
	}
	raw, err := ReplaceCredential(ctx, db, "test-lease", "owner")
	check(t, err)
	valid, err := VerifyCredential(ctx, db, "test-lease", "owner", raw)
	check(t, err)
	if !valid {
		t.Fatal("credential rejected")
	}
	if _, err := ReplaceCredential(ctx, db, "unregistered", "owner"); err == nil {
		t.Fatal("unregistered purpose accepted")
	}
	var c credential
	check(t, db.Where("owner_id = ? AND kind = ?", "owner", "test-lease").First(&c).Error)
	if c.Verifier == raw || strings.Contains(c.Verifier, raw) {
		t.Fatal("plaintext credential stored")
	}
	check(t, Migrate(db))
	var retained e.Account
	check(t, db.First(&retained, "id = ?", a.ID).Error)
	if db.Migrator().HasTable("machine_identities") || !db.Migrator().HasTable("agent_identities") {
		t.Fatal("identity table names changed")
	}
}
func TestGenericOIDCVerification(t *testing.T) {
	fake := fakeidp.New("client", "secret")
	h := httptest.NewServer(fake)
	defer h.Close()
	fake.URL = h.URL
	cfg := DefaultConfig()
	cfg.Development = true
	cfg.Providers = map[string]ProviderConfig{"oidc": {ClientID: "client", ClientSecret: "secret", IssuerURL: fake.GoogleIssuer()}}
	s := NewService(nil)
	check(t, s.Configure(cfg))
	provider := s.providers["oidc"]
	login := func(nonce string) string {
		t.Helper()
		fake.SetNext("google", fakeidp.Identity{Subject: "subject", Email: "person@example.test", EmailVerified: true})
		target, err := provider.AuthURL(context.Background(), "state", nonce, strings.Repeat("v", 43))
		check(t, err)
		client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := client.Get(target)
		check(t, err)
		res.Body.Close()
		redirect, err := url.Parse(res.Header.Get("Location"))
		check(t, err)
		return redirect.Query().Get("code")
	}
	identity, err := provider.Exchange(context.Background(), login("nonce"), "nonce", strings.Repeat("v", 43))
	check(t, err)
	if identity.Provider != "oidc" || identity.Email != "person@example.test" {
		t.Fatalf("wrong identity: %#v", identity)
	}
	if _, err = provider.Exchange(context.Background(), login("nonce"), "wrong", strings.Repeat("v", 43)); err == nil {
		t.Fatal("nonce mismatch accepted")
	}
	cfg.Development = false
	if err = s.Configure(cfg); err == nil {
		t.Fatal("production HTTP issuer accepted")
	}
}
func TestRegistryRejectsUnknownAndUnsafeKinds(t *testing.T) {
	db := pgtest.Open(t)
	if err := Register(db, Extensions{Kinds: map[string]Kind{"Bad": {Table: "bad; DROP TABLE users", Scope: "project"}}}); err == nil {
		t.Fatal("unsafe table accepted")
	}
	check(t, Register(db, Extensions{}))
	if _, err := (Visibility{DB: db}).Filter(WithServicePrincipal(context.Background()), db.Model(&Probe{}), &Probe{}); err == nil {
		t.Fatal("unknown kind bypassed registry")
	}
}

func TestMigrationRejectsFutureRevision(t *testing.T) {
	db := pgtest.Open(t)
	check(t, Migrate(db))
	check(t, db.Model(&schemaRevision{}).Where("id = 1").Update("version", 2).Error)
	if Migrate(db) == nil {
		t.Fatal("future schema accepted")
	}
	var r schemaRevision
	check(t, db.First(&r, 1).Error)
	if r.Version != 2 {
		t.Fatal("future schema downgraded")
	}
}

func TestPurgeRegisteredManifestKindRollsBackHookFailure(t *testing.T) {
	db := pgtest.Open(t)
	failHook := true
	hookCalls := 0
	check(t, Register(db, Extensions{Kinds: map[string]Kind{"Probe": {Table: "probes", IDColumn: "uid", Scope: "project", CredentialOwner: true,
		PurgeHook: func(ctx context.Context, tx *gorm.DB, account e.AccountID) error {
			hookCalls++
			if err := tx.Exec("DELETE FROM probes WHERE account_id = ?", account).Error; err != nil {
				return err
			}
			if failHook {
				return errors.New("injected failure")
			}
			return nil
		},
	}}}))
	check(t, Migrate(db))
	check(t, db.AutoMigrate(&Probe{}))
	account := e.Account{Resource: e.Resource{ID: "purge-account", Name: "purge", Revision: 1, Status: "deletion-pending"}}
	check(t, db.Create(&account).Error)
	probe := Probe{ObjectMeta: manifest.ObjectMeta{UID: "ff193ec4-4057-4c51-a0e7-56e54bac2270", Name: "owned", Account: "purge-account", Project: "project"}}
	check(t, db.Create(&probe).Error)
	request := e.AccountDeletionRequest{SystemRecord: e.SystemRecord{ID: "request", Status: "approved", Revision: 1}, TargetAccountID: "purge-account", ApprovalMode: "single", ExecuteAfter: time.Now().Add(-time.Minute)}
	check(t, db.Create(&request).Error)
	check(t, db.Create(&e.AccountDeletionApproval{SystemRecord: e.SystemRecord{ID: "approval", Status: "approved"}, RequestID: request.ID, UserID: "operator"}).Error)
	cfg := DefaultConfig()
	cfg.Purge.Enabled = true
	service := NewServiceWithConfig(db, &cfg)
	if done, err := service.ExecuteDuePurge(context.Background()); err == nil || done {
		t.Fatal("hook failure did not abort purge")
	}
	var n int64
	check(t, db.Model(&Probe{}).Count(&n).Error)
	if n != 1 || hookCalls != 1 {
		t.Fatalf("hook rollback: rows=%d calls=%d", n, hookCalls)
	}
	failHook = false
	check(t, db.Model(&request).Updates(map[string]any{"status": "approved", "execute_after": time.Now().Add(-time.Minute)}).Error)
	done, err := service.ExecuteDuePurge(context.Background())
	check(t, err)
	if !done {
		t.Fatal("purge did not complete")
	}
	check(t, db.Model(&Probe{}).Count(&n).Error)
	if n != 0 || hookCalls != 2 {
		t.Fatalf("purge: rows=%d calls=%d", n, hookCalls)
	}
}
