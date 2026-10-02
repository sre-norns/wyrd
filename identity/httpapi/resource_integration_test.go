package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
	"github.com/sre-norns/wyrd/identity/client"
	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
	"github.com/sre-norns/wyrd/internal/pgtest"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type routerTransport struct{ handler http.Handler }

func (r routerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	r.handler.ServeHTTP(w, req)
	return w.Result(), nil
}
func TestCanonicalHTTPWorkflow(t *testing.T) {
	db := pgtest.Open(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(server.Register(db, server.Extensions{}))
	must(server.Migrate(db))
	cfg := server.DefaultConfig()
	srv := server.NewServiceWithConfig(db, &cfg)
	ctx := context.Background()
	must(srv.ProvisionUser(ctx, "owner@example.test", "correct horse battery", false))
	var user e.User
	must(db.Where("email = ?", "owner@example.test").First(&user).Error)
	account, _, err := srv.Accounts().CreateOrUpdate(server.WithPrincipal(ctx, e.Principal{Type: "user", UserID: user.ID}), e.Account{Resource: e.Resource{Name: "acme"}})
	must(err)
	tokens, err := server.IssueSession(ctx, db, cfg, user, e.AccountID(account.ID), cfg.WebClientID)
	must(err)
	bearer := tokens["access_token"].(string)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Mount(router, srv, Config{})
	c, err := client.New("https://identity.test", client.Config{Token: bearer, HTTPClient: &http.Client{Transport: routerTransport{router}}})
	must(err)
	raw := func(method, path, body, etag, key string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	create := `{"apiVersion":"identity.sre-norns.com/v1","kind":"projects","metadata":{"name":"demo","labels":{"keep":"yes","remove":"old","status":"label"}},"spec":{"description":"description","target":"target"}}`
	first := raw("POST", "/v1/accounts/"+account.ID+"/projects", create, "", "create-project")
	if first.Code != 201 {
		t.Fatalf("create: %d %s", first.Code, first.Body)
	}
	var project e.Project
	must(resource.Decode(first.Body.Bytes(), &project))
	if project.ID == "" || project.AccountID != e.AccountID(account.ID) || first.Header().Get("ETag") != `"1"` {
		t.Fatalf("created %+v %v", project, first.Header())
	}
	replay := raw("POST", "/v1/accounts/"+account.ID+"/projects", create, "", "create-project")
	if replay.Code != 201 || replay.Header().Get("ETag") != first.Header().Get("ETag") || !sameJSON(replay.Body.Bytes(), first.Body.Bytes()) {
		t.Fatalf("retry: %d %s", replay.Code, replay.Body)
	}
	var count int64
	must(db.Model(&e.Project{}).Count(&count).Error)
	if count != 1 {
		t.Fatalf("duplicate create %d", count)
	}
	path := "/v1/projects/" + project.ID
	patch := `{"metadata":{"labels":{"remove":null,"add":"new"}},"spec":{"description":""}}`
	for _, test := range []struct {
		etag   string
		status int
	}{{"", 428}, {`"9"`, 412}, {`"1"`, 200}, {`"1"`, 412}} {
		got := raw("PATCH", path, patch, test.etag, "")
		if got.Code != test.status {
			t.Fatalf("patch %s: %d %s", test.etag, got.Code, got.Body)
		}
	}
	got, found, err := c.Projects().Get(ctx, e.ProjectID(project.ID))
	must(err)
	if !found || got.Revision != 2 || got.Description != "" || got.Target != "target" || got.Labels["keep"] != "yes" || got.Labels["add"] != "new" || got.Actor.UserID != user.ID {
		t.Fatalf("updated %+v", got)
	}
	if _, exists := got.Labels["remove"]; exists {
		t.Fatal("label was not removed")
	}
	fields, _ := manifest.ParseSelector("status.phase=active")
	labels, _ := manifest.ParseSelector("status=label")
	items, page, err := c.Projects().List(ctx, manifest.SearchQuery{Fields: fields, Selector: labels, Limit: 1})
	must(err)
	if len(items) != 1 || page.Total == nil || *page.Total != 1 {
		t.Fatalf("namespaces: %+v %+v", items, page)
	}
	foreign, _ := manifest.ParseSelector("metadata.account=foreign")
	items, page, err = c.Projects().List(ctx, manifest.SearchQuery{Fields: foreign})
	must(err)
	if len(items) != 0 || *page.Total != 0 {
		t.Fatal("foreign account filter bypassed visibility")
	}
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/v1/accounts/" + account.ID + "/projects", `{"name":"flat"}`, 422},
		{"PATCH", path, `{"status":{"phase":"inactive"}}`, 422},
		{"PATCH", path, `{"metadata":{"account":"foreign"}}`, 422},
		{"GET", "/v1/projects?fields=unknown=value", "", 400},
	} {
		result := raw(test.method, test.path, test.body, `"2"`, test.body)
		if result.Code != test.status {
			t.Fatalf("rejection %s: %d %s", test.body, result.Code, result.Body)
		}
		if !strings.Contains(result.Body.String(), `"requestId"`) {
			t.Fatalf("missing canonical problem %s", result.Body)
		}
	}
	changed, err := client.Resource[e.Project](c).Transition(ctx, path, got, "suspend")
	must(err)
	if changed.Status != "suspended" || changed.Revision != 3 {
		t.Fatalf("command result %+v", changed)
	}
	// A one-time credential is returned by its create operation, never a read or replay.
	agent, err := c.AgentIdentities().Create(ctx, e.AccountID(account.ID), e.AgentIdentity{Resource: e.Resource{Name: "worker"}})
	must(err)
	retryctx := client.WithRequestOptions(ctx, client.RequestOptions{IdempotencyKey: "mint-token"})
	token, err := c.AgentIdentityTokens().Create(retryctx, e.AgentIdentityID(agent.ID), e.AgentIdentityToken{})
	must(err)
	if token.Token == "" {
		t.Fatal("create omitted one-time token")
	}
	repeated, err := c.AgentIdentityTokens().Create(retryctx, e.AgentIdentityID(agent.ID), e.AgentIdentityToken{})
	must(err)
	if repeated.ID != token.ID || repeated.Token != "" {
		t.Fatal("retry duplicated or disclosed credential")
	}
	read := raw("GET", "/v1/agent-identity-tokens/"+token.ID, "", "", "")
	if read.Code != 200 || strings.Contains(read.Body.String(), token.Token) || strings.Contains(read.Body.String(), `"resource":`) {
		t.Fatalf("credential read %d %s", read.Code, read.Body)
	}
	// Profile null uses the same canonical writable spec and its current ETag.
	profile, found, err := c.PersonalProfile().Get(ctx)
	must(err)
	if !found {
		t.Fatal("profile missing")
	}
	name := "Example"
	profile.DisplayName = &name
	profile, err = c.PersonalProfile().Update(ctx, profile)
	must(err)
	profile.DisplayName = nil
	profile, err = c.PersonalProfile().Update(ctx, profile)
	must(err)
	if profile.DisplayName != nil || profile.LastModifiedBy.UserID != user.ID {
		t.Fatal("null profile edit lost")
	}
	// A different content type cannot bypass the canonical parser.
	req := httptest.NewRequest("PATCH", path, strings.NewReader(patch))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 415 {
		t.Fatalf("media type: %d %s", w.Code, w.Body)
	}
	// Query resources decode independently of their enclosing protocol DTO.
}

func sameJSON(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}
