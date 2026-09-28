// Package fakeidp is a local Google and GitHub identity provider for tests and
// local development. It never contacts a real provider. Do not use it in
// production.
//
// Google OpenID Connect is served below /google, the GitHub web flow below
// /github, and the GitHub REST API below /github/api. The authorization
// endpoints approve at once and redirect with a code for the next configured
// identity, so a browser test needs no provider user interface.
package fakeidp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// Identity is the account that the fake provider authenticates next.
type Identity struct {
	Subject       string `json:"subject"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	// Emails replaces the GitHub email list. When it is nil, the list has one
	// primary email with the EmailVerified state.
	Emails []GitHubEmail `json:"emails,omitempty"`
	// Deny makes the authorization endpoint return access_denied.
	Deny bool `json:"deny,omitempty"`
}

// GitHubEmail is one entry of the GitHub /user/emails response.
type GitHubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

type grant struct {
	provider, redirect, challenge, nonce string
	identity                             Identity
	issued                               time.Time
}

// Server is an http.Handler. Set URL before use, or use Start helpers from a
// test server. The exported fields change behavior for the next requests.
type Server struct {
	URL          string
	ClientID     string
	ClientSecret string

	mu     sync.Mutex
	issued []string
	next   map[string]Identity
	codes  map[string]grant
	tokens map[string]Identity
	keys   []*rsa.PrivateKey
	kids   []string
	// Claims changes the Google ID token claims before signing.
	Claims func(map[string]any)
	// Failures maps a request path to the HTTP status to return instead.
	Failures map[string]int
	// SignWith signs Google ID tokens with an unpublished key when true.
	SignWith *rsa.PrivateKey
}

// New returns a fake provider with one signing key.
func New(clientID, clientSecret string) *Server {
	s := &Server{ClientID: clientID, ClientSecret: clientSecret, next: map[string]Identity{}, codes: map[string]grant{}, tokens: map[string]Identity{}, Failures: map[string]int{}}
	s.RotateKey()
	return s
}

// SetNext selects the identity for the next authorizations of provider
// ("google" or "github").
func (s *Server) SetNext(provider string, identity Identity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next[provider] = identity
}

// RotateKey adds a new current signing key. Old keys stay published.
func (s *Server) RotateKey() {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = append([]*rsa.PrivateKey{key}, s.keys...)
	s.kids = append([]string{random()[:12]}, s.kids...)
}

// GoogleIssuer and GitHubWeb, and GitHubAPI are the endpoint overrides for the Exp-Bench provider configuration.
func (s *Server) GoogleIssuer() string { return strings.TrimRight(s.URL, "/") + "/google" }
func (s *Server) GitHubWeb() string    { return strings.TrimRight(s.URL, "/") + "/github" }
func (s *Server) GitHubAPI() string    { return strings.TrimRight(s.URL, "/") + "/github/api" }

func random() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	status := s.Failures[r.URL.Path]
	s.mu.Unlock()
	if status != 0 {
		writeJSON(w, status, map[string]string{"error": "server_error"})
		return
	}
	switch r.URL.Path {
	case "/google/.well-known/openid-configuration":
		issuer := s.GoogleIssuer()
		writeJSON(w, 200, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/auth", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}})
	case "/google/keys":
		s.mu.Lock()
		set := jose.JSONWebKeySet{}
		for i, key := range s.keys {
			set.Keys = append(set.Keys, jose.JSONWebKey{Key: key.Public(), KeyID: s.kids[i], Algorithm: "RS256", Use: "sig"})
		}
		s.mu.Unlock()
		writeJSON(w, 200, set)
	case "/google/auth":
		s.authorize(w, r, "google")
	case "/github/login/oauth/authorize":
		s.authorize(w, r, "github")
	case "/google/token":
		s.token(w, r, "google")
	case "/github/login/oauth/access_token":
		s.token(w, r, "github")
	case "/github/api/user":
		if identity, ok := s.bearer(r); ok {
			writeJSON(w, 200, map[string]any{"id": json.Number(identity.Subject), "login": "user-" + identity.Subject})
			return
		}
		writeJSON(w, 401, map[string]string{"message": "Bad credentials"})
	case "/github/api/user/emails":
		identity, ok := s.bearer(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"message": "Bad credentials"})
			return
		}
		emails := identity.Emails
		if emails == nil {
			emails = []GitHubEmail{{Email: identity.Email, Primary: true, Verified: identity.EmailVerified}}
		}
		writeJSON(w, 200, emails)
	case "/control/next":
		// Local development and browser tests select the next identity here.
		var body struct {
			Provider string   `json:"provider"`
			Identity Identity `json:"identity"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil || (body.Provider != "google" && body.Provider != "github") {
			writeJSON(w, 400, map[string]string{"error": "invalid_request"})
			return
		}
		s.SetNext(body.Provider, body.Identity)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request, provider string) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || q.Get("client_id") != s.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		writeJSON(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	s.mu.Lock()
	identity := s.next[provider]
	out := redirect.Query()
	out.Set("state", q.Get("state"))
	if identity.Deny {
		out.Set("error", "access_denied")
	} else {
		code := random()
		s.codes[code] = grant{provider: provider, redirect: redirect.String(), challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), identity: identity, issued: time.Now()}
		out.Set("code", code)
	}
	s.mu.Unlock()
	redirect.RawQuery = out.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request, provider string) {
	if r.ParseForm() != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	s.mu.Lock()
	g, found := s.codes[r.PostForm.Get("code")]
	// Codes work once, as with real providers.
	delete(s.codes, r.PostForm.Get("code"))
	s.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if id != s.ClientID || secret != s.ClientSecret || !found || g.provider != provider || g.redirect != r.PostForm.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
		return
	}
	access := random()
	s.mu.Lock()
	s.tokens[access] = g.identity
	s.mu.Unlock()
	response := map[string]any{"access_token": access, "token_type": "bearer", "scope": "email"}
	if provider == "google" {
		response["id_token"] = s.idToken(g)
	}
	s.mu.Lock()
	s.issued = append(s.issued, access)
	if token, ok := response["id_token"].(string); ok {
		s.issued = append(s.issued, token)
	}
	s.mu.Unlock()
	writeJSON(w, 200, response)
}

func (s *Server) idToken(g grant) string {
	now := time.Now()
	claims := map[string]any{"iss": s.GoogleIssuer(), "aud": s.ClientID, "sub": g.identity.Subject, "email": g.identity.Email, "email_verified": g.identity.EmailVerified, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": g.nonce}
	s.mu.Lock()
	key, kid := s.keys[0], s.kids[0]
	if s.SignWith != nil {
		key = s.SignWith
	}
	change := s.Claims
	s.mu.Unlock()
	if change != nil {
		change(claims)
	}
	payload, _ := json.Marshal(claims)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", kid).WithType("JWT"))
	if err != nil {
		panic(err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		panic(err)
	}
	token, err := signed.CompactSerialize()
	if err != nil {
		panic(err)
	}
	return token
}

func (s *Server) bearer(r *http.Request) (Identity, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return Identity{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, found := s.tokens[token]
	return identity, found
}

// Issued returns every access token and ID token that the fake issued. Tests
// use it to prove that the service stores no provider credential.
func (s *Server) Issued() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.issued...)
}
