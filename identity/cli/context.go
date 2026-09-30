package cli

import (
	"fmt"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ContextCmd shows and sets the project a profile's commands address, like a
// kubectl context. The account is the one the profile signed in to. Mount it
// as `context`.
type ContextCmd struct {
	Show  ContextShowCmd  `cmd:"" default:"1" help:"Show the profile's account and project"`
	Use   ContextUseCmd   `cmd:"" help:"Set the project the profile's commands address"`
	Clear ContextClearCmd `cmd:"" help:"Forget the profile's project"`
}

type (
	ContextShowCmd struct{}
	ContextUseCmd  struct {
		Project string `arg:"" help:"Project name or ID"`
	}
	ContextClearCmd struct{}
)

// Context is what `context show` prints.
type Context struct {
	Profile     string          `json:"profile"`
	Endpoint    string          `json:"endpoint"`
	AccountID   model.AccountID `json:"account_id,omitempty"`
	ProjectID   model.ProjectID `json:"project_id,omitempty"`
	ProjectName string          `json:"project_name,omitempty"`
}

func (c Context) TableHeader(bool) []string {
	return []string{"PROFILE", "ENDPOINT", "ACCOUNT", "PROJECT"}
}

func (c Context) TableRow(bool) []any {
	project := c.ProjectName
	if project == "" {
		project = string(c.ProjectID)
	}
	return []any{c.Profile, c.Endpoint, dash(string(c.AccountID)), dash(project)}
}

func (c *ContextShowCmd) Run(env *Env) error {
	selected, err := env.selectStored()
	if err != nil {
		return err
	}
	p := selected.Profile
	return RenderResource(env.Output, Context{Profile: selected.Name, Endpoint: p.Endpoint, AccountID: p.AccountID, ProjectID: p.ProjectID, ProjectName: p.ProjectName}, nil)
}

// Run resolves the project on the server, so a typo fails here rather than on
// every later command: by ID first, then by name within the profile's account.
func (c *ContextUseCmd) Run(env *Env) error {
	selected, err := env.selectStored()
	if err != nil {
		return err
	}
	project, err := env.findProject(selected.Profile, c.Project)
	if err != nil {
		return err
	}
	return env.updateProfile(selected.Name, func(p *Profile) {
		p.ProjectID, p.ProjectName = model.ProjectID(project.ID), project.Name
	})
}

func (c *ContextClearCmd) Run(env *Env) error {
	selected, err := env.selectStored()
	if err != nil {
		return err
	}
	return env.updateProfile(selected.Name, func(p *Profile) {
		p.ProjectID, p.ProjectName = "", ""
	})
}

// selectStored is Select for commands that change a stored profile: an
// explicit token has none to change.
func (e *Env) selectStored() (Selected, error) {
	if e.Token != "" {
		return Selected{}, fmt.Errorf("a context belongs to a profile; drop the explicit token")
	}
	selected, err := e.Select()
	if err != nil {
		return selected, err
	}
	if !selected.Found {
		return selected, fmt.Errorf("no profile; run %s auth login", e.App.Name)
	}
	return selected, nil
}

func (e *Env) updateProfile(name string, change func(*Profile)) error {
	store, err := e.App.LoadProfiles()
	if err != nil {
		return err
	}
	p, found := store.Profiles[name]
	if !found {
		return fmt.Errorf("profile %q does not exist", name)
	}
	change(&p)
	store.Profiles[name] = p
	return e.App.SaveProfiles(store)
}

func (e *Env) findProject(p Profile, nameOrID string) (model.Project, error) {
	api, err := e.Client(p.Endpoint, p.Token)
	if err != nil {
		return model.Project{}, err
	}
	project, found, err := api.Projects().Get(e.ctx(), model.ProjectID(nameOrID))
	if err != nil {
		return project, err
	}
	if found {
		return project, nil
	}
	if p.AccountID == "" {
		return project, fmt.Errorf("project %q not found", nameOrID)
	}
	projects, _, err := api.Projects().ListForAccount(e.ctx(), p.AccountID, manifest.SearchQuery{Name: nameOrID, Limit: 2})
	if err != nil {
		return project, err
	}
	var exact []model.Project
	for _, candidate := range projects {
		if candidate.Name == nameOrID {
			exact = append(exact, candidate)
		}
	}
	switch len(exact) {
	case 0:
		return project, fmt.Errorf("project %q not found in account %s", nameOrID, p.AccountID)
	case 1:
		return exact[0], nil
	}
	return project, fmt.Errorf("more than one project is named %q; use its ID", nameOrID)
}
