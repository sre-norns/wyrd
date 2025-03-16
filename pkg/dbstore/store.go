package dbstore

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type orderByColumn struct {
	Column string
	Order  Order
}

type expandDetails struct {
	OrderBy orderByColumn
	Query   manifest.SearchQuery
}

type orderDetails struct {
	OrderColumns []orderByColumn
}

type transactionContext struct {
	Config          SchemaConfig
	unScoped        bool
	disableCounting bool
	Omit            manifest.StringSet
	Expand          map[string]expandDetails
	Order           orderDetails

	withVersion *manifest.Version
}

func newTransactionContext(config SchemaConfig) transactionContext {
	return transactionContext{
		Config: config,
		Omit:   map[string]struct{}{},
		Expand: map[string]expandDetails{},
	}
}

type Order int

const (
	OrderAscending  = Order(0)
	OrderDescending = Order(1)
)

// Option represents option that can be passed to some of the store methods to change results processing
type Option func(any, transactionContext) transactionContext

// Count option allows toggle if Find... calls should count number of potential matches.
// Disabling counting can lead to some performance improvements on large datasets and complex queries.
func Count(value bool) Option {
	return func(a any, tc transactionContext) transactionContext {
		tc.disableCounting = !value
		return tc
	}
}

// Omit option allows to specify what fields should be omitted when writing or reading an entry
func Omit(value ...string) Option {
	return func(a any, tc transactionContext) transactionContext {
		for _, v := range value {
			tc.Omit[v] = struct{}{}
		}
		return tc
	}
}

// Expand option instruct fetch operation to pull associated entries in one-to-many relation
func Expand(value string, searchQuery manifest.SearchQuery) Option {
	return func(a any, tc transactionContext) transactionContext {
		tc.Expand[value] = expandDetails{
			Query: searchQuery,
		}

		return tc
	}
}

