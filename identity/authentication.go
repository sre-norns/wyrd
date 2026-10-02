package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	e "github.com/sre-norns/wyrd/identity/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Config struct {
	ProductName string
	WebClientID string
	MailFooter  string

	ProjectAccessEmailEnabled bool
	InvitationEmailEnabled    bool
	InvitationWorkerInterval  time.Duration
	InvitationMaxAttempts     int
	SendIdentityMail          func(context.Context, string, string, string) error `json:"-"`
	Purge                     e.PurgePolicy
	Development               bool
	Issuer                    string
	Clients                   map[string][]string
	AccountProvisioning       string
	AccessDuration            time.Duration
	RefreshDuration           time.Duration
	// ObserveWorker receives the outcome of each background worker iteration.
	ObserveWorker func(ctx context.Context, worker string, err error, worked bool) `json:"-"`
	// TelemetryStatus returns optional telemetry components for system health.
	// They never change readiness or the overall health status.
	TelemetryStatus func() map[string]string `json:"-"`
	// Providers holds upstream sign-in registrations by name. A provider
	// without credentials is disabled.
	Providers map[string]ProviderConfig `json:"-"`
	// AuthenticationRateLimit is the number of /oauth requests that one
	// address can make in one minute.
	AuthenticationRateLimit int64
	// ObserveProvider receives each provider outcome: a callback result or a
	// failure category. The values come from a fixed vocabulary.
	ObserveProvider func(ctx context.Context, provider, outcome string) `json:"-"`
	// ProviderTransport replaces the base HTTP transport of provider requests.
	ProviderTransport http.RoundTripper `json:"-"`
}

const WebClientID = "identity-web"

func DefaultConfig() Config {
	return Config{ProductName: "SRE-Norns", WebClientID: WebClientID, ProjectAccessEmailEnabled: true, InvitationEmailEnabled: true, InvitationWorkerInterval: 10 * time.Second, InvitationMaxAttempts: 3, Purge: e.PurgePolicy{RecoveryDelaySeconds: 604800, ApprovalMode: "dual", StepUpMaxAgeSeconds: 300, AssuranceMethod: "password", WorkerIntervalSeconds: 30, RetryLimit: 3, MaxResourceCount: 100000}, Issuer: "http://localhost:8080", Clients: map[string][]string{"identityctl": {}, WebClientID: {"http://localhost:8080/oauth/callback"}}, AccountProvisioning: "self-service", AuthenticationRateLimit: 120, AccessDuration: 15 * time.Minute, RefreshDuration: 30 * 24 * time.Hour}
}

func (s *Service) Configure(c Config) error {
	if c.InvitationWorkerInterval < time.Second || c.InvitationWorkerInterval > time.Minute || c.InvitationMaxAttempts < 1 || c.InvitationMaxAttempts > 5 {
		return invalid("Invitation worker interval must be 1 to 60 seconds and attempts must be 1 to 5.")
	}
	u, err := url.Parse(c.Issuer)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		return invalid("Issuer must use HTTPS, except for local development.")
	}
	if c.AccessDuration <= 0 || c.RefreshDuration <= c.AccessDuration || len(c.Clients) == 0 {
		return invalid("Authentication configuration is invalid.")
	}
	if !slices.Contains([]string{"self-service", "system-admin-only"}, c.AccountProvisioning) {
		return invalid("Unsupported provisioning mode.")
	}
	if c.AuthenticationRateLimit < 10 || c.AuthenticationRateLimit > 100000 {
		return invalid("The authentication rate limit must be 10 to 100000 requests per minute.")
	}
	if err := validatePurgeConfig(c); err != nil {
		return err
	}
	enabled, err := validProviders(c)
	if err != nil {
		return err
	}
	c.Providers = enabled
	*s.config = c
	s.providers = newProviders(c, enabled)
	s.providerStatus.Clear()
	return nil
}

