package identity

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

func supportAccount(ctx context.Context, db *gorm.DB, account e.AccountID, operation string) error {
	if systemAuthority(ctx, db) {
		return nil
	}
	if principal(ctx).Scope != e.ScopeSystem && principal(ctx).Type == "user" && principal(ctx).AccountID == account && accountRole(ctx, db, account) == "owner" && operation == "archive" {
		return nil
	}
	return forbidden()
}

func impactTarget(db *gorm.DB, account e.AccountID, operation, target string) (int64, error) {
	if operation == "revoke-membership" {
		member, err := load[e.AccountMembership](db, target)
		if err != nil {
			return 0, err
		}
		if member.AccountID != account {
			return 0, missing()
		}
		return member.Revision, nil
	}
	switch operation {
	case "suspend", "resume", "archive", "restore", "purge":
		item, err := load[e.Account](db, string(account))
		return item.Revision, err
	default:
		if f := extensions(db).ImpactTarget; f != nil {
			return f(db, account, operation, target)
		}
		return 0, invalid("Unsupported impact operation.")
	}
}

func previewMaterial(db *gorm.DB, account e.AccountID, operation string) (map[string]int64, string, error) {
	if account != "" {
		return accountImpact(db, account)
	}
	counts := map[string]int64{}
	var hashes []string
	for _, table := range previewTables(db) {
		var row struct {
			Count  int64
			Digest string
		}
		if err := db.Table(table).Select("count(*) AS count, md5(COALESCE(string_agg(id || ':' || revision::text || ':' || status, ',' ORDER BY id), '')) AS digest").Scan(&row).Error; err != nil {
			return nil, "", err
		}
		counts[table] = row.Count
		hashes = append(hashes, row.Digest)
	}
	return counts, digest(strings.Join(hashes, ":")), nil
}

func (s *Service) CreateImpactPreview(ctx context.Context, account e.AccountID, action e.SystemAction) (out e.ImpactPreview, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		if err := supportAccount(ctx, tx, account, action.Operation); err != nil {
			return err
		}
		target := action.TargetID
		if target == "" {
			target = string(account)
		}
		revision, err := impactTarget(tx, account, action.Operation, target)
		if err != nil {
			return err
		}
		counts, material, err := previewMaterial(tx, account, action.Operation)
		if err != nil {
			return err
		}
		out = e.ImpactPreview{SystemRecord: newSystemRecord(ctx, "ready"), TargetAccountID: account, TargetID: target, Operation: action.Operation, TargetRevision: revision, Counts: counts, MaterialDigest: material, SessionID: principal(ctx).CredentialID, ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		return recordSystemAudit(ctx, tx, account, out.ID, "create-impact-preview", "succeeded", e.SystemAction{}, false)
	})
	return
}

func (s *Service) ImpactPreviews(ctx context.Context, account e.AccountID, q e.SystemQuery) (out e.SystemPage[e.ImpactPreview], err error) {
	db := database(ctx, s.db)
	if err = supportAccount(ctx, db, account, "archive"); err != nil {
		return
	}
	items, page, err := systemPageOf(db.Model(&e.ImpactPreview{}).Where("target_account_id = ? AND actor_id = ? AND session_id = ?", account, principal(ctx).UserID, principal(ctx).CredentialID), q, systemNewestFirst[e.ImpactPreview]())
	if err != nil {
		return out, err
	}
	return systemPage(db, items, page)
}

func consumePreview(ctx context.Context, db *gorm.DB, account e.AccountID, target, operation, id string, revision int64) (e.ImpactPreview, error) {
	var preview e.ImpactPreview
	if id == "" {
		return preview, conflict("preview-required")
	}
	if err := db.Where("id = ?", id).First(&preview).Error; err != nil {
		return preview, conflict("invalid-preview")
	}
	t, err := now(db)
	if err != nil {
		return preview, err
	}
	if preview.ActorID != principal(ctx).UserID || preview.SessionID != principal(ctx).CredentialID || preview.TargetAccountID != account || preview.TargetID != target || preview.Operation != operation || preview.TargetRevision != revision || preview.ConsumedAt != nil || !preview.ExpiresAt.After(t) {
		return preview, conflict("stale-preview")
	}
	_, material, err := previewMaterial(db, account, operation)
	if err != nil {
		return preview, err
	}
	if material != preview.MaterialDigest {
		return preview, conflict("stale-preview")
	}
	preview.ConsumedAt = &t
	preview.Status = "consumed"
	preview.Revision++
	preview.UpdatedAt = t
	return preview, db.Save(&preview).Error
}

