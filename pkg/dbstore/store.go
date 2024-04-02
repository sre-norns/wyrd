package dbstore

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type Store interface {
	Create(ctx context.Context, value any) error
	Get(ctx context.Context, value any, id manifest.ResourceID) (exists bool, err error)
	Delete(ctx context.Context, value any, id manifest.VersionedResourceID) (existed bool, err error)
	Update(ctx context.Context, value any, id manifest.VersionedResourceID) (exists bool, err error)
	Find(ctx context.Context, dest any, searchQuery manifest.SearchQuery) (count int64, err error)

	// GetByToken(ctx context.Context, value any, token ApiToken) (bool, error)
	// GetWithVersion(ctx context.Context, dest any, id wyrd.VersionedResourceId) (bool, error)
}
