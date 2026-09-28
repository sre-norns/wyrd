package identity

import (
	"context"

	expbench "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type projectsService struct {
	db *gorm.DB
}

func (p *projectsService) List(ctx context.Context, query manifest.SearchQuery) (result []expbench.Project, total int64, err error) {
	return list[expbench.Project](ctx, p.db, query, "")
}

func (p *projectsService) ListForAccount(ctx context.Context, accountID expbench.AccountID, query manifest.SearchQuery) (result []expbench.Project, total int64, err error) {
	return list[expbench.Project](ctx, p.db, query, "account_id = ?", accountID)
}

func (p *projectsService) Get(ctx context.Context, id expbench.ProjectID) (resource expbench.Project, exists bool, commError error) {
	return get[expbench.Project](ctx, p.db, string(id))
}

func (p *projectsService) CreateOrUpdate(ctx context.Context, manifest expbench.Project) (resource expbench.Project, create bool, commError error) {
	return upsert(ctx, p.db, manifest)
}

func (p *projectsService) Create(ctx context.Context, manifest expbench.Project) (resource expbench.Project, commError error) {
	return create(ctx, p.db, manifest)
}

func (p *projectsService) Update(ctx context.Context, manifest expbench.Project) (resource expbench.Project, commError error) {
	resource, _, commError = upsert(ctx, p.db, manifest)
	return
}
