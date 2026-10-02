package identity

import (
	"context"
	"errors"
	"net/mail"
	"slices"
	"strings"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

type invitationDelivery struct {
	ID                        string      `gorm:"primaryKey"`
	AccountID                 e.AccountID `gorm:"index"`
	InvitationID              string      `gorm:"index"`
	CreatedAt                 time.Time
	Generation                int64
	Claim                     string
	LeaseUntil                *time.Time
	e.InvitationEmailDelivery `gorm:"embedded"`
}

func invitationEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return "", invalidFields("Enter a valid email address.", map[string]string{"email": "Enter a single email address."})
	}
	return email, nil
}

func invitationMode(cfg *Config, mode string) (string, error) {
	if mode == "" {
		mode = "manual"
	}
	if !slices.Contains([]string{"manual", "email"}, mode) {
		return "", invalid("Unsupported invitation delivery mode.")
	}
	if mode == "email" && (cfg == nil || !cfg.InvitationEmailEnabled || cfg.SendIdentityMail == nil) {
		return "", problem(503, "mail-unavailable", "Invitation email is not configured or enabled. Contact your service administrator.")
	}
	return mode, nil
}

func invitationRecipient(db *gorm.DB, i e.AccountInvitation) (user, e.AccountMembership, error) {
	var u user
	var m e.AccountMembership
	err := db.Where("email = ?", i.Email).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return u, m, nil
	}
	if err != nil {
		return u, m, err
	}
	if u.Status != "active" {
		return u, m, problem(409, "recipient-unavailable", "This recipient cannot use the invitation. Contact your service administrator.")
	}
	err = db.Where("account_id = ? AND user_id = ?", i.AccountID, u.ID).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return u, m, nil
	}
	if err != nil {
		return u, m, err
	}
	if m.Status != "active" || (i.Recovery && m.Role != "owner") {
		return u, m, problem(409, "membership-unavailable", "Existing account access requires administrator review. Invitations cannot restore access or change a role.")
	}
	return u, m, nil
}

func newInvitation(ctx context.Context, tx *gorm.DB, cfg *Config, input e.AccountInvitation) (out e.AccountInvitation, err error) {
	mode, err := invitationMode(cfg, input.Delivery)
	if err != nil {
		return out, err
	}
	email, err := invitationEmail(input.Email)
	if err != nil {
		return out, err
	}
	if !slices.Contains([]string{"owner", "admin", "member"}, input.Role) {
		return out, invalid("Select a supported account role.")
	}
	a, err := load[e.Account](tx, string(input.AccountID))
	if err != nil {
		return out, err
	}
	if a.Status != "active" {
		return out, conflict("account-inactive")
	}
	out = e.AccountInvitation{Resource: e.Resource{AccountID: input.AccountID}, Email: email, Role: input.Role, Delivery: mode, Recovery: input.Recovery}
	_, member, err := invitationRecipient(tx, out)
	if err != nil {
		return out, err
	}
	if member.ID != "" && !input.Recovery {
		return out, &Problem{Status: 409, Code: "already-member", Detail: "This user is already a member. Manage their existing role.", Fields: map[string]string{"membership_id": member.ID}}
	}
	var pending e.AccountInvitation
	err = tx.Where("account_id = ? AND lower(trim(email)) = ? AND status = 'pending' AND expires_at > clock_timestamp()", out.AccountID, email).First(&pending).Error
	if err == nil {
		return out, &Problem{Status: 409, Code: "invitation-pending", Detail: "A pending invitation already exists. Resend or revoke it.", Fields: map[string]string{"invitation_id": pending.ID}}
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return out, err
	}
	t, err := now(tx)
	if err != nil {
		return out, err
	}
	out.Status, out.ExpiresAt = "pending", t.Add(7*24*time.Hour)
	if err = insert(ctx, tx, &out); err != nil {
		return out, err
	}
	if mode == "manual" {
		out.Token = secret()
		err = tx.Create(&credential{ID: newID(), OwnerID: out.ID, Kind: "invitation", Verifier: digest(out.Token)}).Error
	} else {
		err = queueInvitation(ctx, tx, &out, t)
	}
	if err == nil {
		err = decorateInvitation(tx, &out)
	}
	return
}

