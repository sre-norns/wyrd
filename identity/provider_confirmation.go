package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	identitymail "github.com/sre-norns/wyrd/identity/mail"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

const (
	confirmRegister = "register"
	confirmLink     = "link"
	confirmNotice   = "notice"
)

const (
	ProviderExistingUser = "existing-user"
	ProviderRegistered   = "registered"
	ProviderLinked       = "linked"
)

type providerMail struct {
	confirmationID, to, subject, body string
}

func (s *Service) sendProviderMail(ctx context.Context, m *providerMail) error {
	if m == nil {
		return nil
	}
	if err := s.config.SendIdentityMail(ctx, m.to, m.subject, m.body); err != nil {
		slog.ErrorContext(ctx, "Failed to send account email", "err", err)
		// Remove the undelivered confirmation so no usable pending state remains.
		_ = s.db.WithContext(ctx).Where("id = ?", m.confirmationID).Delete(&providerConfirmation{}).Error
		return problem(503, "mail-unavailable", "The service cannot send account email. Try again later or contact your service administrator.")
	}
	return nil
}

func mailUnavailable() error {
	return problem(503, "mail-unavailable", "Account email is not configured. Contact your service administrator.")
}

func (s *Service) prepareProviderConfirmation(tx *gorm.DB, purpose, userID, to string, t upstreamAuthTransaction, identity providerIdentity, accountName string) (*providerMail, error) {
	if !s.IdentityMailAvailable() {
		return nil, mailUnavailable()
	}
	current, err := now(tx)
	if err != nil {
		return nil, err
	}
	authenticated := identity.AuthenticatedAt
	subject := identity.Subject
	expires := current.Add(30 * time.Minute)
	var invitation *invitationContinuation
	if t.Purpose == purposeInvitation {
		flow, pending, err := invitationForTransaction(tx, t)
		if err != nil {
			return nil, err
		}
		if pending.ExpiresAt.Before(expires) {
			expires = pending.ExpiresAt
		}
		invitation = &flow
	}
	activate := func() error {
		// The transaction cannot restart, but it stays readable for the resume.
		if err := tx.Model(&upstreamAuthTransaction{}).Where("id = ?", t.ID).Updates(map[string]any{"stage": "confirming", "used": true, "subject": &subject, "email": identity.Email, "authenticated_at": &authenticated, "account_name": accountName, "expires_at": expires}).Error; err != nil {
			return err
		}
		if invitation != nil {
			return tx.Model(invitation).Update("expires_at", expires).Error
		}
		return nil
	}

	// A repeated equivalent request uses the confirmation that the person
	// already received. Rebind it to the newest outer transaction so that the
	// existing link resumes the current browser flow without sending more mail.
	if purpose != confirmNotice {
		var recent providerConfirmation
		query := tx.Where("email = ? AND purpose = ? AND user_id = ? AND provider = ? AND subject = ? AND account_name = ? AND used = false AND created_at > ? AND expires_at > ?", to, purpose, userID, identity.Provider, identity.Subject, accountName, current.Add(-time.Minute), current)
		if t.Purpose == purposeInvitation {
			query = query.Where("transaction_id IN (SELECT id FROM upstream_auth_transactions WHERE invitation_flow_id = ?)", t.InvitationFlowID)
		}
		err := query.Order("created_at DESC").First(&recent).Error
		if err == nil {
			if err := activate(); err != nil {
				return nil, err
			}
			if err := tx.Model(&providerConfirmation{}).Where("id = ?", recent.ID).Updates(map[string]any{"transaction_id": t.ID, "expires_at": expires}).Error; err != nil {
				return nil, err
			}
			return nil, tx.Model(&providerConfirmation{}).Where("provider = ? AND subject = ? AND id <> ? AND used = false", identity.Provider, identity.Subject, recent.ID).Update("used", true).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	var count int64
	query := tx.Model(&providerConfirmation{}).Where("email = ? AND created_at > ?", to, current.Add(-time.Minute))
	if purpose != confirmNotice {
		// A notice directs a new user to registration. It must not suppress the
		// actionable confirmation that registration requires.
		query = query.Where("purpose IN ?", []string{confirmRegister, confirmLink})
	}
	if err := query.Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		if purpose != confirmNotice {
			return nil, problem(429, "confirmation-cooldown", "A confirmation email was sent recently. Wait one minute and start again.")
		}
		if err := activate(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err := activate(); err != nil {
		return nil, err
	}
	if err := tx.Where("expires_at < ?", current).Delete(&providerConfirmation{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&providerConfirmation{}).Where("provider = ? AND subject = ? AND used = false", identity.Provider, identity.Subject).Update("used", true).Error; err != nil {
		return nil, err
	}
	token := secret()
	c := providerConfirmation{ID: newID(), Verifier: digest(token), Purpose: purpose, UserID: userID, Provider: identity.Provider, Subject: identity.Subject, Email: to, TransactionID: t.ID, AccountName: accountName, CreatedAt: current, ExpiresAt: expires, Used: purpose == confirmNotice}
	if err := tx.Create(&c).Error; err != nil {
		return nil, err
	}
	label := providerLabel(identity.Provider)
	issuer := strings.TrimRight(s.config.Issuer, "/")
	name, target := "confirm-sign-in", issuer+"/oauth/providers/confirmation?"+url.Values{"token": {token}}.Encode()
	if purpose == confirmNotice {
		name, target = "new-sign-in", issuer+"/oauth/register"
	}
	message := renderMail(s.config, name, identitymail.Data{Provider: label, URL: target})
	return &providerMail{confirmationID: c.ID, to: to, subject: message.Subject, body: message.Text}, nil

}

func (s *Service) requestProviderLink(tx *gorm.DB, t upstreamAuthTransaction, identity providerIdentity) (*providerMail, error) {
	var existing user
	err := tx.Where("email = ?", identity.Email).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.prepareProviderConfirmation(tx, confirmNotice, "", identity.Email, t, identity, "")
	}
	if err != nil {
		return nil, err
	}
	if existing.Status != "active" {
		// Do not reactivate or contact an inactive user. The response is generic.
		return nil, tx.Model(&upstreamAuthTransaction{}).Where("id = ?", t.ID).Updates(map[string]any{"stage": "confirming", "used": true}).Error
	}
	return s.prepareProviderConfirmation(tx, confirmLink, existing.ID, existing.Email, t, identity, "")
}

func (s *Service) providerRegistration(ctx context.Context, t upstreamAuthTransaction, identity providerIdentity, result ProviderResult) (ProviderResult, error) {
	var ticket string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		m, linked, err := linkedMethod(tx, identity)
		if err != nil {
			return err
		}
		if linked {
			// The identity already has a user. Log in instead of creating another one.
			result.Outcome = ProviderExistingUser
			if t.ClientID == "" {
				return nil
			}
			var u user
			if err := tx.Where("id = ? AND status = 'active'", m.UserID).First(&u).Error; err != nil {
				return oauthError("access_denied")
			}
			t.Purpose = purposeLogin
			result.Purpose, result.Outcome = purposeLogin, ProviderAuthenticated
			ticket, err = issueProviderTicket(tx, t, u.ID, identity)
			return err
		}
		current, err := now(tx)
		if err != nil {
			return err
		}
		subject, authenticated := identity.Subject, identity.AuthenticatedAt
		result.Outcome = ProviderAccountName
		return tx.Model(&upstreamAuthTransaction{}).Where("id = ?", t.ID).Updates(map[string]any{"stage": "authenticated", "subject": &subject, "email": identity.Email, "authenticated_at": &authenticated, "expires_at": current.Add(30 * time.Minute)}).Error
	})
	if ticket != "" {
		result.Form.Set("auth_ticket", ticket)
	}
	return result, err
}

func invalidProviderTransaction() error {
	return problem(400, "provider-invalid", "This sign-in attempt is invalid or expired. Start sign-in again.")
}

func pendingRegistration(tx *gorm.DB, cookie string) (upstreamAuthTransaction, error) {
	var t upstreamAuthTransaction
	if len(cookie) != 43 {
		return t, invalidProviderTransaction()
	}
	err := tx.Where("verifier = ? AND purpose = ? AND stage = 'authenticated' AND used = false AND expires_at > clock_timestamp()", digest(cookie), purposeRegister).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && (t.Subject == nil || t.AuthenticatedAt == nil)) {
		return t, invalidProviderTransaction()
	}
	return t, err
}

