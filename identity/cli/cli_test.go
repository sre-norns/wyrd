package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// fakeServer answers the identity routes the kit calls.
type fakeServer struct {
	mu        sync.Mutex
	polls     int
	refreshes int
	revoked   []string
	revokeErr bool
	account   model.AccountID
	tokenSeq  int
	projects  []model.Project
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = r.ParseForm()
	write := func(status int, v any) {
		w.WriteHeader(status)
		wire, err := resource.Encode(v)
		if err != nil {
			panic(err)
		}
		_ = json.NewEncoder(w).Encode(wire)
	}
	tokens := func() {
		f.tokenSeq++
		write(200, model.TokenResponse{AccessToken: "at" + string(rune('0'+f.tokenSeq)), RefreshToken: "rt" + string(rune('0'+f.tokenSeq)), Scope: model.ScopeAccount, AccountID: f.account, ExpiresIn: 900})
	}
	switch {
	case r.URL.Path == "/oauth/device_authorization":
		write(200, map[string]any{"device_code": "dc", "user_code": "UC", "verification_uri": "http://idp/oauth/device", "verification_uri_complete": "http://idp/oauth/device?user_code=UC", "expires_in": 30, "interval": 1})
	case r.URL.Path == "/oauth/token" && r.PostForm.Get("grant_type") == "refresh_token":
		f.refreshes++
		tokens()
	case r.URL.Path == "/oauth/token":
		f.polls++
		if f.polls == 1 {
			write(400, map[string]string{"error": "authorization_pending"})
			return
		}
		tokens()
	case r.URL.Path == "/oauth/revoke":
		f.revoked = append(f.revoked, r.PostForm.Get("token"))
		if f.revokeErr {
			write(500, map[string]string{"error": "server_error"})
			return
		}
		write(200, map[string]any{})
	case r.URL.Path == "/v1/principal":
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer at") {
			write(401, map[string]string{"code": "unauthenticated"})
			return
		}
		write(200, model.Principal{Type: "user", Scope: model.ScopeAccount, UserID: "u1", AccountID: f.account, AccountRole: "owner"})
	case strings.HasPrefix(r.URL.Path, "/v1/projects/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/projects/")
		for _, p := range f.projects {
			if p.ID == id {
				write(200, p)
				return
			}
		}
		write(404, map[string]string{"code": "not-found"})
	case r.URL.Path == "/v1/accounts/"+string(f.account)+"/projects":
		var items []model.Project
		for _, p := range f.projects {
			if strings.Contains(p.Name, r.URL.Query().Get("name")) {
				items = append(items, p)
			}
		}
		write(200, map[string]any{"items": items})
	default:
		write(404, map[string]string{"code": "not-found"})
	}
}

func project(id, name string) model.Project {
	return model.Project{Resource: model.Resource{ID: id, Name: name}}
}

func testEnv(t *testing.T, server *fakeServer) (*Env, *bytes.Buffer) {
	t.Helper()
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	stdout := &bytes.Buffer{}
	return &Env{
		App:      App{Name: "testctl", ClientID: "testctl", ConfigDir: "testctl", ProfilesEnv: "TESTCTL_PROFILES"},
		Context:  context.Background(),
		Endpoint: httpServer.URL,
		Timeout:  10 * time.Second,
		Output:   Output{Format: FormatTable, Stdout: stdout},
		Stderr:   &bytes.Buffer{},
	}, stdout
}

func isolate(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profiles.json")
	t.Setenv("TESTCTL_PROFILES", path)
	return path
}