type oauthGrant struct {
	Scope                e.SessionScope `gorm:"not null;default:account"`
	PreferredContext     string
	AuthenticatedAt      time.Time
	AuthenticationMethod string
	PollInterval         int
	Denied               bool
	ID                   string `gorm:"primaryKey"`
	Kind                 string
	ClientID             string
	Verifier             string `gorm:"uniqueIndex"`
	UserCode             string `gorm:"uniqueIndex"`
	UserID               string
	AccountID            e.AccountID
	RedirectURI          string
	Challenge            string
	// DeviceGrantID binds an authentication proof to one device request.
	DeviceGrantID string
	ExpiresAt     time.Time
	Approved      bool
	Used          bool
	LastPoll      time.Time
}

type BrowserAuthorization struct {
	Email            string
	ChooseAuthority  bool
	SystemAvailable  bool
	PreferredContext string
	ClientID         string
	RedirectURI      string
	Challenge        string
	State            string
	UserCode         string
	// AuthTicket carries a short-lived proof of the password or provider
	// check so the workspace-selection step does not require it again.
	AuthTicket string
	Accounts   []e.Account
	// AuthenticationMethod is the safe method name behind AuthTicket.
	AuthenticationMethod string
	// Providers lists the enabled upstream sign-in providers.
	Providers []string
}

type AuthorizationRedirect struct{ URL string }

func (s *Service) ProvisionUser(ctx context.Context, email, password string, admin bool) error {
	if len(password) < 12 || len(password) > 72 || !strings.Contains(email, "@") {
		return invalid("Use an email address and a password of 12 to 72 bytes.")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&user{}).Where("email = ?", strings.ToLower(email)).Count(&n).Error; err != nil {
			return err
		}
		if n != 0 {
			return nil
		}
		u := user{ID: newID(), Email: strings.ToLower(email), Password: hash, SystemAdmin: admin, Status: "active", Revision: 1}
		u.LastModifiedBy = publicActor(mutationActor(ctx))
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		if err := activateEmailMethod(tx, u.ID); err != nil {
			return err
		}
		if admin {
			return tx.Create(&e.SystemEntitlement{LastModifiedBy: publicActor(mutationActor(ctx)), UserID: u.ID, Status: "active", Revision: 1, UpdatedAt: time.Now().UTC()}).Error
		}
		p := e.Principal{Type: "user", Scope: e.ScopeAccount, UserID: u.ID}
		ctx = WithPrincipal(ctx, p)
		a := e.Account{}
		a.Name = email
		if err := insert(ctx, tx, &a); err != nil {
			return err
		}
		return afterCreate(ctx, tx, &a)
	})
}

func (s *Service) Authenticate(ctx context.Context, token string) (e.Principal, error) {
	p := e.Principal{}
	if token == "" {
		return p, unauthenticated()
	}
	db := database(ctx, s.db)
	var c credential
	if err := db.Where("verifier = ? AND used = false", digest(token)).First(&c).Error; err != nil {
		return p, unauthenticated()
	}
	t, err := now(db)
	if err != nil {
		return p, err
	}
	switch c.Kind {
	case "access":
		session, err := load[e.Session](db, c.OwnerID)
		if err != nil || session.Status != "active" || !session.ExpiresAt.After(t) {
			return p, unauthenticated()
		}
		u, err := load[user](db, session.UserID)
		if err != nil || u.Status != "active" {
			return p, unauthenticated()
		}
		if err := validateSessionScope(db, u, session.Scope, session.AccountID); err != nil {
			return e.Principal{}, unauthenticated()
		}
		p = e.Principal{Type: "user", Scope: session.Scope, UserID: u.ID, AccountID: session.AccountID, CredentialID: session.ID, SystemAdmin: session.Scope == e.ScopeSystem}
		if session.Scope == e.ScopeSystem {
			return p, nil
		}
	case "agent":
		token, err := load[e.AgentIdentityToken](db, c.OwnerID)
		if err != nil || token.Status != "active" || (token.ExpiresAt != nil && !token.ExpiresAt.After(t)) {
			return p, unauthenticated()
		}
		agent, err := load[e.AgentIdentity](db, string(token.AgentID))
		if err != nil || agent.Status != "active" {
			return p, unauthenticated()
		}
		p = e.Principal{Type: "agent", Scope: e.ScopeAccount, AgentID: e.AgentIdentityID(agent.ID), AccountID: agent.AccountID, CredentialID: token.ID}
	default:
		return p, unauthenticated()
	}
	a, err := load[e.Account](db, string(p.AccountID))
	if err != nil || a.Status != "active" {
		return e.Principal{}, unauthenticated()
	}
	if p.Type == "agent" {
		// This operational timestamp does not change the resource revision. Keep the
		// latest value when concurrent requests finish in a different order.
		if err := db.Exec("UPDATE agent_identities SET last_seen_at = GREATEST(COALESCE(last_seen_at, ?), ?) WHERE id = ?", t, t, p.AgentID).Error; err != nil {
			return e.Principal{}, err
		}
	}
	return p, nil
}

