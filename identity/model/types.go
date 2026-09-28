package model

import (
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type SessionID string

type Session struct {
	Resource
	Scope                SessionScope `json:"scope" gorm:"not null;default:account"`
	AuthenticatedAt      time.Time    `json:"authenticated_at"`
	AuthenticationMethod string       `json:"authentication_method"`
	UserID               string       `json:"user_id" gorm:"index"`
	ClientID             string       `json:"client_id"`
	Origin               string       `json:"origin,omitempty"`
	IPAddress            string       `json:"ip_address,omitempty"`
	UserAgent            string       `json:"user_agent,omitempty"`
	ExpiresAt            time.Time    `json:"expires_at"`
	RefreshExpiresAt     time.Time    `json:"refresh_expires_at"`
}

type Principal struct {
	Scope        SessionScope             `json:"scope"`
	AccountRole  string                   `json:"account_role,omitempty"`
	ProjectIDs   []ProjectID              `json:"project_ids,omitempty"`
	RoleGrants   map[ProjectID][]RoleType `json:"role_grants,omitempty"`
	Type         string                   `json:"type"`
	UserID       string                   `json:"user_id,omitempty"`
	AccountID    AccountID                `json:"account_id,omitempty"`
	AgentID      AgentIdentityID          `json:"agent_id,omitempty"`
	CredentialID string                   `json:"credential_id"`
	SystemAdmin  bool                     `json:"system_admin"`
	// SystemEntitled allows system sign-in. It does not grant this session system authority.
	SystemEntitled bool `json:"system_entitled"`
}

type ServiceConfiguration struct {
	APIVersions         []string `json:"api_versions"`
	OAuthDiscovery      string   `json:"oauth_discovery"`
	Grants              []string `json:"grants"`
	AccountProvisioning string   `json:"account_provisioning"`
	// SignInProviders names the enabled upstream sign-in providers only.
	SignInProviders []string `json:"sign_in_providers"`
}

type AccountID string

type Account struct {
	Resource
	LifecycleReason string             `json:"lifecycle_reason,omitempty"`
	LifecycleAt     *time.Time         `json:"lifecycle_at,omitempty"`
	Description     string             `json:"description"`
	OwnerEmail      string             `json:"owner_email,omitempty" gorm:"-"`
	Delivery        string             `json:"delivery,omitempty" gorm:"-"`
	OwnerInvitation *AccountInvitation `json:"owner_invitation,omitempty" gorm:"-"`
}

type AccountMembershipID string

type AccountMembership struct {
	Resource
	UserID string `json:"user_id" gorm:"index"`
	Role   string `json:"role"`
	// Safe human display data. Email is the verified account email and remains
	// the primary label when DisplayName is null. Neither exposes a credential.
	Email       string  `json:"email,omitempty" gorm:"-"`
	DisplayName *string `json:"display_name" gorm:"-"`
}

type AccountInvitationID string

type AccountInvitation struct {
	Resource
	Delivery      string                   `json:"delivery,omitempty" gorm:"not null;default:manual"`
	Generation    int64                    `json:"-"`
	Recovery      bool                     `json:"-"`
	EmailDelivery *InvitationEmailDelivery `json:"email_delivery,omitempty" gorm:"-"`
	Email         string                   `json:"email"`
	Role          string                   `json:"role"`
	ExpiresAt     time.Time                `json:"expires_at"`
	AcceptedBy    string                   `json:"accepted_by,omitempty"`
	MembershipID  AccountMembershipID      `json:"membership_id,omitempty"`
	Token         string                   `json:"token,omitempty" gorm:"-"`
}

type AgentIdentityID string

type AgentIdentity struct {
	Resource
	Description string `json:"description"`
	// LastSeenAt records the most recent authenticated agent request, not process liveness.
	LastSeenAt *time.Time `json:"last_seen_at,omitempty" gorm:"->"`
}

type AgentIdentityTokenID string

type AgentIdentityToken struct {
	Resource
	AgentID   AgentIdentityID `json:"agent_id" gorm:"index"`
	ExpiresAt *time.Time      `json:"expires_at,omitempty"`
	Token     string          `json:"token,omitempty" gorm:"-"`
}

type ProjectID string

type Project struct {
	Resource
	Description      string                  `json:"description"`
	Target           string                  `json:"target"`
	CurrentContextID ProjectContextVersionID `json:"current_context_id,omitempty"`
}

type ProjectMembershipID string

type ProjectMembership struct {
	Resource
	UserID string `json:"user_id" gorm:"index"`
	// Safe human display data. Email is the verified account email and remains
	// the primary label when DisplayName is null. Neither exposes a credential.
	Email       string  `json:"email,omitempty" gorm:"-"`
	DisplayName *string `json:"display_name" gorm:"-"`
}

type ProjectContextVersionID string

type RoleType string

type AgentAuthorizationID string

type AgentAuthorization struct {
	Resource
	AgentID AgentIdentityID `json:"agent_id" gorm:"index"`
	Roles   []RoleType      `json:"roles" gorm:"serializer:json;type:jsonb"`
	// ActivePackages is active-work state kept distinct from the authorization
	// state above: the count of active, unexpired work packages the agent owns in
	// this project.
	ActivePackages int64 `json:"active_packages" gorm:"-"`
}

type Limit struct {
	Resource
	// Reason is recorded in the immutable change summary for this mutation.
	Reason         string `json:"reason,omitempty" gorm:"-"`
	Value          int64  `json:"value"`
	Unit           string `json:"unit"`
	PeriodSeconds  int64  `json:"period_seconds,omitempty"`
	Effective      int64  `json:"effective" gorm:"-"`
	Usage          int64  `json:"usage" gorm:"-"`
	LimitingSource string `json:"limiting_source" gorm:"-"`
	OverLimit      bool   `json:"over_limit" gorm:"-"`
}

type Resource struct {
	Authority string            `json:"authority"`
	Links     map[string]string `json:"links,omitempty" gorm:"-"`
	ID        string            `json:"id" gorm:"primaryKey"`
	Name      string            `json:"name"`
	AccountID AccountID         `json:"account_id,omitempty" gorm:"index"`
	ProjectID ProjectID         `json:"project_id,omitempty" gorm:"index"`
	Status    string            `json:"status" gorm:"index"`
	Labels    manifest.Labels   `json:"labels" gorm:"serializer:json;type:jsonb"`
	Revision  int64             `json:"revision"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Actor     Principal         `json:"actor" gorm:"serializer:json;type:jsonb"`
}

func (r *Resource) Metadata() *Resource { return r }

type RoleConfiguration struct {
	Enabled                   *bool    `json:"enabled"`
	Instructions              []string `json:"instructions"`
	MaxTasks                  *int     `json:"max_tasks"`
	LeaseSeconds              *int     `json:"lease_seconds"`
	AbsoluteLeaseSeconds      *int     `json:"absolute_lease_seconds"`
	ConcurrentExperiments     *int     `json:"concurrent_experiments"`
	AssignmentIntervalSeconds *int     `json:"assignment_interval_seconds"`
	Ordering                  string   `json:"ordering"`
}
