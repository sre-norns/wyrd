package identity

import (
	"context"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

func PageQuery(db *gorm.DB, q e.SystemQuery) (*gorm.DB, int, error) { return pageQuery(db, q) }
func NextCursor(id string) string                                   { return nextCursor(id) }
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
