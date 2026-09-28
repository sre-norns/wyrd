package identity

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	e "github.com/sre-norns/wyrd/identity/model"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"
)

type ProviderConfig struct {
	ClientID     string
	ClientSecret string `json:"-"`
	// IssuerURL overrides the Google OpenID Connect issuer.
	IssuerURL string
	// WebURL and APIURL override the GitHub web and REST API base URLs.
	WebURL string
	APIURL string
}

var providerNames = []string{e.SignInGoogle, e.SignInGitHub, "oidc"}

func providerLabel(name string) string {
	if name == "oidc" {
		return "OpenID Connect"
	}
	if name == e.SignInGitHub {
		return "GitHub"
	}
	return "Google"
}

func providerCallback(issuer, name string) string {
	return strings.TrimRight(issuer, "/") + "/oauth/providers/" + name + "/callback"
}

func validProviders(c Config) (map[string]ProviderConfig, error) {
	enabled := map[string]ProviderConfig{}
	for name, p := range c.Providers {
		if !slices.Contains(providerNames, name) {
			return nil, invalid("Unsupported sign-in provider.")
		}
		label := providerLabel(name)
		if p.ClientID == "" && p.ClientSecret == "" {
			continue
		}
		if p.ClientID == "" || p.ClientSecret == "" {
			return nil, invalid(label + " sign-in requires both a client ID and a client secret.")
		}
		for _, value := range []string{p.ClientID, p.ClientSecret} {
			if len(value) > 512 || strings.TrimSpace(value) != value || strings.ContainsAny(value, " \t\r\n") {
				return nil, invalid(label + " sign-in credentials are invalid.")
			}
		}
		if name == "oidc" {
			u, err := url.Parse(p.IssuerURL)
			if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(c.Development && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
				return nil, invalid("OIDC requires an HTTPS issuer URL; local HTTP is available in development.")
			}
			if p.WebURL != "" || p.APIURL != "" {
				return nil, invalid("OIDC endpoints are discovered from the issuer.")
			}
		}
		for _, override := range []string{p.IssuerURL, p.WebURL, p.APIURL} {
			if name == "oidc" {
				continue
			}
			if override == "" {
				continue
			}
			if !c.Development {
				return nil, invalid(label + " endpoint overrides are available only in development.")
			}
			u, err := url.Parse(override)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
				return nil, invalid(label + " endpoint override is invalid.")
			}
		}
		enabled[name] = p
	}
	if len(enabled) > 0 {
		issuer, err := url.Parse(c.Issuer)
		if err != nil || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
			return nil, invalid("The issuer cannot produce a valid provider callback URL.")
		}
		local := issuer.Hostname() == "localhost" || issuer.Hostname() == "127.0.0.1"
		if issuer.Scheme != "https" && !c.Development && !local {
			return nil, invalid("Provider sign-in requires an HTTPS issuer outside local development.")
		}
	}
	return enabled, nil
}

type providerIdentity struct {
	Provider        string
	Subject         string
	Email           string
	AuthenticatedAt time.Time
}

type upstreamProvider interface {
	AuthURL(ctx context.Context, state, nonce, verifier string) (string, error)
	Exchange(ctx context.Context, code, nonce, verifier string) (providerIdentity, error)
}

const (
	providerDenied          = "denied"
	providerInvalidCallback = "invalid_callback"
	providerExchangeFailed  = "exchange_failed"
	providerInvalidIdentity = "invalid_identity"
	providerEmailUnverified = "email_unverified"
	providerUnavailable     = "unavailable"
)

type providerError struct {
	Provider string
	Category string
}

func (p *providerError) Error() string {
	return p.Provider + " sign-in failed: " + p.Category
}

func providerFailure(provider, category string) error {
	return &providerError{Provider: provider, Category: category}
}

func providerCategory(err error) string {
	var p *providerError
	if errors.As(err, &p) {
		return p.Category
	}
	return providerUnavailable
}

const providerResponseLimit = 1 << 20

var errProviderResponseTooLarge = errors.New("provider response exceeds the size limit")

type providerTransport struct {
	provider string
	base     http.RoundTripper
}

func (t providerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx, span := otel.Tracer("wyrd/identity/providers").Start(r.Context(), "provider "+r.Method, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("identity.provider", t.provider), attribute.String("http.request.method", r.Method), attribute.String("server.address", r.URL.Hostname())))
	defer span.End()
	response, err := t.base.RoundTrip(r.WithContext(ctx))
	if err != nil {
		span.SetStatus(codes.Error, "request failed")
		return nil, errors.New("provider request failed")
	}
	span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
	if response.StatusCode >= 500 {
		span.SetStatus(codes.Error, "provider error")
	}
	if response.ContentLength > providerResponseLimit {
		response.Body.Close()
		return nil, errProviderResponseTooLarge
	}
	response.Body = &cappedBody{ReadCloser: response.Body, remaining: providerResponseLimit}
	return response, nil
}

type cappedBody struct {
	io.ReadCloser
	remaining int64
}

func (c *cappedBody) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		// Report an oversized body instead of a silently truncated one.
		var probe [1]byte
		if n, _ := c.ReadCloser.Read(probe[:]); n > 0 {
			return 0, errProviderResponseTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.ReadCloser.Read(p)
	c.remaining -= int64(n)
	return n, err
}

