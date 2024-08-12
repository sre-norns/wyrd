package dbstore

import (
	"errors"
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type gormStoreTransaction struct {
	db *gorm.DB

	config Config
}

func (tx *gormStoreTransaction) Rollback() {
	tx.db = tx.db.Rollback()
}

func (tx *gormStoreTransaction) Commit() error {
	tx.db = tx.db.Commit()
	return tx.db.Error
}

func (tx *gormStoreTransaction) Create(value any, options ...Option) error {
	return applyOptions(tx.db, value, options...).Create(value).Error
}

func (tx *gormStoreTransaction) Update(newValue any, id manifest.VersionedResourceID, options ...Option) (exists bool, err error) {
	rx := applyOptions(tx.db, newValue, options...).Where(fmt.Sprintf("%s = ?", tx.config.VersionColumnName), id.Version).Save(newValue)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}

	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) GetByUID(dest any, id manifest.ResourceID, options ...Option) (bool, error) {
	rx := applyOptions(tx.db, dest, options...).First(dest, id)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) GetByName(dest any, name manifest.ResourceName, options ...Option) (bool, error) {
	rx := applyOptions(tx.db, dest, options...).Where("name = ?", name).First(dest)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) Delete(value any, id manifest.ResourceID, version manifest.Version, options ...Option) (existed bool, err error) {
	t := applyOptions(tx.db, value, options...)
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
	rx := applyOptions(tx.db.Model(model).Unscoped(), nil, options...).Where(fmt.Sprintf("%s = ?", tx.config.IDColumnName), id).Where(fmt.Sprintf("%s IS NOT NULL", tx.config.DeletedAtColumnName)).Update(tx.config.DeletedAtColumnName, nil)
	if errors.Is(rx.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return rx.RowsAffected == 1, rx.Error
}

func (tx *gormStoreTransaction) AddLinked(value any, link string, owner any, options ...Option) error {
	return applyOptions(tx.db.Model(owner), value, options...).Association(link).Append(value)
}

func (tx *gormStoreTransaction) RemoveLinked(value any, link string, owner any) error {
	return tx.db.Model(owner).Association(link).Delete(value)
}

func RollbackOnPanic(tx *gormStoreTransaction) {
	if r := recover(); r != nil {
		tx.Rollback()
	}
}
