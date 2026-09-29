package identity

import (
	"context"

	expbench "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type agentAuthorizationsService struct {
	db *gorm.DB
}

func (a *agentAuthorizationsService) List(ctx context.Context, projectID expbench.ProjectID, query manifest.SearchQuery) (result []expbench.AgentAuthorization, page manifest.Page, err error) {
	return list[expbench.AgentAuthorization](ctx, a.db, query, "project_id = ?", projectID)
}

func (a *agentAuthorizationsService) Get(ctx context.Context, id expbench.AgentAuthorizationID) (resource expbench.AgentAuthorization, exists bool, commError error) {
	return get[expbench.AgentAuthorization](ctx, a.db, string(id))
}

func (a *agentAuthorizationsService) Create(ctx context.Context, projectID expbench.ProjectID, authorization expbench.AgentAuthorization) (resource expbench.AgentAuthorization, commError error) {
	authorization.ProjectID = projectID
	return create(ctx, a.db, authorization)
}

func (a *agentAuthorizationsService) CreateOrUpdate(ctx context.Context, authorization expbench.AgentAuthorization) (resource expbench.AgentAuthorization, created bool, commError error) {
	return upsert(ctx, a.db, authorization)
}
