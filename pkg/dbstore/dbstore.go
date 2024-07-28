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
	ErrNoDBObject                  = fmt.Errorf("nil DB connection passed")
	ErrUnexpectedSelectorOperator  = fmt.Errorf("unexpected requirements operator")
	ErrNoRequirementsValueProvided = fmt.Errorf("no value for a requirement is provided")
)

type Config struct {
	IDColumnName        string
	VersionColumnName   string
	LabelsColumnName    string
	CreatedAtColumnName string
	DeletedAtColumnName string
}

type rawJSONSQL struct {
	Key   string
	Value string
}

type DBStore struct {
	db *gorm.DB

	config Config
}

func NewDBStore(db *gorm.DB, cfg Config) (TransitionalStore, error) {
	if db == nil {
		return nil, ErrNoDBObject
	}

	if cfg.IDColumnName == "" {
		cfg.IDColumnName = "uid"
	}
	if cfg.LabelsColumnName == "" {
		cfg.LabelsColumnName = "labels"
	}
	if cfg.VersionColumnName == "" {
		cfg.VersionColumnName = "version"
	}
	if cfg.CreatedAtColumnName == "" {
		cfg.CreatedAtColumnName = "created_at"
	}
	if cfg.DeletedAtColumnName == "" {
		cfg.DeletedAtColumnName = "deleted_at"
	}

	return &DBStore{
		db:     db,
		config: cfg,
	}, nil
}

func applyOptions(tx *gorm.DB, value any, options ...Option) *gorm.DB {
	tContext := newTransactionContext()
	for _, o := range options {
		tContext = o(value, tContext)
	}

	for omit := range tContext.Omit {
		tx = tx.Omit(omit)
	}

	for expand := range tContext.Expand {
		tx = tx.Preload(expand)
	}

	if tContext.unScoped {
		tx = tx.Unscoped()
	}

	return tx
}

func limitedQuery(inTx *gorm.DB, query manifest.SearchQuery) *gorm.DB {
	if query.Offset > 0 {
		inTx = inTx.Offset(int(query.Offset))
	}
	if query.Limit > 0 {
		inTx = inTx.Limit(int(query.Limit))
	}

	return inTx
}

func matchName(tx *gorm.DB, jfield string, query manifest.SearchQuery) *gorm.DB {
	if query.Name != "" {
		return tx.Where(jfield+" LIKE ?", fmt.Sprintf("%%%s%%", query.Name))
	}

	return tx
}

func limitTimeRange(tx *gorm.DB, column string, from time.Time, till time.Time) *gorm.DB {
	nilTime := time.Time{}
	if from != nilTime {
		if till != nilTime {
			tx = tx.Where(fmt.Sprintf("%s BETWEEN ? AND ?", column), from, till)
		} else {
			tx = tx.Where(fmt.Sprintf("%s  >= ?", column), from)
		}
	} else if till != nilTime {
		tx = tx.Where(fmt.Sprintf("%s < ?", column), till)
	}

	return tx
}

func (s *DBStore) singleTransaction(ctx context.Context) *gormStoreTransaction {
	return &gormStoreTransaction{
		db:     s.db.WithContext(ctx),
		config: s.config,
	}
}

func (s *DBStore) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("failed to access DB interface: %w", err)
	}

	// TODO: Return connection stats for more info
	return sqlDB.PingContext(ctx)
}

func (s *DBStore) Begin(ctx context.Context) (StoreTransaction, error) {
	tx := s.db.WithContext(ctx).Begin()
	return &gormStoreTransaction{
		db:     tx,
		config: s.config,
	}, tx.Error
}

func (s *DBStore) Create(ctx context.Context, value any, options ...Option) error {
	return s.singleTransaction(ctx).Create(value, options...)
}

func (s *DBStore) FindLinked(ctx context.Context, dest any, link string, owner any, searchQuery manifest.SearchQuery, options ...Option) (totalCount int64, err error) {
	tx := applyOptions(s.db.Model(owner).WithContext(ctx), dest, options...)
	tx, err = withSelector(tx, s.config.LabelsColumnName, s.config.CreatedAtColumnName, searchQuery)
	if err != nil {
		return
	}

	association := tx.Association(link)
	err = association.Find(dest)

	return association.Count(), err
}

func (s *DBStore) AddLinked(ctx context.Context, value any, link string, owner any, options ...Option) error {
	return s.singleTransaction(ctx).AddLinked(value, link, owner, options...)
}

func (s *DBStore) RemoveLinked(ctx context.Context, value any, link string, owner any) error {
	return s.singleTransaction(ctx).RemoveLinked(value, link, owner)
}

func (s *DBStore) Get(ctx context.Context, dest any, id manifest.ResourceID, options ...Option) (bool, error) {
	return s.singleTransaction(ctx).Get(dest, id, options...)
}

func (s *DBStore) GetWithVersion(ctx context.Context, dest any, id manifest.VersionedResourceID, options ...Option) (bool, error) {
	tx := applyOptions(s.db.WithContext(ctx), dest, options...)
	tx = tx.Where(fmt.Sprintf("%s = ?", s.config.VersionColumnName), id.Version).First(dest, id.ID)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DBStore) Update(ctx context.Context, value any, id manifest.VersionedResourceID, options ...Option) (bool, error) {
	return s.singleTransaction(ctx).Update(value, id, options...)
}

func (s *DBStore) Delete(ctx context.Context, value any, id manifest.ResourceID, version manifest.Version, options ...Option) (bool, error) {
	return s.singleTransaction(ctx).Delete(value, id, version, options...)
}

func (s *DBStore) Restore(ctx context.Context, model any, id manifest.ResourceID) (existed bool, err error) {
	return s.singleTransaction(ctx).Restore(model, id)
}

