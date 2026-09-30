package client

import (
	"context"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// agentIdentitiesClient is the REST client for the AgentIdentitiesService API group.
type agentIdentitiesClient struct {
	*Client
}

func (c *agentIdentitiesClient) List(ctx context.Context, accountID model.AccountID, query manifest.SearchQuery) (result []model.AgentIdentity, page manifest.Page, err error) {
	return Resource[model.AgentIdentity](c.Client).List(ctx, ResourcePath("v1", "accounts", string(accountID), "agent-identities"), query)
}

func (c *agentIdentitiesClient) Get(ctx context.Context, id model.AgentIdentityID) (resource model.AgentIdentity, exists bool, commError error) {
	return Resource[model.AgentIdentity](c.Client).Get(ctx, ResourcePath("v1", "agent-identities", string(id)))
}

func (c *agentIdentitiesClient) Create(ctx context.Context, accountID model.AccountID, identity model.AgentIdentity) (resource model.AgentIdentity, commError error) {
	return Resource[model.AgentIdentity](c.Client).Post(ctx, ResourcePath("v1", "accounts", string(accountID), "agent-identities"), identity)
}

func (c *agentIdentitiesClient) CreateOrUpdate(ctx context.Context, identity model.AgentIdentity) (resource model.AgentIdentity, created bool, commError error) {
	return Resource[model.AgentIdentity](c.Client).Patch(ctx, ResourcePath("/v1/agent-identities", identity.ID), identity)
}

// agentIdentityTokensClient is the REST client for the AgentIdentityTokensService API group.
type agentIdentityTokensClient struct {
	*Client
}

func (c *agentIdentityTokensClient) List(ctx context.Context, agentID model.AgentIdentityID, query manifest.SearchQuery) (result []model.AgentIdentityToken, page manifest.Page, err error) {
	return Resource[model.AgentIdentityToken](c.Client).List(ctx, ResourcePath("v1", "agent-identities", string(agentID), "tokens"), query)
}

func (c *agentIdentityTokensClient) Get(ctx context.Context, id model.AgentIdentityTokenID) (resource model.AgentIdentityToken, exists bool, commError error) {
	return Resource[model.AgentIdentityToken](c.Client).Get(ctx, ResourcePath("v1", "agent-identity-tokens", string(id)))
}

func (c *agentIdentityTokensClient) Create(ctx context.Context, agentID model.AgentIdentityID, token model.AgentIdentityToken) (resource model.AgentIdentityToken, commError error) {
	return Resource[model.AgentIdentityToken](c.Client).Post(ctx, ResourcePath("v1", "agent-identities", string(agentID), "tokens"), token)
}

func (c *agentIdentityTokensClient) CreateOrUpdate(ctx context.Context, token model.AgentIdentityToken) (resource model.AgentIdentityToken, created bool, commError error) {
	return Resource[model.AgentIdentityToken](c.Client).Patch(ctx, ResourcePath("/v1/agent-identity-tokens", token.ID), token)
}
