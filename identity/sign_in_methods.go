package identity

import (
	"context"
	"errors"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

type userSignInMethod struct {
	LastModifiedBy e.ResourceActor `gorm:"serializer:json;type:jsonb"`
	ID             string          `gorm:"primaryKey;size:36"`
	UserID         string          `gorm:"not null;index;size:36"`
	Method         string          `gorm:"not null;size:16"`
	Subject        *string         `gorm:"size:255"`
	Status         string          `gorm:"not null;size:16"`
	CreatedAt      time.Time
	LastUsedAt     *time.Time
	RevokedAt      *time.Time
	Revision       int64 `gorm:"not null;default:1"`
}

type upstreamAuthTransaction struct {
	ID               string      `gorm:"primaryKey;size:36"`
	Verifier         string      `gorm:"uniqueIndex;size:64"`
	StateDigest      string      `gorm:"size:64"`
	Provider         string      `gorm:"not null;size:16"`
	Purpose          string      `gorm:"not null;size:16"`
	Stage            string      `gorm:"not null;size:16"`
	ClientID         string      `gorm:"size:200"`
	RedirectURI      string      `gorm:"size:2048"`
	Challenge        string      `gorm:"size:128"`
	State            string      `gorm:"size:256"`
	PreferredContext string      `gorm:"size:64"`
	DeviceGrantID    string      `gorm:"size:36"`
	UserCode         string      `gorm:"size:16"`
	InvitationFlowID string      `gorm:"index;size:36"`
	AccountID        e.AccountID `gorm:"index;size:36"`
	Generation       int64
	Subject          *string `gorm:"size:255"`
	Email            string  `gorm:"size:254"`
	AuthenticatedAt  *time.Time
	AccountName      string `gorm:"size:200"`
	CreatedAt        time.Time
	ExpiresAt        time.Time `gorm:"index"`
	Used             bool
}

type providerConfirmation struct {
	ID            string `gorm:"primaryKey;size:36"`
	Verifier      string `gorm:"uniqueIndex;size:64"`
	Purpose       string `gorm:"not null;size:16"`
	UserID        string `gorm:"index;size:36"`
	Provider      string `gorm:"not null;size:16"`
	Subject       string `gorm:"not null;size:255"`
	Email         string `gorm:"not null;index;size:254"`
	TransactionID string `gorm:"index;size:36"`
	AccountName   string `gorm:"size:200"`
	CreatedAt     time.Time
	ExpiresAt     time.Time `gorm:"index"`
	Used          bool
}

func migrateSignInMethods(db *gorm.DB) error {
	for _, sql := range []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS active_user_sign_in_method ON user_sign_in_methods(user_id,method) WHERE status = 'active'",
		"CREATE UNIQUE INDEX IF NOT EXISTS active_provider_subject ON user_sign_in_methods(method,subject) WHERE status = 'active' AND subject IS NOT NULL",
		`INSERT INTO user_sign_in_methods (id, user_id, method, status, created_at, revision)
			SELECT gen_random_uuid()::text, u.id, 'email', 'active', clock_timestamp(), 1 FROM users u
			WHERE u.password IS NOT NULL AND length(u.password) > 0
			AND NOT EXISTS (SELECT 1 FROM user_sign_in_methods m WHERE m.user_id = u.id AND m.method = 'email' AND m.status = 'active')
			ON CONFLICT DO NOTHING`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			return err
		}
	}
	return nil
}

func activateEmailMethod(tx *gorm.DB, userID string) error {
	return tx.Exec("INSERT INTO user_sign_in_methods (id, user_id, method, status, created_at, revision, last_modified_by) VALUES (?, ?, 'email', 'active', clock_timestamp(), 1, ?) ON CONFLICT (user_id, method) WHERE status = 'active' DO NOTHING", newID(), userID, actorJSON(e.ResourceActor{Type: "user", UserID: userID})).Error
}

func methodName(authentication string) string {
	if authentication == "password" {
		return e.SignInEmail
	}
	return authentication
}

func activeMethod(tx *gorm.DB, userID, authentication string) bool {
	var count int64
	return tx.Model(&userSignInMethod{}).Where("user_id = ? AND method = ? AND status = 'active'", userID, methodName(authentication)).Count(&count).Error == nil && count == 1
}

func markMethodUsed(tx *gorm.DB, userID, authentication string, at time.Time) error {
	return tx.Model(&userSignInMethod{}).Where("user_id = ? AND method = ? AND status = 'active'", userID, methodName(authentication)).Update("last_used_at", at).Error
}

type signInMethodService struct {
	db *gorm.DB
}

func publicSignInMethod(m userSignInMethod, u user) e.SignInMethod {
	status := m.Status
	if m.Method == e.SignInEmail && status == "active" && len(u.Password) == 0 {
		status = "inactive"
	}
	return e.SignInMethod{LastModifiedBy: m.LastModifiedBy, ID: m.ID, Method: m.Method, Status: status, CreatedAt: m.CreatedAt, LastUsedAt: m.LastUsedAt, Revision: m.Revision}
}

func (s *signInMethodService) List(ctx context.Context) ([]e.SignInMethod, error) {
	u, err := profileUser(ctx, s.db)
	if err != nil {
		return nil, err
	}
	var rows []userSignInMethod
	if err := database(ctx, s.db).Where("user_id = ? AND status = 'active'", u.ID).Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	methods := []e.SignInMethod{}
	for _, row := range rows {
		if method := publicSignInMethod(row, u); method.Status == "active" {
			methods = append(methods, method)
		}
	}
	return methods, nil
}

