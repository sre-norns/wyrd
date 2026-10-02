package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
	"github.com/sre-norns/wyrd/internal/pgtest"
	"gorm.io/gorm"
)

func TestCanonicalAuditSnapshotPreservesPolicyAndRollback(t *testing.T) {
	db := pgtest.Open(t)
	reject := false
	calls := 0
	check(t, Register(db, Extensions{Auditor: AuditorFunc(func(ctx context.Context, tx *gorm.DB, a Audit) error {
		calls++
		if _, ok := a.Resource.(*e.AgentIdentityToken); !ok {
			t.Fatalf("policy lost domain value: %T", a.Resource)
		}
		var token e.AgentIdentityToken
		check(t, resource.Decode(a.Snapshot, &token))
		if token.ID != "snapshot-token" || token.Token != "" || strings.Contains(string(a.Snapshot), "one-time-secret") || !strings.Contains(string(a.Snapshot), `"apiVersion":"identity.sre-norns.com/v1"`) {
			t.Fatalf("unsafe snapshot: %s", a.Snapshot)
		}
		if reject {
			return errors.New("audit unavailable")
		}
		return nil
	})}))
	check(t, Migrate(db))
	token := e.AgentIdentityToken{Resource: e.Resource{ID: "snapshot-token", Name: "snapshot", Status: "active", Revision: 1}, Token: "one-time-secret"}
	persist := func() error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Save(&token).Error; err != nil {
				return err
			}
			return record(context.Background(), tx, &token, "create", "")
		})
	}
	check(t, persist())
	reject = true
	token.Revision = 2
	if persist() == nil {
		t.Fatal("audit error did not abort")
	}
	var saved e.AgentIdentityToken
	check(t, db.First(&saved, "id = ?", token.ID).Error)
	if saved.Revision != 1 || calls != 2 {
		t.Fatalf("snapshot rollback: %+v, calls %d", saved, calls)
	}
}

func TestMembershipRevocationAttributionAndMethodHeartbeat(t *testing.T) {
	db := pgtest.Open(t)
	check(t, Register(db, Extensions{}))
	check(t, Migrate(db))
	check(t, db.Create(&e.Account{Resource: e.Resource{ID: "account", Name: "membership-test", Status: "active", Revision: 1}}).Error)
	original := e.Principal{Type: "user", UserID: "original"}
	actor := e.Principal{Type: "user", UserID: "operator", CredentialID: "never-public"}
	ctx := WithPrincipal(context.Background(), actor)
	member := e.AccountMembership{Resource: e.Resource{ID: "member", AccountID: "account", Revision: 1, Status: "active", Actor: original, Authority: "grant"}, UserID: "person", Role: "member"}
	project := e.ProjectMembership{Resource: e.Resource{ID: "project-member", AccountID: "account", Revision: 1, Status: "active", Actor: original, Authority: "grant"}, UserID: "person"}
	session := e.Session{Resource: e.Resource{ID: "session", AccountID: "account", Revision: 1, Status: "active", Actor: original, Authority: "grant"}, UserID: "person"}
	check(t, db.Create(&member).Error)
	check(t, db.Create(&project).Error)
	check(t, db.Create(&session).Error)
	check(t, db.Transaction(func(tx *gorm.DB) error { return revokeMembership(ctx, tx, &member) }))
	check(t, db.First(&project, "id = ?", project.ID).Error)
	check(t, db.First(&session, "id = ?", session.ID).Error)
	for _, r := range []e.Resource{member.Resource, project.Resource, session.Resource} {
		if r.Status != "revoked" || r.Revision != 2 || r.Actor.UserID != actor.UserID || r.Authority != "grant" {
			t.Fatalf("revocation: %+v", r)
		}
	}
	check(t, activateEmailMethod(db, "person"))
	var method userSignInMethod
	check(t, db.Where("user_id = ?", "person").First(&method).Error)
	if method.LastModifiedBy.UserID != "person" {
		t.Fatalf("method creator: %+v", method)
	}
	check(t, markMethodUsed(db, "person", "password", time.Now().UTC()))
	var used userSignInMethod
	check(t, db.First(&used, "id = ?", method.ID).Error)
	if used.Revision != method.Revision || used.LastModifiedBy != method.LastModifiedBy || used.LastUsedAt == nil {
		t.Fatalf("heartbeat changed version attribution: %+v", used)
	}
}
