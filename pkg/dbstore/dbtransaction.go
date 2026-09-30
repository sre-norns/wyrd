package dbstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type gormStoreTransaction struct {
	db *gorm.DB

	config SchemaConfig

	visibility Visibility
}

func (tx *gormStoreTransaction) context() context.Context {
	return tx.db.Statement.Context
}

func (tx *gormStoreTransaction) Rollback() {
	tx.db = tx.db.Rollback()
}

func (tx *gormStoreTransaction) Commit() error {
	tx.db = tx.db.Commit()
	return tx.db.Error
}

func (tx *gormStoreTransaction) Create(value any, options ...Option) error {
	tc := compileOptions(tx.config, value, options...)
	if err := applyScope(tc, value); err != nil {
		return err
	}
	if err := admit(tx.context(), tx.visibility, value); err != nil {
		return err
	}

	rtx, _, err := applyContext(tx.db, tx.config, tc)
	if err != nil {
		return err
	}
	return rtx.Create(value).Error
}

func (tx *gormStoreTransaction) Update(newValue any, id manifest.ResourceID, options ...Option) (exists bool, err error) {
	tc := compileOptions(tx.config, newValue, options...)
	if err := applyScope(tc, newValue); err != nil {
		return false, err
	}
	if err := admit(tx.context(), tx.visibility, newValue); err != nil {
		return false, err
	}

	rtx, _, err := applyContext(tx.db.Model(newValue), tx.config, tc)
	if err != nil {
		return false, err
	}
	if rtx, err = filterVisible(tx.context(), tx.visibility, rtx, newValue); err != nil {
		return false, err
	}

	rtx = rtx.Updates(newValue)
	if errors.Is(rtx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}

	return rtx.RowsAffected == 1, rtx.Error
}

func (tx *gormStoreTransaction) CreateOrUpdate(newValue any, options ...Option) (exists bool, err error) {
	tc := compileOptions(tx.config, newValue, options...)
	if err := applyScope(tc, newValue); err != nil {
		return false, err
	}
	if err := admit(tx.context(), tx.visibility, newValue); err != nil {
		return false, err
	}
	if err := ensureVisibleIfExists(tx.context(), tx.visibility, tx.db, tx.config, newValue); err != nil {
		return false, err
	}

	rx, _, err := applyContext(tx.db, tx.config, tc)
	if err != nil {
		return false, err
	}
	if tc.withVersion != nil {
		// A versioned save writes only over that version. gorm's Save falls
		// back to an upsert when its UPDATE matches no row -- the version guard
		// is a WHERE clause, so a stale write would overwrite the newer row. An
		// explicit Select("*") disables the fallback and keeps zero values.
		rx = rx.Select("*").Save(newValue)
		return rx.RowsAffected == 1, rx.Error
	}
	rx = rx.Save(newValue)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}

	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) GetByUID(dest any, id manifest.ResourceID, options ...Option) (bool, error) {
	rx, _, err := applyOptions(tx.db, tx.config, dest, options...)
	if err != nil {
		return false, err
	}
	if rx, err = filterVisible(tx.context(), tx.visibility, rx, dest); err != nil {
		return false, err
	}

	rx = rx.First(dest, fmt.Sprintf("%s = ?", tx.config.IDColumnName), id)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) GetByName(dest any, name manifest.ResourceName, options ...Option) (bool, error) {
	rx, _, err := applyOptions(tx.db, tx.config, dest, options...)
	if err != nil {
		return false, err
	}
	if rx, err = filterVisible(tx.context(), tx.visibility, rx, dest); err != nil {
		return false, err
	}

	rx = rx.Where(fmt.Sprintf("%s = ?", tx.config.NameColumnName), name).First(dest)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) Delete(value any, id manifest.ResourceID, version manifest.Version, options ...Option) (existed bool, err error) {
	t, _, err := applyOptions(tx.db, tx.config, value, options...)
	if err != nil {
		return false, err
	}
	if t, err = filterVisible(tx.context(), tx.visibility, t, value); err != nil {
		return false, err
	}

	if version > 0 {
		t = t.Where(fmt.Sprintf("%s = ?", tx.config.VersionColumnName), version)
	}
	rx := t.Delete(value, fmt.Sprintf("%s = ?", tx.config.IDColumnName), id)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) Restore(model any, id manifest.ResourceID, options ...Option) (existed bool, err error) {
	rx, _, err := applyOptions(tx.db.Model(model).Unscoped(), tx.config, nil, options...)
	if err != nil {
		return false, err
	}
	if rx, err = filterVisible(tx.context(), tx.visibility, rx, model); err != nil {
		return false, err
	}

	rx = rx.Where(fmt.Sprintf("%s = ?", tx.config.IDColumnName), id).Where(fmt.Sprintf("%s IS NOT NULL", tx.config.DeletedAtColumnName)).Update(tx.config.DeletedAtColumnName, nil)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) AddLinked(value any, link string, owner any, options ...Option) error {
	if err := admit(tx.context(), tx.visibility, owner); err != nil {
		return err
	}
	if err := admit(tx.context(), tx.visibility, value); err != nil {
		return err
	}

	rx, _, err := applyOptions(tx.db.Model(owner), tx.config, value, options...)
	if err != nil {
		return err
	}
	return rx.Association(link).Append(value)
}

func (tx *gormStoreTransaction) RemoveLinked(value any, link string, owner any) error {
	if err := admit(tx.context(), tx.visibility, owner); err != nil {
		return err
	}
	return tx.db.Model(owner).Association(link).Delete(value)
}

func (tx *gormStoreTransaction) ClearLinked(link string, owner any) error {
	if err := admit(tx.context(), tx.visibility, owner); err != nil {
		return err
	}
	return tx.db.Model(owner).Association(link).Clear()
}

// RollbackOnPanic function sets a recovery point for a DB transaction.
// In case a panic is caught and recovered when a DB transaction is opened, it will be rollback'd.
// This function is designed to be called as `defer`'d after a new transaction is opened.
func RollbackOnPanic(tx *gormStoreTransaction) {
	if r := recover(); r != nil {
		tx.Rollback()
	}
}
