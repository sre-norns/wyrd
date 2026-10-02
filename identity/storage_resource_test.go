package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

func TestSharedQueryExpiryAndLabelNamespaces(t *testing.T) {
	db, s := newPagingService(t)
	a := newPagingOwner(t, db, s, "owner@example.test", "one")
	i, err := s.AccountInvitations().Create(a.owner, a.account, e.AccountInvitation{Email: "invitee@example.test", Role: "member"})
	check(t, err)
	check(t, db.Model(&e.AccountInvitation{}).Where("id = ?", i.ID).Updates(map[string]any{"expires_at": time.Now().Add(-time.Minute), "labels": `{"status":"pending","snake_key":"opaque"}`}).Error)
	fields, err := manifest.ParseSelector("status.phase=expired")
	check(t, err)
	labels, err := manifest.ParseSelector("status=pending")
	check(t, err)
	items, page, err := s.AccountInvitations().List(a.owner, a.account, manifest.SearchQuery{Fields: fields, Selector: labels, Limit: 1})
	check(t, err)
	if len(items) != 1 || items[0].Status != "expired" || page.Total == nil || *page.Total != 1 {
		t.Fatalf("computed selection %+v %+v", items, page)
	}
	fields, _ = manifest.ParseSelector("status.phase=pending")
	items, _, err = s.AccountInvitations().List(a.owner, a.account, manifest.SearchQuery{Fields: fields})
	check(t, err)
	if len(items) != 0 {
		t.Fatal("expired invitation selected as pending")
	}
	fields, _ = manifest.ParseSelector("metadata.name>1")
	_, _, err = s.AccountInvitations().List(a.owner, a.account, manifest.SearchQuery{Fields: fields})
	requireProblem(t, err, "invalid-field-selector")
	fields, _ = manifest.ParseSelector("spec.unknown=value")
	_, _, err = s.AccountInvitations().List(a.owner, a.account, manifest.SearchQuery{Fields: fields})
	requireProblem(t, err, "unknown-field")
}
func TestScopedResourceNames(t *testing.T) {
	db, s := newPagingService(t)
	a := newPagingOwner(t, db, s, "a@example.test", "one")
	b := newPagingOwner(t, db, s, "b@example.test", "two")
	_, err := s.Projects().Create(a.owner, e.Project{Resource: e.Resource{Name: "demo", AccountID: a.account}})
	check(t, err)
	_, err = s.Projects().Create(a.owner, e.Project{Resource: e.Resource{Name: "DEMO", AccountID: a.account}})
	if err == nil {
		t.Fatal("duplicate scoped name accepted")
	}
	_, err = s.Projects().Create(b.owner, e.Project{Resource: e.Resource{Name: "demo", AccountID: b.account}})
	check(t, err)
}
func TestCanonicalReplayOwnershipAndSecrets(t *testing.T) {
	db, s := newPagingService(t)
	ctx := context.Background()
	check(t, s.ProvisionUser(ctx, "system@example.test", "correct horse battery", true))
	var u user
	check(t, db.Where("email = ?", "system@example.test").First(&u).Error)
	cfg := DefaultConfig()
	tokens, err := IssueSession(ctx, db, cfg, u, "", cfg.WebClientID, oauthGrant{Scope: e.ScopeSystem, AuthenticatedAt: time.Now(), AuthenticationMethod: "email"})
	check(t, err)
	token := tokens["access_token"].(string)
	req := WithRequest(ctx, Request{Method: "POST", Target: "/v1/system/accounts"})
	calls := 0
	fn := func(context.Context) HTTPOutcome {
		calls++
		return HTTPOutcome{AccountID: "created-account", Status: 201, Header: http.Header{"Etag": {`"1"`}}, Body: []byte(`{"resource":{"metadata":{"uid":"created-account","labels":{"token":"user-label","refreshToken":"also-label"}}},"ownerInvitation":{"resource":{"metadata":{"uid":"invitation"}},"token":"one-time"},"leaseToken":"lease","accessToken":"access","refreshToken":"refresh","invitationToken":"invite","access_token":"oauth"}`)}
	}
	first, err := s.TransactHTTP(req, token, "key", "canonical-create", fn)
	check(t, err)
	if !strings.Contains(string(first.Body), "one-time") {
		t.Fatal("initial result redacted")
	}
	replay, err := s.TransactHTTP(req, token, "key", "canonical-create", fn)
	check(t, err)
	if calls != 1 || replay.Header.Get("ETag") != `"1"` {
		t.Fatalf("replayed effects %d %+v", calls, replay)
	}
	var result map[string]any
	check(t, json.Unmarshal(replay.Body, &result))
	for _, key := range []string{"leaseToken", "accessToken", "refreshToken", "invitationToken", "access_token"} {
		if _, ok := result[key]; ok {
			t.Fatalf("replay leaked %s", key)
		}
	}
	if _, ok := result["ownerInvitation"].(map[string]any)["token"]; ok {
		t.Fatal("nested token leaked")
	}
	if !strings.Contains(string(replay.Body), "user-label") || !strings.Contains(string(replay.Body), "also-label") {
		t.Fatal("redaction modified label keys")
	}
	var record idempotencyRecord
	check(t, db.First(&record).Error)
	if record.AccountID != "created-account" {
		t.Fatalf("wrong replay owner %s", record.AccountID)
	}
	if _, err = s.TransactHTTP(req, token, "key", "different-body", fn); err == nil {
		t.Fatal("conflicting retry accepted")
	}
}
