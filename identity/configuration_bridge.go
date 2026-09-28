package identity

import (
	"gorm.io/gorm"
)

func UserDisplay(db *gorm.DB, userID string) (string, *string, error) { return userDisplay(db, userID) }
