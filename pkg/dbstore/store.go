package dbstore

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type TransactionContext struct {
	Omit   map[string]struct{}
	Expand map[string]struct{}
}

func NewTransactionContext() TransactionContext {
	return TransactionContext{
		Omit:   map[string]struct{}{},
		Expand: map[string]struct{}{},
	}
}

type Option func(any, TransactionContext) TransactionContext

func Omit(value string) Option {
	return func(a any, tc TransactionContext) TransactionContext {
		tc.Omit[value] = struct{}{}
		return tc
	}
}

func Expand(value string) Option {
	return func(a any, tc TransactionContext) TransactionContext {
		tc.Expand[value] = struct{}{}
		return tc
	}
}

// Store interface defines for manifest.ResourceModel storage
type Store interface {
	Create(ctx context.Context, value any, options ...Option) error
	Find(ctx context.Context, dest any, searchQuery manifest.SearchQuery, options ...Option) (count int64, err error)
	Get(ctx context.Context, value any, id manifest.ResourceID, options ...Option) (exists bool, err error)
	GetWithVersion(ctx context.Context, dest any, id manifest.VersionedResourceID, options ...Option) (bool, error)
	Delete(ctx context.Context, value any, id manifest.VersionedResourceID) (existed bool, err error)
	Update(ctx context.Context, value any, id manifest.VersionedResourceID) (exists bool, err error)

	Upsert(ctx context.Context, value any, options ...Option) error

	CreateLinked(ctx context.Context, value any, link string, owner any, options ...Option) error
}
