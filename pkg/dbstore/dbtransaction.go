package dbstore

import (
	"errors"
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type gormStoreTransaction struct {
	db *gorm.DB

	config SchemaConfig
}

func (tx *gormStoreTransaction) Rollback() {
	tx.db = tx.db.Rollback()
}

func (tx *gormStoreTransaction) Commit() error {
	tx.db = tx.db.Commit()
	return tx.db.Error
}

func (tx *gormStoreTransaction) Create(value any, options ...Option) error {
	rtx, _ := applyOptions(tx.db, tx.config, value, options...)
	return rtx.Create(value).Error
}

func (tx *gormStoreTransaction) Update(newValue any, id manifest.ResourceID, options ...Option) (exists bool, err error) {
	rtx, _ := applyOptions(tx.db.Model(newValue), tx.config, newValue, options...)
	rtx = rtx.Updates(newValue)
	if errors.Is(rtx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}

	return rtx.RowsAffected == 1, rtx.Error
}

func (tx *gormStoreTransaction) CreateOrUpdate(newValue any, options ...Option) (exists bool, err error) {
	rx, _ := applyOptions(tx.db, tx.config, newValue, options...)
	rx = rx.Save(newValue)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}

	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) GetByUID(dest any, id manifest.ResourceID, options ...Option) (bool, error) {
	rx, _ := applyOptions(tx.db, tx.config, dest, options...)
	rx = rx.First(dest, id)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) GetByName(dest any, name manifest.ResourceName, options ...Option) (bool, error) {
	rx, _ := applyOptions(tx.db, tx.config, dest, options...)
	rx = rx.Where("name = ?", name).First(dest)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) Delete(value any, id manifest.ResourceID, version manifest.Version, options ...Option) (existed bool, err error) {
	t, _ := applyOptions(tx.db, tx.config, value, options...)
	if version > 0 {
		t = t.Where(fmt.Sprintf("%s = ?", tx.config.VersionColumnName), version)
	}
	rx := t.Delete(value, id)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) Restore(model any, id manifest.ResourceID, options ...Option) (existed bool, err error) {
	rx, _ := applyOptions(tx.db.Model(model).Unscoped(), tx.config, nil, options...)
	rx = rx.Where(fmt.Sprintf("%s = ?", tx.config.IDColumnName), id).Where(fmt.Sprintf("%s IS NOT NULL", tx.config.DeletedAtColumnName)).Update(tx.config.DeletedAtColumnName, nil)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) AddLinked(value any, link string, owner any, options ...Option) error {
	rx, _ := applyOptions(tx.db.Model(owner), tx.config, value, options...)
	return rx.Association(link).Append(value)
}

func (tx *gormStoreTransaction) RemoveLinked(value any, link string, owner any) error {
	return tx.db.Model(owner).Association(link).Delete(value)
}

func (tx *gormStoreTransaction) ClearLinked(link string, owner any) error {
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
