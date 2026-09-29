package model

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type ProjectsService interface {
	List(ctx context.Context, query manifest.SearchQuery) (result []Project, page manifest.Page, err error)
	ListForAccount(ctx context.Context, accountID AccountID, query manifest.SearchQuery) (result []Project, page manifest.Page, err error)
	Get(ctx context.Context, id ProjectID) (resource Project, exists bool, commError error)
	CreateOrUpdate(ctx context.Context, manifest Project) (resource Project, create bool, commError error)
	Create(ctx context.Context, manifest Project) (resource Project, commError error)
	Update(ctx context.Context, manifest Project) (resource Project, commError error)
}
