package client

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// These cases cover every identity route httpapi.Mount registers under /v1.
func TestServiceRoutes(t *testing.T) {
	cases := []struct{ service, operation, method, path string }{
		{"ServiceConfig", "Get", "GET", "/v1/service-configuration"},
		{"Principal", "Get", "GET", "/v1/principal"},
		{"PersonalProfile", "Get", "GET", "/v1/profile"},
		{"PersonalProfile", "Update", "PATCH", "/v1/profile"},
		{"PersonalProfile", "AccessibleAccounts", "GET", "/v1/profile/accounts"},
		{"SignInMethods", "List", "GET", "/v1/profile/sign-in-methods"},
		{"SignInMethods", "Get", "GET", "/v1/profile/sign-in-methods/id"},
		{"SignInMethods", "Revoke", "PATCH", "/v1/profile/sign-in-methods/id"},
		{"Sessions", "List", "GET", "/v1/sessions"},
		{"Sessions", "Get", "GET", "/v1/sessions/id"},
		{"Sessions", "CreateOrUpdate", "PATCH", "/v1/sessions/id"},
		{"Accounts", "List", "GET", "/v1/accounts"},
		{"Accounts", "CreateOrUpdate", "POST", "/v1/accounts"},
		{"Accounts", "Get", "GET", "/v1/accounts/id"},
		{"Accounts", "CreateOrUpdate", "PATCH", "/v1/accounts/id"},
		{"AccountMemberships", "List", "GET", "/v1/accounts/id/memberships"},
		{"AccountMemberships", "Create", "POST", "/v1/accounts/id/memberships"},
		{"AccountMemberships", "Get", "GET", "/v1/account-memberships/id"},
		{"AccountMemberships", "CreateOrUpdate", "PATCH", "/v1/account-memberships/id"},
		{"AccountInvitations", "List", "GET", "/v1/accounts/id/invitations"},
		{"AccountInvitations", "Create", "POST", "/v1/accounts/id/invitations"},
		{"AccountInvitations", "Get", "GET", "/v1/account-invitations/id"},
		{"AccountInvitations", "CreateOrUpdate", "PATCH", "/v1/account-invitations/id"},
		{"AccountInvitations", "RequestDelivery", "POST", "/v1/account-invitations/id/deliveries"},
		{"AccountInvitations", "Accept", "POST", "/v1/account-invitations/id/acceptance"},
		{"AgentIdentities", "List", "GET", "/v1/accounts/id/agent-identities"},
		{"AgentIdentities", "Create", "POST", "/v1/accounts/id/agent-identities"},
		{"AgentIdentities", "Get", "GET", "/v1/agent-identities/id"},
		{"AgentIdentities", "CreateOrUpdate", "PATCH", "/v1/agent-identities/id"},
		{"AgentIdentityTokens", "List", "GET", "/v1/agent-identities/id/tokens"},
		{"AgentIdentityTokens", "Create", "POST", "/v1/agent-identities/id/tokens"},
		{"AgentIdentityTokens", "Get", "GET", "/v1/agent-identity-tokens/id"},
		{"AgentIdentityTokens", "CreateOrUpdate", "PATCH", "/v1/agent-identity-tokens/id"},
		{"Projects", "List", "GET", "/v1/projects"},
		{"Projects", "ListForAccount", "GET", "/v1/accounts/id/projects"},
		{"Projects", "Create", "POST", "/v1/accounts/id/projects"},
		{"Projects", "Get", "GET", "/v1/projects/id"},
		{"Projects", "CreateOrUpdate", "PATCH", "/v1/projects/id"},
		{"Projects", "Update", "PATCH", "/v1/projects/id"},
		{"ProjectMemberships", "List", "GET", "/v1/projects/id/memberships"},
		{"ProjectMemberships", "Create", "POST", "/v1/projects/id/memberships"},
		{"ProjectMemberships", "Get", "GET", "/v1/project-memberships/id"},
		{"ProjectMemberships", "CreateOrUpdate", "PATCH", "/v1/project-memberships/id"},
		{"AgentAuthorizations", "List", "GET", "/v1/projects/id/agent-authorizations"},
		{"AgentAuthorizations", "Create", "POST", "/v1/projects/id/agent-authorizations"},
		{"AgentAuthorizations", "Get", "GET", "/v1/agent-authorizations/id"},
		{"AgentAuthorizations", "CreateOrUpdate", "PATCH", "/v1/agent-authorizations/id"},
		{"Directory", "ProjectMemberCandidates", "GET", "/v1/projects/id/member-candidates"},
		{"Directory", "ProjectAgentCandidates", "GET", "/v1/projects/id/agent-candidates"},
		{"Directory", "AgentProjectAuthorizations", "GET", "/v1/agent-identities/id/project-authorizations"},
	}
	for _, tc := range cases {
		t.Run(tc.service+"/"+tc.operation+"/"+tc.method, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != tc.method || r.URL.Path != "/prefix"+tc.path {
					t.Fatalf("got %s %s, want %s %s", r.Method, r.URL.Path, tc.method, tc.path)
				}
				if tc.method == "POST" && r.Header.Get("Idempotency-Key") == "" {
					t.Fatal("missing idempotency key")
				}
				if tc.method == "PATCH" && r.Header.Get("If-Match") != `"1"` {
					t.Fatal("missing precondition")
				}
				if r.URL.Query().Get("limit") == "2" {
					return reply(200, `{"items":[{"id":"returned"}],"limit":2,"next":"c2","total":3}`), nil
				}
				if tc.operation == "List" && tc.service == "SignInMethods" {
					return reply(200, `{"items":[{"id":"returned"}]}`), nil
				}
				status := 200
				if tc.method == "POST" {
					status = 201
				}
				return reply(status, `{"id":"returned","revision":2}`), nil
			})
			service := reflect.ValueOf(c).MethodByName(tc.service).Call(nil)[0]
			method := service.MethodByName(tc.operation)
			args := make([]reflect.Value, method.Type().NumIn())
			for i := range args {
				typ := method.Type().In(i)
				switch typ {
				case reflect.TypeOf((*context.Context)(nil)).Elem():
					args[i] = reflect.ValueOf(context.Background())
				case reflect.TypeOf(manifest.SearchQuery{}):
					args[i] = reflect.ValueOf(manifest.SearchQuery{Limit: 2})
				case reflect.TypeOf(url.Values{}):
					args[i] = reflect.ValueOf(url.Values{})
				default:
					v := reflect.New(typ).Elem()
					if typ.Kind() == reflect.String {
						v.SetString("id")
					} else if typ == reflect.TypeOf(model.PersonalProfile{}) {
						v.FieldByName("Revision").SetInt(1)
					} else if typ == reflect.TypeOf(model.SignInMethod{}) {
						v.FieldByName("ID").SetString("id")
						v.FieldByName("Revision").SetInt(1)
					} else if meta, ok := v.Addr().Interface().(interface{ Metadata() *model.Resource }); ok {
						meta.Metadata().AccountID = "id"
						meta.Metadata().Revision = 1
						if tc.method == "PATCH" {
							meta.Metadata().ID = "id"
						}
					}
					args[i] = v
				}
			}
			results := method.Call(args)
			if err := results[len(results)-1].Interface(); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("calls: %d", calls)
			}
			if results[0].Kind() == reflect.Slice && len(results) == 3 {
				page := results[1].Interface().(manifest.Page)
				if results[0].Len() != 1 || page.Total == nil || *page.Total != 3 || page.Next != "c2" || page.Limit != 2 {
					t.Fatalf("list response: %v", results)
				}
			}
		})
	}
}
