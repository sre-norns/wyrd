package client

import (
	"context"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// directoryClient searches the people and machine identities a project can
// add, and the projects a machine identity is authorized in.
type directoryClient struct {
	*Client
}

func (c *directoryClient) ProjectMemberCandidates(ctx context.Context, projectID model.ProjectID, term string, query manifest.SearchQuery) ([]model.ProjectMemberCandidate, manifest.Page, error) {
	values, err := SearchValues(query)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	values.Set("q", term)
	return Resource[model.ProjectMemberCandidate](c.Client).ListValues(ctx, ResourcePath("v1", "projects", string(projectID), "member-candidates"), values)
}

func (c *directoryClient) ProjectAgentCandidates(ctx context.Context, projectID model.ProjectID, term string, query manifest.SearchQuery) ([]model.ProjectAgentCandidate, manifest.Page, error) {
	values, err := SearchValues(query)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	values.Set("q", term)
	return Resource[model.ProjectAgentCandidate](c.Client).ListValues(ctx, ResourcePath("v1", "projects", string(projectID), "agent-candidates"), values)
}

func (c *directoryClient) AgentProjectAuthorizations(ctx context.Context, agentID model.AgentIdentityID, query manifest.SearchQuery) ([]model.AgentProjectAuthorization, manifest.Page, error) {
	return Resource[model.AgentProjectAuthorization](c.Client).List(ctx, ResourcePath("v1", "agent-identities", string(agentID), "project-authorizations"), query)
}