// ExpandOrdered option instruct fetch operation to pull associated entries in one-to-many relation and sort them in ASCENDING order BY `updated_at“.
// Note if the model of association don't have `updated_at` column - transaction will fail.
func ExpandOrdered(value string, order Order, searchQuery manifest.SearchQuery) Option {
	return func(a any, tc transactionContext) transactionContext {
		tc.Expand[value] = expandDetails{
			Query: searchQuery,
			OrderBy: orderByColumn{
				Column: tc.Config.UpdatedAtColumnName,
				Order:  order,
			},
		}
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

func WithVersion(v manifest.Version) Option {
	return func(a any, tc transactionContext) transactionContext {
		tc.withVersion = &v
		return tc
	}
}

func OrderByCreatedAt(order Order) Option {
	return func(a any, tc transactionContext) transactionContext {
		tc.Order.OrderColumns = append(tc.Order.OrderColumns, orderByColumn{
			Column: tc.Config.CreatedAtColumnName,
			Order:  order,
		})

		return tc
	}
}

func OrderBy(column string, order Order) Option {
	return func(a any, tc transactionContext) transactionContext {
		tc.Order.OrderColumns = append(tc.Order.OrderColumns, orderByColumn{
			Column: column,
			Order:  order,
		})

		return tc
	}
}

// Pinger interface is implemented by stores backed by a DB or remote object store where connectivity to the remote storage can be disrupted.
type Pinger interface {
	// Ping performs basic connectivity check to the underlying DB and returns nil if ok.
	Ping(context.Context) error
}

// LabelStore interface defines methods that a store may implement to provide means of working withe Model labels.
// Labels are part of by models [manifest.Model] metadata and not created directly in the store.
// Thus this interface only provides method to query labels and their values.
type LabelStore interface {
	// FindNames returns a set of names for a model type.
	FindNames(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.StringSet, error)

	// FindLabels returns a set of label keys for a given model type.
	FindLabels(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.StringSet, error)

	// FindLabelValues returns a set of label values for a given model type and label key.
	FindLabelValues(ctx context.Context, model any, key string, searchQuery manifest.SearchQuery, options ...Option) (manifest.StringSet, error)
}

// Store interface defines and interface for various implementations of object storage.
// Implementations of store are expected to persist instances of manifest.ResourceModel and retrieve them.
// In case underlying implementing is using a DB, extra method 'Ping' to verify connectivity is provided.
type Store interface {
	// Find returns models from the store that matched search query parameters.
	// manifest.SearchQuery - defines limits and offset.
	// [options] control how results are returned and expansion of collections.
	Find(ctx context.Context, dest any, searchQuery manifest.SearchQuery, options ...Option) (total int64, err error)

	// GetByUID return at most one entry from the store identified by the UUID.
	GetByUID(ctx context.Context, value any, id manifest.ResourceID, options ...Option) (exists bool, err error)
	// GetByName return at most one entry from the store identified by the Name.
	// See Kubernetes docs on Object Names and IDs: https://kubernetes.io/docs/concepts/overview/working-with-objects/names
	GetByName(ctx context.Context, value any, id manifest.ResourceName, options ...Option) (exists bool, err error)

	// Create inserts a new value into the store, updating inserted models ID.
	Create(ctx context.Context, value any, options ...Option) error
	CreateOrUpdate(ctx context.Context, newValue any, options ...Option) (exists bool, err error)

	// Update an entry in the store
	Update(ctx context.Context, newValue any, id manifest.ResourceID, options ...Option) (exists bool, err error)

	// Delete an entry from the store
	// Note: it is not an error to delete non-existent value. (false, nil) will be returned in such case.
	Delete(ctx context.Context, model any, id manifest.ResourceID, version manifest.Version, options ...Option) (existed bool, err error)

	// Restore previously deleted entry identified by the ID in the DB.
	// Note: for a value to be restorable the model must support soft-delete functionality. It means a model must have config.DeletedAtColumnName field.
	// Note: also an entry must have been soft-deleted before it can be restored. Restoring non-deleted value is not an error.
	Restore(ctx context.Context, model any, id manifest.ResourceID, options ...Option) (existed bool, err error)
}

// AssociationStore defines an interface for store that deals with associations.
// It provides CRUD operations to Add, Find, Remove associations to a model.
// If a resource being associated/linked does not exist in the store - it will be first created.
type AssociationStore interface {
	// FindLinked returns models previously associated with the owner that match searchQuery. Use empty query to select all.
	FindLinked(ctx context.Context, dest any, link string, owner any, searchQuery manifest.SearchQuery, options ...Option) (totalCount int64, err error)

	// AddLinked adds a value to the store and associate it with the owner.
	AddLinked(ctx context.Context, value any, link string, owner any, options ...Option) error

	// RemoveLinked removes a value associate from the owner.
	// By default the value is itself is not deleted, because, depending on the way the data is modeled,
	// there can multiple references to the value (Many-to-Many association)
	RemoveLinked(ctx context.Context, value any, link string, owner any) error

	// ClearLinked removes all associate from the owner.
	// Values are not deleted from the store as there might be other references to them.
	ClearLinked(ctx context.Context, link string, owner any) error
}

// Transaction interface defines a transaction that has been initiated and can be either Committed or Rollback'd.
type Transaction interface {
	// Rollback signals that transaction should be aborted and all not-yet-committed changed rollback'd.
	// There is no expectation that if transaction has already been Committed, that Rollback will roll it back.
	// This is due to intended usage of Rollback with `defer`.
	Rollback()

	// Commit commits all pending actions within the context of this transaction.
	// Calling Commit and following it with Rollback is safe.
	// Calling Rollback and following it with Commit is safe. But pointless.
	Commit() error
}

// Transactional interface defines a type that can initiate [StoreTransaction].
// Note that [StoreTransaction]. will be executed with the context passed to Begin() method
type Transactional interface {
	// Begin opens a transaction and produces [StoreTransaction] object to execute queries.
	// Note: It's a caller responsibility to call Transaction.Commit() or Transaction.Rollback() to complete the transaction.
	// Failure to properly close transaction with Commit or Rollback leads to resource leakage.
	Begin(context.Context) (StoreTransaction, error)
}

type StoreTransaction interface {
	Transaction

	// Create a new entry in the store within a context the open transaction
	// Note value's ID field will be update to the Primary key on a newly created entry.
	Create(newValue any, options ...Option) error

	// Update an entry in the store within a context the open transaction
	Update(value any, id manifest.ResourceID, options ...Option) (exists bool, err error)

	// Delete an entry from the store within a context the open transaction
	Delete(model any, id manifest.ResourceID, version manifest.Version, options ...Option) (existed bool, err error)

	// GetByUID retrieves an entry identified by UID from the store.
	GetByUID(destValue any, id manifest.ResourceID, options ...Option) (exists bool, err error)

	// GetByName retrieves an entry identified by name from the store.
	GetByName(destValue any, id manifest.ResourceName, options ...Option) (exists bool, err error)

	// AddLinked associates a new entry with the owner
	AddLinked(model any, link string, owner any, options ...Option) error

	// RemoveLinked removes previously associated model from the owner
	RemoveLinked(model any, link string, owner any) error

	// ClearLinked removes all associations of 'link' from the owner.
	// Note: this call only 'unlinks' models, not deleting the record them-selfs.
	ClearLinked(link string, owner any) error
}

// TransactionalStore provides interface for a store that also implements [Transactional] interface.
// It is expected that most stores backed by a relational DB will implement this interface.
type TransactionalStore interface {
	Transactional
	Store
}

// TransitionalStore is a type alias to support migration.
//
// Deprecated: TransitionalStore is a typo that got noticed too late.
// Correct type name is TransactionalStore. Incorrect spelling is here as an alias to support
// migration for a couple of projects that used misspelled name.
// It will be removed before 1.0 release.
type TransitionalStore = TransactionalStore