func oauthError(code string) error {
	return problem(400, code, "The OAuth request cannot be completed.")
}

func (o *oauthService) metadata() map[string]any {
	issuer := strings.TrimRight(o.config.Issuer, "/")
	return map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/oauth/authorize", "device_authorization_endpoint": issuer + "/oauth/device_authorization", "token_endpoint": issuer + "/oauth/token", "revocation_endpoint": issuer + "/oauth/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "urn:ietf:params:oauth:grant-type:device_code", "refresh_token", "account_switch"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}}
}

func (o *oauthService) approve(ctx context.Context, form url.Values) (any, error) {
	client := form.Get("client_id")
	device := request(ctx).DeviceFlow || form.Get("user_code") != ""
	redirects, ok := o.config.Clients[client]
	if !device && !ok {
		return nil, oauthError("invalid_client")
	}
	if !device && (form.Get("response_type") != "code" || !slices.Contains(redirects, form.Get("redirect_uri")) || form.Get("code_challenge_method") != "S256" || len(form.Get("code_challenge")) != 43) {
		return nil, oauthError("invalid_request")
	}
	page := BrowserAuthorization{PreferredContext: form.Get("preferred_context"), ClientID: client, RedirectURI: form.Get("redirect_uri"), Challenge: form.Get("code_challenge"), State: form.Get("state"), UserCode: form.Get("user_code"), Providers: enabledProviders(*o.config)}
	var grant oauthGrant
	if device && form.Get("user_code") != "" {
		if err := o.db.Where("user_code = ? AND kind = 'device'", digest(strings.ToUpper(form.Get("user_code")))).First(&grant).Error; err != nil {
			return nil, oauthError("invalid_request")
		}
		page.ClientID = grant.ClientID
		if page.PreferredContext == "" {
			page.PreferredContext = grant.PreferredContext
		}
	}
	var u user
	var authTicket oauthGrant
	usingTicket := false
	if ticket := form.Get("auth_ticket"); ticket != "" {
		// The ticket proves the password was already checked in this login. It lets
		// the workspace-selection step proceed without asking for the password again.
		if err := o.db.Where("verifier = ? AND kind = 'authentication'", digest(ticket)).First(&authTicket).Error; err != nil {
			return nil, oauthError("access_denied")
		}
		if authTicket.Used || !authTicket.ExpiresAt.After(time.Now().UTC()) {
			return nil, oauthError("access_denied")
		}
		// The proof is valid only for the transaction that created it.
		if device && (authTicket.DeviceGrantID == "" || authTicket.DeviceGrantID != grant.ID) {
			return nil, oauthError("access_denied")
		}
		if !device && (authTicket.DeviceGrantID != "" || authTicket.ClientID != page.ClientID || authTicket.RedirectURI != page.RedirectURI || authTicket.Challenge != page.Challenge) {
			return nil, oauthError("access_denied")
		}
		if err := o.db.Where("id = ? AND status = 'active'", authTicket.UserID).First(&u).Error; err != nil {
			return nil, oauthError("access_denied")
		}
		usingTicket = true
	} else {
		if form.Get("password") == "" {
			return page, nil
		}
		if err := o.db.Where("email = ? AND status = 'active'", strings.ToLower(form.Get("email"))).First(&u).Error; err != nil {
			return nil, oauthError("access_denied")
		}
		if bcrypt.CompareHashAndPassword(u.Password, []byte(form.Get("password"))) != nil {
			return nil, oauthError("access_denied")
		}
	}
	var accounts []e.Account
	if err := o.db.Where("status = 'active' AND id IN (SELECT account_id FROM account_memberships WHERE user_id = ? AND status = 'active')", u.ID).Find(&accounts).Error; err != nil {
		return nil, err
	}
	selectedScope := e.ScopeAccount
	chosen := form.Get("account_id")
	selection := form.Get("authority_context")
	if selection == "system" || form.Get("scope") == "system" {
		selectedScope = e.ScopeSystem
	} else if selection != "" {
		chosen = selection
	}
	page.SystemAvailable = entitled(o.db, u.ID)
	if len(accounts) == 0 && !page.SystemAvailable {
		return nil, oauthError("access_denied")
	}
	if selectedScope == e.ScopeSystem && (chosen != "" || !page.SystemAvailable) {
		return nil, oauthError("access_denied")
	}
	if selection == "" && chosen == "" && form.Get("scope") == "" {
		if page.PreferredContext == "system" || (page.SystemAvailable && len(accounts) == 0) {
			if !page.SystemAvailable {
				return nil, oauthError("access_denied")
			}
			selectedScope = e.ScopeSystem
		} else if len(accounts) == 1 && (!page.SystemAvailable || page.PreferredContext == "account") {
			chosen = accounts[0].ID
		}
	}
	// After provider authentication, the person still makes an explicit
	// approval decision. The first-party login page always approves.
	if (selectedScope != e.ScopeSystem && chosen == "") || (usingTicket && form.Get("decision") == "") {
		page.Email = u.Email
		page.ChooseAuthority = true
		page.Accounts = accounts
		page.AuthenticationMethod = "password"
		if usingTicket {
			page.AuthTicket = form.Get("auth_ticket")
			page.AuthenticationMethod = authTicket.AuthenticationMethod
		} else {
			ticket := secret()
			if err := o.db.Create(&oauthGrant{ID: newID(), Kind: "authentication", ClientID: page.ClientID, RedirectURI: page.RedirectURI, Challenge: page.Challenge, DeviceGrantID: grant.ID, Verifier: digest(ticket), UserCode: newID(), UserID: u.ID, AuthenticatedAt: time.Now().UTC(), AuthenticationMethod: "password", ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}).Error; err != nil {
				return nil, err
			}
			page.AuthTicket = ticket
		}
		return page, nil
	}
	if selectedScope == e.ScopeAccount && !slices.ContainsFunc(accounts, func(a e.Account) bool { return a.ID == chosen }) {
		return nil, oauthError("access_denied")
	}
	if !device && form.Get("decision") == "deny" {
		redirect, _ := url.Parse(page.RedirectURI)
		q := redirect.Query()
		q.Set("error", "access_denied")
		if page.State != "" {
			q.Set("state", page.State)
		}
		redirect.RawQuery = q.Encode()
		return AuthorizationRedirect{URL: redirect.String()}, nil
	}
	var output any
	err := o.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		currentUser, err := load[user](tx, u.ID)
		if err != nil {
			return oauthError("access_denied")
		}
		// A ticket keeps the method and time of the original authentication.
		// Account selection does not refresh them.
		method, authenticatedAt := "password", time.Time{}
		if usingTicket {
			method, authenticatedAt = authTicket.AuthenticationMethod, authTicket.AuthenticatedAt
		}
		if method == "password" {
			if len(currentUser.Password) == 0 || !bytes.Equal(currentUser.Password, u.Password) {
				return oauthError("access_denied")
			}
		} else if !activeMethod(tx, currentUser.ID, method) {
			return oauthError("access_denied")
		}
		if err := validateSessionScope(tx, currentUser, selectedScope, e.AccountID(chosen)); err != nil {
			return oauthError("access_denied")
		}
		t, err := now(tx)
		if err != nil {
			return err
		}
		if usingTicket {
			// Consume the one-time ticket so it cannot mint a second authorization.
			result := tx.Model(&oauthGrant{}).Where("id = ? AND used = false", authTicket.ID).Update("used", true)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return oauthError("access_denied")
			}
		}
		if authenticatedAt.IsZero() {
			authenticatedAt = t
		}
		if err := markMethodUsed(tx, currentUser.ID, method, t); err != nil {
			return err
		}
		if device {
			var g oauthGrant
			if err := tx.Where("user_code = ? AND kind = 'device'", digest(strings.ToUpper(form.Get("user_code")))).First(&g).Error; err != nil {
				return oauthError("invalid_grant")
			}
			if !g.ExpiresAt.After(t) || g.Used || g.Approved || g.Denied {
				return oauthError("invalid_grant")
			}
			g.Denied = form.Get("decision") == "deny"
			g.Approved = !g.Denied
			g.UserID = u.ID
			g.AccountID = e.AccountID(chosen)
			g.Scope, g.AuthenticatedAt, g.AuthenticationMethod = selectedScope, authenticatedAt, method
			output = map[string]string{"status": "approved"}
			return tx.Save(&g).Error
		}
		code := secret()
		g := oauthGrant{Scope: selectedScope, AuthenticatedAt: authenticatedAt, AuthenticationMethod: method, ID: newID(), Kind: "authorization_code", ClientID: client, Verifier: digest(code), UserCode: newID(), UserID: u.ID, AccountID: e.AccountID(chosen), RedirectURI: page.RedirectURI, Challenge: page.Challenge, Approved: true, ExpiresAt: t.Add(5 * time.Minute)}
		if err := tx.Create(&g).Error; err != nil {
			return err
		}
		redirect, _ := url.Parse(page.RedirectURI)
		q := redirect.Query()
		q.Set("code", code)
		if page.State != "" {
			q.Set("state", page.State)
		}
		redirect.RawQuery = q.Encode()
		output = AuthorizationRedirect{URL: redirect.String()}
		return nil
	})
	return output, err
}

