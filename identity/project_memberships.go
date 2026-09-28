package identity

import (
	"context"

	expbench "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type projectMembershipsService struct {
	config *Config
	db     *gorm.DB
}

func (p *projectMembershipsService) List(ctx context.Context, projectID expbench.ProjectID, query manifest.SearchQuery) (result []expbench.ProjectMembership, total int64, err error) {
	return list[expbench.ProjectMembership](ctx, p.db, query, "project_id = ?", projectID)
}

func (p *projectMembershipsService) Get(ctx context.Context, id expbench.ProjectMembershipID) (resource expbench.ProjectMembership, exists bool, commError error) {
	return get[expbench.ProjectMembership](ctx, p.db, string(id))
}

func (p *projectMembershipsService) Create(ctx context.Context, projectID expbench.ProjectID, membership expbench.ProjectMembership) (resource expbench.ProjectMembership, commError error) {
	membership.ProjectID = projectID
	out, _, err := p.grant(ctx, membership, true)
	return out, err
}

func (p *projectMembershipsService) CreateOrUpdate(ctx context.Context, membership expbench.ProjectMembership) (resource expbench.ProjectMembership, created bool, commError error) {
	return p.grant(ctx, membership, false)
}
