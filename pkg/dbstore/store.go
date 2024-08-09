package dbstore

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type transactionContext struct {
	unScoped bool
	Omit     map[string]struct{}
	Expand   map[string]manifest.SearchQuery
}

func newTransactionContext() transactionContext {
	return transactionContext{
		Omit:   map[string]struct{}{},
		Expand: map[string]manifest.SearchQuery{},
	}
}

type Option func(any, transactionContext) transactionContext

// Omit option allows to specify what fields should be omitted when writing or reading an entry
func Omit(value string) Option {
	return func(a any, tc transactionContext) transactionContext {
		tc.Omit[value] = struct{}{}
		return tc
	}
}

// Expand option instruct fetch operation to pull associated entries in one-to-many relation
func Expand(value string, searchQuery manifest.SearchQuery) Option {
	return func(a any, tc transactionContext) transactionContext {
		tc.Expand[value] = searchQuery
		return tc
	}
}

// IncludeDeleted enable operation to apply to soft-deleted entries too.
func IncludeDeleted() Option {
	return func(a any, tc transactionContext) transactionContext {
		tc.unScoped = true
		return tc
	}
}

// Store interface defines for manifest.ResourceModel storage
type Store interface {
	// Ping performs basic connectivity check to the store.
	Ping(context.Context) error

	Create(ctx context.Context, value any, options ...Option) error
	GetByUID(ctx context.Context, value any, id manifest.ResourceID, options ...Option) (exists bool, err error)
	GetByName(ctx context.Context, value any, id manifest.ResourceName, options ...Option) (exists bool, err error)
	GetWithVersion(ctx context.Context, dest any, id manifest.VersionedResourceID, options ...Option) (bool, error)
	Update(ctx context.Context, newValue any, id manifest.VersionedResourceID, options ...Option) (exists bool, err error)
	Delete(ctx context.Context, model any, id manifest.ResourceID, version manifest.Version, options ...Option) (existed bool, err error)
	Restore(ctx context.Context, model any, id manifest.ResourceID) (existed bool, err error)

	AddLinked(ctx context.Context, value any, link string, owner any, options ...Option) error
	RemoveLinked(ctx context.Context, value any, link string, owner any) error

	Find(ctx context.Context, dest any, searchQuery manifest.SearchQuery, options ...Option) (count int64, err error)
	FindLinked(ctx context.Context, dest any, link string, owner any, searchQuery manifest.SearchQuery, options ...Option) (totalCount int64, err error)

	FindNames(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.StringSet, error)
	FindLabels(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.StringSet, error)
	FindLabelValues(ctx context.Context, model any, key string, searchQuery manifest.SearchQuery, options ...Option) (manifest.StringSet, error)
}

type Transitional interface {
	Begin(context.Context) (StoreTransaction, error)
}

type StoreTransaction interface {
	Rollback()
	Commit() error

	Create(value any, options ...Option) error
	Update(newValue any, id manifest.VersionedResourceID, options ...Option) (exists bool, err error)
	Delete(model any, id manifest.ResourceID, version manifest.Version, options ...Option) (existed bool, err error)
	GetByUID(destValue any, id manifest.ResourceID, options ...Option) (exists bool, err error)
	GetByName(destValue any, id manifest.ResourceName, options ...Option) (exists bool, err error)

	AddLinked(value any, link string, owner any, options ...Option) error
	RemoveLinked(model any, link string, owner any) error
}

type TransitionalStore interface {
	Transitional
	Store
}