func TestLoginStoresTheSessionAsTheDefaultProfile(t *testing.T) {
	path := isolate(t)
	server := &fakeServer{account: "acme"}
	env, _ := testEnv(t, server)

	name, p, err := (&LoginCmd{}).Login(env)
	if err != nil {
		t.Fatal(err)
	}
	if name != "default" || p.Token == "" || p.RefreshToken == "" || p.AccountID != "acme" || p.Endpoint != env.Endpoint {
		t.Fatalf("profile %q: %+v", name, p)
	}
	if !strings.Contains(env.Stderr.(*bytes.Buffer).String(), "http://idp/oauth/device?user_code=UC") {
		t.Error("login did not tell the person where to approve")
	}
	store, err := env.App.LoadProfiles()
	if err != nil || store.Default != "default" || store.Profiles["default"].Token != p.Token {
		t.Fatalf("stored %+v %v", store, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("the profile file holds credentials and must be private: %v %v", info.Mode(), err)
	}
}

// Signing in again to the same account keeps the project context; another
// account's projects are not this one's, so it drops it.
func TestLoginAgainKeepsTheContextOfTheSameAccount(t *testing.T) {
	isolate(t)
	server := &fakeServer{account: "acme"}
	env, _ := testEnv(t, server)
	store := Profiles{Default: "default", Profiles: map[string]Profile{
		"default": {Endpoint: env.Endpoint, Type: PrincipalUser, Scope: model.ScopeAccount, AccountID: "acme", ProjectID: "p1", ProjectName: "web"},
	}}
	if err := env.App.SaveProfiles(store); err != nil {
		t.Fatal(err)
	}
	if _, p, err := (&LoginCmd{}).Login(env); err != nil || p.ProjectID != "p1" {
		t.Fatalf("same account: %+v %v", p, err)
	}

	server.account, server.polls = "other", 0
	if _, p, err := (&LoginCmd{}).Login(env); err != nil || p.ProjectID != "" {
		t.Fatalf("another account kept the context: %+v %v", p, err)
	}
}

func TestSelectRefreshesAnExpiringSession(t *testing.T) {
	isolate(t)
	server := &fakeServer{account: "acme"}
	env, _ := testEnv(t, server)
	if err := env.App.SaveProfiles(Profiles{Default: "work", Profiles: map[string]Profile{
		"work": {Endpoint: env.Endpoint, Type: PrincipalUser, Scope: model.ScopeAccount, AccountID: "acme", Token: "old", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(10 * time.Second), ProjectID: "p1"},
	}}); err != nil {
		t.Fatal(err)
	}
	selected, err := env.Select()
	if err != nil {
		t.Fatal(err)
	}
	if server.refreshes != 1 || selected.Profile.Token == "old" || selected.Profile.ProjectID != "p1" {
		t.Fatalf("refresh %d: %+v", server.refreshes, selected.Profile)
	}
	store, _ := env.App.LoadProfiles()
	if store.Profiles["work"].Token != selected.Profile.Token {
		t.Error("the refreshed session was not saved; the spent refresh token would be used again")
	}

	// A refresh that answers for another account is refused, not stored.
	store.Profiles["work"] = Profile{Endpoint: env.Endpoint, Type: PrincipalUser, Scope: model.ScopeAccount, AccountID: "elsewhere", Token: "t", RefreshToken: "r", ExpiresAt: time.Now()}
	_ = env.App.SaveProfiles(store)
	if _, err := env.Select(); err == nil || !strings.Contains(err.Error(), "authority") {
		t.Fatalf("authority change: %v", err)
	}
}

func TestSelectRefusesAProfileOfAnotherEndpoint(t *testing.T) {
	isolate(t)
	env, _ := testEnv(t, &fakeServer{})
	_ = env.App.SaveProfiles(Profiles{Default: "prod", Profiles: map[string]Profile{
		"prod": {Endpoint: "https://prod.example", Type: PrincipalUser, Token: "t"},
	}})
	env.EndpointExplicit = true
	if _, err := env.Select(); err == nil || !strings.Contains(err.Error(), "endpoint differs") {
		t.Fatalf("a token for prod would be sent to %s: %v", env.Endpoint, err)
	}
	env.Token = "explicit"
	if selected, err := env.Select(); err != nil || selected.Found || selected.Profile.Token != "explicit" {
		t.Fatalf("an explicit token bypasses profiles: %+v %v", selected, err)
	}
}

func TestLogoutRevokesAndForgetsCredentials(t *testing.T) {
	for _, failing := range []bool{false, true} {
		isolate(t)
		server := &fakeServer{revokeErr: failing}
		env, _ := testEnv(t, server)
		_ = env.App.SaveProfiles(Profiles{Default: "default", Profiles: map[string]Profile{
			"default": {Endpoint: env.Endpoint, Type: PrincipalUser, Scope: model.ScopeAccount, AccountID: "acme", Token: "t", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour), ProjectID: "p1"},
		}})
		err := (&LogoutCmd{}).Run(env)
		if (err != nil) != failing {
			t.Fatalf("revocation failing=%t: %v", failing, err)
		}
		if len(server.revoked) != 1 || server.revoked[0] != "r" {
			t.Fatalf("revoked %v, want the refresh token", server.revoked)
		}
		store, _ := env.App.LoadProfiles()
		p := store.Profiles["default"]
		if p.Token != "" || p.RefreshToken != "" {
			t.Fatalf("failing=%t: credentials left on disk", failing)
		}
		if p.Endpoint == "" || p.ProjectID != "p1" {
			t.Fatal("logout forgot the profile's endpoint or context")
		}
		if _, err := env.Select(); err == nil || !strings.Contains(err.Error(), "auth login") {
			t.Fatalf("a signed-out profile must say how to sign in: %v", err)
		}
	}
}

