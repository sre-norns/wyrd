package client

import (
	"context"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// agentAuthorizationsClient is the REST client for the AgentAuthorizationsService API group.
type agentAuthorizationsClient struct {
	*Client
}

func (c *agentAuthorizationsClient) List(ctx context.Context, projectID model.ProjectID, query manifest.SearchQuery) (result []model.AgentAuthorization, page manifest.Page, err error) {
	return Resource[model.AgentAuthorization](c.Client).List(ctx, ResourcePath("v1", "projects", string(projectID), "agent-authorizations"), query)
}

func (c *agentAuthorizationsClient) Get(ctx context.Context, id model.AgentAuthorizationID) (resource model.AgentAuthorization, exists bool, commError error) {
	return Resource[model.AgentAuthorization](c.Client).Get(ctx, ResourcePath("v1", "agent-authorizations", string(id)))
}

func (c *agentAuthorizationsClient) Create(ctx context.Context, projectID model.ProjectID, authorization model.AgentAuthorization) (resource model.AgentAuthorization, commError error) {
	return Resource[model.AgentAuthorization](c.Client).Post(ctx, ResourcePath("v1", "projects", string(projectID), "agent-authorizations"), authorization)
}

func (c *agentAuthorizationsClient) CreateOrUpdate(ctx context.Context, authorization model.AgentAuthorization) (resource model.AgentAuthorization, created bool, commError error) {
	return Resource[model.AgentAuthorization](c.Client).Patch(ctx, ResourcePath("/v1/agent-authorizations", authorization.ID), authorization)
}
