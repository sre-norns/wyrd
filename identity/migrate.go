package identity

import (
	"errors"
	"fmt"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

type schemaRevision struct {
	ID      int `gorm:"primaryKey"`
	Version int
}

func (schemaRevision) TableName() string { return "identity_schema_revisions" }

// Migrate preserves existing identity tables and records the module revision separately.
func Migrate(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return fmt.Errorf("identity requires PostgreSQL")
	}
	if db.Migrator().HasTable(&schemaRevision{}) {
		var revision schemaRevision
		err := db.First(&revision, 1).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if revision.Version > 1 {
			return fmt.Errorf("identity schema version %d is newer than supported version 1", revision.Version)
		}
	}
	if err := db.AutoMigrate(&e.Session{}, &e.Account{}, &e.AccountMembership{}, &e.AccountInvitation{}, &e.AgentIdentity{}, &e.AgentIdentityToken{}, &e.Project{}, &e.ProjectMembership{}, &e.AgentAuthorization{}, &credential{}, &user{}, &oauthGrant{}, &requestWindow{}, &identityLink{}, &projectAccessMail{}, &invitationDelivery{}, &invitationContinuation{}, &e.SystemEntitlement{}, &e.ImpactPreview{}, &e.OwnerRecovery{}, &e.StepUpAuthorization{}, &e.AccountDeletionRequest{}, &e.AccountDeletionApproval{}, &e.AccountPurgeTombstone{}, &e.SystemActivity{}, &userSignInMethod{}, &upstreamAuthTransaction{}, &providerConfirmation{}, &schemaRevision{}, &idempotencyRecord{}); err != nil {
		return err
	}
	for _, sql := range []string{
		"UPDATE users SET revision = 1 WHERE revision = 0",
		"CREATE UNIQUE INDEX IF NOT EXISTS account_resource_name ON accounts (lower(btrim(name)))",
		"CREATE UNIQUE INDEX IF NOT EXISTS project_resource_name ON projects (account_id, lower(btrim(name)))",
		"CREATE UNIQUE INDEX IF NOT EXISTS agent_resource_name ON agent_identities (account_id, lower(btrim(name)))",
		"CREATE UNIQUE INDEX IF NOT EXISTS account_member ON account_memberships(account_id,user_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS project_member ON project_memberships(project_id,user_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS project_agent ON agent_authorizations(project_id,agent_id)",
	} {
		if err := db.Exec(sql).Error; err != nil {
			return err
		}
	}
	if err := migrateSignInMethods(db); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		if err := migrateSystemAuthority(tx); err != nil {
			return err
		}
		return tx.Save(&schemaRevision{ID: 1, Version: 1}).Error
	})
}