func (s *Service) PendingProviderRegistration(ctx context.Context, cookie string) (provider string, err error) {
	t, err := pendingRegistration(s.db.WithContext(ctx), cookie)
	return t.Provider, err
}

func validAccountName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return name, invalidFields("Enter an account name of 1 to 200 bytes.", map[string]string{"account_name": "Enter an account name of 1 to 200 bytes."})
	}
	return name, nil
}

func accountNameInUse(tx *gorm.DB, name string) error {
	var count int64
	if err := tx.Model(&e.Account{}).Where("lower(btrim(name)) = lower(?)", name).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		p := problem(409, "account-name-in-use", "This account name is already in use. Choose another name.").(*Problem)
		p.Fields = map[string]string{"account_name": "Choose another account name."}
		return p
	}
	return nil
}

func (s *Service) SubmitProviderAccountName(ctx context.Context, cookie, name string) (ProviderResult, error) {
	result := ProviderResult{Purpose: purposeRegister, Form: url.Values{}}
	name, err := validAccountName(name)
	if err != nil {
		return result, err
	}
	if !s.RegistrationEnabled() {
		return result, forbidden()
	}
	var mail *providerMail
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		t, err := pendingRegistration(tx, cookie)
		if err != nil {
			return err
		}
		result.Provider, result.Form = t.Provider, t.outerForm()
		if err := accountNameInUse(tx, name); err != nil {
			return err
		}
		identity := providerIdentity{Provider: t.Provider, Subject: *t.Subject, Email: t.Email, AuthenticatedAt: *t.AuthenticatedAt}
		if _, linked, err := linkedMethod(tx, identity); err != nil || linked {
			if linked {
				return providerLinkedElsewhere(identity.Provider)
			}
			return err
		}
		var existing user
		userErr := tx.Where("email = ?", identity.Email).First(&existing).Error
		switch {
		case userErr == nil && existing.Status == "active":
			mail, err = s.prepareProviderConfirmation(tx, confirmLink, existing.ID, existing.Email, t, identity, "")
		case userErr == nil:
			err = tx.Model(&upstreamAuthTransaction{}).Where("id = ?", t.ID).Updates(map[string]any{"stage": "confirming", "used": true}).Error
		case errors.Is(userErr, gorm.ErrRecordNotFound):
			mail, err = s.prepareProviderConfirmation(tx, confirmRegister, "", identity.Email, t, identity, name)
		default:
			err = userErr
		}
		return err
	})
	if err != nil {
		return result, err
	}
	result.Outcome = ProviderConfirmationSent
	return result, s.sendProviderMail(ctx, mail)
}

