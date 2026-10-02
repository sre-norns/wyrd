// Typed wire schemas and their explicit domain-field ownership mappings.
// Database models are never serialized directly by the resource transport.
package resource

import (
	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"time"
)

type AccountSpec struct {
	Description string `json:"description" yaml:"description"`
}
type AccountStatus struct {
	State           `yaml:",inline"`
	LifecycleReason string     `json:"lifecycleReason,omitempty" yaml:"lifecycleReason,omitempty"`
	LifecycleAt     *time.Time `json:"lifecycleAt,omitempty" yaml:"lifecycleAt,omitempty"`
}
type ProjectSpec struct {
	Description string `json:"description" yaml:"description"`
	Target      string `json:"target" yaml:"target"`
}
type ProjectStatus struct {
	State            `yaml:",inline"`
	CurrentContextID model.ProjectContextVersionID `json:"currentContextId,omitempty" yaml:"currentContextId,omitempty"`
}
type AccountMembershipSpec struct {
	UserID string `json:"userId" yaml:"userId"`
	Role   string `json:"role" yaml:"role"`
}
type AccountMembershipStatus struct {
	State       `yaml:",inline"`
	Email       string  `json:"email,omitempty" yaml:"email,omitempty"`
	DisplayName *string `json:"displayName" yaml:"displayName"`
}
type ProjectMembershipSpec struct {
	UserID string `json:"userId" yaml:"userId"`
}
type ProjectMembershipStatus struct {
	State       `yaml:",inline"`
	Email       string  `json:"email,omitempty" yaml:"email,omitempty"`
	DisplayName *string `json:"displayName" yaml:"displayName"`
}
type AccountInvitationSpec struct {
	Email    string `json:"email" yaml:"email"`
	Role     string `json:"role" yaml:"role"`
	Delivery string `json:"delivery,omitempty" yaml:"delivery,omitempty"`
}
type AccountInvitationStatus struct {
	State         `yaml:",inline"`
	EmailDelivery *Delivery                 `json:"emailDelivery,omitempty" yaml:"emailDelivery,omitempty"`
	ExpiresAt     time.Time                 `json:"expiresAt" yaml:"expiresAt"`
	AcceptedBy    string                    `json:"acceptedBy,omitempty" yaml:"acceptedBy,omitempty"`
	MembershipID  model.AccountMembershipID `json:"membershipId,omitempty" yaml:"membershipId,omitempty"`
}
type AgentIdentitySpec struct {
	Description string `json:"description" yaml:"description"`
}
type AgentIdentityStatus struct {
	State      `yaml:",inline"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty" yaml:"lastSeenAt,omitempty"`
}
type AgentIdentityTokenSpec struct {
	AgentID   model.AgentIdentityID `json:"agentId" yaml:"agentId"`
	ExpiresAt *time.Time            `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
}
type AgentIdentityTokenStatus struct {
	State `yaml:",inline"`
}
type AgentAuthorizationSpec struct {
	AgentID model.AgentIdentityID `json:"agentId" yaml:"agentId"`
	Roles   []model.RoleType      `json:"roles" yaml:"roles"`
}
type AgentAuthorizationStatus struct {
	State          `yaml:",inline"`
	ActivePackages int64 `json:"activePackages" yaml:"activePackages"`
}
type SessionSpec struct {
}
type SessionStatus struct {
	State                `yaml:",inline"`
	Scope                model.SessionScope `json:"scope" yaml:"scope"`
	AuthenticatedAt      time.Time          `json:"authenticatedAt" yaml:"authenticatedAt"`
	AuthenticationMethod string             `json:"authenticationMethod" yaml:"authenticationMethod"`
	UserID               string             `json:"userId" yaml:"userId"`
	ClientID             string             `json:"clientId" yaml:"clientId"`
	Origin               string             `json:"origin,omitempty" yaml:"origin,omitempty"`
	IPAddress            string             `json:"ipAddress,omitempty" yaml:"ipAddress,omitempty"`
	UserAgent            string             `json:"userAgent,omitempty" yaml:"userAgent,omitempty"`
	ExpiresAt            time.Time          `json:"expiresAt" yaml:"expiresAt"`
	RefreshExpiresAt     time.Time          `json:"refreshExpiresAt" yaml:"refreshExpiresAt"`
}
type LimitSpec struct {
	Value         int64  `json:"value" yaml:"value"`
	Unit          string `json:"unit" yaml:"unit"`
	PeriodSeconds int64  `json:"periodSeconds,omitempty" yaml:"periodSeconds,omitempty"`
}
type LimitStatus struct {
	State          `yaml:",inline"`
	Effective      int64  `json:"effective" yaml:"effective"`
	Usage          int64  `json:"usage" yaml:"usage"`
	LimitingSource string `json:"limitingSource" yaml:"limitingSource"`
	OverLimit      bool   `json:"overLimit" yaml:"overLimit"`
}
type PersonalProfileSpec struct {
	DisplayName *string `json:"displayName" yaml:"displayName"`
}
type PersonalProfileStatus struct {
	State `yaml:",inline"`
	Email string `json:"email" yaml:"email"`
}
type SignInMethodSpec struct {
}
type SignInMethodStatus struct {
	State      `yaml:",inline"`
	Method     string     `json:"method" yaml:"method"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty" yaml:"lastUsedAt,omitempty"`
}
type SystemAccountSpec struct {
	Description string `json:"description" yaml:"description"`
}
type SystemAccountStatus struct {
	State           `yaml:",inline"`
	OwnerSetup      string                      `json:"ownerSetup" yaml:"ownerSetup"`
	Counts          map[string]int64            `json:"counts" yaml:"counts"`
	Limits          []manifest.ResourceManifest `json:"limits" yaml:"limits"`
	OverLimit       bool                        `json:"overLimit" yaml:"overLimit"`
	SupportStatus   string                      `json:"supportStatus" yaml:"supportStatus"`
	LifecycleReason string                      `json:"lifecycleReason" yaml:"lifecycleReason"`
	LifecycleAt     *time.Time                  `json:"lifecycleAt,omitempty" yaml:"lifecycleAt,omitempty"`
}
type SystemMembershipSpec struct {
	UserID string `json:"userId" yaml:"userId"`
	Role   string `json:"role" yaml:"role"`
}
type SystemMembershipStatus struct {
	State        `yaml:",inline"`
	Email        string `json:"email" yaml:"email"`
	ActiveOwners int64  `json:"activeOwners" yaml:"activeOwners"`
}
type SystemInvitationSpec struct {
	Email    string `json:"email" yaml:"email"`
	Role     string `json:"role" yaml:"role"`
	Delivery string `json:"delivery" yaml:"delivery"`
}
type SystemInvitationStatus struct {
	State         `yaml:",inline"`
	EmailDelivery *Delivery `json:"emailDelivery,omitempty" yaml:"emailDelivery,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt" yaml:"expiresAt"`
}
type ImpactPreviewSpec struct {
	TargetAccountID model.AccountID `json:"targetAccountId" yaml:"targetAccountId"`
	TargetID        string          `json:"targetId" yaml:"targetId"`
	Operation       string          `json:"operation" yaml:"operation"`
}
type ImpactPreviewStatus struct {
	InitiatorID    string `json:"initiatorId,omitempty" yaml:"initiatorId,omitempty"`
	State          `yaml:",inline"`
	TargetRevision int64            `json:"targetRevision" yaml:"targetRevision"`
	Counts         map[string]int64 `json:"counts" yaml:"counts"`
	ExpiresAt      time.Time        `json:"expiresAt" yaml:"expiresAt"`
	ConsumedAt     *time.Time       `json:"consumedAt,omitempty" yaml:"consumedAt,omitempty"`
}
type OwnerRecoverySpec struct {
	TargetAccountID      model.AccountID `json:"targetAccountId" yaml:"targetAccountId"`
	PreviousMembershipID string          `json:"previousMembershipId" yaml:"previousMembershipId"`
	ReplacementEmail     string          `json:"replacementEmail" yaml:"replacementEmail"`
	Reason               string          `json:"reason" yaml:"reason"`
	Reference            string          `json:"reference,omitempty" yaml:"reference,omitempty"`
}
type OwnerRecoveryStatus struct {
	InitiatorID             string `json:"initiatorId,omitempty" yaml:"initiatorId,omitempty"`
	State                   `yaml:",inline"`
	EmailDelivery           *Delivery `json:"emailDelivery,omitempty" yaml:"emailDelivery,omitempty"`
	InvitationID            string    `json:"invitationId" yaml:"invitationId"`
	ReplacementMembershipID string    `json:"replacementMembershipId,omitempty" yaml:"replacementMembershipId,omitempty"`
	InvitationStatus        string    `json:"invitationStatus" yaml:"invitationStatus"`
	ExpiresAt               time.Time `json:"expiresAt" yaml:"expiresAt"`
}
type StepUpAuthorizationSpec struct {
	TargetAccountID model.AccountID `json:"targetAccountId" yaml:"targetAccountId"`
	Action          string          `json:"action" yaml:"action"`
}
type StepUpAuthorizationStatus struct {
	InitiatorID     string `json:"initiatorId,omitempty" yaml:"initiatorId,omitempty"`
	State           `yaml:",inline"`
	AuthenticatedAt time.Time  `json:"authenticatedAt" yaml:"authenticatedAt"`
	AssuranceMethod string     `json:"assuranceMethod" yaml:"assuranceMethod"`
	ExpiresAt       time.Time  `json:"expiresAt" yaml:"expiresAt"`
	ConsumedAt      *time.Time `json:"consumedAt,omitempty" yaml:"consumedAt,omitempty"`
}
type AccountDeletionRequestSpec struct {
	TargetAccountID model.AccountID `json:"targetAccountId" yaml:"targetAccountId"`
	Reason          string          `json:"reason" yaml:"reason"`
	Reference       string          `json:"reference,omitempty" yaml:"reference,omitempty"`
}
type AccountDeletionRequestStatus struct {
	InitiatorID  string `json:"initiatorId,omitempty" yaml:"initiatorId,omitempty"`
	State        `yaml:",inline"`
	Counts       map[string]int64            `json:"counts" yaml:"counts"`
	ApprovalMode string                      `json:"approvalMode" yaml:"approvalMode"`
	ExecuteAfter time.Time                   `json:"executeAfter" yaml:"executeAfter"`
	CompletedAt  *time.Time                  `json:"completedAt,omitempty" yaml:"completedAt,omitempty"`
	CancelReason string                      `json:"cancelReason,omitempty" yaml:"cancelReason,omitempty"`
	FailureCode  string                      `json:"failureCode,omitempty" yaml:"failureCode,omitempty"`
	Attempts     int                         `json:"attempts" yaml:"attempts"`
	Approvals    []manifest.ResourceManifest `json:"approvals" yaml:"approvals"`
}
type AccountDeletionApprovalSpec struct {
	RequestID string `json:"requestId" yaml:"requestId"`
}
type AccountDeletionApprovalStatus struct {
	InitiatorID     string `json:"initiatorId,omitempty" yaml:"initiatorId,omitempty"`
	State           `yaml:",inline"`
	UserID          string    `json:"userId" yaml:"userId"`
	AuthenticatedAt time.Time `json:"authenticatedAt" yaml:"authenticatedAt"`
	AssuranceMethod string    `json:"assuranceMethod" yaml:"assuranceMethod"`
}
type AccountPurgeTombstoneSpec struct {
	TargetAccountID model.AccountID `json:"targetAccountId" yaml:"targetAccountId"`
	RequestID       string          `json:"requestId" yaml:"requestId"`
	Reason          string          `json:"reason" yaml:"reason"`
	Reference       string          `json:"reference,omitempty" yaml:"reference,omitempty"`
}
type AccountPurgeTombstoneStatus struct {
	InitiatorID string `json:"initiatorId,omitempty" yaml:"initiatorId,omitempty"`
	State       `yaml:",inline"`
	ApproverIDs []string         `json:"approverIds" yaml:"approverIds"`
	Counts      map[string]int64 `json:"counts" yaml:"counts"`
}
type SystemActivitySpec struct {
}
type SystemActivityStatus struct {
	InitiatorID     string `json:"initiatorId,omitempty" yaml:"initiatorId,omitempty"`
	State           `yaml:",inline"`
	TargetAccountID model.AccountID    `json:"targetAccountId,omitempty" yaml:"targetAccountId,omitempty"`
	Kind            string             `json:"kind" yaml:"kind"`
	Action          string             `json:"action" yaml:"action"`
	TargetID        string             `json:"targetId" yaml:"targetId"`
	Outcome         string             `json:"outcome" yaml:"outcome"`
	Reason          string             `json:"reason,omitempty" yaml:"reason,omitempty"`
	Reference       string             `json:"reference,omitempty" yaml:"reference,omitempty"`
	RequestID       string             `json:"requestId" yaml:"requestId"`
	SessionScope    model.SessionScope `json:"sessionScope" yaml:"sessionScope"`
	AssuranceMethod string             `json:"assuranceMethod,omitempty" yaml:"assuranceMethod,omitempty"`
	ChangeIDs       []string           `json:"changeIds" yaml:"changeIds"`
}
type SystemEntitlementSpec struct {
}
type SystemEntitlementStatus struct {
	State `yaml:",inline"`
}

