package dbstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrNoDBObject error is returned by NewDBStore when nil `db` argument is passed
	ErrNoDBObject = errors.New("nil DB connection passed")

	// ErrUnexpectedSelectorOperator error is returned by a number of FindXXX functions when search query .Requirements returns an error.
	// In cases like that, search query can not be converted into SQL statements
	ErrUnexpectedSelectorOperator = errors.New("unexpected requirements operator")

	// ErrNoRequirementsValueProvided error is returned when some of the query selector's requirements can not be converted into SQL query.
	// For example, selector `key=` has no value and thus will not be converted into a valid SQL expression.
	ErrNoRequirementsValueProvided = errors.New("no value for a requirement is provided")
)

// SchemaConfig determines how a model is mapped into DB columns.
// Default implementation is designed for manifest.ObjectMeta, but advanced users are free to override the syntax.
type SchemaConfig struct {
	IDColumnName        string
	NameColumnName      string
	VersionColumnName   string
	LabelsColumnName    string
	CreatedAtColumnName string
	UpdatedAtColumnName string
	DeletedAtColumnName string
}

type rawJSONSQL struct {
	Key   string
	Value string
}

type DBStore struct {
	db *gorm.DB

	config SchemaConfig
}

// ManifestModel is the default schema config compatible with manifest.ObjectMeta
var ManifestModel = SchemaConfig{
	IDColumnName:        "uid",
	VersionColumnName:   "version",
	NameColumnName:      "name",
	LabelsColumnName:    "labels",
	CreatedAtColumnName: "created_at",
	UpdatedAtColumnName: "updated_at",
	DeletedAtColumnName: "deleted_at",
}

// NewDBStore creates a new instance of DBStore:
// TransactionalStore wrapper of GORM.
func NewDBStore(db *gorm.DB, cfg SchemaConfig) (*DBStore, error) {
	if db == nil {
		return nil, ErrNoDBObject
	}

	return &DBStore{
		db:     db,
		config: cfg,
	}, nil
}

func (c orderByColumn) Clause() clause.OrderByColumn {
	return clause.OrderByColumn{
		Column: clause.Column{Name: c.Column},
		Desc:   c.Order == OrderDescending,
	}
}

func applyOptions(db *gorm.DB, config SchemaConfig, value any, options ...Option) (tx, ctx *gorm.DB) {
	tContext := newTransactionContext(config)
	for _, o := range options {
		tContext = o(value, tContext)
	}

	tx, ctx = db, db
	if tContext.unScoped {
		tx = tx.Unscoped()
		ctx = ctx.Unscoped()
	}

	// Process Omit options. Note they don't affect counting
	if len(tContext.Omit) > 0 {
		tx = tx.Omit(tContext.Omit.Slice()...)
	}

	// Process Expand options. Note they don't affect counting
	for relation, expand := range tContext.Expand {
		args := []any{}
		if !expand.Query.Empty() {
			args = []any{func(db *gorm.DB) *gorm.DB {
				stx, _, _ := withQuery(db, nil, config, expand.Query)
				if expand.OrderBy.Column == "" {
					return stx
				}

				return stx.Order(expand.OrderBy.Clause())
			}}
		}

		tx = tx.Preload(relation, args...)
	}

	if tContext.withVersion != nil {
		req := clause.Eq{
			Column: clause.Column{Name: config.VersionColumnName},
			Value:  *tContext.withVersion,
		}

		ctx = ctx.Where(req)
		tx = tx.Where(req)
	}

	for _, orderBy := range tContext.Order.OrderColumns {
		ctx = ctx.Order(orderBy.Clause())
		tx = tx.Order(orderBy.Clause())
	}

	if tContext.disableCounting {
		ctx = nil
	}

	return
}

func limitedQuery(tx *gorm.DB, query manifest.SearchQuery) *gorm.DB {
	if tx == nil {
		return tx
	}

	if query.Offset > 0 {
		tx = tx.Offset(int(query.Offset))
	}
	if query.Limit > 0 {
		tx = tx.Limit(int(query.Limit))
	}

	return tx
}

func matchName(tx *gorm.DB, jfield string, query manifest.SearchQuery) *gorm.DB {
	if tx == nil {
		return tx
	}

	if query.Name == "" {
		return tx
	}

	return tx.Where(clause.Like{
		Column: clause.Column{Name: jfield},
		Value:  fmt.Sprintf("%%%s%%", query.Name),
	})
}

