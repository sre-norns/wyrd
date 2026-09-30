package client

import (
	"context"
	"net/url"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// projectsClient is the REST client for the ProjectsService API group.
type projectsClient struct {
	*Client
}

func (c *projectsClient) List(ctx context.Context, query manifest.SearchQuery) (result []model.Project, page manifest.Page, err error) {
	values, err := projectSearchValues(ctx, query)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	return Resource[model.Project](c.Client).ListValues(ctx, "/v1/projects", values)
}

func (c *projectsClient) ListForAccount(ctx context.Context, accountID model.AccountID, query manifest.SearchQuery) (result []model.Project, page manifest.Page, err error) {
	values, err := projectSearchValues(ctx, query)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	return Resource[model.Project](c.Client).ListValues(ctx, ResourcePath("v1", "accounts", string(accountID), "projects"), values)
}

// projectSearchValues builds the project collection query values, adding the
// free-text `q` term when one is attached to the context.
func projectSearchValues(ctx context.Context, query manifest.SearchQuery) (url.Values, error) {
	values, err := SearchValues(query)
	if err != nil {
		return nil, err
	}
	if term := projectSearchTerm(ctx); term != "" {
		values.Set("q", term)
	}
	return values, nil
}

func (c *projectsClient) Get(ctx context.Context, id model.ProjectID) (resource model.Project, exists bool, commError error) {
	return Resource[model.Project](c.Client).Get(ctx, ResourcePath("v1", "projects", string(id)))
}

func (c *projectsClient) CreateOrUpdate(ctx context.Context, manifest model.Project) (resource model.Project, create bool, commError error) {
	if manifest.ID == "" {
		return Resource[model.Project](c.Client).PostStatus(ctx, ResourcePath("/v1/accounts", string(manifest.AccountID), "projects"), manifest)
	}
	return Resource[model.Project](c.Client).Patch(ctx, ResourcePath("/v1/projects", manifest.ID), manifest)
}

func (c *projectsClient) Create(ctx context.Context, manifest model.Project) (resource model.Project, commError error) {
	return Resource[model.Project](c.Client).Post(ctx, ResourcePath("v1", "accounts", string(manifest.AccountID), "projects"), manifest)
}

func (c *projectsClient) Update(ctx context.Context, manifest model.Project) (resource model.Project, commError error) {
	value, _, err := Resource[model.Project](c.Client).Patch(ctx, ResourcePath("/v1/projects", manifest.ID), manifest)
	return value, err
}

type projectSearchKey struct{}

// WithProjectSearch attaches a case-insensitive free-text project search term
// that the project collection clients send as the `q` query parameter.
func WithProjectSearch(ctx context.Context, term string) context.Context {
	return context.WithValue(ctx, projectSearchKey{}, term)
}
func projectSearchTerm(ctx context.Context) string {
	term, _ := ctx.Value(projectSearchKey{}).(string)
	return term
}
