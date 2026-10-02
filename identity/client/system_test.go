package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
)

func TestEverySharedSystemRouteHasClient(t *testing.T) {
	cases := []struct{ operation, method, path string }{
		{"Accounts", "GET", "accounts"}, {"Account", "GET", "accounts/:id"},
		{"Memberships", "GET", "accounts/:id/memberships"}, {"Invitations", "GET", "accounts/:id/invitations"},
		{"ImpactPreviews", "GET", "accounts/:id/impact-previews"}, {"OwnerRecoveries", "GET", "accounts/:id/owner-recoveries"},
		{"OwnerRecovery", "GET", "owner-recoveries/:id"}, {"DeletionRequests", "GET", "deletion-requests"}, {"DeletionRequest", "GET", "deletion-requests/:id"},
		{"Activity", "GET", "activity"}, {"AuditEvents", "GET", "audit-events"}, {"Changes", "GET", "changes"}, {"Configuration", "GET", "configuration"},
		{"CreateAccount", "POST", "accounts"}, {"CreateImpactPreview", "POST", "accounts/:id/impact-previews"}, {"CreateStepUp", "POST", "accounts/:id/step-up-authorizations"},
		{"ChangeLifecycle", "PATCH", "accounts/:id"}, {"RevokeMembership", "PATCH", "account-memberships/:id"},
		{"CreateOwnerRecovery", "POST", "accounts/:id/owner-recoveries"}, {"CompleteOwnerRecovery", "PATCH", "owner-recoveries/:id"},
		{"RequestDeletion", "POST", "accounts/:id/deletion-requests"}, {"ApproveDeletion", "POST", "deletion-requests/:id/approvals"}, {"ChangeDeletionRequest", "PATCH", "deletion-requests/:id"},
		{"RequestInvitationDelivery", "POST", "account-invitations/:id/deliveries"}, {"RevokeInvitation", "POST", "account-invitations/:id/revocations"},
		{"CreateFirstOwnerInvitation", "POST", "accounts/:id/owner-invitations"},
	}
	inventory := map[string]bool{}
	data, err := os.ReadFile("../docs/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var routes []struct{ Method, Path string }
	if err = json.Unmarshal(data, &routes); err != nil {
		t.Fatal(err)
	}
	for _, r := range routes {
		if strings.HasPrefix(r.Path, "/v1/system/") {
			inventory[r.Method+" "+r.Path] = true
		}
	}
	for _, tc := range cases {
		key := tc.method + " /v1/system/" + tc.path
		if !inventory[key] {
			t.Fatalf("client route absent from inventory: %s", key)
		}
		delete(inventory, key)
		t.Run(tc.operation, func(t *testing.T) {
			var typ reflect.Type
			calls := 0
			conditional := false
			c := testClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != tc.method || r.URL.Path != "/prefix/v1/system/"+strings.ReplaceAll(tc.path, ":id", "record") {
					t.Fatalf("route %s %s", r.Method, r.URL.Path)
				}
				if conditional && r.Header.Get("If-Match") != `"7"` {
					t.Fatalf("precondition: %v", r.Header)
				}
				if tc.method == "POST" && r.Header.Get("Idempotency-Key") != "repeat-this-operation" {
					t.Fatal("lost caller's replay key")
				}
				if tc.method != "GET" {
					raw, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(raw), "apiVersion") || strings.Contains(string(raw), "lastModifiedBy") {
						t.Fatalf("command fabricated a resource: %s", raw)
					}
				}
				value := reflect.New(typ).Elem().Interface()
				var wire any
				var err error
				if tc.method == "GET" {
					wire, err = resource.Encode(value)
				} else {
					wire, err = resource.EncodeResult(value)
				}
				if err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(wire)
				if err != nil {
					t.Fatal(err)
				}
				return reply(200, string(raw)), nil
			})
			method := reflect.ValueOf(c.System()).MethodByName(tc.operation)
			typ = method.Type().Out(0)
			args := []reflect.Value{reflect.ValueOf(WithRequestOptions(context.Background(), RequestOptions{IdempotencyKey: "repeat-this-operation"}))}
			for i := 1; i < method.Type().NumIn(); i++ {
				v := reflect.New(method.Type().In(i)).Elem()
				if v.Kind() == reflect.String {
					v.SetString("record")
				}
				if meta, ok := v.Addr().Interface().(interface{ SystemMetadata() *model.SystemRecord }); ok {
					meta.SystemMetadata().ID = "record"
					meta.SystemMetadata().Revision = 7
					conditional = true
				}
				args = append(args, v)
			}
			results := method.Call(args)
			if err := results[1].Interface(); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("requests: %d", calls)
			}
		})
	}
	if len(inventory) != 0 {
		t.Fatalf("system routes missing a client: %v", inventory)
	}
}

func TestSystemClientRequiresReadVersionAndPreservesQueryTime(t *testing.T) {
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unversioned edit reached transport")
		return nil, nil
	})
	_, err := c.System().ChangeLifecycle(context.Background(), model.SystemAccount{SystemRecord: model.SystemRecord{ID: "account"}}, model.SystemAction{})
	var p *Problem
	if !errors.As(err, &p) || p.Status != 428 {
		t.Fatalf("missing precondition: %v", err)
	}
	at := time.Date(2026, 10, 2, 1, 2, 3, 123, time.FixedZone("offset", 3600))
	q, err := SystemSearchValues(model.SystemQuery{From: &at, Limit: 2, Search: "space & plus+", Cursor: "opaque/+="})
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("from") != at.Format(time.RFC3339Nano) || q.Get("limit") != "2" || q.Get("q") != "space & plus+" || q.Get("cursor") != "opaque/+=" {
		t.Fatalf("query: %v", q)
	}
}