func issueSession(ctx context.Context, db *gorm.DB, cfg Config, u user, account e.AccountID, client string, grant ...oauthGrant) (map[string]any, error) {
	req := request(ctx)
	s := e.Session{
		Scope:     e.ScopeAccount,
		UserID:    u.ID,
		ClientID:  client,
		Origin:    boundedSessionMetadata(req.Origin, 512),
		IPAddress: boundedSessionMetadata(req.IPAddress, 64),
		UserAgent: boundedSessionMetadata(req.UserAgent, 512),
	}
	if len(grant) > 0 {
		s.Scope, s.AuthenticatedAt, s.AuthenticationMethod = grant[0].Scope, grant[0].AuthenticatedAt, grant[0].AuthenticationMethod
	}
	s.AccountID = account
	s.ExpiresAt = time.Now().UTC().Add(cfg.AccessDuration)
	s.RefreshExpiresAt = time.Now().UTC().Add(cfg.RefreshDuration)
	ctx = WithPrincipal(ctx, e.Principal{Type: "user", Scope: s.Scope, UserID: u.ID, AccountID: account, SystemAdmin: s.Scope == e.ScopeSystem})
	if err := insert(ctx, db, &s); err != nil {
		return nil, err
	}
	return sessionTokens(db, cfg, &s)
}

