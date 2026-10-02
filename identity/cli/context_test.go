package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
)

func TestContextNameLookupFollowsPagesBeforeChangingTheProfile(t *testing.T) {
	for _, result := range []string{"found", "ambiguous", "later-error"} {
		t.Run(result, func(t *testing.T) {
			isolate(t)
			var cursors []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/projects/prod" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if r.URL.Path != "/v1/accounts/acme/projects" || r.URL.Query().Get("name") != "prod" || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("unexpected lookup: %s", r.URL)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				cursor := r.URL.Query().Get("cursor")
				cursors = append(cursors, cursor)
				var items []model.Project
				next := ""
				switch cursor {
				case "":
					items = []model.Project{project("east", "prod-east"), project("west", "prod-west")}
					next = "second+/="
				case "second+/=":
					items = []model.Project{project("target", "prod")}
					if result != "found" {
						next = "third"
					}
				case "third":
					if result == "later-error" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					items = []model.Project{project("duplicate", "prod")}
				default:
					t.Errorf("unexpected cursor %q", cursor)
				}
				wire, err := resource.Encode(items)
				if err != nil {
					t.Error(err)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"items": wire, "limit": 2, "next": next})
			}))
			defer server.Close()
			env := &Env{App: App{ProfilesEnv: "TESTCTL_PROFILES"}, Context: context.Background(), Endpoint: server.URL}
			before := Profile{Endpoint: server.URL, Type: PrincipalUser, Scope: model.ScopeAccount, AccountID: "acme", Token: "test-token", ExpiresAt: time.Now().Add(time.Hour), ProjectID: "original", ProjectName: "original"}
			if err := env.App.SaveProfiles(Profiles{Default: "work", Profiles: map[string]Profile{"work": before}}); err != nil {
				t.Fatal(err)
			}
			err := (&ContextUseCmd{Project: "prod"}).Run(env)
			store, loadErr := env.App.LoadProfiles()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			after := store.Profiles["work"]
			wantCursors := []string{"", "second+/="}
			if result == "found" {
				if err != nil || after.ProjectID != "target" || after.ProjectName != "prod" {
					t.Fatalf("exact match on page two: project=%s, error=%v", after.ProjectID, err)
				}
			} else {
				wantCursors = append(wantCursors, "third")
				if err == nil || after.ProjectID != before.ProjectID || after.ProjectName != before.ProjectName {
					t.Fatalf("failed lookup changed context: project=%s, error=%v", after.ProjectID, err)
				}
				if result == "ambiguous" && !strings.Contains(err.Error(), "use its ID") {
					t.Errorf("expected ambiguous-name error, got %v", err)
				}
			}
			if !reflect.DeepEqual(cursors, wantCursors) {
				t.Errorf("pages requested: %q; want %q", cursors, wantCursors)
			}
		})
	}
}