func TestStatusShowsThePrincipal(t *testing.T) {
	isolate(t)
	env, stdout := testEnv(t, &fakeServer{account: "acme"})
	_ = env.App.SaveProfiles(Profiles{Default: "default", Profiles: map[string]Profile{
		"default": {Endpoint: env.Endpoint, Type: PrincipalUser, Scope: model.ScopeAccount, AccountID: "acme", Token: "at1", ExpiresAt: time.Now().Add(time.Hour), ProjectName: "web"},
	}})
	if err := (&StatusCmd{}).Run(env); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PROFILE", "default", "acme", "owner", "web"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, stdout)
		}
	}
}

func TestStatusOfARevokedSessionSaysToSignIn(t *testing.T) {
	isolate(t)
	env, _ := testEnv(t, &fakeServer{account: "acme"})
	_ = env.App.SaveProfiles(Profiles{Default: "default", Profiles: map[string]Profile{
		"default": {Endpoint: env.Endpoint, Type: PrincipalUser, Scope: model.ScopeAccount, AccountID: "acme", Token: "revoked-elsewhere", ExpiresAt: time.Now().Add(time.Hour)},
	}})
	if err := (&StatusCmd{}).Run(env); err == nil || !strings.Contains(err.Error(), "testctl auth login") {
		t.Fatalf("a revoked session: %v", err)
	}
}

func TestContextUseResolvesTheProject(t *testing.T) {
	isolate(t)
	server := &fakeServer{account: "acme", projects: []model.Project{project("p1", "web"), project("p2", "web-staging"), project("p3", "twin"), project("p4", "twin")}}
	env, _ := testEnv(t, server)
	_ = env.App.SaveProfiles(Profiles{Default: "default", Profiles: map[string]Profile{
		"default": {Endpoint: env.Endpoint, Type: PrincipalUser, Scope: model.ScopeAccount, AccountID: "acme", Token: "at1", ExpiresAt: time.Now().Add(time.Hour)},
	}})
	stored := func() Profile {
		store, _ := env.App.LoadProfiles()
		return store.Profiles["default"]
	}

	if err := (&ContextUseCmd{Project: "web"}).Run(env); err != nil {
		t.Fatal(err)
	}
	if p := stored(); p.ProjectID != "p1" || p.ProjectName != "web" {
		t.Fatalf("by name: %+v (a prefix match must not win)", p)
	}
	if err := (&ContextUseCmd{Project: "p2"}).Run(env); err != nil || stored().ProjectName != "web-staging" {
		t.Fatalf("by ID: %+v %v", stored(), err)
	}
	if err := (&ContextUseCmd{Project: "twin"}).Run(env); err == nil || !strings.Contains(err.Error(), "use its ID") {
		t.Fatalf("ambiguous: %v", err)
	}
	if err := (&ContextUseCmd{Project: "missing"}).Run(env); err == nil {
		t.Fatal("a missing project became the context")
	}
	if err := (&ContextClearCmd{}).Run(env); err != nil || stored().ProjectID != "" {
		t.Fatalf("clear: %v", err)
	}
}

func TestProfilesReadLegacyLocationsAndWriteTheCurrentOne(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no user configuration directory")
	}
	app := App{ConfigDir: "exp-bench", LegacyConfigDirs: []string{"experibench"}, ProfilesEnv: "TESTCTL_UNSET"}
	legacy := filepath.Join(configDir, "experibench", "profiles.json")
	_ = os.MkdirAll(filepath.Dir(legacy), 0700)
	body := []byte(`{"default":"old","profiles":{"old":{"endpoint":"https://legacy.example.test","type":"user"}}}`)
	if err = os.WriteFile(legacy, body, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := app.LoadProfiles()
	if err != nil || store.Default != "old" || store.Profiles["old"].Scope != model.ScopeAccount {
		t.Fatalf("legacy profiles not read: %+v %v", store, err)
	}
	if err = app.SaveProfiles(store); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(configDir, "exp-bench", "profiles.json")); err != nil {
		t.Fatalf("current profile file not written: %v", err)
	}
	if unchanged, _ := os.ReadFile(legacy); string(unchanged) != string(body) {
		t.Fatal("legacy profile file changed")
	}

	// A product's Getenv hook decides what the variable is called.
	app.Getenv = func(name string) string {
		if name == "TESTCTL_UNSET" {
			return "/from/hook.json"
		}
		return ""
	}
	if path, _ := app.ProfilesPath(); path != "/from/hook.json" {
		t.Fatalf("profiles path %q ignored the Getenv hook", path)
	}
}

