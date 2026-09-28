package identity

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type invitationContinuation struct {
	ID           string      `gorm:"primaryKey"`
	Verifier     string      `gorm:"uniqueIndex"`
	AccountID    e.AccountID `gorm:"index"`
	InvitationID string      `gorm:"index"`
	Generation   int64
	ExpiresAt    time.Time
	ClientID     string
	RedirectURI  string
	Challenge    string
	State        string
	Used         bool
	// A provider authentication for this continuation waits for an explicit
	// join. ProviderUserID names an existing linked user; ProviderSubject
	// names a new identity for a new user.
	ProviderMethod          string
	ProviderSubject         *string
	ProviderUserID          string
	ProviderAuthenticatedAt *time.Time
}

type InvitationPage struct {
	Email         string
	AccountName   string
	Role          string
	Inviter       string
	ExpiresAt     time.Time
	ExistingUser  bool
	AlreadyMember bool
	CSRF          string
	// ProviderMethod is the provider that authenticated the recipient for
	// this continuation, if any.
	ProviderMethod string
	BrowserAuthorization
}

func (s *Service) StartInvitation(ctx context.Context, id, token string) (cookie string, err error) {
	if len(token) != 43 {
		return "", invalidIdentityLink()
	}
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		i, err := load[e.AccountInvitation](tx, id)
		if err != nil {
			return invalidIdentityLink()
		}
		var count int64
		if err = tx.Model(&credential{}).Where("owner_id = ? AND kind = 'invitation' AND verifier = ? AND used = false", id, digest(token)).Count(&count).Error; err != nil {
			return err
		}
		t, err := now(tx)
		if err != nil {
			return err
		}
		if count != 1 || !slices.Contains([]string{"pending", "accepted"}, i.Status) || !i.ExpiresAt.After(t) {
			return invalidIdentityLink()
		}
		cookie = secret()
		expiry := t.Add(10 * time.Minute)
		if i.ExpiresAt.Before(expiry) {
			expiry = i.ExpiresAt
		}
		flow := invitationContinuation{ID: newID(), Verifier: digest(cookie), AccountID: i.AccountID, InvitationID: i.ID, Generation: i.Generation, ExpiresAt: expiry}
		if err = tx.Where("expires_at <= ?", t).Delete(&invitationContinuation{}).Error; err != nil {
			return err
		}
		return tx.Create(&flow).Error
	})
	return
}

func invitationFlow(tx *gorm.DB, cookie string) (flow invitationContinuation, i e.AccountInvitation, err error) {
	if len(cookie) != 43 {
		return flow, i, invalidIdentityLink()
	}
	err = tx.Where("verifier = ? AND used = false AND expires_at > clock_timestamp()", digest(cookie)).First(&flow).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return flow, i, invalidIdentityLink()
	}
	if err != nil {
		return
	}
	i, err = load[e.AccountInvitation](tx, flow.InvitationID)
	if err != nil {
		return flow, i, invalidIdentityLink()
	}
	if i.Generation != flow.Generation || !slices.Contains([]string{"pending", "accepted"}, i.Status) || !i.ExpiresAt.After(time.Now()) {
		return flow, i, invalidIdentityLink()
	}
	account, err := load[e.Account](tx, string(i.AccountID))
	if err != nil {
		return flow, i, err
	}
	if account.Status != "active" {
		return flow, i, problem(409, "account-inactive", "This account is unavailable. Contact its administrator.")
	}
	return flow, i, nil
}

