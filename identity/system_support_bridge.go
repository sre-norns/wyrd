package identity

import (
	"context"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

func ConsumePreview(ctx context.Context, db *gorm.DB, account e.AccountID, target, operation, id string, revision int64) (e.ImpactPreview, error) {
	return consumePreview(ctx, db, account, target, operation, id, revision)
}
func RecentAuthentication(ctx context.Context, db *gorm.DB, maxAge int64, method string) error {
	return recentAuthentication(ctx, db, maxAge, method)
}
func ValidAccountTransition(status, operation string) (string, bool) {
	return validAccountTransition(status, operation)
}