func boundedSessionMetadata(value string, limit int) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, "\uFFFD"))
	if len(value) <= limit {
		return value
	}
	for len(value) > limit {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

func sessionTokens(db *gorm.DB, cfg Config, s *e.Session) (map[string]any, error) {
	access, refresh := secret(), secret()
	if err := db.Create(&[]credential{{ID: newID(), OwnerID: s.ID, Kind: "access", Verifier: digest(access)}, {ID: newID(), OwnerID: s.ID, Kind: "refresh", Verifier: digest(refresh)}}).Error; err != nil {
		return nil, err
	}
	response := map[string]any{"scope": s.Scope, "access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": int(cfg.AccessDuration.Seconds())}
	if s.Scope == e.ScopeAccount {
		response["account_id"] = s.AccountID
	}
	return response, nil
}

func (o *oauthService) exchange(ctx context.Context, form url.Values) (any, error) {
	client := form.Get("client_id")
	if _, ok := o.config.Clients[client]; !ok {
		return nil, oauthError("invalid_client")
	}
	var output any
	var protocolErr error
	err := o.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		t, err := now(tx)
		if err != nil {
			return err
		}
		grantType := form.Get("grant_type")
		if grantType == "refresh_token" {
			var c credential
			if err := tx.Where("verifier = ? AND kind = 'refresh'", digest(form.Get("refresh_token"))).First(&c).Error; err != nil {
				return oauthError("invalid_grant")
			}
			s, err := load[e.Session](tx, c.OwnerID)
			if err != nil || s.ClientID != client {
				return oauthError("invalid_grant")
			}
			if c.Used {
				s.Status = "revoked"
				protocolErr = oauthError("invalid_grant")
				return save(ctx, tx, &s)
			}
			if s.Status != "active" || !s.RefreshExpiresAt.After(t) {
				return oauthError("invalid_grant")
			}
			u, err := load[user](tx, s.UserID)
			if err != nil {
				return oauthError("invalid_grant")
			}
			if err := validateSessionScope(tx, u, s.Scope, s.AccountID); err != nil {
				return err
			}
			if selected := form.Get("scope"); selected != "" && selected != string(s.Scope) {
				return oauthError("invalid_grant")
			}
			if selected := form.Get("account_id"); selected != "" && selected != string(s.AccountID) {
				return oauthError("invalid_grant")
			}
			if err := tx.Model(&credential{}).Where("owner_id = ? AND kind IN ?", s.ID, []string{"access", "refresh"}).Update("used", true).Error; err != nil {
				return err
			}
			s.ExpiresAt = t.Add(o.config.AccessDuration)
			if err := save(ctx, tx, &s); err != nil {
				return err
			}
			output, err = sessionTokens(tx, *o.config, &s)
			return err
		}
		if grantType == "account_switch" {
			// A signed-in user moves to another of their accounts by reusing the
			// current session, without re-entering credentials. The source session
			// proves prior authentication; membership in the target is verified.
			var c credential
			if err := tx.Where("verifier = ? AND kind = 'refresh'", digest(form.Get("refresh_token"))).First(&c).Error; err != nil {
				return oauthError("invalid_grant")
			}
			s, err := load[e.Session](tx, c.OwnerID)
			if err != nil || s.ClientID != client {
				return oauthError("invalid_grant")
			}
			if c.Used || s.Status != "active" || s.Scope != e.ScopeAccount || !s.RefreshExpiresAt.After(t) {
				return oauthError("invalid_grant")
			}
			u, err := load[user](tx, s.UserID)
			if err != nil {
				return oauthError("invalid_grant")
			}
			target := e.AccountID(form.Get("account_id"))
			if target == "" || target == s.AccountID {
				return oauthError("invalid_request")
			}
			if err := validateSessionScope(tx, u, e.ScopeAccount, target); err != nil {
				return err
			}
			// Retire the source session and its tokens so they cannot be reused.
			if err := tx.Model(&credential{}).Where("owner_id = ? AND kind IN ?", s.ID, []string{"access", "refresh"}).Update("used", true).Error; err != nil {
				return err
			}
			s.Status = "revoked"
			if err := save(ctx, tx, &s); err != nil {
				return err
			}
			output, err = issueSession(ctx, tx, *o.config, u, target, client,
				oauthGrant{Scope: e.ScopeAccount, AuthenticatedAt: s.AuthenticatedAt, AuthenticationMethod: s.AuthenticationMethod})
			return err
		}
		code := form.Get("code")
		kind := "authorization_code"
		if grantType == "urn:ietf:params:oauth:grant-type:device_code" {
			code = form.Get("device_code")
			kind = "device"
		} else if grantType != "authorization_code" {
			return oauthError("unsupported_grant_type")
		}
		var g oauthGrant
		if err := tx.Where("verifier = ? AND kind = ? AND client_id = ?", digest(code), kind, client).First(&g).Error; err != nil {
			return oauthError("invalid_grant")
		}
		if g.Used {
			return oauthError("invalid_grant")
		}
		if !g.ExpiresAt.After(t) {
			if kind == "device" {
				return oauthError("expired_token")
			}
			return oauthError("invalid_grant")
		}
		if g.Denied {
			return oauthError("access_denied")
		}
		if kind == "device" {
			if g.PollInterval < 5 {
				g.PollInterval = 5
			}
			if !g.LastPoll.IsZero() && t.Sub(g.LastPoll) < time.Duration(g.PollInterval)*time.Second {
				g.PollInterval += 5
				g.LastPoll = t
				protocolErr = oauthError("slow_down")
				return tx.Save(&g).Error
			}
			g.LastPoll = t
			if err := tx.Save(&g).Error; err != nil {
				return err
			}
			if !g.Approved {
				protocolErr = oauthError("authorization_pending")
				return nil
			}
		} else {
			verifier := form.Get("code_verifier")
			if !validPKCE(verifier) {
				return oauthError("invalid_grant")
			}
			sum := sha256.Sum256([]byte(verifier))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != g.Challenge || form.Get("redirect_uri") != g.RedirectURI {
				return oauthError("invalid_grant")
			}
		}
		u, err := load[user](tx, g.UserID)
		if err != nil || u.Status != "active" {
			return oauthError("invalid_grant")
		}
		// A provider removed after approval cannot complete the grant.
		if g.AuthenticationMethod != "password" && !activeMethod(tx, u.ID, g.AuthenticationMethod) {
			return oauthError("invalid_grant")
		}
		if err := validateSessionScope(tx, u, g.Scope, g.AccountID); err != nil {
			return err
		}
		g.Used = true
		if err := tx.Save(&g).Error; err != nil {
			return err
		}
		output, err = issueSession(ctx, tx, *o.config, u, g.AccountID, client, g)
		return err
	})
	if err == nil {
		err = protocolErr
	}
	return output, err
}

