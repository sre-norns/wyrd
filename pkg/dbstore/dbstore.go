package dbstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

var (
	ErrNoDBObject                  = fmt.Errorf("nil DB connection passed")
	ErrUnexpectedSelectorOperator  = fmt.Errorf("unexpected requirements operator")
	ErrNoRequirementsValueProvided = fmt.Errorf("no value for a requirement is provided")
	ErrNonSelectableRequirements   = fmt.Errorf("non-selectable requirements")
)

type Config struct {
	VersionColumnName   string
	LabelsColumnName    string
	CreatedAtColumnName string
}

type DBStore struct {
	db *gorm.DB

	config Config
}

func NewDBStore(db *gorm.DB, cfg Config) (Store, error) {
	if db == nil {
		return nil, ErrNoDBObject
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

	return &DBStore{
		db:     db,
		config: cfg,
	}, nil
}

func (s *DBStore) Create(ctx context.Context, value any) error {
	return s.db.WithContext(ctx).Create(value).Error
}

func (s *DBStore) Get(ctx context.Context, dest any, id manifest.ResourceID) (bool, error) {
	tx := s.db.WithContext(ctx).First(dest, id)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DBStore) GetWithVersion(ctx context.Context, dest any, id manifest.VersionedResourceID) (bool, error) {
	tx := s.db.WithContext(ctx).Where(fmt.Sprintf("%s = ?", s.config.VersionColumnName), id.Version).First(dest, id)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DBStore) Update(ctx context.Context, value any, id manifest.VersionedResourceID) (bool, error) {
	tx := s.db.WithContext(ctx).Where(fmt.Sprintf("%s = ?", s.config.VersionColumnName), id.Version).Save(value)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DBStore) Delete(ctx context.Context, value any, id manifest.VersionedResourceID) (bool, error) {
	tx := s.db.WithContext(ctx).Where(fmt.Sprintf("%s = ?", s.config.VersionColumnName), id.Version).Delete(value, id.ID)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return tx.RowsAffected == 1, tx.Error
}

func (s *DBStore) Find(ctx context.Context, resources any, searchQuery manifest.SearchQuery) (int64, error) {
	tx, err := s.withSelector(ctx, searchQuery)
	if err != nil {
		return 0, err
	}

	rtx := tx.Order(s.config.CreatedAtColumnName).Find(resources)

	// log.Print("[DEBUG] SQL: ", tx.ToSQL(func(tx *gorm.DB) *gorm.DB {
	// 	for _, c := range qs {
	// 		tx = tx.Where(c)
	// 	}
	// 	return tx.Find(&[]Scenario{})
	// 	return rtx
	// }))

	return rtx.RowsAffected, rtx.Error
}

func (s *DBStore) withSelector(ctx context.Context, query manifest.SearchQuery) (*gorm.DB, error) {
	reqs, ok := query.Selector.Requirements()
	if !ok { // Selector has no requirements, easy way out
		return nil, ErrNonSelectableRequirements
	}

	qs := make([]any, 0, len(reqs))
	for _, req := range reqs {
		switch req.Operator() {
		case manifest.Equals, manifest.DoubleEquals:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery(s.config.LabelsColumnName).Equals(value, req.Key()))
		case manifest.NotEquals:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			// not-equals means it exists but value not equal
			qs = append(qs,
				JSONQuery(s.config.LabelsColumnName).HasKey(req.Key()),
				JSONQuery(s.config.LabelsColumnName).NotEquals(value, req.Key()),
			)
		case manifest.GreaterThan:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery(s.config.LabelsColumnName).GreaterThan(value, req.Key()))
		case manifest.LessThan:
			value, ok := req.Values().Any()
			if !ok {
				return nil, ErrNoRequirementsValueProvided
			}
			qs = append(qs, JSONQuery(s.config.LabelsColumnName).LessThan(value, req.Key()))

		case manifest.In:
			qs = append(qs, JSONQuery(s.config.LabelsColumnName).KeyIn(req.Key(), req.Values()))
		case manifest.NotIn:
			qs = append(qs, JSONQuery(s.config.LabelsColumnName).KeyNotIn(req.Key(), req.Values()))
		case manifest.Exists:
			qs = append(qs, JSONQuery(s.config.LabelsColumnName).HasKey(req.Key()))
		case manifest.DoesNotExist:
			qs = append(qs, JSONQuery(s.config.LabelsColumnName).HasNoKey(req.Key()))
		default:
			return nil, fmt.Errorf("%w: `%v`", ErrUnexpectedSelectorOperator, req.Operator())
		}
	}

	tx := s.db.WithContext(ctx).Offset(int(query.Offset)).Limit(int(query.Limit))
	for _, c := range qs {
		tx = tx.Where(c)
	}

	return tx, nil
}
