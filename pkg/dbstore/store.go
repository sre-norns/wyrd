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
	Ping(context.Context) error

	Create(ctx context.Context, value any, options ...Option) error
	Get(ctx context.Context, value any, id manifest.ResourceID, options ...Option) (exists bool, err error)
	GetWithVersion(ctx context.Context, dest any, id manifest.VersionedResourceID, options ...Option) (bool, error)
	Update(ctx context.Context, newValue any, id manifest.VersionedResourceID, options ...Option) (exists bool, err error)
	Delete(ctx context.Context, model any, id manifest.VersionedResourceID) (existed bool, err error)

	AddLinked(ctx context.Context, value any, link string, owner any, options ...Option) error
	RemoveLinked(ctx context.Context, value any, link string, owner any) error

	Find(ctx context.Context, dest any, searchQuery manifest.SearchQuery, options ...Option) (count int64, err error)
	FindLinked(ctx context.Context, dest any, link string, owner any, searchQuery manifest.SearchQuery, options ...Option) (totalCount int64, err error)

	FindNames(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.Labels, error)
	FindLabels(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.Labels, error)
	FindLabelValues(ctx context.Context, model any, key string, searchQuery manifest.SearchQuery, options ...Option) (manifest.Labels, error)
}

type Transitional interface {
	Begin(context.Context) (StoreTransaction, error)
}

type StoreTransaction interface {
	Rollback()
	Commit() error

	Create(value any, options ...Option) error
	Update(newValue any, id manifest.VersionedResourceID, options ...Option) (exists bool, err error)
	Delete(model any, id manifest.VersionedResourceID) (existed bool, err error)
	Get(value any, id manifest.ResourceID, options ...Option) (exists bool, err error)

	AddLinked(value any, link string, owner any, options ...Option) error
	RemoveLinked(model any, link string, owner any) error
}

type TransitionalStore interface {
	Transitional
	Store
}
