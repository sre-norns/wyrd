package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Principal types a profile holds credentials for.
const (
	PrincipalUser  = "user"
	PrincipalAgent = "agent"
)

// Profile is one stored sign-in: where, as whom, and in which project.
type Profile struct {
	Scope        model.SessionScope    `json:"scope"`
	Endpoint     string                `json:"endpoint"`
	Type         string                `json:"type"`
	AccountID    model.AccountID       `json:"account_id,omitempty"`
	AgentID      model.AgentIdentityID `json:"agent_id,omitempty"`
	Token        string                `json:"token,omitempty"`
	RefreshToken string                `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time             `json:"expires_at,omitempty"`
	// ProjectID is the context: the project commands address unless told
	// otherwise. ProjectName is how it was named, for display.
	ProjectID   model.ProjectID `json:"project_id,omitempty"`
	ProjectName string          `json:"project_name,omitempty"`
}

// Redacted is the profile without its credentials.
func (p Profile) Redacted() Profile { p.Token = ""; p.RefreshToken = ""; return p }

// Profiles is the profile file.
type Profiles struct {
	Default  string             `json:"default"`
	Profiles map[string]Profile `json:"profiles"`
}

// ProfilesPath is where the profiles are written.
func (a App) ProfilesPath() (string, error) {
	if a.ProfilesEnv != "" {
		if path := a.getenv(a.ProfilesEnv); path != "" {
			return path, nil
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, a.ConfigDir, "profiles.json"), nil
}

// readProfileFile reads the profile file. Without an explicit path, it falls
// back to the legacy locations when the current file does not exist.
func (a App) readProfileFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if !errors.Is(err, os.ErrNotExist) || (a.ProfilesEnv != "" && a.getenv(a.ProfilesEnv) != "") {
		return data, err
	}
	dir, dirErr := os.UserConfigDir()
	if dirErr != nil {
		return nil, err
	}
	for _, legacy := range a.LegacyConfigDirs {
		data, legacyErr := os.ReadFile(filepath.Join(dir, legacy, "profiles.json"))
		if !errors.Is(legacyErr, os.ErrNotExist) {
			return data, legacyErr
		}
	}
	return nil, err
}

func (a App) LoadProfiles() (Profiles, error) {
	store := Profiles{Profiles: map[string]Profile{}}
	path, err := a.ProfilesPath()
	if err != nil {
		return store, err
	}
	data, err := a.readProfileFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	if err = json.Unmarshal(data, &store); err != nil {
		return store, fmt.Errorf("read profiles: %w", err)
	}
	if store.Profiles == nil {
		store.Profiles = map[string]Profile{}
	}
	for name, p := range store.Profiles {
		if p.Scope == "" {
			p.Scope = model.ScopeAccount
			store.Profiles[name] = p
		}
	}
	return store, nil
}

// SaveProfiles replaces the profile file atomically, readable only by its
// owner: it holds credentials.
func (a App) SaveProfiles(store Profiles) error {
	path, err := a.ProfilesPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".profiles-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// ProfileEntry is a named, credential-free view of a stored profile.
type ProfileEntry struct {
	Name    string  `json:"name"`
	Default bool    `json:"default"`
	Profile Profile `json:"profile"`
}

func newProfileEntry(name string, isDefault bool, p Profile) ProfileEntry {
	return ProfileEntry{Name: name, Default: isDefault, Profile: p.Redacted()}
}

func (e ProfileEntry) TableHeader(wide bool) []string {
	if wide {
		return []string{"NAME", "DEFAULT", "TYPE", "SCOPE", "ENDPOINT", "ACCOUNT", "PROJECT", "AGENT", "EXPIRES"}
	}
	return []string{"NAME", "DEFAULT", "TYPE", "SCOPE", "ENDPOINT", "PROJECT", "EXPIRES"}
}

func (e ProfileEntry) TableRow(wide bool) []any {
	defaultMark := ""
	if e.Default {
		defaultMark = "*"
	}
	row := []any{e.Name, defaultMark, e.Profile.Type, e.Profile.Scope, e.Profile.Endpoint}
	if wide {
		row = append(row, e.Profile.AccountID)
	}
	row = append(row, e.Profile.projectLabel())
	if wide {
		row = append(row, e.Profile.AgentID)
	}
	if e.Profile.Token == "" && e.Profile.ExpiresAt.IsZero() {
		return append(row, "signed out")
	}
	return append(row, humanizeExpiry(e.Profile.ExpiresAt))
}

func (p Profile) projectLabel() string {
	switch {
	case p.ProjectName != "":
		return p.ProjectName
	case p.ProjectID != "":
		return string(p.ProjectID)
	}
	return "-"
}

// humanizeExpiry renders how long until a profile token expires.
func humanizeExpiry(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Until(t)
	if d <= 0 {
		return "expired"
	}
	return "in " + ShortDuration(d)
}

type (
	ProfileListCmd struct{}
	ProfileShowCmd struct {
		Name string `arg:"" optional:"" help:"Profile name; default profile if omitted"`
	}
	ProfileUseCmd struct {
		Name string `arg:"" help:"Profile name"`
	}
	ProfileRemoveCmd struct {
		Name string `arg:"" help:"Profile name"`
	}
	// ProfileCmd manages local profiles. Mount it as `profile`.
	ProfileCmd struct {
		List   ProfileListCmd   `cmd:"" help:"List local profiles without credentials"`
		Show   ProfileShowCmd   `cmd:"" help:"Show a profile without credentials"`
		Use    ProfileUseCmd    `cmd:"" help:"Set the default profile"`
		Remove ProfileRemoveCmd `cmd:"" help:"Remove a local profile"`
	}
)

func (c *ProfileListCmd) Run(env *Env) error {
	store, err := env.App.LoadProfiles()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(store.Profiles))
	for name := range store.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]ProfileEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, newProfileEntry(name, name == store.Default, store.Profiles[name]))
	}
	total := int64(len(entries))
	return RenderList(env.Output, entries, manifest.Page{Limit: uint(len(entries)), Total: &total})
}

func (c *ProfileShowCmd) Run(env *Env) error {
	store, err := env.App.LoadProfiles()
	if err != nil {
		return err
	}
	name := c.Name
	if name == "" {
		name = env.ProfileName
	}
	if name == "" {
		name = store.Default
	}
	p, found := store.Profiles[name]
	if !found {
		return fmt.Errorf("profile %q does not exist", name)
	}
	return RenderResource(env.Output, newProfileEntry(name, name == store.Default, p), nil)
}

func (c *ProfileUseCmd) Run(env *Env) error {
	store, err := env.App.LoadProfiles()
	if err != nil {
		return err
	}
	if _, found := store.Profiles[c.Name]; !found {
		return fmt.Errorf("profile %q does not exist", c.Name)
	}
	store.Default = c.Name
	return env.App.SaveProfiles(store)
}

func (c *ProfileRemoveCmd) Run(env *Env) error {
	store, err := env.App.LoadProfiles()
	if err != nil {
		return err
	}
	if _, found := store.Profiles[c.Name]; !found {
		return fmt.Errorf("profile %q does not exist", c.Name)
	}
	delete(store.Profiles, c.Name)
	if store.Default == c.Name {
		store.Default = ""
	}
	return env.App.SaveProfiles(store)
}
