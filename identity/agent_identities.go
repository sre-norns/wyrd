package identity

import (
	"context"

	expbench "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type agentIdentitiesService struct {
	db *gorm.DB
}

func (a *agentIdentitiesService) List(ctx context.Context, accountID expbench.AccountID, query manifest.SearchQuery) (result []expbench.AgentIdentity, total int64, err error) {
	return list[expbench.AgentIdentity](ctx, a.db, query, "account_id = ?", accountID)
}

func (a *agentIdentitiesService) Get(ctx context.Context, id expbench.AgentIdentityID) (resource expbench.AgentIdentity, exists bool, commError error) {
	return get[expbench.AgentIdentity](ctx, a.db, string(id))
}

func (a *agentIdentitiesService) Create(ctx context.Context, accountID expbench.AccountID, identity expbench.AgentIdentity) (resource expbench.AgentIdentity, commError error) {
	identity.AccountID = accountID
	return create(ctx, a.db, identity)
}

func (a *agentIdentitiesService) CreateOrUpdate(ctx context.Context, identity expbench.AgentIdentity) (resource expbench.AgentIdentity, created bool, commError error) {
	return upsert(ctx, a.db, identity)
}

type agentIdentityTokensService struct {
	db *gorm.DB
}

func (a *agentIdentityTokensService) List(ctx context.Context, agentID expbench.AgentIdentityID, query manifest.SearchQuery) (result []expbench.AgentIdentityToken, total int64, err error) {
	return list[expbench.AgentIdentityToken](ctx, a.db, query, "agent_id = ?", agentID)
}

func (a *agentIdentityTokensService) Get(ctx context.Context, id expbench.AgentIdentityTokenID) (resource expbench.AgentIdentityToken, exists bool, commError error) {
	return get[expbench.AgentIdentityToken](ctx, a.db, string(id))
}

func (a *agentIdentityTokensService) Create(ctx context.Context, agentID expbench.AgentIdentityID, token expbench.AgentIdentityToken) (resource expbench.AgentIdentityToken, commError error) {
	token.AgentID = agentID
	return create(ctx, a.db, token)
}

func (a *agentIdentityTokensService) CreateOrUpdate(ctx context.Context, token expbench.AgentIdentityToken) (resource expbench.AgentIdentityToken, created bool, commError error) {
	return upsert(ctx, a.db, token)
}
