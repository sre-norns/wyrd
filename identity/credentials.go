package identity

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// ReplaceCredential revokes prior verifiers and creates a token for a registered
// product purpose. The caller owns the transaction and the token's domain use.
func ReplaceCredential(ctx context.Context, db *gorm.DB, purpose, owner string) (string, error) {
	if _, ok := extensions(db).CredentialPurposes[purpose]; !ok {
		return "", fmt.Errorf("unregistered credential purpose: %s", purpose)
	}
	db = database(ctx, db)
	if err := db.Model(&credential{}).Where("owner_id = ? AND kind = ?", owner, purpose).Update("used", true).Error; err != nil {
		return "", err
	}
	token := secret()
	if err := db.Create(&credential{ID: newID(), OwnerID: owner, Kind: purpose, Verifier: digest(token)}).Error; err != nil {
		return "", err
	}
	return token, nil
}
func VerifyCredential(ctx context.Context, db *gorm.DB, purpose, owner, token string) (bool, error) {
	if _, ok := extensions(db).CredentialPurposes[purpose]; !ok {
		return false, fmt.Errorf("unregistered credential purpose: %s", purpose)
	}
	var n int64
	err := database(ctx, db).Model(&credential{}).Where("owner_id = ? AND kind = ? AND verifier = ? AND used = false", owner, purpose, digest(token)).Count(&n).Error
	return n == 1, err
}