func (s *Service) ReviewInvitation(ctx context.Context, cookie string, form url.Values) (page InvitationPage, err error) {
	if form.Get("client_id") != s.config.WebClientID || !slices.Contains(s.config.Clients[s.config.WebClientID], form.Get("redirect_uri")) || form.Get("response_type") != "code" || form.Get("code_challenge_method") != "S256" || len(form.Get("code_challenge")) != 43 || !validPKCE(form.Get("code_challenge")) || form.Get("state") == "" || len(form.Get("state")) > 256 {
		return page, oauthError("invalid_request")
	}
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		flow, i, err := invitationFlow(tx, cookie)
		if err != nil {
			return err
		}
		if flow.ClientID == "" {
			flow.ClientID, flow.RedirectURI, flow.Challenge, flow.State = form.Get("client_id"), form.Get("redirect_uri"), form.Get("code_challenge"), form.Get("state")
			if err = tx.Save(&flow).Error; err != nil {
				return err
			}
		} else if flow.ClientID != form.Get("client_id") || flow.RedirectURI != form.Get("redirect_uri") || flow.Challenge != form.Get("code_challenge") || flow.State != form.Get("state") {
			return invalidIdentityLink()
		}
		u, m, err := invitationRecipient(tx, i)
		if err != nil {
			return err
		}
		a, err := load[e.Account](tx, string(i.AccountID))
		if err != nil {
			return err
		}
		page = InvitationPage{Email: i.Email, AccountName: a.Name, Role: i.Role, Inviter: "Account administrator", ExpiresAt: i.ExpiresAt, ExistingUser: u.ID != "", AlreadyMember: m.ID != "", CSRF: digest("invitation-csrf:" + cookie), ProviderMethod: flow.ProviderMethod, BrowserAuthorization: BrowserAuthorization{ClientID: flow.ClientID, RedirectURI: flow.RedirectURI, Challenge: flow.Challenge, State: flow.State, Providers: enabledProviders(*s.config)}}
		if m.ID != "" {
			page.Role = m.Role
		}
		if i.Actor.Scope == e.ScopeSystem {
			page.Inviter = "Service administrator"
		} else if inviter, err := load[user](tx, i.Actor.UserID); err == nil {
			page.Inviter = inviter.Email
		}
		return nil
	})
	return
}

func (s *Service) JoinInvitation(ctx context.Context, cookie string, form url.Values) (redirect AuthorizationRedirect, err error) {
	page, err := s.ReviewInvitation(ctx, cookie, form)
	if err != nil {
		return redirect, err
	}
	if form.Get("decision") != "join" || subtle.ConstantTimeCompare([]byte(form.Get("csrf")), []byte(page.CSRF)) != 1 {
		return redirect, forbidden()
	}
	if !strings.EqualFold(strings.TrimSpace(form.Get("email")), page.Email) {
		return redirect, invalid("Log in with the invited email address.")
	}
	if form.Get("sign_in") == "provider" {
		return s.joinWithProvider(ctx, cookie)
	}
	password := form.Get("password")
	var before user
	if page.ExistingUser {
		if form.Get("new_user") == "true" {
			return redirect, problem(409, "account-exists", "This email now has a user account. Log in with its existing password.")
		}
		if err = s.db.WithContext(ctx).Where("email = ? AND status = 'active'", page.Email).First(&before).Error; err != nil {
			return redirect, oauthError("access_denied")
		}
		if bcrypt.CompareHashAndPassword(before.Password, []byte(password)) != nil {
			return redirect, problem(400, "access_denied", "The email or password is incorrect.")
		}
	} else {
		if form.Get("new_user") != "true" || len(password) < 12 || len(password) > 72 {
			return redirect, invalid("Use a password of 12 to 72 bytes.")
		}
		if password != form.Get("confirm_password") {
			return redirect, invalid("The passwords do not match.")
		}
		before.Password, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return redirect, err
		}
	}
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		flow, i, err := invitationFlow(tx, cookie)
		if err != nil {
			return err
		}
		var u user
		userErr := tx.Where("email = ?", i.Email).First(&u).Error
		if userErr != nil && !errors.Is(userErr, gorm.ErrRecordNotFound) {
			return userErr
		}
		if page.ExistingUser {
			if userErr != nil || u.Status != "active" || u.ID != before.ID || !bytes.Equal(u.Password, before.Password) {
				return oauthError("access_denied")
			}
		} else {
			if userErr == nil {
				return problem(409, "account-exists", "This email now has a user account. Log in with its existing password.")
			}
			u = user{ID: newID(), Email: i.Email, Password: before.Password, Status: "active"}
			if err = tx.Create(&u).Error; err != nil {
				return err
			}
			if err = activateEmailMethod(tx, u.ID); err != nil {
				return err
			}
		}
		if _, err = acceptInvitation(ctx, tx, &i, u); err != nil {
			return err
		}
		t, err := now(tx)
		if err != nil {
			return err
		}
		code := secret()
		grant := oauthGrant{ID: newID(), Kind: "authorization_code", Scope: e.ScopeAccount, ClientID: flow.ClientID, Verifier: digest(code), UserCode: newID(), UserID: u.ID, AccountID: i.AccountID, RedirectURI: flow.RedirectURI, Challenge: flow.Challenge, Approved: true, AuthenticatedAt: t, AuthenticationMethod: "password", ExpiresAt: t.Add(5 * time.Minute)}
		if err = tx.Create(&grant).Error; err != nil {
			return err
		}
		flow.Used = true
		if err = tx.Save(&flow).Error; err != nil {
			return err
		}
		target, _ := url.Parse(flow.RedirectURI)
		q := target.Query()
		q.Set("code", code)
		q.Set("state", flow.State)
		target.RawQuery = q.Encode()
		redirect.URL = target.String()
		return nil
	})
	return
}