func providerLinkedElsewhere(provider string) error {
	return problem(409, "provider-linked", "This "+providerLabel(provider)+" account is already linked to another user.")
}

type ProviderConfirmationPage struct {
	Provider    string
	Register    bool
	Email       string
	AccountName string
}

func validProviderConfirmation(tx *gorm.DB, token string) (providerConfirmation, error) {
	var c providerConfirmation
	if len(token) != 43 {
		return c, invalidIdentityLink()
	}
	err := tx.Where("verifier = ? AND used = false AND purpose IN ? AND expires_at > clock_timestamp()", digest(token), []string{confirmRegister, confirmLink}).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c, invalidIdentityLink()
	}
	return c, err
}

func (s *Service) ProviderConfirmation(ctx context.Context, token string) (ProviderConfirmationPage, error) {
	c, err := validProviderConfirmation(s.db.WithContext(ctx), token)
	return ProviderConfirmationPage{Provider: c.Provider, Register: c.Purpose == confirmRegister, Email: c.Email, AccountName: c.AccountName}, err
}

func (s *Service) CompleteProviderConfirmation(ctx context.Context, token string) (ProviderResult, error) {
	result := ProviderResult{Purpose: purposeLogin, Form: url.Values{}}
	var ticket string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		c, err := validProviderConfirmation(tx, token)
		if err != nil {
			return err
		}
		used := tx.Model(&providerConfirmation{}).Where("id = ? AND used = false", c.ID).Update("used", true)
		if used.Error != nil {
			return used.Error
		}
		if used.RowsAffected != 1 {
			return invalidIdentityLink()
		}
		result.Provider = c.Provider
		var t upstreamAuthTransaction
		if err := tx.Where("id = ? AND expires_at > clock_timestamp()", c.TransactionID).First(&t).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		authenticated := time.Now().UTC()
		if t.AuthenticatedAt != nil {
			authenticated = *t.AuthenticatedAt
		}
		identity := providerIdentity{Provider: c.Provider, Subject: c.Subject, Email: c.Email, AuthenticatedAt: authenticated}
		if _, linked, err := linkedMethod(tx, identity); err != nil || linked {
			if linked {
				return providerLinkedElsewhere(c.Provider)
			}
			return err
		}
		var u user
		if c.Purpose == confirmRegister {
			if u, err = s.registerProviderUser(ctx, tx, identity, c.AccountName); err != nil {
				return err
			}
			result.Outcome = ProviderRegistered
		} else {
			if err := tx.Where("id = ? AND status = 'active'", c.UserID).First(&u).Error; err != nil || u.Email != c.Email {
				return invalidIdentityLink()
			}
			if err := linkProviderMethod(tx, u.ID, identity); err != nil {
				return err
			}
			result.Outcome = ProviderLinked
		}
		if t.ID == "" {
			return nil
		}
		// Resume the flow that started the provider transaction.
		result.Purpose, result.Form = t.Purpose, t.outerForm()
		switch {
		case t.Purpose == purposeInvitation:
			result.Outcome = ProviderInvitation
			result.InvitationCookie = secret()
			return recordInvitationProvider(tx, t, u.ID, identity, result.InvitationCookie)
		case t.Purpose == purposeRegister && t.ClientID != "":
			t.Purpose, result.Purpose = purposeLogin, purposeLogin
			fallthrough
		case t.Purpose == purposeLogin || t.Purpose == purposeAuthorize || t.Purpose == purposeDevice:
			result.Outcome = ProviderAuthenticated
			ticket, err = issueProviderTicket(tx, t, u.ID, identity)
			return err
		}
		return nil
	})
	if ticket != "" {
		result.Form.Set("auth_ticket", ticket)
	}
	return result, err
}

