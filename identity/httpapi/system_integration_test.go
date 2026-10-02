package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
	"github.com/sre-norns/wyrd/identity/client"
	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
	"github.com/sre-norns/wyrd/internal/pgtest"
	"gorm.io/gorm"
)

func TestSystemCanonicalApprovalReplayAndPurge(t *testing.T) {
	db := pgtest.Open(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshotCalls := 0
	must(server.Register(db, server.Extensions{Auditor: server.AuditorFunc(func(_ context.Context, _ *gorm.DB, a server.Audit) error {
		if a.SystemAction != nil {
			snapshotCalls++
			var event e.SystemActivity
			if err := resource.Decode(a.Snapshot, &event); err != nil {
				return err
			}
			if event.Action != a.Action || event.TargetAccountID != a.Target.AccountID {
				t.Fatalf("system snapshot lost provenance: %+v", event)
			}
		}
		return nil
	})})) // replay cleanup must need no host table registration
	must(server.Migrate(db))
	cfg := server.DefaultConfig()
	cfg.Purge.Enabled = true
	cfg.Purge.ApprovalMode = "dual"
	srv := server.NewServiceWithConfig(db, &cfg)
	ctx := context.Background()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Mount(router, srv, Config{})
	MountSystem(router, srv)
	operator := func(email string) (*client.SystemClient, string) {
		t.Helper()
		must(srv.ProvisionUser(ctx, email, "correct horse battery", true))
		var u e.User
		must(db.Where("email = ?", email).First(&u).Error)
		tokens, err := server.IssueSession(ctx, db, cfg, u, "", cfg.WebClientID, server.StorageOauthGrant{Scope: e.ScopeSystem, AuthenticatedAt: time.Now().UTC(), AuthenticationMethod: "password"})
		must(err)
		c, err := client.New("https://identity.test", client.Config{Token: tokens["access_token"].(string), HTTPClient: &http.Client{Transport: routerTransport{router}}})
		must(err)
		return c.System(), u.ID
	}
	a, initiator := operator("operator-a@example.test")
	b, approver := operator("operator-b@example.test")
	_, badErr := a.CreateAccount(ctx, e.SystemAccountCreate{Name: "Invalid", OwnerEmail: "invalid", Reason: "field test"})
	var fieldProblem *client.Problem
	if !errors.As(badErr, &fieldProblem) || fieldProblem.Fields["owner_email"] == "" {
		t.Fatalf("command field path: %v", badErr)
	}
	retry := client.WithRequestOptions(ctx, client.RequestOptions{IdempotencyKey: "create-account"})
	var command e.SystemAccountCreate
	example, err := os.ReadFile("../examples/system-account-create.json")
	must(err)
	must(json.Unmarshal(example, &command))
	created, err := a.CreateAccount(retry, command)
	must(err)
	if created.ID == "" || created.InvitationToken == "" || created.LastModifiedBy.UserID != initiator {
		t.Fatalf("create: %+v", created)
	}
	replay, err := a.CreateAccount(retry, command)
	must(err)
	if replay.ID != created.ID || replay.InvitationToken != "" {
		t.Fatalf("replay leaked or duplicated: %+v", replay)
	}
	var count int64
	must(db.Table("accounts").Count(&count).Error)
	if count != 1 {
		t.Fatalf("duplicate account: %d", count)
	}
	must(db.Table("idempotency_records").Where("account_id = ?", created.ID).Count(&count).Error)
	if count != 1 {
		t.Fatalf("canonical create ownership missing: %d", count)
	}
	page, err := a.Accounts(ctx, e.SystemQuery{Limit: 1})
	must(err)
	if len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("nested resources: %+v", page)
	}
	invitations, err := a.Invitations(ctx, created.ID, e.SystemQuery{})
	must(err)
	if len(invitations.Items) != 1 || invitations.Items[0].ID != created.OwnerInvitationID {
		t.Fatalf("invitations: %+v", invitations)
	}
	// Recovery returns a one-time invitation only on its first mutation result.
	owner := e.User{ID: "recovery-owner", Email: "existing-owner@example.test", Status: "active", Revision: 1}
	member := e.AccountMembership{Resource: e.Resource{ID: "owner-membership", AccountID: e.AccountID(created.ID), Name: "owner", Status: "active", Revision: 1}, UserID: owner.ID, Role: "owner"}
	must(db.Create(&owner).Error)
	must(db.Create(&member).Error)
	recoveryAction := e.SystemAction{TargetID: member.ID, ReplacementEmail: "replacement@example.test", Delivery: "manual", Reason: "recover account"}
	recoverContext := client.WithRequestOptions(ctx, client.RequestOptions{IdempotencyKey: "recover-owner"})
	recovery, err := a.CreateOwnerRecovery(recoverContext, created.SystemAccount, recoveryAction)
	must(err)
	if recovery.InvitationToken == "" {
		t.Fatal("recovery omitted one-time token")
	}
	repeated, err := a.CreateOwnerRecovery(recoverContext, created.SystemAccount, recoveryAction)
	must(err)
	if repeated.ID != recovery.ID || repeated.InvitationToken != "" {
		t.Fatal("recovery replay duplicated or leaked")
	}
	readRecovery, err := a.OwnerRecovery(ctx, recovery.ID)
	must(err)
	if readRecovery.InvitationToken != "" {
		t.Fatal("recovery read leaked credential")
	}
	invitations, err = b.Invitations(ctx, created.ID, e.SystemQuery{})
	must(err)
	for _, invitation := range invitations.Items {
		if invitation.ID == recovery.InvitationID {
			_, err = b.RevokeInvitation(ctx, invitation, e.SystemAction{Reason: "cancel recovery"})
			must(err)
		}
	}
	readRecovery, err = a.OwnerRecovery(ctx, recovery.ID)
	must(err)
	if readRecovery.Status != "cancelled" || readRecovery.ActorID != initiator || readRecovery.LastModifiedBy.UserID != approver {
		t.Fatalf("recovery cancellation attribution: %+v", readRecovery)
	}
	// Seed active account-owned credentials to exercise archive's bulk attribution.
	oldActor := e.Principal{Type: "user", UserID: "previous"}
	session := e.Session{Resource: e.Resource{ID: "owned-session", Name: "session", AccountID: e.AccountID(created.ID), Revision: 1, Status: "active", Actor: oldActor, Authority: "original-grant"}}
	token := e.AgentIdentityToken{Resource: e.Resource{ID: "owned-token", Name: "token", AccountID: e.AccountID(created.ID), Revision: 1, Status: "active", Actor: oldActor, Authority: "original-grant"}}
	must(db.Create(&session).Error)
	must(db.Create(&token).Error)
	preview, err := a.CreateImpactPreview(ctx, created.ID, e.SystemAction{Operation: "archive"})
	must(err)
	action := e.SystemAction{Operation: "archive", PreviewID: preview.ID, Confirmation: created.ID, Reason: "test archive"}
	_, err = a.ChangeLifecycle(client.WithRequestOptions(ctx, client.RequestOptions{IfMatch: `"99"`}), created.SystemAccount, action)
	var problem *client.Problem
	if !errors.As(err, &problem) || problem.Status != 412 {
		t.Fatalf("stale edit: %v", err)
	}
	archived, err := a.ChangeLifecycle(ctx, created.SystemAccount, action)
	must(err)
	if archived.Status != "archived" || archived.LastModifiedBy.UserID != initiator {
		t.Fatalf("archive: %+v", archived)
	}
	must(db.First(&session, "id = ?", session.ID).Error)
	must(db.First(&token, "id = ?", token.ID).Error)
	for _, r := range []e.Resource{session.Resource, token.Resource} {
		if r.Status != "revoked" || r.Revision != 2 || r.Actor.UserID != initiator || r.Authority != "original-grant" {
			t.Fatalf("bulk attribution: %+v", r)
		}
	}
	step, err := a.CreateStepUp(ctx, created.ID, e.SystemAction{Operation: "purge"})
	must(err)
	preview, err = a.CreateImpactPreview(ctx, created.ID, e.SystemAction{Operation: "purge"})
	must(err)
	request, err := a.RequestDeletion(ctx, archived, e.SystemAction{Operation: "purge", PreviewID: preview.ID, StepUpID: step.ID, Confirmation: created.ID, RecoveryAcknowledged: true, Reason: "test deletion"})
	must(err)
	if request.ActorID != initiator || request.LastModifiedBy.UserID != initiator {
		t.Fatalf("initiator: %+v", request)
	}
	_, err = a.ApproveDeletion(ctx, request, e.SystemAction{Confirmation: created.ID, Reason: "self approval"})
	if !errors.As(err, &problem) || problem.Code != "independent-approval-required" {
		t.Fatalf("self approval: %v", err)
	}
	step, err = b.CreateStepUp(ctx, created.ID, e.SystemAction{Operation: "approve-purge"})
	must(err)
	approved, err := b.ApproveDeletion(ctx, request, e.SystemAction{StepUpID: step.ID, Confirmation: created.ID, Reason: "independent approval"})
	must(err)
	if approved.ActorID != initiator || approved.LastModifiedBy.UserID != approver || len(approved.Approvals) != 1 || approved.Approvals[0].LastModifiedBy.UserID != approver {
		t.Fatalf("approval attribution: %+v", approved)
	}
	// A decoded/encoded read must retain immutable ownership independently of its last modifier.
	wire, err := resource.Encode(approved)
	must(err)
	data, err := json.Marshal(wire)
	must(err)
	if !strings.Contains(string(data), `"initiatorId":"`+initiator+`"`) || strings.Contains(string(data), "credentialId") {
		t.Fatalf("approval wire: %s", data)
	}
	must(db.Model(&e.AccountDeletionRequest{}).Where("id = ?", request.ID).Update("execute_after", time.Now().Add(-time.Minute)).Error)
	done, err := srv.ExecuteDuePurge(ctx)
	must(err)
	if !done {
		t.Fatal("purge did not run")
	}
	must(db.Table("idempotency_records").Where("account_id = ?", created.ID).Count(&count).Error)
	if count != 0 {
		t.Fatalf("account-owned replays retained: %d", count)
	}
	completed, err := b.DeletionRequest(ctx, request.ID)
	must(err)
	if completed.Status != "completed" || completed.ActorID != initiator || completed.LastModifiedBy.Type != "service" || len(completed.Approvals) != 1 {
		t.Fatalf("retained history: %+v", completed)
	}
	if snapshotCalls == 0 {
		t.Fatal("system mutations emitted no canonical snapshots")
	}
	var tomb e.AccountPurgeTombstone
	must(db.Where("target_account_id = ?", created.ID).First(&tomb).Error)
	if tomb.ActorID != initiator || tomb.LastModifiedBy.Type != "service" {
		t.Fatalf("tombstone: %+v", tomb)
	}
}