func providerHTTPClient(provider string, base http.RoundTripper) *http.Client {
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: providerTransport{provider: provider, base: base}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func newProviders(c Config, enabled map[string]ProviderConfig) map[string]upstreamProvider {
	providers := map[string]upstreamProvider{}
	for name, p := range enabled {
		client := providerHTTPClient(name, c.ProviderTransport)
		callback := providerCallback(c.Issuer, name)
		if name == e.SignInGoogle || name == "oidc" {
			providers[name] = &googleProvider{name: name, config: p, client: client, callback: callback}
		} else {
			providers[name] = &githubProvider{config: p, client: client, callback: callback}
		}
	}
	return providers
}

func validProviderEmail(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	return value, err == nil && address.Address == value && len(value) <= 254
}

type googleProvider struct {
	name     string
	config   ProviderConfig
	client   *http.Client
	callback string
	mu       sync.Mutex
	provider *oidc.Provider
}

func (g *googleProvider) discover(ctx context.Context) (*oidc.Provider, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.provider != nil {
		return g.provider, nil
	}
	issuer := g.config.IssuerURL
	if issuer == "" {
		issuer = "https://accounts.google.com"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, g.client), issuer)
	if err != nil {
		return nil, providerFailure(g.providerName(), providerUnavailable)
	}
	g.provider = p
	return p, nil
}

func (g *googleProvider) oauth(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{ClientID: g.config.ClientID, ClientSecret: g.config.ClientSecret, Endpoint: p.Endpoint(), RedirectURL: g.callback, Scopes: []string{oidc.ScopeOpenID, "email"}}
}

func (g *googleProvider) AuthURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	p, err := g.discover(ctx)
	if err != nil {
		return "", err
	}
	return g.oauth(p).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "select_account")), nil
}

func (g *googleProvider) Exchange(ctx context.Context, code, nonce, verifier string) (providerIdentity, error) {
	p, err := g.discover(ctx)
	if err != nil {
		return providerIdentity{}, err
	}
	token, err := g.oauth(p).Exchange(context.WithValue(ctx, oauth2.HTTPClient, g.client), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return providerIdentity{}, providerFailure(g.providerName(), providerExchangeFailed)
	}
	raw, _ := token.Extra("id_token").(string)
	if raw == "" {
		return providerIdentity{}, providerFailure(g.providerName(), providerInvalidIdentity)
	}
	idToken, err := p.VerifierContext(oidc.ClientContext(ctx, g.client), &oidc.Config{ClientID: g.config.ClientID}).Verify(oidc.ClientContext(ctx, g.client), raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return providerIdentity{}, providerFailure(g.providerName(), providerInvalidIdentity)
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil || idToken.Subject == "" || len(idToken.Subject) > 255 {
		return providerIdentity{}, providerFailure(g.providerName(), providerInvalidIdentity)
	}
	email, ok := validProviderEmail(claims.Email)
	if !ok || !claims.EmailVerified {
		return providerIdentity{}, providerFailure(g.providerName(), providerEmailUnverified)
	}
	return providerIdentity{Provider: g.providerName(), Subject: idToken.Subject, Email: email, AuthenticatedAt: idToken.IssuedAt.UTC()}, nil
}

type githubProvider struct {
	config   ProviderConfig
	client   *http.Client
	callback string
}

func (g *githubProvider) oauth() *oauth2.Config {
	web := strings.TrimRight(g.config.WebURL, "/")
	if web == "" {
		web = "https://github.com"
	}
	return &oauth2.Config{ClientID: g.config.ClientID, ClientSecret: g.config.ClientSecret, Endpoint: oauth2.Endpoint{AuthURL: web + "/login/oauth/authorize", TokenURL: web + "/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams}, RedirectURL: g.callback, Scopes: []string{"user:email"}}
}

func (g *githubProvider) AuthURL(_ context.Context, state, _, verifier string) (string, error) {
	return g.oauth().AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("allow_signup", "false")), nil
}

func (g *githubProvider) api(ctx context.Context, token, path string, v any) error {
	base := strings.TrimRight(g.config.APIURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Accept", "application/vnd.github+json")
	r.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := g.client.Do(r)
	if err != nil {
		return providerFailure(e.SignInGitHub, providerUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return providerFailure(e.SignInGitHub, providerUnavailable)
	}
	if err := json.NewDecoder(response.Body).Decode(v); err != nil {
		return providerFailure(e.SignInGitHub, providerInvalidIdentity)
	}
	return nil
}

func (g *githubProvider) Exchange(ctx context.Context, code, _, verifier string) (providerIdentity, error) {
	token, err := g.oauth().Exchange(context.WithValue(ctx, oauth2.HTTPClient, g.client), code, oauth2.VerifierOption(verifier))
	if err != nil || token.AccessToken == "" {
		return providerIdentity{}, providerFailure(e.SignInGitHub, providerExchangeFailed)
	}
	var current struct {
		ID int64 `json:"id"`
	}
	if err := g.api(ctx, token.AccessToken, "/user", &current); err != nil {
		return providerIdentity{}, err
	}
	if current.ID <= 0 {
		return providerIdentity{}, providerFailure(e.SignInGitHub, providerInvalidIdentity)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := g.api(ctx, token.AccessToken, "/user/emails", &emails); err != nil {
		return providerIdentity{}, err
	}
	for _, candidate := range emails {
		if candidate.Primary && candidate.Verified {
			if email, ok := validProviderEmail(candidate.Email); ok {
				return providerIdentity{Provider: e.SignInGitHub, Subject: strconv.FormatInt(current.ID, 10), Email: email, AuthenticatedAt: time.Now().UTC()}, nil
			}
		}
	}
	return providerIdentity{}, providerFailure(e.SignInGitHub, providerEmailUnverified)
}

func (g *googleProvider) providerName() string {
	if g.name == "" {
		return e.SignInGoogle
	}
	return g.name
}
