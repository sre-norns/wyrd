package identity

import (
	"context"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

func SystemPageOf[T any](tx *gorm.DB, q e.SystemQuery, keys dbstore.Keyset[T]) ([]T, manifest.Page, error) {
	return systemPageOf(tx, q, keys)
}
func NewSystemPage[T any](db *gorm.DB, items []T, page manifest.Page) (e.SystemPage[T], error) {
	return systemPage(db, items, page)
}
func PageError(err error) error { return pageError(err) }
func SystemPrecondition(ctx context.Context, revision int64) error {
	return systemPrecondition(ctx, revision)
}
func RequireReason(action e.SystemAction) error { return requireReason(action) }
func SystemAudit(ctx context.Context, db *gorm.DB, account e.AccountID, target, operation, outcome string, action e.SystemAction) error {
	return systemAudit(ctx, db, account, target, operation, outcome, action)
}
func RecordSystemAudit(ctx context.Context, db *gorm.DB, account e.AccountID, target, operation, outcome string, action e.SystemAction, projectAccount bool) error {
	return recordSystemAudit(ctx, db, account, target, operation, outcome, action, projectAccount)
}

func RejectedSystemAudit(ctx context.Context, db *gorm.DB, cause error) error {
	return rejectedSystemAudit(ctx, db, cause)
}
func SystemTargetRevision(db *gorm.DB, id string) int64 { return systemTargetRevision(db, id) }