func (s *Service) bindInvitationProvider(ctx context.Context, t *upstreamAuthTransaction, form url.Values) error {
	flow, i, err := invitationFlow(s.db.WithContext(ctx), form.Get("invitation_cookie"))
	if err != nil {
		return err
	}
	if flow.ClientID == "" {
		return invalidIdentityLink()
	}
	t.InvitationFlowID, t.AccountID, t.Generation = flow.ID, i.AccountID, flow.Generation
	t.ClientID, t.RedirectURI, t.Challenge, t.State = flow.ClientID, flow.RedirectURI, flow.Challenge, flow.State
	if flow.ExpiresAt.Before(t.ExpiresAt) {
		t.ExpiresAt = flow.ExpiresAt
	}
	return nil
}

func invitationForTransaction(tx *gorm.DB, t upstreamAuthTransaction) (invitationContinuation, e.AccountInvitation, error) {
	var flow invitationContinuation
	if err := tx.Where("id = ? AND used = false AND expires_at > clock_timestamp()", t.InvitationFlowID).First(&flow).Error; err != nil {
		return flow, e.AccountInvitation{}, invalidIdentityLink()
	}
	i, err := load[e.AccountInvitation](tx, flow.InvitationID)
	if err != nil || flow.Generation != t.Generation || i.Generation != t.Generation || i.AccountID != t.AccountID || !slices.Contains([]string{"pending", "accepted"}, i.Status) || !i.ExpiresAt.After(time.Now()) {
		return flow, i, invalidIdentityLink()
	}
	return flow, i, nil
}

func recordInvitationProvider(tx *gorm.DB, t upstreamAuthTransaction, userID string, identity providerIdentity, resumedCookie string) error {
	flow, _, err := invitationForTransaction(tx, t)
	if err != nil {
		return err
	}
	authenticated := identity.AuthenticatedAt
	flow.ProviderMethod, flow.ProviderUserID, flow.ProviderAuthenticatedAt, flow.ProviderSubject = identity.Provider, userID, &authenticated, nil
	if userID == "" {
		subject := identity.Subject
		flow.ProviderSubject = &subject
	}
	if resumedCookie != "" {
		flow.Verifier = digest(resumedCookie)
	}
	return tx.Save(&flow).Error
}

