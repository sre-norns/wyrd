package cli

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/sre-norns/wyrd/identity/client"

	"github.com/sre-norns/wyrd/identity/model"
)

// AuthCmd signs in and out. Mount it as `auth`.
type AuthCmd struct {
	Login  LoginCmd  `cmd:"" help:"Sign in through the browser and store the session in a profile"`
	Logout LogoutCmd `cmd:"" help:"End the profile's session and forget its credentials"`
	Status StatusCmd `cmd:"" help:"Show who the profile signs in as"`
}

// LoginCmd is the device grant: the person approves the CLI in a browser,
// which works over SSH and on machines without one.
type LoginCmd struct {
	System bool `help:"Request an account-independent system session"`
}

func (c *LoginCmd) Run(env *Env) error {
	_, _, err := c.Login(env)
	return err
}

// Login signs in, stores the session in the invocation's profile ("default"
// when none is named) and makes it the default profile if there is none. It
// returns the profile for a product that does more after signing in.
func (c *LoginCmd) Login(env *Env) (string, Profile, error) {
	store, err := env.App.LoadProfiles()
	if err != nil {
		return "", Profile{}, err
	}
	name := env.ProfileName
	if name == "" {
		name = "default"
	}
	endpoint := env.Endpoint
	previous, existed := store.Profiles[name]
	if !env.EndpointExplicit && existed && previous.Endpoint != "" {
		// Signing a profile in again keeps it where it was.
		endpoint = previous.Endpoint
	}
	api, err := env.Client(endpoint, "")
	if err != nil {
		return "", Profile{}, err
	}
	preferred := string(model.ScopeAccount)
	if c.System {
		preferred = string(model.ScopeSystem)
	}
	device, err := api.StartDeviceAuthorization(env.ctx(), env.App.ClientID, preferred)
	if err != nil {
		return "", Profile{}, err
	}
	fmt.Fprintf(env.stderr(), "Open %s and approve code %s.\n", device.VerificationURIComplete, device.UserCode)
	tokens, err := api.AwaitDeviceToken(env.ctx(), env.App.ClientID, device)
	if err != nil {
		return "", Profile{}, err
	}
	p, err := tokenProfile(endpoint, tokens)
	if err != nil {
		return "", Profile{}, err
	}
	if (c.System && p.Scope != model.ScopeSystem) || (!c.System && p.Scope != model.ScopeAccount) {
		return "", Profile{}, fmt.Errorf("the selected authority differs from the requested profile context")
	}
	if existed && sameEndpoint(previous.Endpoint, p.Endpoint) && previous.AccountID == p.AccountID {
		// The same account again: its project is still the context.
		p.ProjectID, p.ProjectName = previous.ProjectID, previous.ProjectName
	}
	store.Profiles[name] = p
	if store.Default == "" {
		store.Default = name
	}
	if err = env.App.SaveProfiles(store); err != nil {
		return "", Profile{}, err
	}
	fmt.Fprintf(env.stderr(), "Signed in to %s as profile %q.\n", p.Endpoint, name)
	return name, p, nil
}

// LogoutCmd revokes a user profile's session on the server and removes the
// credentials of any profile. The profile stays, with its endpoint and
// context, for the next `auth login`.
type LogoutCmd struct{}

func (c *LogoutCmd) Run(env *Env) error {
	store, err := env.App.LoadProfiles()
	if err != nil {
		return err
	}
	name := env.ProfileName
	if name == "" {
		name = store.Default
	}
	p, found := store.Profiles[name]
	if !found {
		return fmt.Errorf("profile %q does not exist", name)
	}
	var revokeErr error
	if p.Type == PrincipalUser {
		token := p.RefreshToken
		if token == "" {
			token = p.Token
		}
		if token != "" {
			api, err := env.Client(p.Endpoint, "")
			if err != nil {
				return err
			}
			revokeErr = api.RevokeToken(env.ctx(), env.App.ClientID, token)
		}
	}
	// Forget the credentials whether or not the server could be told: a
	// failed revocation must not leave them on disk.
	p.Token, p.RefreshToken, p.ExpiresAt = "", "", time.Time{}
	store.Profiles[name] = p
	if err = env.App.SaveProfiles(store); err != nil {
		return err
	}
	if revokeErr != nil {
		return fmt.Errorf("signed out locally, but the server did not revoke the session: %w", revokeErr)
	}
	fmt.Fprintf(env.stderr(), "Signed out of profile %q.\n", name)
	return nil
}

// StatusCmd shows who the invocation's profile signs in as, as the server
// sees it.
type StatusCmd struct{}

// Status is what `auth status` prints.
type Status struct {
	Profile   string                `json:"profile,omitempty"`
	Endpoint  string                `json:"endpoint"`
	Type      string                `json:"type"`
	Scope     model.SessionScope    `json:"scope"`
	UserID    string                `json:"user_id,omitempty"`
	AgentID   model.AgentIdentityID `json:"agent_id,omitempty"`
	AccountID model.AccountID       `json:"account_id,omitempty"`
	Role      string                `json:"account_role,omitempty"`
	Project   string                `json:"project,omitempty"`
	ExpiresAt time.Time             `json:"expires_at,omitzero"`
}

func (s Status) TableHeader(wide bool) []string {
	if wide {
		return []string{"PROFILE", "ENDPOINT", "TYPE", "SCOPE", "USER", "AGENT", "ACCOUNT", "ROLE", "PROJECT", "EXPIRES"}
	}
	return []string{"PROFILE", "ENDPOINT", "TYPE", "ACCOUNT", "ROLE", "PROJECT", "EXPIRES"}
}

func (s Status) TableRow(wide bool) []any {
	row := []any{dash(s.Profile), s.Endpoint, s.Type}
	if wide {
		row = append(row, s.Scope, dash(s.UserID), dash(string(s.AgentID)))
	}
	return append(row, dash(string(s.AccountID)), dash(s.Role), dash(s.Project), humanizeExpiry(s.ExpiresAt))
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (c *StatusCmd) Run(env *Env) error {
	selected, err := env.Select()
	if err != nil {
		return err
	}
	if selected.Profile.Token == "" {
		return fmt.Errorf("not signed in; run %s auth login", env.App.Name)
	}
	api, err := env.Client(selected.Profile.Endpoint, selected.Profile.Token)
	if err != nil {
		return err
	}
	principal, found, err := api.Principal().Get(env.ctx())
	var problem *client.Problem
	if errors.As(err, &problem) && problem.Status == http.StatusUnauthorized {
		// Revoked elsewhere -- another device, the web sessions page -- or
		// expired past refreshing.
		return fmt.Errorf("the server no longer accepts this profile's session; run %s auth login", env.App.Name)
	}
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("the server does not recognise this credential; run %s auth login", env.App.Name)
	}
	return RenderResource(env.Output, Status{
		Profile:   selected.Name,
		Endpoint:  selected.Profile.Endpoint,
		Type:      principal.Type,
		Scope:     principal.Scope,
		UserID:    principal.UserID,
		AgentID:   principal.AgentID,
		AccountID: principal.AccountID,
		Role:      principal.AccountRole,
		Project:   selected.Profile.projectLabel(),
		ExpiresAt: selected.Profile.ExpiresAt,
	}, nil)
}
