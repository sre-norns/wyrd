// Package client is the REST client of the identity routes httpapi.Mount
// serves: OAuth, the principal, personal profile, sessions, accounts,
// projects, memberships, invitations and machine identities. Every product
// mounts the same routes, so every product's client and CLI use this one.
//
// The transport -- Exchange, Resource and Problem -- is exported so a
// product's own client can be built on it, sharing one request path and one
// set of RequestOptions for its routes and these.
package client

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sre-norns/wyrd/identity/model"
)

// Config is the connection a client makes.
type Config struct {
	HTTPClient *http.Client
	// Token authenticates resource requests. OAuth requests never carry it.
	Token   string
	Timeout time.Duration
}

type Client struct {
	baseURL *url.URL
	config  Config
}

// New returns a client of the server at baseURL. A trailing /v1 is accepted
// and dropped: routes carry their own version.
func New(baseURL string, config Config) (*Client, error) {
	endpoint, err := url.Parse(baseURL)
	if err != nil || endpoint == nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("API endpoint must be an absolute HTTP or HTTPS URL without credentials, query, or fragment")
	}
	endpoint.Path = strings.TrimSuffix(strings.TrimRight(endpoint.Path, "/"), "/v1")
	endpoint.RawPath = ""
	if config.Timeout < 0 {
		return nil, fmt.Errorf("API timeout must not be negative")
	}

	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}

	return &Client{baseURL: endpoint, config: config}, nil
}

var (
	_ model.ServiceConfigService       = (*serviceConfigClient)(nil)
	_ model.OAuthService               = (*oauthClient)(nil)
	_ model.PrincipalService           = (*principalClient)(nil)
	_ model.PersonalProfileService     = (*personalProfileClient)(nil)
	_ model.SignInMethodService        = (*signInMethodClient)(nil)
	_ model.SessionsService            = (*sessionsClient)(nil)
	_ model.AccountsService            = (*accountsClient)(nil)
	_ model.AccountMembershipsService  = (*accountMembershipsClient)(nil)
	_ model.AccountInvitationsService  = (*accountInvitationsClient)(nil)
	_ model.AgentIdentitiesService     = (*agentIdentitiesClient)(nil)
	_ model.AgentIdentityTokensService = (*agentIdentityTokensClient)(nil)
	_ model.AgentAuthorizationsService = (*agentAuthorizationsClient)(nil)
	_ model.ProjectsService            = (*projectsClient)(nil)
	_ model.ProjectMembershipsService  = (*projectMembershipsClient)(nil)
	_ model.DirectoryService           = (*directoryClient)(nil)
)

func (c *Client) ServiceConfig() model.ServiceConfigService { return &serviceConfigClient{c} }
func (c *Client) OAuth() model.OAuthService                 { return &oauthClient{c} }
func (c *Client) Principal() model.PrincipalService         { return &principalClient{c} }
func (c *Client) PersonalProfile() model.PersonalProfileService {
	return &personalProfileClient{c}
}
func (c *Client) SignInMethods() model.SignInMethodService { return &signInMethodClient{c} }
func (c *Client) Sessions() model.SessionsService          { return &sessionsClient{c} }
func (c *Client) Accounts() model.AccountsService          { return &accountsClient{c} }
func (c *Client) AccountMemberships() model.AccountMembershipsService {
	return &accountMembershipsClient{c}
}
func (c *Client) AccountInvitations() model.AccountInvitationsService {
	return &accountInvitationsClient{c}
}
func (c *Client) AgentIdentities() model.AgentIdentitiesService { return &agentIdentitiesClient{c} }
func (c *Client) AgentIdentityTokens() model.AgentIdentityTokensService {
	return &agentIdentityTokensClient{c}
}
func (c *Client) AgentAuthorizations() model.AgentAuthorizationsService {
	return &agentAuthorizationsClient{c}
}
func (c *Client) Projects() model.ProjectsService { return &projectsClient{c} }
func (c *Client) ProjectMemberships() model.ProjectMembershipsService {
	return &projectMembershipsClient{c}
}
func (c *Client) Directory() model.DirectoryService { return &directoryClient{c} }