func (s *Service) providerInvitation(ctx context.Context, t upstreamAuthTransaction, identity providerIdentity, result ProviderResult) (ProviderResult, error) {
	var mail *providerMail
	err := mutation(ctx, s.db, func(tx *gorm.DB) error {
		_, i, err := invitationForTransaction(tx, t)
		if err != nil {
			return err
		}
		var invited user
		userErr := tx.Where("email = ?", i.Email).First(&invited).Error
		if userErr != nil && !errors.Is(userErr, gorm.ErrRecordNotFound) {
			return userErr
		}
		m, linked, err := linkedMethod(tx, identity)
		if err != nil {
			return err
		}
		switch {
		case linked && userErr == nil && m.UserID == invited.ID:
			if invited.Status != "active" {
				return oauthError("access_denied")
			}
			result.Outcome = ProviderInvitation
			return recordInvitationProvider(tx, t, invited.ID, identity, "")
		case linked:
			return providerLinkedElsewhere(identity.Provider)
		case userErr == nil:
			if invited.Status != "active" {
				return oauthError("access_denied")
			}
			result.Outcome = ProviderConfirmationSent
			mail, err = s.prepareProviderConfirmation(tx, confirmLink, invited.ID, invited.Email, t, identity, "")
			return err
		}
		result.Outcome = ProviderInvitation
		return recordInvitationProvider(tx, t, "", identity, "")
	})
	if err != nil {
		return result, err
	}
	return result, s.sendProviderMail(ctx, mail)
}

func (s *Service) joinWithProvider(ctx context.Context, cookie string) (redirect AuthorizationRedirect, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		flow, i, err := invitationFlow(tx, cookie)
		if err != nil {
			return err
		}
		if flow.ProviderMethod == "" || flow.ProviderAuthenticatedAt == nil {
			return problem(400, "provider-invalid", "Sign in with a provider before you join the account.")
		}
		var u user
		userErr := tx.Where("email = ?", i.Email).First(&u).Error
		if userErr != nil && !errors.Is(userErr, gorm.ErrRecordNotFound) {
			return userErr
		}
		identity := providerIdentity{Provider: flow.ProviderMethod, AuthenticatedAt: *flow.ProviderAuthenticatedAt}
		if flow.ProviderUserID != "" {
			if userErr != nil || u.ID != flow.ProviderUserID || u.Status != "active" || !activeMethod(tx, u.ID, flow.ProviderMethod) {
				return oauthError("access_denied")
			}
		} else {
			if userErr == nil {
				return problem(409, "account-exists", "This email now has a user account. Log in with one of its sign-in methods.")
			}
			if flow.ProviderSubject == nil {
				return oauthError("access_denied")
			}
			identity.Subject = *flow.ProviderSubject
			if _, linked, err := linkedMethod(tx, identity); err != nil || linked {
				if linked {
					return providerLinkedElsewhere(identity.Provider)
				}
				return err
			}
			u = user{ID: newID(), Email: i.Email, Status: "active", Revision: 1}
			if err = tx.Create(&u).Error; err != nil {
				return err
			}
			if err = linkProviderMethod(tx, u.ID, identity); err != nil {
				return err
			}
		}
		if _, err = acceptInvitation(ctx, tx, &i, u); err != nil {
			return err
		}
		t, err := now(tx)
		if err != nil {
			return err
		}
		if err = markMethodUsed(tx, u.ID, identity.Provider, t); err != nil {
			return err
		}
		code := secret()
		grant := oauthGrant{ID: newID(), Kind: "authorization_code", Scope: e.ScopeAccount, ClientID: flow.ClientID, Verifier: digest(code), UserCode: newID(), UserID: u.ID, AccountID: i.AccountID, RedirectURI: flow.RedirectURI, Challenge: flow.Challenge, Approved: true, AuthenticatedAt: identity.AuthenticatedAt, AuthenticationMethod: identity.Provider, ExpiresAt: t.Add(5 * time.Minute)}
		if err = tx.Create(&grant).Error; err != nil {
			return err
		}
		flow.Used, flow.ProviderSubject = true, nil
		if err = tx.Save(&flow).Error; err != nil {
			return err
		}
		target, _ := url.Parse(flow.RedirectURI)
		q := target.Query()
		q.Set("code", code)
		q.Set("state", flow.State)
		target.RawQuery = q.Encode()
		redirect.URL = target.String()
		return nil
	})
	return
}
