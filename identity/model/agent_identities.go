package model

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type AgentIdentitiesService interface {
	List(ctx context.Context, accountID AccountID, query manifest.SearchQuery) (result []AgentIdentity, total int64, err error)
	Get(ctx context.Context, id AgentIdentityID) (resource AgentIdentity, exists bool, commError error)
	Create(ctx context.Context, accountID AccountID, identity AgentIdentity) (resource AgentIdentity, commError error)
	CreateOrUpdate(ctx context.Context, identity AgentIdentity) (resource AgentIdentity, created bool, commError error)
}

type AgentIdentityTokensService interface {
	List(ctx context.Context, agentID AgentIdentityID, query manifest.SearchQuery) (result []AgentIdentityToken, total int64, err error)
	Get(ctx context.Context, id AgentIdentityTokenID) (resource AgentIdentityToken, exists bool, commError error)
	Create(ctx context.Context, agentID AgentIdentityID, token AgentIdentityToken) (resource AgentIdentityToken, commError error)
	CreateOrUpdate(ctx context.Context, token AgentIdentityToken) (resource AgentIdentityToken, created bool, commError error)
}
