package identity

import (
	"context"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

type StorageOauthGrant = oauthGrant

func IssueSession(ctx context.Context, db *gorm.DB, cfg Config, u user, account e.AccountID, client string, grant ...oauthGrant) (map[string]any, error) {
	return issueSession(ctx, db, cfg, u, account, client, grant...)
}
func BoundedSessionMetadata(value string, limit int) string {
	return boundedSessionMetadata(value, limit)
}
