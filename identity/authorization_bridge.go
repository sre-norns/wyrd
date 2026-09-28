package identity

import (
	"context"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

func AccountRole(ctx context.Context, db *gorm.DB, account e.AccountID) string {
	return accountRole(ctx, db, account)
}
func AccountAdmin(ctx context.Context, db *gorm.DB, account e.AccountID) bool {
	return accountAdmin(ctx, db, account)
}
func ProjectAdmin(ctx context.Context, db *gorm.DB, project e.ProjectID) bool {
	return projectAdmin(ctx, db, project)
}
func AgentGrant(ctx context.Context, db *gorm.DB, project e.ProjectID, role e.RoleType) bool {
	return agentGrant(ctx, db, project, role)
}

func Visible(ctx context.Context, db *gorm.DB, k string) (*gorm.DB, error) {
	return visible(ctx, db, k)
}

// PrepareResource validates an identity resource inside a caller-owned transaction.
func PrepareResource(ctx context.Context, db *gorm.DB, v any, creating bool) error {
	return prepare(ctx, db, v, creating)
}

// AfterCreateResource initializes identity ownership inside the create transaction.
func AfterCreateResource(ctx context.Context, db *gorm.DB, v any) error {
	return afterCreate(ctx, db, v)
}
