package identity

import (
	"context"

	"gorm.io/gorm"
)

func ValidatePurgeConfig(c Config) error { return validatePurgeConfig(c) }

func Entitled(db *gorm.DB, userID string) bool              { return entitled(db, userID) }
func SystemAuthority(ctx context.Context, db *gorm.DB) bool { return systemAuthority(ctx, db) }