func init() {
	register(model.Account{}, "accounts", AccountSpec{}, AccountStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "Description", Domain: "description", Name: "description", Section: "spec", Create: true, Patch: true},
		{GoName: "LifecycleReason", Domain: "lifecycle_reason", Name: "lifecycleReason", Section: "status", Create: false, Patch: false},
		{GoName: "LifecycleAt", Domain: "lifecycle_at", Name: "lifecycleAt", Section: "status", Create: false, Patch: false},
		{GoName: "OwnerEmail", Domain: "owner_email", Name: "ownerEmail", Section: "input", Create: true, Patch: false},
		{GoName: "Delivery", Domain: "delivery", Name: "delivery", Section: "input", Create: true, Patch: false},
	})
	register(model.Project{}, "projects", ProjectSpec{}, ProjectStatus{}, []manifest.Scope{manifest.ScopeAccount}, []Field{
		{GoName: "Description", Domain: "description", Name: "description", Section: "spec", Create: true, Patch: true},
		{GoName: "Target", Domain: "target", Name: "target", Section: "spec", Create: true, Patch: true},
		{GoName: "CurrentContextID", Domain: "current_context_id", Name: "currentContextId", Section: "status", Create: false, Patch: false},
	})
	register(model.AccountMembership{}, "account-memberships", AccountMembershipSpec{}, AccountMembershipStatus{}, []manifest.Scope{manifest.ScopeAccount}, []Field{
		{GoName: "UserID", Domain: "user_id", Name: "userId", Section: "spec", Create: true, Patch: false},
		{GoName: "Role", Domain: "role", Name: "role", Section: "spec", Create: true, Patch: true},
		{GoName: "Email", Domain: "email", Name: "email", Section: "status", Create: false, Patch: false},
		{GoName: "DisplayName", Domain: "display_name", Name: "displayName", Section: "status", Create: false, Patch: false},
	})
	register(model.ProjectMembership{}, "project-memberships", ProjectMembershipSpec{}, ProjectMembershipStatus{}, []manifest.Scope{manifest.ScopeProject}, []Field{
		{GoName: "UserID", Domain: "user_id", Name: "userId", Section: "spec", Create: true, Patch: false},
		{GoName: "Email", Domain: "email", Name: "email", Section: "status", Create: false, Patch: false},
		{GoName: "DisplayName", Domain: "display_name", Name: "displayName", Section: "status", Create: false, Patch: false},
	})
	register(model.AccountInvitation{}, "account-invitations", AccountInvitationSpec{}, AccountInvitationStatus{}, []manifest.Scope{manifest.ScopeAccount}, []Field{
		{GoName: "Email", Domain: "email", Name: "email", Section: "spec", Create: true, Patch: false},
		{GoName: "Role", Domain: "role", Name: "role", Section: "spec", Create: true, Patch: false},
		{GoName: "Delivery", Domain: "delivery", Name: "delivery", Section: "spec", Create: true, Patch: false},
		{GoName: "EmailDelivery", Domain: "email_delivery", Name: "emailDelivery", Section: "status", Create: false, Patch: false},
		{GoName: "ExpiresAt", Domain: "expires_at", Name: "expiresAt", Section: "status", Create: false, Patch: false},
		{GoName: "AcceptedBy", Domain: "accepted_by", Name: "acceptedBy", Section: "status", Create: false, Patch: false},
		{GoName: "MembershipID", Domain: "membership_id", Name: "membershipId", Section: "status", Create: false, Patch: false},
	})
	register(model.AgentIdentity{}, "agent-identities", AgentIdentitySpec{}, AgentIdentityStatus{}, []manifest.Scope{manifest.ScopeAccount}, []Field{
		{GoName: "Description", Domain: "description", Name: "description", Section: "spec", Create: true, Patch: true},
		{GoName: "LastSeenAt", Domain: "last_seen_at", Name: "lastSeenAt", Section: "status", Create: false, Patch: false},
	})
	register(model.AgentIdentityToken{}, "agent-identity-tokens", AgentIdentityTokenSpec{}, AgentIdentityTokenStatus{}, []manifest.Scope{manifest.ScopeAccount}, []Field{
		{GoName: "AgentID", Domain: "agent_id", Name: "agentId", Section: "spec", Create: false, Patch: false},
		{GoName: "ExpiresAt", Domain: "expires_at", Name: "expiresAt", Section: "spec", Create: true, Patch: false},
	})
	register(model.AgentAuthorization{}, "agent-authorizations", AgentAuthorizationSpec{}, AgentAuthorizationStatus{}, []manifest.Scope{manifest.ScopeProject}, []Field{
		{GoName: "AgentID", Domain: "agent_id", Name: "agentId", Section: "spec", Create: true, Patch: false},
		{GoName: "Roles", Domain: "roles", Name: "roles", Section: "spec", Create: true, Patch: true},
		{GoName: "ActivePackages", Domain: "active_packages", Name: "activePackages", Section: "status", Create: false, Patch: false},
	})
	register(model.Session{}, "sessions", SessionSpec{}, SessionStatus{}, []manifest.Scope{manifest.ScopeSystem, manifest.ScopeAccount}, []Field{
		{GoName: "Scope", Domain: "scope", Name: "scope", Section: "status", Create: false, Patch: false},
		{GoName: "AuthenticatedAt", Domain: "authenticated_at", Name: "authenticatedAt", Section: "status", Create: false, Patch: false},
		{GoName: "AuthenticationMethod", Domain: "authentication_method", Name: "authenticationMethod", Section: "status", Create: false, Patch: false},
		{GoName: "UserID", Domain: "user_id", Name: "userId", Section: "status", Create: false, Patch: false},
		{GoName: "ClientID", Domain: "client_id", Name: "clientId", Section: "status", Create: false, Patch: false},
		{GoName: "Origin", Domain: "origin", Name: "origin", Section: "status", Create: false, Patch: false},
		{GoName: "IPAddress", Domain: "ip_address", Name: "ipAddress", Section: "status", Create: false, Patch: false},
		{GoName: "UserAgent", Domain: "user_agent", Name: "userAgent", Section: "status", Create: false, Patch: false},
		{GoName: "ExpiresAt", Domain: "expires_at", Name: "expiresAt", Section: "status", Create: false, Patch: false},
		{GoName: "RefreshExpiresAt", Domain: "refresh_expires_at", Name: "refreshExpiresAt", Section: "status", Create: false, Patch: false},
	})
	register(model.Limit{}, "limits", LimitSpec{}, LimitStatus{}, []manifest.Scope{manifest.ScopeSystem, manifest.ScopeAccount, manifest.ScopeProject}, []Field{
		{GoName: "Value", Domain: "value", Name: "value", Section: "spec", Create: false, Patch: true},
		{GoName: "Unit", Domain: "unit", Name: "unit", Section: "spec", Create: false, Patch: false},
		{GoName: "PeriodSeconds", Domain: "period_seconds", Name: "periodSeconds", Section: "spec", Create: false, Patch: true},
		{GoName: "Effective", Domain: "effective", Name: "effective", Section: "status", Create: false, Patch: false},
		{GoName: "Usage", Domain: "usage", Name: "usage", Section: "status", Create: false, Patch: false},
		{GoName: "LimitingSource", Domain: "limiting_source", Name: "limitingSource", Section: "status", Create: false, Patch: false},
		{GoName: "OverLimit", Domain: "over_limit", Name: "overLimit", Section: "status", Create: false, Patch: false},
		{GoName: "Reason", Domain: "reason", Name: "reason", Section: "input", Create: false, Patch: true},
	})
	register(model.PersonalProfile{}, "personal-profiles", PersonalProfileSpec{}, PersonalProfileStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "DisplayName", Domain: "display_name", Name: "displayName", Section: "spec", Create: false, Patch: true},
		{GoName: "Email", Domain: "email", Name: "email", Section: "status", Create: false, Patch: false},
	})
	register(model.SignInMethod{}, "sign-in-methods", SignInMethodSpec{}, SignInMethodStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "Method", Domain: "method", Name: "method", Section: "status", Create: false, Patch: false},
		{GoName: "LastUsedAt", Domain: "last_used_at", Name: "lastUsedAt", Section: "status", Create: false, Patch: false},
	})
	register(model.SystemAccount{}, "system-accounts", SystemAccountSpec{}, SystemAccountStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "Description", Domain: "description", Name: "description", Section: "spec", Create: false, Patch: false},
		{GoName: "OwnerSetup", Domain: "owner_setup", Name: "ownerSetup", Section: "status", Create: false, Patch: false},
		{GoName: "Counts", Domain: "counts", Name: "counts", Section: "status", Create: false, Patch: false},
		{GoName: "Limits", Domain: "limits", Name: "limits", Section: "status", Create: false, Patch: false},
		{GoName: "OverLimit", Domain: "over_limit", Name: "overLimit", Section: "status", Create: false, Patch: false},
		{GoName: "SupportStatus", Domain: "support_status", Name: "supportStatus", Section: "status", Create: false, Patch: false},
		{GoName: "LifecycleReason", Domain: "lifecycle_reason", Name: "lifecycleReason", Section: "status", Create: false, Patch: false},
		{GoName: "LifecycleAt", Domain: "lifecycle_at", Name: "lifecycleAt", Section: "status", Create: false, Patch: false},
	})
	register(model.SystemMembership{}, "system-account-memberships", SystemMembershipSpec{}, SystemMembershipStatus{}, []manifest.Scope{manifest.ScopeAccount}, []Field{
		{GoName: "UserID", Domain: "user_id", Name: "userId", Section: "spec", Create: false, Patch: false},
		{GoName: "Role", Domain: "role", Name: "role", Section: "spec", Create: false, Patch: false},
		{GoName: "Email", Domain: "email", Name: "email", Section: "status", Create: false, Patch: false},
		{GoName: "ActiveOwners", Domain: "active_owners", Name: "activeOwners", Section: "status", Create: false, Patch: false},
	})
	register(model.SystemInvitation{}, "system-account-invitations", SystemInvitationSpec{}, SystemInvitationStatus{}, []manifest.Scope{manifest.ScopeAccount}, []Field{
		{GoName: "Email", Domain: "email", Name: "email", Section: "spec", Create: false, Patch: false},
		{GoName: "Role", Domain: "role", Name: "role", Section: "spec", Create: false, Patch: false},
		{GoName: "Delivery", Domain: "delivery", Name: "delivery", Section: "spec", Create: false, Patch: false},
		{GoName: "EmailDelivery", Domain: "email_delivery", Name: "emailDelivery", Section: "status", Create: false, Patch: false},
		{GoName: "ExpiresAt", Domain: "expires_at", Name: "expiresAt", Section: "status", Create: false, Patch: false},
	})
	register(model.ImpactPreview{}, "impact-previews", ImpactPreviewSpec{}, ImpactPreviewStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "ActorID", Domain: "actor_id", Name: "initiatorId", Section: "status"},
		{GoName: "TargetAccountID", Domain: "account_id", Name: "targetAccountId", Section: "spec", Create: false, Patch: false},
		{GoName: "TargetID", Domain: "target_id", Name: "targetId", Section: "spec", Create: false, Patch: false},
		{GoName: "Operation", Domain: "operation", Name: "operation", Section: "spec", Create: false, Patch: false},
		{GoName: "TargetRevision", Domain: "target_revision", Name: "targetRevision", Section: "status", Create: false, Patch: false},
		{GoName: "Counts", Domain: "counts", Name: "counts", Section: "status", Create: false, Patch: false},
		{GoName: "ExpiresAt", Domain: "expires_at", Name: "expiresAt", Section: "status", Create: false, Patch: false},
		{GoName: "ConsumedAt", Domain: "consumed_at", Name: "consumedAt", Section: "status", Create: false, Patch: false},
	})
	register(model.OwnerRecovery{}, "owner-recoveries", OwnerRecoverySpec{}, OwnerRecoveryStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "ActorID", Domain: "actor_id", Name: "initiatorId", Section: "status"},
		{GoName: "TargetAccountID", Domain: "account_id", Name: "targetAccountId", Section: "spec", Create: false, Patch: false},
		{GoName: "PreviousMembershipID", Domain: "previous_membership_id", Name: "previousMembershipId", Section: "spec", Create: false, Patch: false},
		{GoName: "ReplacementEmail", Domain: "replacement_email", Name: "replacementEmail", Section: "spec", Create: false, Patch: false},
		{GoName: "Reason", Domain: "reason", Name: "reason", Section: "spec", Create: false, Patch: false},
		{GoName: "Reference", Domain: "reference", Name: "reference", Section: "spec", Create: false, Patch: false},
		{GoName: "EmailDelivery", Domain: "email_delivery", Name: "emailDelivery", Section: "status", Create: false, Patch: false},
		{GoName: "InvitationID", Domain: "invitation_id", Name: "invitationId", Section: "status", Create: false, Patch: false},
		{GoName: "ReplacementMembershipID", Domain: "replacement_membership_id", Name: "replacementMembershipId", Section: "status", Create: false, Patch: false},
		{GoName: "InvitationStatus", Domain: "invitation_status", Name: "invitationStatus", Section: "status", Create: false, Patch: false},
		{GoName: "ExpiresAt", Domain: "expires_at", Name: "expiresAt", Section: "status", Create: false, Patch: false},
	})
	register(model.StepUpAuthorization{}, "step-up-authorizations", StepUpAuthorizationSpec{}, StepUpAuthorizationStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "ActorID", Domain: "actor_id", Name: "initiatorId", Section: "status"},
		{GoName: "TargetAccountID", Domain: "account_id", Name: "targetAccountId", Section: "spec", Create: false, Patch: false},
		{GoName: "Action", Domain: "action", Name: "action", Section: "spec", Create: false, Patch: false},
		{GoName: "AuthenticatedAt", Domain: "authenticated_at", Name: "authenticatedAt", Section: "status", Create: false, Patch: false},
		{GoName: "AssuranceMethod", Domain: "assurance_method", Name: "assuranceMethod", Section: "status", Create: false, Patch: false},
		{GoName: "ExpiresAt", Domain: "expires_at", Name: "expiresAt", Section: "status", Create: false, Patch: false},
		{GoName: "ConsumedAt", Domain: "consumed_at", Name: "consumedAt", Section: "status", Create: false, Patch: false},
	})
	register(model.AccountDeletionRequest{}, "deletion-requests", AccountDeletionRequestSpec{}, AccountDeletionRequestStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "ActorID", Domain: "actor_id", Name: "initiatorId", Section: "status"},
		{GoName: "TargetAccountID", Domain: "account_id", Name: "targetAccountId", Section: "spec", Create: false, Patch: false},
		{GoName: "Reason", Domain: "reason", Name: "reason", Section: "spec", Create: false, Patch: false},
		{GoName: "Reference", Domain: "reference", Name: "reference", Section: "spec", Create: false, Patch: false},
		{GoName: "Counts", Domain: "counts", Name: "counts", Section: "status", Create: false, Patch: false},
		{GoName: "ApprovalMode", Domain: "approval_mode", Name: "approvalMode", Section: "status", Create: false, Patch: false},
		{GoName: "ExecuteAfter", Domain: "execute_after", Name: "executeAfter", Section: "status", Create: false, Patch: false},
		{GoName: "CompletedAt", Domain: "completed_at", Name: "completedAt", Section: "status", Create: false, Patch: false},
		{GoName: "CancelReason", Domain: "cancel_reason", Name: "cancelReason", Section: "status", Create: false, Patch: false},
		{GoName: "FailureCode", Domain: "failure_code", Name: "failureCode", Section: "status", Create: false, Patch: false},
		{GoName: "Attempts", Domain: "attempts", Name: "attempts", Section: "status", Create: false, Patch: false},
		{GoName: "Approvals", Domain: "approvals", Name: "approvals", Section: "status", Create: false, Patch: false},
	})
	register(model.AccountDeletionApproval{}, "deletion-approvals", AccountDeletionApprovalSpec{}, AccountDeletionApprovalStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "ActorID", Domain: "actor_id", Name: "initiatorId", Section: "status"},
		{GoName: "RequestID", Domain: "request_id", Name: "requestId", Section: "spec", Create: false, Patch: false},
		{GoName: "UserID", Domain: "user_id", Name: "userId", Section: "status", Create: false, Patch: false},
		{GoName: "AuthenticatedAt", Domain: "authenticated_at", Name: "authenticatedAt", Section: "status", Create: false, Patch: false},
		{GoName: "AssuranceMethod", Domain: "assurance_method", Name: "assuranceMethod", Section: "status", Create: false, Patch: false},
	})
	register(model.AccountPurgeTombstone{}, "purge-tombstones", AccountPurgeTombstoneSpec{}, AccountPurgeTombstoneStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "ActorID", Domain: "actor_id", Name: "initiatorId", Section: "status"},
		{GoName: "TargetAccountID", Domain: "account_id", Name: "targetAccountId", Section: "spec", Create: false, Patch: false},
		{GoName: "RequestID", Domain: "request_id", Name: "requestId", Section: "spec", Create: false, Patch: false},
		{GoName: "Reason", Domain: "reason", Name: "reason", Section: "spec", Create: false, Patch: false},
		{GoName: "Reference", Domain: "reference", Name: "reference", Section: "spec", Create: false, Patch: false},
		{GoName: "ApproverIDs", Domain: "approver_ids", Name: "approverIds", Section: "status", Create: false, Patch: false},
		{GoName: "Counts", Domain: "counts", Name: "counts", Section: "status", Create: false, Patch: false},
	})
	register(model.SystemActivity{}, "system-activity", SystemActivitySpec{}, SystemActivityStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{
		{GoName: "ActorID", Domain: "actor_id", Name: "initiatorId", Section: "status"},
		{GoName: "TargetAccountID", Domain: "account_id", Name: "targetAccountId", Section: "status", Create: false, Patch: false},
		{GoName: "Kind", Domain: "kind", Name: "kind", Section: "status", Create: false, Patch: false},
		{GoName: "Action", Domain: "action", Name: "action", Section: "status", Create: false, Patch: false},
		{GoName: "TargetID", Domain: "target_id", Name: "targetId", Section: "status", Create: false, Patch: false},
		{GoName: "Outcome", Domain: "outcome", Name: "outcome", Section: "status", Create: false, Patch: false},
		{GoName: "Reason", Domain: "reason", Name: "reason", Section: "status", Create: false, Patch: false},
		{GoName: "Reference", Domain: "reference", Name: "reference", Section: "status", Create: false, Patch: false},
		{GoName: "RequestID", Domain: "request_id", Name: "requestId", Section: "status", Create: false, Patch: false},
		{GoName: "SessionScope", Domain: "session_scope", Name: "sessionScope", Section: "status", Create: false, Patch: false},
		{GoName: "AssuranceMethod", Domain: "assurance_method", Name: "assuranceMethod", Section: "status", Create: false, Patch: false},
		{GoName: "ChangeIDs", Domain: "change_ids", Name: "changeIds", Section: "status", Create: false, Patch: false},
	})
	register(model.SystemEntitlement{}, "system-entitlements", SystemEntitlementSpec{}, SystemEntitlementStatus{}, []manifest.Scope{manifest.ScopeSystem}, []Field{})
}