func limitTimeRange(tx *gorm.DB, column string, from time.Time, till time.Time) *gorm.DB {
	if tx == nil {
		return tx
	}

	if !from.IsZero() {
		if !till.IsZero() {
			tx = tx.Where(fmt.Sprintf("%s BETWEEN ? AND ?", column), from, till)
		} else {
			tx = tx.Where(clause.Gte{
				Column: clause.Column{Name: column},
				Value:  from,
			})

		}
	} else if !till.IsZero() {
		tx = tx.Where(clause.Lt{
			Column: clause.Column{Name: column},
			Value:  till,
		})
	}

	return tx
}

func (s *DBStore) singleTransaction(ctx context.Context) *gormStoreTransaction {
	return &gormStoreTransaction{
		db:     s.db.WithContext(ctx),
		config: s.config,
	}
}

// Ping implements [Pinger] interface for the DBStore, using SQL DB PingContext,
// which send empty "SELECT" to the DB to check if it is able to process requests.
func (s *DBStore) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("failed to access DB interface: %w", err)
	}

	// TODO: Return connection stats for more info
	return sqlDB.PingContext(ctx)
}

// Begin opens a transaction and produces [StoreTransaction] object to execute queries.
// It is an implementation of Transactional interface.
// Note: It's a caller responsibility to call Transaction.Commit() or Transaction.Rollback() to complete the transaction.
// Failure to properly close transaction with Commit or Rollback leads to resource leakage.
func (s *DBStore) Begin(ctx context.Context) (StoreTransaction, error) {
	tx := s.db.WithContext(ctx).Begin()
	return &gormStoreTransaction{
		db:     tx,
		config: s.config,
	}, tx.Error
}

// CreateOrUpdate inserts a new value into the store if the models.ID is nil, otherwise it updates it.
func (s *DBStore) CreateOrUpdate(ctx context.Context, value any, options ...Option) (exists bool, err error) {
	return s.singleTransaction(ctx).CreateOrUpdate(value, options...)
}

// Create inserts a new value into the store, updating inserted models ID.
func (s *DBStore) Create(ctx context.Context, value any, options ...Option) error {
	return s.singleTransaction(ctx).Create(value, options...)
}

// FindLinked returns models previously associated with the owner that match searchQuery.
// This is an implementation of AssociationStore interface.
// Using empty query will select all linked entries. Use with caution as in that case number of results returned from DB in unbounded.
func (s *DBStore) FindLinked(ctx context.Context, dest any, link string, owner any, searchQuery manifest.SearchQuery, options ...Option) (totalCount int64, err error) {
	tx, xtx := applyOptions(s.db.Model(owner).WithContext(ctx), s.config, dest, options...)
	tx, xtx, err = withQuery(tx, xtx, s.config, searchQuery)
	if err != nil {
		return
	}

	// Note, don't simply the following as order matters here
	err = tx.Association(link).Find(dest)
	if err != nil {
		return
	}

	totalCount = xtx.Association(link).Count()

	return
}

// AddLinked adds a link to the value associate it with the owner.
func (s *DBStore) AddLinked(ctx context.Context, value any, link string, owner any, options ...Option) error {
	return s.singleTransaction(ctx).AddLinked(value, link, owner, options...)
}

// RemoveLinked removes a value associate from the owner.
// By default the value is itself is not deleted, because, depending on the way the data is modeled,
// there can multiple references to the value (Many-to-Many association)
func (s *DBStore) RemoveLinked(ctx context.Context, value any, link string, owner any) error {
	return s.singleTransaction(ctx).RemoveLinked(value, link, owner)
}

// ClearLinked removes all associate from the owner.
// Values are not deleted from the store as there might be other references to them.
func (s *DBStore) ClearLinked(ctx context.Context, link string, owner any) error {
	return s.singleTransaction(ctx).ClearLinked(link, owner)
}

// GetByUID finds at most one entry in the store identified by the UUID if there is one.
// If Entry is found, it is written into dest variable. Thus dest must be a pointer to a variable to store result. Type of the dest determines which model to find.
// Return values indicate if entry with such id were found, and if there was an error while fetching the value.
// In case of an error or if returned value is false, dest is not updated.
func (s *DBStore) GetByUID(ctx context.Context, dest any, id manifest.ResourceID, options ...Option) (bool, error) {
	return s.singleTransaction(ctx).GetByUID(dest, id, options...)
}

