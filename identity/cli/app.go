// Package cli is the command-line kit every product's CLI shares: device-grant
// login, named profiles, a project context, output formats and manifest
// input. A product describes itself with an App, binds an *Env built from its
// own global flags, and mounts the kit's commands beside its own.
//
// The kit's commands are kong command structs: kong reads their tags, so the
// kit itself does not import kong.
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sre-norns/wyrd/identity/client"
	"github.com/sre-norns/wyrd/identity/model"
)

// App describes the product CLI the kit serves.
type App struct {
	// Name is the command users type, e.g. "urthctl"; messages name it.
	Name string
	// ClientID is the OAuth client the product registers for its CLI.
	ClientID string
	// ConfigDir names the directory under the user configuration directory
	// that holds profiles.json.
	ConfigDir string
	// LegacyConfigDirs are read, never written, when ConfigDir has no
	// profiles yet: where an earlier release kept them.
	LegacyConfigDirs []string
	// ProfilesEnv names the variable that points at a profile file instead.
	ProfilesEnv string
	// Getenv reads the environment; os.Getenv when nil. A product with
	// renamed variables supplies one that falls back to the old names.
	Getenv func(string) string
}

func (a App) getenv(name string) string {
	if a.Getenv != nil {
		return a.Getenv(name)
	}
	return os.Getenv(name)
}

// Env is one invocation: the product's global flags, resolved. Kit commands
// take it as their Run argument; bind it with kong.Bind.
type Env struct {
	App     App
	Context context.Context
	// Endpoint is the API server the flags or the environment name.
	Endpoint string
	// EndpointExplicit is set when the user named Endpoint rather than it
	// being the product's default. A profile for another endpoint is then an
	// error rather than silently used.
	EndpointExplicit bool
	// Token is an explicit bearer credential. It bypasses profiles.
	Token string
	// ProfileName is the profile this invocation uses; the default profile
	// when empty.
	ProfileName string
	Timeout     time.Duration
	HTTPClient  *http.Client
	Output      Output
	Stderr      io.Writer
}

func (e *Env) stderr() io.Writer {
	if e.Stderr != nil {
		return e.Stderr
	}
	return os.Stderr
}

func (e *Env) ctx() context.Context {
	if e.Context != nil {
		return e.Context
	}
	return context.Background()
}

// Client is an identity client of endpoint authenticated with token.
func (e *Env) Client(endpoint, token string) (*client.Client, error) {
	return client.New(endpoint, client.Config{HTTPClient: e.HTTPClient, Token: token, Timeout: e.Timeout})
}

// Selected is the credential an invocation runs with.
type Selected struct {
	// Name of the profile, or empty for an explicit token or no profile.
	Name    string
	Profile Profile
	// Found is false when no profile applies: an explicit token, or none
	// stored. Profile then carries only Endpoint and Token.
	Found bool
}

// Select resolves the profile this invocation runs with. An explicit token
// wins over profiles. A user profile whose token expires within 30 seconds is
// refreshed and saved first; a refresh that changes the profile's authority
// is refused rather than stored.
func (e *Env) Select() (Selected, error) {
	explicit := Selected{Profile: Profile{Endpoint: e.Endpoint, Token: e.Token}}
	if e.Token != "" {
		return explicit, nil
	}
	store, err := e.App.LoadProfiles()
	if err != nil {
		return explicit, err
	}
	name := e.ProfileName
	if name == "" {
		name = store.Default
	}
	if name == "" {
		return explicit, nil
	}
	p, found := store.Profiles[name]
	if !found {
		return explicit, fmt.Errorf("profile %q does not exist", name)
	}
	if e.EndpointExplicit && !sameEndpoint(e.Endpoint, p.Endpoint) {
		return explicit, fmt.Errorf("endpoint differs from profile %q; select the correct profile or provide an explicit token", name)
	}
	if p.Type != PrincipalUser && p.Type != PrincipalAgent {
		return explicit, fmt.Errorf("profile has an invalid principal type")
	}
	if p.Token == "" {
		return explicit, fmt.Errorf("profile %q is signed out; run %s auth login", name, e.App.Name)
	}
	if p.Type == PrincipalUser && !p.ExpiresAt.IsZero() && time.Until(p.ExpiresAt) <= 30*time.Second {
		if p.RefreshToken == "" {
			return explicit, fmt.Errorf("profile has expired; run %s auth login", e.App.Name)
		}
		api, err := e.Client(p.Endpoint, "")
		if err != nil {
			return explicit, err
		}
		tokens, err := api.RefreshToken(e.ctx(), e.App.ClientID, p.RefreshToken)
		if err != nil {
			return explicit, err
		}
		if tokens.Scope != p.Scope || tokens.AccountID != p.AccountID {
			return explicit, fmt.Errorf("refresh response changes profile authority; sign in again")
		}
		refreshed, err := tokenProfile(p.Endpoint, tokens)
		if err != nil {
			return explicit, err
		}
		refreshed.ProjectID, refreshed.ProjectName = p.ProjectID, p.ProjectName
		p = refreshed
		store.Profiles[name] = p
		if err = e.App.SaveProfiles(store); err != nil {
			return explicit, err
		}
	}
	return Selected{Name: name, Profile: p, Found: true}, nil
}

func sameEndpoint(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

// tokenProfile is the user profile a token response establishes. A response
// without credentials, expiry or one unambiguous authority is refused.
func tokenProfile(endpoint string, tokens model.TokenResponse) (Profile, error) {
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.ExpiresIn <= 0 {
		return Profile{}, fmt.Errorf("OAuth response lacks valid credentials or expiry")
	}
	if tokens.Scope != model.ScopeAccount && tokens.Scope != model.ScopeSystem {
		return Profile{}, fmt.Errorf("OAuth response lacks an authority scope")
	}
	if (tokens.Scope == model.ScopeSystem && tokens.AccountID != "") || (tokens.Scope == model.ScopeAccount && tokens.AccountID == "") {
		return Profile{}, fmt.Errorf("OAuth response has mixed authority")
	}
	return Profile{Endpoint: endpoint, Type: PrincipalUser, Scope: tokens.Scope, AccountID: tokens.AccountID, Token: tokens.AccessToken, RefreshToken: tokens.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)}, nil
}