func queueInvitation(ctx context.Context, tx *gorm.DB, i *e.AccountInvitation, t time.Time) error {
	var count int64
	err := tx.Model(&invitationDelivery{}).Where("account_id = ? AND created_at > ? AND invitation_id IN (SELECT id FROM account_invitations WHERE account_id = ? AND lower(trim(email)) = ?)", i.AccountID, t.Add(-time.Minute), i.AccountID, i.Email).Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return problem(429, "rate-limited", "Wait one minute before sending another invitation to this email.")
	}
	if err = invalidateInvitation(tx, i.ID); err != nil {
		return err
	}
	i.Generation++
	i.Delivery = "email"
	if err = tx.Model(i).Updates(map[string]any{"generation": i.Generation, "delivery": i.Delivery}).Error; err != nil {
		return err
	}
	job := invitationDelivery{ID: newID(), AccountID: i.AccountID, InvitationID: i.ID, CreatedAt: t, Generation: i.Generation, InvitationEmailDelivery: e.InvitationEmailDelivery{State: "queued", NextRetryAt: &t}}
	return tx.Create(&job).Error
}

func invalidateInvitation(tx *gorm.DB, id string) error {
	if err := tx.Where("owner_id = ? AND kind = 'invitation'", id).Delete(&credential{}).Error; err != nil {
		return err
	}
	if err := tx.Where("invitation_id = ?", id).Delete(&invitationContinuation{}).Error; err != nil {
		return err
	}
	return tx.Model(&invitationDelivery{}).Where("invitation_id = ? AND state IN ?", id, []string{"queued", "sending"}).Updates(map[string]any{"state": "cancelled", "next_retry_at": nil, "lease_until": nil}).Error
}

func decorateInvitation(db *gorm.DB, i *e.AccountInvitation) error {
	i.EmailDelivery = &e.InvitationEmailDelivery{State: "not-requested"}
	var job invitationDelivery
	err := db.Where("invitation_id = ?", i.ID).Order("created_at DESC, id DESC").First(&job).Error
	if err == nil {
		i.EmailDelivery = &job.InvitationEmailDelivery
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

func manageInvitation(ctx context.Context, tx *gorm.DB, i e.AccountInvitation, action e.SystemAction) error {
	if principal(ctx).Scope == e.ScopeSystem {
		if !systemAuthority(ctx, tx) {
			return forbidden()
		}
		return requireReason(action)
	}
	if !accountAdmin(ctx, tx, i.AccountID) || (i.Role == "owner" && accountRole(ctx, tx, i.AccountID) != "owner") {
		return forbidden()
	}
	return nil
}

func revokeInvitation(ctx context.Context, tx *gorm.DB, i *e.AccountInvitation) error {
	if i.Status == "accepted" {
		return conflict("invitation-accepted")
	}
	if err := invalidateInvitation(tx, i.ID); err != nil {
		return err
	}
	i.Status = "revoked"
	i.Generation++
	if err := save(ctx, tx, i); err != nil {
		return err
	}
	return tx.Model(&e.OwnerRecovery{}).Where("invitation_id = ? AND status = 'pending'", i.ID).Updates(systemMutation(ctx, map[string]any{"status": "cancelled", "revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC()})).Error
}

func (s *Service) RequestInvitationDelivery(ctx context.Context, id string, action e.SystemAction) (out e.AccountInvitation, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		var err error
		out, err = load[e.AccountInvitation](tx, id)
		if err != nil {
			return err
		}
		if err = manageInvitation(ctx, tx, out, action); err != nil {
			return err
		}
		if err = precondition(ctx, &out.Resource); err != nil {
			return err
		}
		if _, err = invitationMode(s.config, "email"); err != nil {
			return err
		}
		if out.Status != "pending" && out.Status != "expired" {
			return conflict("invitation-inactive")
		}
		a, err := load[e.Account](tx, string(out.AccountID))
		if err != nil {
			return err
		}
		if a.Status != "active" {
			return conflict("account-inactive")
		}
		if _, _, err = invitationRecipient(tx, out); err != nil {
			return err
		}
		var pending e.AccountInvitation
		duplicate := tx.Where("account_id = ? AND lower(trim(email)) = ? AND id <> ? AND status = 'pending' AND expires_at > clock_timestamp()", out.AccountID, out.Email, out.ID).First(&pending).Error
		if duplicate == nil {
			return &Problem{Status: 409, Code: "invitation-pending", Detail: "A pending invitation already exists. Resend or revoke it.", Fields: map[string]string{"invitation_id": pending.ID}}
		}
		if !errors.Is(duplicate, gorm.ErrRecordNotFound) {
			return duplicate
		}
		t, err := now(tx)
		if err != nil {
			return err
		}
		if err = queueInvitation(ctx, tx, &out, t); err != nil {
			return err
		}
		out.Status, out.ExpiresAt = "pending", t.Add(7*24*time.Hour)
		if err = save(ctx, tx, &out); err != nil {
			return err
		}
		if principal(ctx).Scope == e.ScopeSystem {
			return systemAudit(ctx, tx, out.AccountID, out.ID, "resend-invitation", "succeeded", action)
		}
		return nil
	})
	if err == nil {
		err = decorateInvitation(database(ctx, s.db), &out)
	}
	return
}

func (s *Service) RevokeInvitation(ctx context.Context, id string, action e.SystemAction) (out e.AccountInvitation, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		var err error
		out, err = load[e.AccountInvitation](tx, id)
		if err != nil {
			return err
		}
		if err = manageInvitation(ctx, tx, out, action); err != nil {
			return err
		}
		if err = precondition(ctx, &out.Resource); err != nil {
			return err
		}
		if err = revokeInvitation(ctx, tx, &out); err != nil {
			return err
		}
		if principal(ctx).Scope == e.ScopeSystem {
			return systemAudit(ctx, tx, out.AccountID, out.ID, "revoke-invitation", "succeeded", action)
		}
		return nil
	})
	if err == nil {
		err = decorateInvitation(database(ctx, s.db), &out)
	}
	return
}

func (s *Service) CreateFirstOwnerInvitation(ctx context.Context, account e.AccountID, input e.FirstOwnerInvitation) (out e.AccountInvitation, err error) {
	action := e.SystemAction{Reason: input.Reason, Reference: input.Reference}
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
		if err = precondition(ctx, &a.Resource); err != nil {
			return err
		}
		var count int64
		if err = tx.Model(&e.AccountMembership{}).Where("account_id = ? AND role = 'owner' AND status = 'active'", account).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return conflict("account-has-owner")
		}
		if err = tx.Model(&e.AccountInvitation{}).Where("account_id = ? AND role = 'owner' AND status = 'pending' AND expires_at > clock_timestamp()", account).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return conflict("owner-invitation-pending")
		}
		out, err = newInvitation(ctx, tx, s.config, e.AccountInvitation{Resource: e.Resource{AccountID: account}, Email: input.Email, Role: "owner", Delivery: input.Delivery})
		if err != nil {
			return err
		}
		return systemAudit(ctx, tx, account, out.ID, "invite-first-owner", "succeeded", action)
	})
	return
}