// GetByName finds at most one entry in the store identified by the name if there is one.
// See Kubernetes docs on Object Names and IDs: https://kubernetes.io/docs/concepts/overview/working-with-objects/names about the difference between ID and a Name.
// If Entry is found, it is written into dest variable. Thus dest must be a pointer to a variable to store result. Type of the dest determines which model to find.
// Return values indicate if entry with such id were found, and if there was an error while fetching the value.
// In case of an error or if returned value is false, dest is not updated.
func (s *DBStore) GetByName(ctx context.Context, dest any, id manifest.ResourceName, options ...Option) (bool, error) {
	return s.singleTransaction(ctx).GetByName(dest, id, options...)
}

// Update updates an entry identified by the ID in the DB.
// Type of the value determines which model to update.
// Return values indicate if entry with such id were found, and if there was an error while fetching the value.
func (s *DBStore) Update(ctx context.Context, value any, id manifest.ResourceID, options ...Option) (update bool, err error) {
	return s.singleTransaction(ctx).Update(value, id, options...)
}

// Delete deletes an entry identified by the ID from the DB.
// Type of the value determines which model to delete. No field of the value is used, thus a pointer to an default value can be safely passed.
// Return values indicate if the entry with such id existed, and if there was an error while fetching the value.
// Note: it is not an error to delete non-existent value. (false, nil) will be returned in such case.
func (s *DBStore) Delete(ctx context.Context, value any, id manifest.ResourceID, version manifest.Version, options ...Option) (existed bool, err error) {
	return s.singleTransaction(ctx).Delete(value, id, version, options...)
}

// Restore restores a previously deleted entry identified by the ID in the DB.
// Type of the model determines which model to restore. No field of the value is used, thus a pointer to an default value can be safely passed.
// Return values indicate if the entry with such id exists, and if there was an error while fetching the value.
// Note: for a value to be restorable the model must support soft-delete functionality. It means a model must have config.DeletedAtColumnName field.
// Note: also an entry must have been soft-deleted before it can be restored. Restoring non-deleted value is not an error.
func (s *DBStore) Restore(ctx context.Context, model any, id manifest.ResourceID, options ...Option) (existed bool, err error) {
	return s.singleTransaction(ctx).Restore(model, id, options...)
}

// Find returns models from the store that matched search query parameters.
// manifest.SearchQuery - defines limits and offset.
// [options] control how results are returned and expansion of collections.
func (s *DBStore) Find(ctx context.Context, dest any, searchQuery manifest.SearchQuery, options ...Option) (total int64, err error) {
	tx, xtx := applyOptions(s.db.WithContext(ctx), s.config, dest, options...)
	tx, xtx, err = withQuery(tx, xtx, s.config, searchQuery)
	if err != nil {
		return 0, err
	}

	if xtx != nil {
		if err = xtx.Model(dest).Count(&total).Error; err != nil {
			return total, err
		}
	}

	return total, tx.Find(dest).Error
}

// FindNames returns a set of names for a model type.
// Type of the model argument determines which model to restore. No field of the value is used, thus a pointer to an default value can be safely passed.
// searchQuery arguments selection matches and pagination.
func (s *DBStore) FindNames(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.StringSet, error) {
	tx, xtx := applyOptions(s.db.Model(model).WithContext(ctx), s.config, model, options...)
	tx, _, err := withQuery(tx, xtx, s.config, searchQuery)
	if err != nil {
		return nil, err
	}

	var names []struct {
		Name string
	}
	rtx := tx.Distinct(s.config.NameColumnName).Scan(&names)

	result := make(manifest.StringSet, len(names))
	for _, l := range names {
		result[l.Name] = struct{}{}
	}

	return result, rtx.Error
}

// FindLabels returns a set of label keys for a given model type.
// Type of the model argument determines which model to restore. No field of the value is used, thus a pointer to an default value can be safely passed.
// searchQuery arguments selection matches and pagination.
func (s *DBStore) FindLabels(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.StringSet, error) {
	tx, _ := applyOptions(s.db.Model(model).WithContext(ctx), s.config, model, options...)
	// SELECT key, value FROM conversations, json_each(cast(conversations.labels as json));
	tx = limitedQuery(tx, searchQuery).Clauses(clause.From{
		Joins: []clause.Join{
			{
				Expression: JSONExtract(s.config.LabelsColumnName),
			},
		},
	})

	var ls []rawJSONSQL
	// rtx := matchName(tx, "key", searchQuery).Select("key", "value").Scan(&ls)
	rtx := matchName(tx, "key", searchQuery).Distinct("key").Scan(&ls)

	// Map json selection into a StringSet
	result := make(manifest.StringSet, len(ls))
	for _, l := range ls {
		result[l.Key] = struct{}{}
	}

	return result, rtx.Error
}

