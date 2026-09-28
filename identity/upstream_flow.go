package identity

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	purposeLogin      = "login"
	purposeAuthorize  = "authorize"
	purposeDevice     = "device"
	purposeRegister   = "register"
	purposeInvitation = "invitation"
)

const (
	ProviderAuthenticated    = "authenticated"
	ProviderConfirmationSent = "confirmation-sent"
	ProviderAccountName      = "account-name"
	ProviderInvitation       = "invitation"
)

type ProviderResult struct {
	Outcome          string
	Provider         string
	Purpose          string
	Form             url.Values
	InvitationCookie string `json:"-"`
}

func (r ProviderResult) Path() string {
	return providerPurposePath(r.Purpose)
}

func providerPurposePath(purpose string) string {
	switch purpose {
	case purposeAuthorize:
		return "/oauth/authorize"
	case purposeDevice:
		return "/oauth/device"
	case purposeRegister:
		return "/oauth/register"
	case purposeInvitation:
		return "/oauth/invitations/authorize"
	}
	return "/oauth/login"
}

func (s *Service) ProviderEnabled(name string) bool {
	_, ok := s.providers[name]
	return ok
}

func (s *Service) SignInProviders() []string { return enabledProviders(*s.config) }

func providerNonce(cookie string) string { return digest("nonce:" + cookie) }

func providerVerifier(cookie string) string {
	sum := sha256.Sum256([]byte("pkce:" + cookie))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func providerProblem(provider, category string) error {
	label := providerLabel(provider)
	switch category {
	case providerDenied:
		return problem(400, "provider-denied", label+" sign-in was cancelled. Start sign-in again or use another sign-in method.")
	case providerInvalidCallback, providerInvalidIdentity:
		return problem(400, "provider-invalid", "This "+label+" sign-in attempt is invalid or expired. Start sign-in again.")
	case providerEmailUnverified:
		return problem(400, "provider-email-unverified", label+" did not confirm a primary email address for this account. Verify the email address with "+label+", or use another sign-in method.")
	}
	return problem(503, "provider-unavailable", label+" sign-in cannot complete now. Use another sign-in method or try again later.")
}

func (s *Service) providerFailed(ctx context.Context, provider string, err error) error {
	category := providerCategory(err)
	slog.WarnContext(ctx, "Provider sign-in failed", "provider", provider, "category", category)
	s.observeProvider(ctx, provider, category)
	return providerProblem(provider, category)
}

func (s *Service) validOuterAuthorization(form url.Values, webOnly bool) bool {
	client := form.Get("client_id")
	redirects, ok := s.config.Clients[client]
	return ok && (!webOnly || client == s.config.WebClientID) && form.Get("response_type") == "code" && slices.Contains(redirects, form.Get("redirect_uri")) && form.Get("code_challenge_method") == "S256" && len(form.Get("code_challenge")) == 43 && validPKCE(form.Get("code_challenge")) && len(form.Get("state")) <= 256 && len(form.Get("preferred_context")) <= 64
}

func (s *Service) StartProvider(ctx context.Context, provider, purpose string, form url.Values) (cookie, location string, err error) {
	p, ok := s.providers[provider]
	if !ok {
		return "", "", problem(404, "provider-unavailable", "This sign-in method is not available.")
	}
	db := s.db.WithContext(ctx)
	t := upstreamAuthTransaction{ID: newID(), Provider: provider, Purpose: purpose, Stage: "started"}
	current, err := now(db)
	if err != nil {
		return "", "", err
	}
	t.CreatedAt, t.ExpiresAt = current, current.Add(10*time.Minute)
	outer := func() {
		t.ClientID, t.RedirectURI, t.Challenge, t.State, t.PreferredContext = form.Get("client_id"), form.Get("redirect_uri"), form.Get("code_challenge"), form.Get("state"), form.Get("preferred_context")
	}
	switch purpose {
	case purposeLogin, purposeAuthorize:
		if !s.validOuterAuthorization(form, purpose == purposeLogin) {
			return "", "", oauthError("invalid_request")
		}
		outer()
	case purposeDevice:
		code := strings.ToUpper(strings.TrimSpace(form.Get("user_code")))
		var grant oauthGrant
		if len(code) != 12 || db.Where("user_code = ? AND kind = 'device'", digest(code)).First(&grant).Error != nil || grant.Used || grant.Approved || grant.Denied {
			return "", "", oauthError("invalid_request")
		}
		if !grant.ExpiresAt.After(current) {
			return "", "", deviceExpired()
		}
		t.DeviceGrantID, t.UserCode, t.ClientID = grant.ID, code, grant.ClientID
		t.PreferredContext = form.Get("preferred_context")
		if t.PreferredContext == "" {
			t.PreferredContext = grant.PreferredContext
		}
		if grant.ExpiresAt.Before(t.ExpiresAt) {
			t.ExpiresAt = grant.ExpiresAt
		}
	case purposeRegister:
		if !s.RegistrationEnabled() {
			return "", "", forbidden()
		}
		if !s.IdentityMailAvailable() {
			return "", "", problem(503, "mail-unavailable", "Account email is not configured. Contact your service administrator.")
		}
		// Registration can resume a web login. Without outer fields, it ends on a
		// completion page.
		if form.Get("client_id") != "" {
			if !s.validOuterAuthorization(form, true) {
				return "", "", oauthError("invalid_request")
			}
			outer()
		}
	case purposeInvitation:
		if err := s.bindInvitationProvider(ctx, &t, form); err != nil {
			return "", "", err
		}
	default:
		return "", "", invalid("Unsupported sign-in request.")
	}
	cookie, state := secret(), secret()
	t.Verifier, t.StateDigest = digest(cookie), digest(state)
	location, err = p.AuthURL(ctx, state, providerNonce(cookie), providerVerifier(cookie))
	if err != nil {
		return "", "", s.providerFailed(ctx, provider, err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at <= ?", current).Delete(&upstreamAuthTransaction{}).Error; err != nil {
			return err
		}
		return tx.Create(&t).Error
	})
	return cookie, location, err
}

