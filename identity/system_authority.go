package identity

import (
	"context"
	"strings"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

func validatePurgeConfig(c Config) error {
	p := c.Purge
	const maxDurationSeconds = int64(time.Duration(1<<63-1) / time.Second)
	if p.RecoveryDelaySeconds <= 0 || p.RecoveryDelaySeconds > maxDurationSeconds || p.WorkerIntervalSeconds > maxDurationSeconds || (p.ApprovalMode != "single" && p.ApprovalMode != "dual") || p.StepUpMaxAgeSeconds <= 0 || p.StepUpMaxAgeSeconds > 900 || p.AssuranceMethod != "password" || p.WorkerIntervalSeconds <= 0 || p.RetryLimit < 1 || p.RetryLimit > 10 || p.MaxResourceCount <= 0 {
		return invalid("Purge policy requires a recovery delay, approval mode, password assurance, bounded authentication age, and worker bounds.")
	}
	if p.Enabled && !c.Development && p.RecoveryDelaySeconds < 86400 {
		return invalid("Production purge requires a recovery delay of at least one day.")
	}
	return nil
}

func migrateSystemAuthority(tx *gorm.DB) error {
	for _, statement := range []string{
		"UPDATE sessions SET scope = 'account' WHERE scope IS NULL OR scope = ''",
		"UPDATE oauth_grants SET scope = 'account' WHERE scope IS NULL OR scope = ''",
		"UPDATE accounts SET status = 'archived' WHERE status = 'inactive'",
		"UPDATE sessions SET status = 'revoked', revision = revision + 1 WHERE status = 'active' AND account_id IN (SELECT id FROM accounts WHERE status = 'archived')",
		"UPDATE agent_identity_tokens SET status = 'revoked', revision = revision + 1 WHERE status = 'active' AND account_id IN (SELECT id FROM accounts WHERE status = 'archived')",
		"INSERT INTO system_entitlements (user_id, status, revision, updated_at) SELECT id, 'active', 1, clock_timestamp() FROM users WHERE system_admin = true ON CONFLICT (user_id) DO NOTHING",
	} {
		if err := tx.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func entitled(db *gorm.DB, userID string) bool {
	var count int64
	return db.Model(&e.SystemEntitlement{}).Where("user_id = ? AND status = 'active'", userID).Count(&count).Error == nil && count == 1
}

func systemAuthority(ctx context.Context, db *gorm.DB) bool {
	p := principal(ctx)
	if p.Type != "user" || p.Scope != e.ScopeSystem || p.AccountID != "" || p.CredentialID == "" {
		return false
	}
	var count int64
	return db.Table("sessions").Joins("JOIN users ON users.id = sessions.user_id").Joins("JOIN system_entitlements ON system_entitlements.user_id = users.id").Where("sessions.id = ? AND sessions.user_id = ? AND sessions.scope = 'system' AND sessions.account_id = '' AND sessions.status = 'active' AND sessions.expires_at > clock_timestamp() AND users.status = 'active' AND system_entitlements.status = 'active'", p.CredentialID, p.UserID).Count(&count).Error == nil && count == 1
}

func (s *Service) AuthorizeRoute(ctx context.Context, path string) error {
	p := principal(ctx)
	if strings.HasPrefix(path, "/v1/system/") {
		if !systemAuthority(ctx, database(ctx, s.db)) {
			return forbidden()
		}
		return nil
	}
	if p.Scope == e.ScopeSystem {
		if path == "/v1/principal" || path == "/v1/sessions/"+p.CredentialID {
			return nil
		}
		return missing()
	}
	return nil
}

func (s *Service) SetSystemEntitlement(ctx context.Context, email, status string) error {
	if status != "active" && status != "suspended" && status != "revoked" {
		return invalid("Use active, suspended, or revoked entitlement status.")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		var u user
		if err := tx.Where("email = ?", strings.ToLower(email)).First(&u).Error; err != nil {
			return missing()
		}
		var row e.SystemEntitlement
		err := tx.Where("user_id = ?", u.ID).First(&row).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		row.LastModifiedBy = publicActor(mutationActor(ctx))
		row.UserID, row.Status, row.Revision, row.UpdatedAt = u.ID, status, row.Revision+1, time.Now().UTC()
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := tx.Model(&u).Update("system_admin", status == "active").Error; err != nil {
			return err
		}
		if status == "revoked" {
			return tx.Model(&e.Session{}).Where("user_id = ? AND scope = 'system'", u.ID).Updates(resourceMutation(ctx, map[string]any{"status": "revoked", "updated_at": row.UpdatedAt, "revision": gorm.Expr("revision + 1")})).Error
		}
		return nil
	})
}

func validateSessionScope(db *gorm.DB, u user, scope e.SessionScope, account e.AccountID) error {
	if u.Status != "active" {
		return oauthError("invalid_grant")
	}
	if scope == e.ScopeSystem {
		if account != "" || !entitled(db, u.ID) {
			return oauthError("invalid_grant")
		}
		return nil
	}
	if scope != e.ScopeAccount || account == "" {
		return oauthError("invalid_grant")
	}
	var count int64
	err := db.Table("account_memberships").Joins("JOIN accounts ON accounts.id = account_memberships.account_id").Where("account_memberships.account_id = ? AND account_memberships.user_id = ? AND account_memberships.status = 'active' AND accounts.status = 'active'", account, u.ID).Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return oauthError("invalid_grant")
	}
	return nil
}