func (s *DBStore) Find(ctx context.Context, resources any, searchQuery manifest.SearchQuery, options ...Option) (int64, error) {
	tx := applyOptions(s.db.WithContext(ctx), resources, options...)
	tx, err := withSelector(tx, s.config.LabelsColumnName, s.config.CreatedAtColumnName, searchQuery)
	if err != nil {
		return 0, err
	}

	rtx := tx.Order(
		clause.OrderByColumn{
			Column: clause.Column{Name: s.config.CreatedAtColumnName},
			Desc:   true,
		}).Find(resources)

	// log.Print("[DEBUG] SQL: ", tx.ToSQL(func(tx *gorm.DB) *gorm.DB {
	// 	for _, c := range qs {
	// 		tx = tx.Where(c)
	// 	}
	// 	return tx.Find(&[]Scenario{})
	// 	return rtx
	// }))

	return rtx.RowsAffected, rtx.Error
}

func (s *DBStore) FindNames(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.Labels, error) {
	tx := limitedQuery(s.db.Model(model).WithContext(ctx), searchQuery)
	tx = matchName(tx, "name", searchQuery)

	var names []struct {
		Name string
	}
	rtx := tx.Distinct("name").Scan(&names)

	result := make(manifest.Labels, len(names))
	for _, l := range names {
		result[l.Name] = ""
	}

	return result, rtx.Error
}

func (s *DBStore) FindLabelValues(ctx context.Context, model any, key string, searchQuery manifest.SearchQuery, options ...Option) (manifest.Labels, error) {
	var ls []rawJSONSQL

	tx := limitedQuery(s.db.Model(model).WithContext(ctx), searchQuery)
	tx = tx.Clauses(clause.From{
		Joins: []clause.Join{
			{
				Expression: JSONExtract(s.config.LabelsColumnName),
			},
		},
	})
	tx = tx.Where("key = ?", key)
	rtx := matchName(tx, "value", searchQuery).Select("key", "value").Scan(&ls)

	result := make(manifest.Labels, len(ls))
	for _, l := range ls {
		result[l.Value] = l.Key
	}

	return result, rtx.Error
}

func (s *DBStore) FindLabels(ctx context.Context, model any, searchQuery manifest.SearchQuery, options ...Option) (manifest.Labels, error) {
	var ls []rawJSONSQL

	tx := limitedQuery(s.db.Model(model).WithContext(ctx), searchQuery)
	// SELECT key, value FROM conversations, json_each(cast(conversations.labels as json));
	tx = tx.Clauses(clause.From{
		Joins: []clause.Join{
			{
				Expression: JSONExtract(s.config.LabelsColumnName),
			},
		},
	})
	rtx := matchName(tx, "key", searchQuery).Select("key", "value").Scan(&ls)

	result := make(manifest.Labels, len(ls))
	for _, l := range ls {
		result[l.Key] = l.Value
	}

	return result, rtx.Error
}

func withSelector(tx *gorm.DB, jcolumn, timeColumn string, query manifest.SearchQuery) (*gorm.DB, error) {
	// Apply offset and limit to the query
	tx = limitedQuery(tx, query)

	// Apply name matcher if any
	tx = matchName(tx, "name", query)

	// Apply time-range limit
	tx = limitTimeRange(tx, timeColumn, query.FromTime, query.TillTime)

	// Convert Label-based selector to the SQL query
	if query.Selector == nil {
		return tx, nil
	}

	reqs, ok := query.Selector.Requirements()
	if !ok { // Selector has no requirements, easy way out
		return nil, manifest.ErrNonSelectableRequirements
	}

	qs := make([]*JSONQueryExpression, 0, len(reqs))
	for _, req := range reqs {
		switch req.Operator() {
		case manifest.Equals, manifest.DoubleEquals:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery(jcolumn).Equals(value, req.Key()))
		case manifest.NotEquals:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			// not-equals means it exists but value not equal
			qs = append(qs,
				JSONQuery(jcolumn).HasKey(req.Key()),
				JSONQuery(jcolumn).NotEquals(value, req.Key()),
			)
		case manifest.GreaterThan:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}

			rsValue, err := strconv.ParseInt(value, 10, 64)
			if err != nil { // Selector has no requirements, easy way out
				return nil, fmt.Errorf("%w: failed to parse value for key `%v` to compare with: %v", manifest.ErrNonSelectableRequirements, req.Key(), err)
			} else {
				fmt.Printf("selecting for GOAT: %v\n", rsValue)
			}

			qs = append(qs, JSONQuery(jcolumn).GreaterThan(rsValue, req.Key()))
		case manifest.LessThan:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}

			rsValue, err := strconv.ParseInt(value, 10, 64)
			if err != nil { // Selector has no requirements, easy way out
				return nil, fmt.Errorf("%w: failed to parse value for key `%v` to compare with: %v", manifest.ErrNonSelectableRequirements, req.Key(), err)
			}

			qs = append(qs, JSONQuery(jcolumn).LessThan(rsValue, req.Key()))
		case manifest.In:
			qs = append(qs, JSONQuery(jcolumn).KeyIn(req.Key(), req.Values()))
		case manifest.NotIn:
			qs = append(qs, JSONQuery(jcolumn).KeyNotIn(req.Key(), req.Values()))
		case manifest.Exists:
			qs = append(qs, JSONQuery(jcolumn).HasKey(req.Key()))
		case manifest.DoesNotExist:
			qs = append(qs, JSONQuery(jcolumn).HasNoKey(req.Key()))
		default:
			return nil, fmt.Errorf("%w: `%v`", ErrUnexpectedSelectorOperator, req.Operator())
		}
	}

	for _, c := range qs {
		tx = tx.Where(c)
	}

	return tx, nil
}