func deviceExpired() error {
	return problem(400, "expired_token", "The device request expired. Run expbctl auth again.")
}

func (t upstreamAuthTransaction) outerForm() url.Values {
	form := url.Values{}
	if t.Purpose == purposeDevice {
		form.Set("user_code", t.UserCode)
	} else if t.ClientID != "" {
		form.Set("client_id", t.ClientID)
		form.Set("redirect_uri", t.RedirectURI)
		form.Set("response_type", "code")
		form.Set("code_challenge_method", "S256")
		form.Set("code_challenge", t.Challenge)
		form.Set("state", t.State)
	}
	if t.PreferredContext != "" {
		form.Set("preferred_context", t.PreferredContext)
	}
	return form
}

func consumeProviderTransaction(db *gorm.DB, provider, cookie, state string) (upstreamAuthTransaction, error) {
	var t upstreamAuthTransaction
	if len(cookie) != 43 {
		return t, errors.New("invalid transaction")
	}
	result := db.Model(&t).Clauses(clause.Returning{}).Where("verifier = ? AND stage = 'started' AND used = false AND expires_at > clock_timestamp()", digest(cookie)).Update("stage", "callback")
	if result.Error != nil {
		return t, result.Error
	}
	if result.RowsAffected != 1 || t.Provider != provider || subtle.ConstantTimeCompare([]byte(t.StateDigest), []byte(digest(state))) != 1 {
		return t, errors.New("invalid transaction")
	}
	return t, nil
}

func (s *Service) FinishProvider(ctx context.Context, provider, cookie string, query url.Values) (ProviderResult, error) {
	result := ProviderResult{Provider: provider, Purpose: purposeLogin}
	p, ok := s.providers[provider]
	if !ok {
		return result, problem(404, "provider-unavailable", "This sign-in method is not available.")
	}
	t, err := consumeProviderTransaction(s.db.WithContext(ctx), provider, cookie, query.Get("state"))
	if err != nil {
		if t.ID != "" {
			result.Purpose, result.Form = t.Purpose, t.outerForm()
		}
		return result, s.providerFailed(ctx, provider, providerFailure(provider, providerInvalidCallback))
	}
	result.Purpose, result.Form = t.Purpose, t.outerForm()
	if code := query.Get("error"); code != "" {
		category := providerInvalidCallback
		if code == "access_denied" {
			category = providerDenied
		}
		return result, s.providerFailed(ctx, provider, providerFailure(provider, category))
	}
	code := query.Get("code")
	if code == "" || len(code) > 2048 {
		return result, s.providerFailed(ctx, provider, providerFailure(provider, providerInvalidCallback))
	}
	identity, err := p.Exchange(ctx, code, providerNonce(cookie), providerVerifier(cookie))
	if err != nil {
		return result, s.providerFailed(ctx, provider, err)
	}
	switch t.Purpose {
	case purposeLogin, purposeAuthorize, purposeDevice:
		result, err = s.providerLogin(ctx, t, identity, result)
	case purposeRegister:
		result, err = s.providerRegistration(ctx, t, identity, result)
	case purposeInvitation:
		result, err = s.providerInvitation(ctx, t, identity, result)
	default:
		return result, s.providerFailed(ctx, provider, providerFailure(provider, providerInvalidCallback))
	}
	if err == nil {
		s.observeProvider(ctx, provider, result.Outcome)
	} else {
		// The provider worked. The Exp-Bench rules rejected the identity.
		s.observeProvider(ctx, provider, "rejected")
	}
	return result, err
}

func linkedMethod(db *gorm.DB, identity providerIdentity) (userSignInMethod, bool, error) {
	var m userSignInMethod
	err := db.Where("method = ? AND subject = ? AND status = 'active'", identity.Provider, identity.Subject).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return m, false, nil
	}
	return m, err == nil, err
}

func issueProviderTicket(tx *gorm.DB, t upstreamAuthTransaction, userID string, identity providerIdentity) (string, error) {
	current, err := now(tx)
	if err != nil {
		return "", err
	}
	ticket := secret()
	grant := oauthGrant{ID: newID(), Kind: "authentication", ClientID: t.ClientID, RedirectURI: t.RedirectURI, Challenge: t.Challenge, DeviceGrantID: t.DeviceGrantID, Verifier: digest(ticket), UserCode: newID(), UserID: userID, AuthenticatedAt: identity.AuthenticatedAt, AuthenticationMethod: identity.Provider, ExpiresAt: current.Add(10 * time.Minute)}
	return ticket, tx.Create(&grant).Error
}

func (s *Service) providerLogin(ctx context.Context, t upstreamAuthTransaction, identity providerIdentity, result ProviderResult) (ProviderResult, error) {
	var ticket string
	var mail *providerMail
	outcome := ProviderAuthenticated
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		m, linked, err := linkedMethod(tx, identity)
		if err != nil {
			return err
		}
		if !linked {
			outcome = ProviderConfirmationSent
			mail, err = s.requestProviderLink(tx, t, identity)
			return err
		}
		var u user
		if err := tx.Where("id = ? AND status = 'active'", m.UserID).First(&u).Error; err != nil {
			return oauthError("access_denied")
		}
		ticket, err = issueProviderTicket(tx, t, u.ID, identity)
		return err
	})
	if err != nil {
		return result, err
	}
	result.Outcome = outcome
	if ticket != "" {
		result.Form.Set("auth_ticket", ticket)
	}
	return result, s.sendProviderMail(ctx, mail)
}
