package dbstore

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type TransactionContext struct {
	Omit   map[string]struct{}
	Expand map[string]manifest.SearchQuery
}

func NewTransactionContext() TransactionContext {
	return TransactionContext{
		Omit:   map[string]struct{}{},
		Expand: map[string]manifest.SearchQuery{},
	}
}

type Option func(any, TransactionContext) TransactionContext

func Omit(value string) Option {
	return func(a any, tc TransactionContext) TransactionContext {
		tc.Omit[value] = struct{}{}
		return tc
	}
}

func Expand(value string, searchQuery manifest.SearchQuery) Option {
	return func(a any, tc TransactionContext) TransactionContext {
		tc.Expand[value] = searchQuery
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

	AddLinked(ctx context.Context, value any, link string, owner any, options ...Option) error
	FindLinked(ctx context.Context, dest any, link string, owner any, searchQuery manifest.SearchQuery, options ...Option) error

	FindNames(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.Labels, error)
	FindLabels(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.Labels, error)
	FindLabelValues(ctx context.Context, model any, key string, searchQuery manifest.SearchQuery, options ...Option) (manifest.Labels, error)
}