func (o *oauthService) device(ctx context.Context, form url.Values) (any, error) {
	client := form.Get("client_id")
	if _, ok := o.config.Clients[client]; !ok {
		return nil, oauthError("invalid_client")
	}
	code := secret()
	userCode := strings.ToUpper(secret()[:12])
	g := oauthGrant{PreferredContext: form.Get("preferred_context"), ID: newID(), Kind: "device", PollInterval: 5, ClientID: client, Verifier: digest(code), UserCode: digest(userCode), ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	if err := o.db.WithContext(ctx).Create(&g).Error; err != nil {
		return nil, err
	}
	uri := strings.TrimRight(o.config.Issuer, "/") + "/oauth/device"
	return map[string]any{"device_code": code, "user_code": userCode, "verification_uri": uri, "verification_uri_complete": uri + "?user_code=" + url.QueryEscape(userCode), "expires_in": 600, "interval": 5}, nil
}

func (o *oauthService) revoke(ctx context.Context, form url.Values) (any, error) {
	client := form.Get("client_id")
	if _, ok := o.config.Clients[client]; !ok {
		return nil, oauthError("invalid_client")
	}
	err := o.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		var c credential
		err := tx.Where("verifier = ? AND kind IN ?", digest(form.Get("token")), []string{"access", "refresh"}).First(&c).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		s, err := load[e.Session](tx, c.OwnerID)
		if err != nil {
			return err
		}
		if s.ClientID != client {
			return nil
		}
		s.Status = "revoked"
		return save(ctx, tx, &s)
	})
	return map[string]any{}, err
}

func validPKCE(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r)) {
			return false
		}
	}
	return true
}