func TestOutputFormats(t *testing.T) {
	created := time.Now().Add(-3 * time.Hour)
	m := manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{APIVersion: "v1", Kind: "scenarios"},
		Metadata: manifest.ObjectMeta{Name: "probe", Version: 4, CreatedAt: &created, Labels: manifest.Labels{"team": "web"}},
	}
	var out bytes.Buffer
	if err := RenderList(Output{Format: FormatWide, Stdout: &out}, []manifest.ResourceManifest{m}, manifest.Page{Next: "c2"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "KIND", "VERSION", "probe", "scenarios", "team=web", "3h", "--cursor c2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("table lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := RenderList(Output{Format: FormatYAML, Stdout: &out}, []manifest.ResourceManifest{m}, manifest.Page{Next: "c2"}); err != nil {
		t.Fatal(err)
	}
	yamlText := out.String()
	if strings.Contains(yamlText, "--cursor") || strings.Contains(yamlText, "{") {
		t.Errorf("YAML must be one block document:\n%s", yamlText)
	}
	if strings.Index(yamlText, "apiVersion") > strings.Index(yamlText, "metadata") {
		t.Errorf("YAML lost the wire's field order:\n%s", yamlText)
	}

	// `get -o yaml` of one resource is a document `apply -f` takes back.
	out.Reset()
	if err := RenderResource(Output{Format: FormatYAML, Stdout: &out}, m, nil); err != nil {
		t.Fatal(err)
	}
	back, _, err := DecodeObject[manifest.ResourceManifest](out.Bytes())
	if err != nil || back.Metadata.Name != "probe" || back.Metadata.Version != 4 || back.Metadata.Labels["team"] != "web" {
		t.Errorf("YAML does not read back: %+v %v\n%s", back, err, out.String())
	}

	out.Reset()
	if err := RenderResource(Output{Format: FormatJSON, Stdout: &out}, newProfileEntry("p", true, Profile{Token: "secret", RefreshToken: "secret"}), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "secret") {
		t.Fatal("a profile printed its credentials")
	}
}

// ownYAML writes itself as YAML differently from its JSON, as a type holding
// a time.Duration does.
type ownYAML struct {
	Timeout time.Duration `json:"timeout"`
}

func (o ownYAML) MarshalYAML() (any, error) {
	return map[string]string{"timeout": o.Timeout.String()}, nil
}

func TestATypeThatWritesItsOwnYAMLKeepsIt(t *testing.T) {
	for _, value := range []any{ownYAML{Timeout: 3 * time.Second}, []ownYAML{{Timeout: 3 * time.Second}}} {
		var out bytes.Buffer
		if err := (Output{Format: FormatYAML, Stdout: &out}).Encode(value); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "timeout: 3s") {
			t.Errorf("%T printed its JSON form:\n%s", value, out.String())
		}
	}
}

func TestDecodeObject(t *testing.T) {
	type doc struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	for _, input := range []string{`{"name":"a","count":2}`, "name: a\ncount: 2\n"} {
		value, fields, err := DecodeObject[doc]([]byte(input))
		if err != nil || value.Name != "a" || value.Count != 2 || len(fields) != 2 {
			t.Fatalf("%q: %+v %v %v", input, value, fields, err)
		}
	}
	for _, input := range []string{`{"name":"a","cuont":2}`, "name: a\ncuont: 2\n", `[]`, `null`} {
		if _, _, err := DecodeObject[doc]([]byte(input)); err == nil {
			t.Errorf("%q was accepted", input)
		}
	}
}

func TestTokenResponsesWithoutOneAuthorityAreRefused(t *testing.T) {
	if _, err := tokenProfile("https://service.test", model.TokenResponse{AccessToken: "a", RefreshToken: "r", ExpiresIn: 1, Scope: model.ScopeSystem}); err != nil {
		t.Fatalf("a system session was refused: %v", err)
	}
	for _, response := range []model.TokenResponse{
		{AccessToken: "a", RefreshToken: "r", ExpiresIn: 1},
		{AccessToken: "a", RefreshToken: "r", ExpiresIn: 1, Scope: model.ScopeSystem, AccountID: "a"},
		{AccessToken: "a", RefreshToken: "r", ExpiresIn: 1, Scope: model.ScopeAccount},
		{AccessToken: "a", ExpiresIn: 1, Scope: model.ScopeAccount, AccountID: "a"},
		{AccessToken: "a", RefreshToken: "r", Scope: model.ScopeAccount, AccountID: "a"},
	} {
		if _, err := tokenProfile("https://service.test", response); err == nil {
			t.Errorf("accepted %+v", response)
		}
	}
}