func acceptInvitation(ctx context.Context, tx *gorm.DB, i *e.AccountInvitation, u user) (member e.AccountMembership, err error) {
	if !strings.EqualFold(i.Email, u.Email) || u.Status != "active" {
		return member, forbidden()
	}
	a, err := load[e.Account](tx, string(i.AccountID))
	if err != nil {
		return member, err
	}
	if a.Status != "active" {
		return member, conflict("account-inactive")
	}
	if i.Status == "accepted" && i.AcceptedBy == u.ID {
		member, err = load[e.AccountMembership](tx, string(i.MembershipID))
		if err == nil && member.Status != "active" {
			err = conflict("membership-unavailable")
		}
		return member, err
	}
	t, err := now(tx)
	if err != nil {
		return member, err
	}
	if i.Status != "pending" || !i.ExpiresAt.After(t) {
		return member, conflict("invitation-inactive")
	}
	_, member, err = invitationRecipient(tx, *i)
	if err != nil {
		return member, err
	}
	actor := WithPrincipal(ctx, e.Principal{Type: "user", Scope: e.ScopeAccount, UserID: u.ID, AccountID: i.AccountID})
	if member.ID == "" {
		member = e.AccountMembership{Resource: e.Resource{AccountID: i.AccountID}, UserID: u.ID, Role: i.Role}
		if err = insert(actor, tx, &member); err != nil {
			return member, err
		}
	}
	i.Status, i.AcceptedBy, i.MembershipID = "accepted", u.ID, e.AccountMembershipID(member.ID)
	if err = save(actor, tx, i); err != nil {
		return member, err
	}
	err = tx.Model(&invitationDelivery{}).Where("invitation_id = ? AND state IN ?", i.ID, []string{"queued", "sending"}).Updates(map[string]any{"state": "cancelled", "next_retry_at": nil, "lease_until": nil}).Error
	return member, err
}