func recentAuthentication(ctx context.Context, db *gorm.DB, maxAge int64, method string) error {
	session, err := load[e.Session](db, principal(ctx).CredentialID)
	if err != nil {
		return conflict("reauthentication-required")
	}
	t, err := now(db)
	if err != nil {
		return err
	}
	if session.Status == "active" && method == "password" && slices.Contains(providerNames, session.AuthenticationMethod) {
		// Provider sign-in never satisfies password assurance.
		return problem(409, "reauthentication-required", "This operation requires a recent password sign-in. "+providerLabel(session.AuthenticationMethod)+" sign-in does not confirm it. If your user has no password, set one with Forgot password on the login page. Then log in with the password and try again.")
	}
	if session.Status != "active" || session.UserID != principal(ctx).UserID || session.AuthenticatedAt.IsZero() || session.AuthenticatedAt.After(t) || t.Sub(session.AuthenticatedAt) > time.Duration(maxAge)*time.Second || session.AuthenticationMethod != method {
		return conflict("reauthentication-required")
	}
	return nil
}

func validAccountTransition(status, operation string) (string, bool) {
	transitions := map[string]map[string]string{"active": {"suspend": "suspended", "archive": "archived"}, "suspended": {"resume": "active", "archive": "archived"}, "archived": {"restore": "active"}}
	next, ok := transitions[status][operation]
	return next, ok
}

func (s *Service) ChangeAccountLifecycle(ctx context.Context, id e.AccountID, action e.SystemAction) (out e.SystemAccount, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		if err := supportAccount(ctx, tx, id, action.Operation); err != nil {
			return err
		}
		if err := requireReason(action); err != nil {
			return err
		}
		account, err := load[e.Account](tx, string(id))
		if err != nil {
			return err
		}
		if err := systemPrecondition(ctx, account.Revision); err != nil {
			return err
		}
		next, ok := validAccountTransition(account.Status, action.Operation)
		if !ok {
			return conflict("invalid-account-transition")
		}
		if principal(ctx).Scope != e.ScopeSystem {
			if account.Status != "active" || action.Confirmation != account.Name {
				return invalid("Confirm the current account name.")
			}
			if err := recentAuthentication(ctx, tx, s.config.Purge.StepUpMaxAgeSeconds, s.config.Purge.AssuranceMethod); err != nil {
				return err
			}
		}
		if action.Confirmation == "" {
			return invalid("Explicit account confirmation is required.")
		}
		if principal(ctx).Scope == e.ScopeSystem && action.Confirmation != string(id) {
			return invalid("Confirm the exact account ID.")
		}
		if _, err := consumePreview(ctx, tx, id, string(id), action.Operation, action.PreviewID, account.Revision); err != nil {
			return err
		}
		t, err := now(tx)
		if err != nil {
			return err
		}
		account.Status = next
		account.Revision++
		account.UpdatedAt = t
		account.Actor = principal(ctx)
		account.LifecycleReason = action.Reason
		account.LifecycleAt = &t
		if err := tx.Save(&account).Error; err != nil {
			return err
		}
		if next == "archived" {
			for _, table := range []string{"sessions", "agent_identity_tokens"} {
				if err := tx.Table(table).Where("account_id = ? AND status = 'active'", id).Updates(map[string]any{"status": "revoked", "revision": gorm.Expr("revision + 1"), "updated_at": t}).Error; err != nil {
					return err
				}
			}
		}
		if err := systemAudit(ctx, tx, id, string(id), action.Operation, "succeeded", action); err != nil {
			return err
		}
		out, err = accountProjection(tx, account)
		return err
	})
	return
}