func (s *signInMethodService) Get(ctx context.Context, id string) (e.SignInMethod, bool, error) {
	u, err := profileUser(ctx, s.db)
	if err != nil {
		return e.SignInMethod{}, false, err
	}
	var row userSignInMethod
	err = database(ctx, s.db).Where("id = ? AND user_id = ?", id, u.ID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return e.SignInMethod{}, false, nil
	}
	if err != nil {
		return e.SignInMethod{}, false, err
	}
	return publicSignInMethod(row, u), true, nil
}

func usableMethods(tx *gorm.DB, u user) (int64, error) {
	var count int64
	query := tx.Model(&userSignInMethod{}).Where("user_id = ? AND status = 'active'", u.ID)
	if len(u.Password) == 0 {
		query = query.Where("method <> ?", e.SignInEmail)
	}
	return count, query.Count(&count).Error
}

func (s *signInMethodService) Revoke(ctx context.Context, method e.SignInMethod) (e.SignInMethod, error) {
	if method.Status != "revoked" {
		return e.SignInMethod{}, invalidFields("Set status to revoked to remove a sign-in method.", map[string]string{"status": "Only revoked is supported."})
	}
	var out e.SignInMethod
	err := mutation(ctx, s.db, func(tx *gorm.DB) error {
		u, err := profileUser(ctx, tx)
		if err != nil {
			return err
		}
		var row userSignInMethod
		if err := tx.Where("id = ? AND user_id = ?", method.ID, u.ID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return missing()
			}
			return err
		}
		if err := profilePrecondition(ctx, row.Revision); err != nil {
			return err
		}
		if row.Method == e.SignInEmail {
			return problem(409, "method-not-removable", "The email and password method cannot be removed. Use password reset to change the password.")
		}
		if row.Status != "active" {
			return problem(409, "already-revoked", "This sign-in method is already removed.")
		}
		count, err := usableMethods(tx, u)
		if err != nil {
			return err
		}
		if count <= 1 {
			return problem(409, "last-sign-in-method", "This is your only sign-in method. Set a password or link another provider before you remove it.")
		}
		t, err := now(tx)
		if err != nil {
			return err
		}
		result := tx.Model(&userSignInMethod{}).Where("id = ? AND revision = ? AND status = 'active'", row.ID, row.Revision).Updates(systemMutation(ctx, map[string]any{"status": "revoked", "revoked_at": t, "revision": row.Revision + 1}))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return problem(412, "precondition-failed", "The resource has changed.")
		}
		if err := tx.Model(&e.Session{}).Where("user_id = ? AND status = 'active' AND authentication_method = ?", u.ID, row.Method).Updates(resourceMutation(ctx, map[string]any{"status": "revoked", "revision": gorm.Expr("revision + 1"), "updated_at": t})).Error; err != nil {
			return err
		}
		// Pending authentication proofs from the provider cannot finish either.
		if err := tx.Where("user_id = ? AND kind = 'authentication' AND authentication_method = ?", u.ID, row.Method).Delete(&oauthGrant{}).Error; err != nil {
			return err
		}
		if err := metadataAudit(ctx, tx, &e.Resource{ID: row.ID, AccountID: principal(ctx).AccountID, Name: row.Method}, "revoke-sign-in-method"); err != nil {
			return err
		}
		row.LastModifiedBy = publicActor(mutationActor(ctx))
		row.Status, row.RevokedAt, row.Revision = "revoked", &t, row.Revision+1
		out = publicSignInMethod(row, u)
		return nil
	})
	return out, err
}

func purgeProviderState(tx *gorm.DB, account e.AccountID, members []string) error {
	var orphans []string
	if len(members) > 0 {
		if err := tx.Raw("SELECT id FROM users WHERE id IN ? AND NOT EXISTS (SELECT 1 FROM account_memberships m WHERE m.user_id = users.id)", members).Scan(&orphans).Error; err != nil {
			return err
		}
	}
	// Remove pending state through explicit account, user, and transaction
	// ownership. Provider subjects are scoped by provider and can also occur in
	// revoked history, so a subject string is not an ownership boundary.
	if err := tx.Exec(`WITH doomed_transactions AS MATERIALIZED (
		SELECT id FROM upstream_auth_transactions WHERE account_id = ?
		UNION
		SELECT transaction_id FROM provider_confirmations WHERE user_id IN ? AND transaction_id <> ''
		UNION
		SELECT t.id FROM upstream_auth_transactions t
		JOIN user_sign_in_methods m
		  ON m.user_id IN ? AND m.status = 'active'
		 AND m.method = t.provider AND m.subject = t.subject
	), deleted_confirmations AS (
		DELETE FROM provider_confirmations
		WHERE user_id IN ? OR transaction_id IN (SELECT id FROM doomed_transactions)
	)
	DELETE FROM upstream_auth_transactions WHERE id IN (SELECT id FROM doomed_transactions)`, account, orphans, orphans, orphans).Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM user_sign_in_methods WHERE user_id IN ? AND method <> 'email'", orphans).Error; err != nil {
		return err
	}
	return nil
}