func linkProviderMethod(tx *gorm.DB, userID string, identity providerIdentity) error {
	if activeMethod(tx, userID, identity.Provider) {
		return problem(409, "provider-method-exists", "Your user already has a linked "+providerLabel(identity.Provider)+" account. Remove it from your profile before you link another one.")
	}
	subject := identity.Subject
	current, err := now(tx)
	if err != nil {
		return err
	}
	return tx.Create(&userSignInMethod{ID: newID(), UserID: userID, Method: identity.Provider, Subject: &subject, Status: "active", CreatedAt: current, Revision: 1}).Error
}

func (s *Service) registerProviderUser(ctx context.Context, tx *gorm.DB, identity providerIdentity, accountName string) (user, error) {
	if !s.RegistrationEnabled() {
		return user{}, forbidden()
	}
	var count int64
	if err := tx.Model(&user{}).Where("email = ?", identity.Email).Count(&count).Error; err != nil {
		return user{}, err
	}
	if count > 0 {
		return user{}, problem(409, "account-exists", "This email already has a user account. Log in and link "+providerLabel(identity.Provider)+" from the login page.")
	}
	if err := resourceLimit(tx, "", "", "accounts"); err != nil {
		return user{}, err
	}
	u := user{ID: newID(), Email: identity.Email, Status: "active", Revision: 1}
	if err := tx.Create(&u).Error; err != nil {
		return u, err
	}
	if err := linkProviderMethod(tx, u.ID, identity); err != nil {
		return u, err
	}
	actor := WithPrincipal(ctx, e.Principal{Type: "user", Scope: e.ScopeAccount, UserID: u.ID})
	account := e.Account{Resource: e.Resource{Name: accountName}}
	// prepare applies the name, uniqueness, and limit rules again under the lock.
	if err := prepare(actor, tx, &account, true); err != nil {
		return u, err
	}
	if err := insert(actor, tx, &account); err != nil {
		return u, err
	}
	return u, afterCreate(actor, tx, &account)
}