func revokeMembership(ctx context.Context, tx *gorm.DB, member *e.AccountMembership) error {
	if member.Status != "active" && member.Status != "suspended" {
		return conflict("membership-not-active")
	}
	var account e.Account
	if err := tx.Where("id = ?", member.AccountID).First(&account).Error; err != nil {
		return err
	}
	if member.Role == "owner" && member.Status == "active" && account.Status == "active" {
		var owners int64
		if err := tx.Model(&e.AccountMembership{}).Where("account_id = ? AND role = 'owner' AND status = 'active'", member.AccountID).Count(&owners).Error; err != nil {
			return err
		}
		if owners <= 1 {
			return conflict("last-owner")
		}
	}
	t, err := now(tx)
	if err != nil {
		return err
	}
	for _, table := range []string{"sessions", "project_memberships"} {
		if err := tx.Table(table).Where("account_id = ? AND user_id = ? AND status <> 'revoked'", member.AccountID, member.UserID).Updates(map[string]any{"status": "revoked", "revision": gorm.Expr("revision + 1"), "updated_at": t}).Error; err != nil {
			return err
		}
	}
	member.Status = "revoked"
	member.Revision++
	member.UpdatedAt = t
	return tx.Save(member).Error
}

func (s *Service) RevokeSystemMembership(ctx context.Context, id string, action e.SystemAction) (out e.SystemMembership, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		if !systemAuthority(ctx, tx) {
			return forbidden()
		}
		if err := requireReason(action); err != nil {
			return err
		}
		member, err := load[e.AccountMembership](tx, id)
		if err != nil {
			return err
		}
		if err := systemPrecondition(ctx, member.Revision); err != nil {
			return err
		}
		if action.Confirmation != id {
			return invalid("Confirm the exact membership ID.")
		}
		if _, err := consumePreview(ctx, tx, member.AccountID, id, "revoke-membership", action.PreviewID, member.Revision); err != nil {
			return err
		}
		if err := revokeMembership(ctx, tx, &member); err != nil {
			return err
		}
		if err := systemAudit(ctx, tx, member.AccountID, id, "revoke-membership", "succeeded", action); err != nil {
			return err
		}
		email, _, err := userDisplay(tx, member.UserID)
		out = e.SystemMembership{SystemRecord: systemRecord(member.Resource), AccountID: member.AccountID, UserID: member.UserID, Email: email, Role: member.Role}
		return err
	})
	return
}

func (s *Service) CreateOwnerRecovery(ctx context.Context, account e.AccountID, action e.SystemAction) (out e.OwnerRecovery, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		if !systemAuthority(ctx, tx) {
			return forbidden()
		}
		if err := requireReason(action); err != nil {
			return err
		}
		a, err := load[e.Account](tx, string(account))
		if err != nil {
			return err
		}
		if a.Status != "active" {
			return conflict("account-inactive")
		}
		if err := systemPrecondition(ctx, a.Revision); err != nil {
			return err
		}
		previous, err := load[e.AccountMembership](tx, action.TargetID)
		if err != nil {
			return err
		}
		if previous.AccountID != account || previous.Role != "owner" || previous.Status != "active" {
			return invalid("Select an active owner membership.")
		}
		email := strings.ToLower(strings.TrimSpace(action.ReplacementEmail))
		if !strings.Contains(email, "@") || len(email) > 254 {
			return invalidFields("Enter the replacement owner's verified email.", map[string]string{"replacement_email": "Enter a valid email."})
		}
		var existing int64
		if err := tx.Model(&e.OwnerRecovery{}).Where("target_account_id = ? AND status = 'pending'", account).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return conflict("owner-recovery-pending")
		}
		previousUser, err := load[user](tx, previous.UserID)
		if err != nil {
			return err
		}
		if strings.EqualFold(previousUser.Email, email) {
			return invalid("Choose a different replacement owner.")
		}
		invitation, err := newInvitation(ctx, tx, s.config, e.AccountInvitation{Resource: e.Resource{AccountID: account}, Email: email, Role: "owner", Delivery: action.Delivery, Recovery: true})
		if err != nil {
			return err
		}
		out = e.OwnerRecovery{SystemRecord: newSystemRecord(ctx, "pending"), TargetAccountID: account, PreviousMembershipID: previous.ID, InvitationID: invitation.ID, ReplacementEmail: email, InvitationStatus: "pending", ExpiresAt: invitation.ExpiresAt, Reason: action.Reason, Reference: action.Reference, InvitationToken: invitation.Token, EmailDelivery: invitation.EmailDelivery}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		return systemAudit(ctx, tx, account, out.ID, "create-owner-recovery", "succeeded", action)
	})
	return
}