// FindLabelValues returns a set of label values for a given model type and label key.
// Type of the model argument determines which model to restore. No field of the value is used, thus a pointer to an default value can be safely passed.
// searchQuery arguments selection matches and pagination.
func (s *DBStore) FindLabelValues(ctx context.Context, model any, key string, searchQuery manifest.SearchQuery, options ...Option) (manifest.StringSet, error) {
	tx, _ := applyOptions(s.db.Model(model).WithContext(ctx), s.config, model, options...)
	tx = limitedQuery(tx, searchQuery).Clauses(clause.From{
		Joins: []clause.Join{
			{
				Expression: JSONExtract(s.config.LabelsColumnName),
			},
		},
	}).Where("key = ?", key)

	var ls []rawJSONSQL
	// rtx := matchName(tx, "value", searchQuery).Select("key", "value").Scan(&ls)
	rtx := matchName(tx, "value", searchQuery).Distinct("value").Scan(&ls)

	result := make(manifest.StringSet, len(ls))
	for _, l := range ls {
		result[l.Value] = struct{}{}
	}

	return result, rtx.Error
}

func withSelector(tx *gorm.DB, jcolumn string, selector manifest.Selector) (*gorm.DB, error) {
	// Convert Label-based selector to the SQL query
	if selector == nil {
		return tx, nil
	}

	reqs, ok := selector.Requirements()
	if !ok { // Selector has no requirements, easy way out
		return nil, manifest.ErrNonSelectableRequirements
	}

	qs := make([]*jsonQueryExpression, 0, len(reqs))
	for _, req := range reqs {
		switch req.Operator() {
		case manifest.Equals, manifest.DoubleEquals:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, jsonQuery(jcolumn).Equals(value, req.Key()))
		case manifest.NotEquals:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			// not-equals means it exists but value not equal
			qs = append(qs,
				jsonQuery(jcolumn).NotEquals(value, req.Key()),
			)
		case manifest.GreaterThan:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}

			rsValue, err := strconv.ParseInt(value, 10, 64)
			if err != nil { // Selector has no requirements, easy way out
				return nil, fmt.Errorf("%w: failed to parse value for key `%v` to compare with: %v", manifest.ErrNonSelectableRequirements, req.Key(), err)
			}

			qs = append(qs, jsonQuery(jcolumn).GreaterThan(rsValue, req.Key()))
		case manifest.LessThan:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}

			rsValue, err := strconv.ParseInt(value, 10, 64)
			if err != nil { // Selector has no requirements, easy way out
				return nil, fmt.Errorf("%w: failed to parse value for key `%v` to compare with: %v", manifest.ErrNonSelectableRequirements, req.Key(), err)
			}

			qs = append(qs, jsonQuery(jcolumn).LessThan(rsValue, req.Key()))
		case manifest.In:
			values := req.Values()
			if values == nil {
				return nil, fmt.Errorf("%w: nil values for key `%v`", manifest.ErrNonSelectableRequirements, req.Key())
			}
			qs = append(qs, jsonQuery(jcolumn).KeyIn(req.Key(), values))
		case manifest.NotIn:
			values := req.Values()
			if values == nil {
				return nil, fmt.Errorf("%w: nil values for key `%v`", manifest.ErrNonSelectableRequirements, req.Key())
			}
			qs = append(qs, jsonQuery(jcolumn).KeyNotIn(req.Key(), values))
		case manifest.Exists:
			qs = append(qs, jsonQuery(jcolumn).HasKey(req.Key()))
		case manifest.DoesNotExist:
			qs = append(qs, jsonQuery(jcolumn).HasNoKey(req.Key()))
		default:
			return nil, fmt.Errorf("%w: `%v`", ErrUnexpectedSelectorOperator, req.Operator())
		}
	}

	for _, c := range qs {
		tx = tx.Where(c)
	}

	return tx, nil
}

func withQuery(tx, ctx *gorm.DB, cfg SchemaConfig, query manifest.SearchQuery) (selecting, counting *gorm.DB, err error) {
	// Apply name matcher if any
	tx = matchName(tx, cfg.NameColumnName, query)
	ctx = matchName(ctx, cfg.NameColumnName, query)

	// Apply time-range limit
	tx = limitTimeRange(tx, cfg.CreatedAtColumnName, query.FromTime, query.TillTime)
	ctx = limitTimeRange(ctx, cfg.CreatedAtColumnName, query.FromTime, query.TillTime)

	tx, err = withSelector(tx, cfg.LabelsColumnName, query.Selector)
	ctx, _ = withSelector(ctx, cfg.LabelsColumnName, query.Selector)

	// Apply offset and limit to the query
	return limitedQuery(tx, query), ctx, err
}
