package dbstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

var (
	// ErrNotVisible is returned when a write names an existing resource the
	// caller cannot see. It is distinct from not-found so that the store never
	// turns it into a create, but a caller should report it as not found: saying
	// more would disclose that the resource exists.
	ErrNotVisible = errors.New("resource is not visible to the caller")

	// ErrNoScopeColumns is returned when [InScope] is used with a [SchemaConfig]
	// that does not name the scope columns.
	ErrNoScopeColumns = errors.New("schema config does not name the scope columns")
)

// Visibility decides which rows the caller of a store operation may see and
// which values it may write. It is how a multi-tenant product makes tenancy a
// property of the store rather than a filter every handler must remember.
//
// A store built with one applies it to every operation: Find and its count,
// Get, the name and label catalogues, linked finds, Update, Delete and Restore
// are filtered; Create, CreateOrUpdate and link changes are admitted first. A
// caller that must see everything -- a control loop acting for the service --
// uses a store without one, and so has to say so.
//
// The context is the operation's own, so an implementation reads the caller's
// identity from it.
type Visibility interface {
	// Filter narrows query to rows of model the caller may see. model is the
	// value the operation is over: a pointer to a model, or to a slice of them.
	Filter(ctx context.Context, query *gorm.DB, model any) (*gorm.DB, error)

	// Admit reports whether the caller may write value as it stands, including
	// the scope it names. A non-nil error refuses the write.
	Admit(ctx context.Context, value any) error
}

// WithVisibility returns a store that applies v to every operation, sharing
// this store's database. The receiver is unchanged.
func (s *DBStore) WithVisibility(v Visibility) *DBStore {
	return &DBStore{
		db:         s.db,
		config:     s.config,
		visibility: v,
	}
}

func filterVisible(ctx context.Context, v Visibility, query *gorm.DB, model any) (*gorm.DB, error) {
	if v == nil || query == nil {
		return query, nil
	}

	filtered, err := v.Filter(ctx, query, model)
	if err != nil {
		return nil, fmt.Errorf("visibility filter: %w", err)
	}

	return filtered, nil
}

func admit(ctx context.Context, v Visibility, value any) error {
	if v == nil {
		return nil
	}

	return v.Admit(ctx, value)
}

// scopeSetter is implemented by every model embedding [manifest.ObjectMeta].
type scopeSetter interface {
	ApplyScope(manifest.ScopeRef) error
}

// applyScope places value in the scope an [InScope] option asked for.
func applyScope(tc transactionContext, value any) error {
	if tc.scope == nil {
		return nil
	}

	setter, ok := value.(scopeSetter)
	if !ok {
		return fmt.Errorf("%w: %T does not carry a scope", manifest.ErrInvalidScope, value)
	}

	return setter.ApplyScope(*tc.scope)
}

// versioned is implemented by every model embedding [manifest.ObjectMeta].
type versioned interface {
	GetVersionedID() manifest.VersionedResourceID
}

// ensureVisibleIfExists refuses a write that names an existing row the caller
// cannot see.
//
// gorm's Save updates by primary key and, when that matches nothing, falls back
// to INSERT ... ON CONFLICT DO UPDATE. A filter on the UPDATE is therefore no
// protection: a filtered-out row makes the UPDATE match nothing, and the
// fallback then overwrites that very row. So the check is made before the save,
// against the row as it stands.
func ensureVisibleIfExists(ctx context.Context, v Visibility, db *gorm.DB, config SchemaConfig, value any) error {
	if v == nil {
		return nil
	}

	id, ok := value.(versioned)
	if !ok || id.GetVersionedID().ID == "" {
		return nil
	}

	where := fmt.Sprintf("%s = ?", config.IDColumnName)
	uid := id.GetVersionedID().ID

	var exists int64
	if err := db.Session(&gorm.Session{NewDB: true}).Unscoped().Model(value).Where(where, uid).Count(&exists).Error; err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}

	visible, err := v.Filter(ctx, db.Session(&gorm.Session{NewDB: true}).Unscoped().Model(value).Where(where, uid), value)
	if err != nil {
		return fmt.Errorf("visibility filter: %w", err)
	}

	var seen int64
	if err := visible.Count(&seen).Error; err != nil {
		return err
	}
	if seen == 0 {
		return fmt.Errorf("%w: %v", ErrNotVisible, uid)
	}

	return nil
}

// DropLegacyNameIndexes removes the name indexes earlier wyrd releases created
// for models, so that names become unique per scope rather than per table.
//
// Before v0.3.0 every table shared an index named idx_name; from v0.3.0 to
// scoping, the unique index was idx_<table>_deleted_name over (name,
// deleted_at). AutoMigrate adds the scoped unique index beside the old one but
// never drops the old one, and the old one, being stricter, would still refuse
// the same name in two projects. Call this once, after AutoMigrate, when a
// product adopts scoped resources. It is a no-op for indexes already gone.
func DropLegacyNameIndexes(db *gorm.DB, models ...any) error {
	migrator := db.Migrator()
	for _, model := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return fmt.Errorf("failed to parse model %T: %w", model, err)
		}

		for _, name := range []string{"idx_" + stmt.Schema.Table + "_deleted_name", "idx_name"} {
			if !migrator.HasIndex(model, name) {
				continue
			}
			if err := migrator.DropIndex(model, name); err != nil {
				return fmt.Errorf("failed to drop legacy index %s on %s: %w", name, stmt.Schema.Table, err)
			}
		}
	}

	return nil
}