func decorateRecovery(db *gorm.DB, recovery *e.OwnerRecovery) error {
	invitation, err := load[e.AccountInvitation](db, recovery.InvitationID)
	if err != nil {
		return err
	}
	if err := decorateInvitation(db, &invitation); err != nil {
		return err
	}
	recovery.EmailDelivery = invitation.EmailDelivery
	recovery.ExpiresAt = invitation.ExpiresAt
	recovery.InvitationStatus = invitation.Status
	if invitation.Status == "pending" && !invitation.ExpiresAt.After(time.Now()) {
		recovery.InvitationStatus = "expired"
	}
	if invitation.Status == "accepted" {
		var member e.AccountMembership
		err := db.Where("account_id = ? AND user_id = ? AND role = 'owner' AND status = 'active'", recovery.TargetAccountID, invitation.AcceptedBy).First(&member).Error
		if err == nil {
			recovery.ReplacementMembershipID = member.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	return nil
}

func (s *Service) OwnerRecovery(ctx context.Context, id string) (out e.OwnerRecovery, err error) {
	db := database(ctx, s.db)
	if !systemAuthority(ctx, db) {
		return out, forbidden()
	}
	out, err = load[e.OwnerRecovery](db, id)
	if err == nil {
		err = decorateRecovery(db, &out)
	}
	return
}

func (s *Service) CompleteOwnerRecovery(ctx context.Context, id string, action e.SystemAction) (out e.OwnerRecovery, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		if !systemAuthority(ctx, tx) {
			return forbidden()
		}
		if err := requireReason(action); err != nil {
			return err
		}
		out, err = load[e.OwnerRecovery](tx, id)
		if err != nil {
			return err
		}
		if err := systemPrecondition(ctx, out.Revision); err != nil {
			return err
		}
		if out.Status != "pending" {
			return conflict("recovery-not-pending")
		}
		if err := decorateRecovery(tx, &out); err != nil {
			return err
		}
		if out.ReplacementMembershipID == "" || out.ReplacementMembershipID == out.PreviousMembershipID {
			return conflict("replacement-owner-required")
		}
		member, err := load[e.AccountMembership](tx, out.PreviousMembershipID)
		if err != nil {
			return err
		}
		if action.Confirmation != id {
			return invalid("Confirm the exact recovery ID.")
		}
		if err := revokeMembership(ctx, tx, &member); err != nil {
			return err
		}
		out.Status = "completed"
		out.Revision++
		out.UpdatedAt = time.Now().UTC()
		if err := tx.Save(&out).Error; err != nil {
			return err
		}
		return systemAudit(ctx, tx, out.TargetAccountID, id, "complete-owner-recovery", "succeeded", action)
	})
	return
}

func (s *Service) OwnerRecoveries(ctx context.Context, account e.AccountID, q e.SystemQuery) (out e.SystemPage[e.OwnerRecovery], err error) {
	db := database(ctx, s.db)
	if !systemAuthority(ctx, db) {
		return out, forbidden()
	}
	items, page, err := systemPageOf(db.Model(&e.OwnerRecovery{}).Where("target_account_id = ?", account), q, systemNewestFirst[e.OwnerRecovery]())
	if err != nil {
		return out, err
	}
	for i := range items {
		if err = decorateRecovery(db, &items[i]); err != nil {
			return
		}
	}
	return systemPage(db, items, page)
}

func (s *Service) CreateSystemAccount(ctx context.Context, input e.SystemAccountCreate) (out e.SystemAccountCreated, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		if !systemAuthority(ctx, tx) {
			return forbidden()
		}
		action := e.SystemAction{Reason: input.Reason, Reference: input.Reference}
		if err := requireReason(action); err != nil {
			return err
		}
		config := *s.config
		config.AccountProvisioning = "system-admin-only"
		accounts := accountsService{db: tx, config: &config}
		resource, _, err := accounts.CreateOrUpdate(ctx, e.Account{Resource: e.Resource{Name: input.Name}, Description: input.Description, OwnerEmail: input.OwnerEmail, Delivery: input.Delivery})
		if err != nil {
			return err
		}
		out.SystemAccount, err = accountProjection(tx, resource)
		if err != nil {
			return err
		}
		out.OwnerInvitationID = resource.OwnerInvitation.ID
		out.InvitationToken = resource.OwnerInvitation.Token
		out.EmailDelivery = resource.OwnerInvitation.EmailDelivery
		return systemAudit(ctx, tx, e.AccountID(resource.ID), resource.ID, "provision-account", "succeeded", action)
	})
	return
}
