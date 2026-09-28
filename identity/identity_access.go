package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"net/url"
	"strings"
	"time"

	identitymail "github.com/sre-norns/wyrd/identity/mail"

	e "github.com/sre-norns/wyrd/identity/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type identityLink struct {
	ID          string `gorm:"primaryKey"`
	Kind        string
	Email       string `gorm:"index"`
	AccountName string
	Verifier    string `gorm:"uniqueIndex"`
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Used        bool
}

type IdentityLinkDetails struct{ Email, AccountName string }

func (s *Service) RegistrationEnabled() bool { return s.config.AccountProvisioning == "self-service" }

func (s *Service) IdentityMailAvailable() bool { return s.config.SendIdentityMail != nil }

func (s *Service) RequestIdentityLink(ctx context.Context, kind, email, name string) error {
	if kind != "register" && kind != "reset" {
		return invalid("Unsupported account access request.")
	}
	if kind == "register" && !s.RegistrationEnabled() {
		return forbidden()
	}
	if !s.IdentityMailAvailable() {
		return problem(503, "mail-unavailable", "Account email is not configured. Contact your service administrator.")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return invalid("Enter a valid email address.")
	}
	name = strings.TrimSpace(name)
	if kind == "register" && (name == "" || len(name) > 200) {
		return invalid("Enter an account name of 1 to 200 bytes.")
	}
	token := secret()
	link := identityLink{ID: newID(), Kind: kind, Email: email, AccountName: name, Verifier: digest(token)}
	send := false
	existingUser := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		t, err := now(tx)
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&identityLink{}).Where("email = ? AND kind = ? AND created_at > ?", email, kind, t.Add(-time.Minute)).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		if err := tx.Where("expires_at < ?", t).Delete(&identityLink{}).Error; err != nil {
			return err
		}
		if kind == "register" {
			if err := accountNameInUse(tx, name); err != nil {
				return err
			}
			if err := tx.Model(&user{}).Where("email = ?", email).Count(&count).Error; err != nil {
				return err
			}
			existingUser = count > 0
			// Retain a consumed record for the cooldown, without a usable verification link.
			link.Used = existingUser
		}
		link.CreatedAt, link.ExpiresAt = t, t.Add(30*time.Minute)
		send = true
		return tx.Create(&link).Error
	})
	if err != nil || !send {
		return err
	}
	path, subject, instruction := "verify-email", "Complete your "+s.config.ProductName+" account", "Verify your email address and choose a password to create your account."
	if kind == "reset" {
		path, subject, instruction = "reset-password", "Reset your "+s.config.ProductName+" password", "Choose a new password for your account."
	}
	target := strings.TrimRight(s.config.Issuer, "/") + "/oauth/" + path + "?" + url.Values{"token": {token}}.Encode()
	message := renderMail(s.config, "identity-link", identitymail.Data{Subject: subject, Instruction: instruction, URL: target})
	if existingUser {
		message = renderMail(s.config, "existing-user", identitymail.Data{URL: strings.TrimRight(s.config.Issuer, "/") + "/sign-in"})
	}
	body := message.Text
	subject = message.Subject

	if err := s.config.SendIdentityMail(ctx, email, subject, body); err != nil {
		slog.ErrorContext(ctx, "Failed to send account email", "err", err)
		// Delete the undelivered token so an immediate retry is possible. Do not expose mail errors.
		_ = s.db.WithContext(ctx).Where("id = ?", link.ID).Delete(&identityLink{}).Error
		return problem(503, "mail-unavailable", "The service cannot send account email. Try again later or contact your service administrator.")
	}
	return nil
}

func validIdentityLink(db *gorm.DB, kind, token string) (identityLink, error) {
	var link identityLink
	if (kind != "register" && kind != "reset") || len(token) != 43 {
		return link, invalidIdentityLink()
	}
	if err := db.Where("kind = ? AND verifier = ? AND used = false AND expires_at > clock_timestamp()", kind, digest(token)).First(&link).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return link, invalidIdentityLink()
		}
		return link, err
	}
	return link, nil
}

func invalidIdentityLink() error {
	return problem(400, "invalid-link", "This link is invalid, expired, or already used. Request a new link.")
}

func (s *Service) IdentityLink(ctx context.Context, kind, token string) (IdentityLinkDetails, error) {
	link, err := validIdentityLink(s.db.WithContext(ctx), kind, token)
	return IdentityLinkDetails{Email: link.Email, AccountName: link.AccountName}, err
}

func (s *Service) CompleteIdentityLink(ctx context.Context, kind, token, password string) error {
	if len(password) < 12 || len(password) > 72 {
		return invalid("Use a password of 12 to 72 bytes.")
	}
	if _, err := s.IdentityLink(ctx, kind, token); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		link, err := validIdentityLink(tx, kind, token)
		if err != nil {
			return err
		}
		var u user
		userErr := tx.Where("email = ?", link.Email).First(&u).Error
		if userErr != nil && !errors.Is(userErr, gorm.ErrRecordNotFound) {
			return userErr
		}
		if kind == "register" {
			if !s.RegistrationEnabled() {
				return forbidden()
			}
			if userErr == nil {
				return problem(409, "account-exists", "This email already has a user account. Log in or reset your password.")
			}
			if err := resourceLimit(tx, "", "", "accounts"); err != nil {
				return err
			}
			u = user{ID: newID(), Email: link.Email, Password: hash, Status: "active"}
			if err := tx.Create(&u).Error; err != nil {
				return err
			}
			if err := activateEmailMethod(tx, u.ID); err != nil {
				return err
			}
			actor := WithPrincipal(ctx, e.Principal{Type: "user", Scope: e.ScopeAccount, UserID: u.ID})
			account := e.Account{Resource: e.Resource{Name: link.AccountName}}
			// prepare rechecks the name rules and uniqueness under the lock.
			if err := prepare(actor, tx, &account, true); err != nil {
				return err
			}
			if err := insert(actor, tx, &account); err != nil {
				return err
			}
			if err := afterCreate(actor, tx, &account); err != nil {
				return err
			}
		} else {
			if userErr != nil || u.Status != "active" {
				return invalidIdentityLink()
			}
			if err := tx.Model(&u).Update("password", hash).Error; err != nil {
				return err
			}
			// A provider-only user establishes the email method here. Provider
			// methods and memberships stay unchanged.
			if err := activateEmailMethod(tx, u.ID); err != nil {
				return err
			}
			if err := tx.Model(&e.Session{}).Where("user_id = ? AND status = 'active'", u.ID).Updates(map[string]any{"status": "revoked", "revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC()}).Error; err != nil {
				return err
			}
			if err := tx.Where("user_id = ?", u.ID).Delete(&oauthGrant{}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&identityLink{}).Where("email = ?", link.Email).Update("used", true).Error
	})
}

func (s *Service) AuthenticationOriginAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	issuer, err := url.Parse(s.config.Issuer)
	return err == nil && origin == issuer.Scheme+"://"+issuer.Host
}
