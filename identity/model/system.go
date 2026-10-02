package model

import "time"

type SessionScope string

const (
	ScopeAccount SessionScope = "account"
	ScopeSystem  SessionScope = "system"
)

type SystemEntitlement struct {
	LastModifiedBy ResourceActor `json:"-" gorm:"serializer:json;type:jsonb"`
	UserID         string        `json:"user_id" gorm:"primaryKey"`
	Status         string        `json:"status"`
	Revision       int64         `json:"revision"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type SystemRecord struct {
	LastModifiedBy ResourceActor `json:"-" gorm:"serializer:json;type:jsonb"`
	ID             string        `json:"id" gorm:"primaryKey"`
	Revision       int64         `json:"revision"`
	Status         string        `json:"status" gorm:"index"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	// ActorID is the immutable initiator used by preview and independent-approval checks.
	ActorID string `json:"actor_id"`
}

type PurgePolicy struct {
	Enabled               bool   `json:"enabled"`
	RecoveryDelaySeconds  int64  `json:"recovery_delay_seconds"`
	ApprovalMode          string `json:"approval_mode"`
	StepUpMaxAgeSeconds   int64  `json:"step_up_max_age_seconds"`
	AssuranceMethod       string `json:"assurance_method"`
	WorkerIntervalSeconds int64  `json:"worker_interval_seconds"`
	RetryLimit            int    `json:"retry_limit"`
	MaxResourceCount      int64  `json:"max_resource_count"`
}

type SystemAccount struct {
	SystemRecord
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	OwnerSetup      string           `json:"owner_setup"`
	Counts          map[string]int64 `json:"counts"`
	Limits          []Limit          `json:"limits"`
	OverLimit       bool             `json:"over_limit"`
	SupportStatus   string           `json:"support_status"`
	LifecycleReason string           `json:"lifecycle_reason"`
	LifecycleAt     *time.Time       `json:"lifecycle_at,omitempty"`
}

type SystemMembership struct {
	SystemRecord
	AccountID    AccountID `json:"account_id"`
	UserID       string    `json:"user_id"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	ActiveOwners int64     `json:"active_owners"`
}

type SystemInvitation struct {
	SystemRecord
	Delivery      string                   `json:"delivery"`
	EmailDelivery *InvitationEmailDelivery `json:"email_delivery,omitempty"`
	AccountID     AccountID                `json:"account_id"`
	Email         string                   `json:"email"`
	Role          string                   `json:"role"`
	ExpiresAt     time.Time                `json:"expires_at"`
}

type ImpactPreview struct {
	SystemRecord
	TargetAccountID AccountID        `json:"account_id" gorm:"index"`
	TargetID        string           `json:"target_id"`
	Operation       string           `json:"operation"`
	TargetRevision  int64            `json:"target_revision"`
	Counts          map[string]int64 `json:"counts" gorm:"serializer:json;type:jsonb"`
	SessionID       string           `json:"-"`
	MaterialDigest  string           `json:"-"`
	ExpiresAt       time.Time        `json:"expires_at"`
	ConsumedAt      *time.Time       `json:"consumed_at,omitempty"`
}

type OwnerRecovery struct {
	SystemRecord
	EmailDelivery           *InvitationEmailDelivery `json:"email_delivery,omitempty" gorm:"-"`
	TargetAccountID         AccountID                `json:"account_id" gorm:"index"`
	PreviousMembershipID    string                   `json:"previous_membership_id"`
	InvitationID            string                   `json:"invitation_id"`
	ReplacementEmail        string                   `json:"replacement_email"`
	ReplacementMembershipID string                   `json:"replacement_membership_id,omitempty" gorm:"-"`
	InvitationStatus        string                   `json:"invitation_status" gorm:"-"`
	ExpiresAt               time.Time                `json:"expires_at"`
	Reason                  string                   `json:"reason"`
	Reference               string                   `json:"reference,omitempty"`
	InvitationToken         string                   `json:"token,omitempty" gorm:"-"`
}

type StepUpAuthorization struct {
	SystemRecord
	TargetAccountID AccountID  `json:"account_id"`
	Action          string     `json:"action"`
	SessionID       string     `json:"-"`
	AuthenticatedAt time.Time  `json:"authenticated_at"`
	AssuranceMethod string     `json:"assurance_method"`
	ExpiresAt       time.Time  `json:"expires_at"`
	ConsumedAt      *time.Time `json:"consumed_at,omitempty"`
}

type AccountDeletionRequest struct {
	SystemRecord
	TargetAccountID AccountID                 `json:"account_id" gorm:"index"`
	Reason          string                    `json:"reason"`
	Reference       string                    `json:"reference,omitempty"`
	Counts          map[string]int64          `json:"counts" gorm:"serializer:json;type:jsonb"`
	ApprovalMode    string                    `json:"approval_mode"`
	ExecuteAfter    time.Time                 `json:"execute_after"`
	CompletedAt     *time.Time                `json:"completed_at,omitempty"`
	CancelReason    string                    `json:"cancel_reason,omitempty"`
	FailureCode     string                    `json:"failure_code,omitempty"`
	Attempts        int                       `json:"attempts"`
	Approvals       []AccountDeletionApproval `json:"approvals" gorm:"-"`
}

type AccountDeletionApproval struct {
	SystemRecord
	RequestID       string    `json:"request_id" gorm:"uniqueIndex:deletion_approver"`
	UserID          string    `json:"user_id" gorm:"uniqueIndex:deletion_approver"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	AssuranceMethod string    `json:"assurance_method"`
}

type AccountPurgeTombstone struct {
	SystemRecord
	TargetAccountID AccountID        `json:"account_id" gorm:"uniqueIndex"`
	RequestID       string           `json:"request_id"`
	Reason          string           `json:"reason"`
	Reference       string           `json:"reference,omitempty"`
	ApproverIDs     []string         `json:"approver_ids" gorm:"serializer:json;type:jsonb"`
	Counts          map[string]int64 `json:"counts" gorm:"serializer:json;type:jsonb"`
}

type SystemAction struct {
	Delivery             string `json:"delivery,omitempty"`
	Operation            string `json:"operation"`
	TargetID             string `json:"target_id,omitempty"`
	PreviewID            string `json:"preview_id,omitempty"`
	Reason               string `json:"reason"`
	Reference            string `json:"reference,omitempty"`
	Confirmation         string `json:"confirmation,omitempty"`
	StepUpID             string `json:"step_up_id,omitempty"`
	RecoveryAcknowledged bool   `json:"recovery_acknowledged,omitempty"`
	ReplacementEmail     string `json:"replacement_email,omitempty"`
}

type SystemActivity struct {
	SystemRecord
	TargetAccountID AccountID    `json:"account_id,omitempty" gorm:"index"`
	Kind            string       `json:"kind" gorm:"index"`
	Action          string       `json:"action"`
	TargetID        string       `json:"target_id"`
	Outcome         string       `json:"outcome"`
	Reason          string       `json:"reason,omitempty"`
	Reference       string       `json:"reference,omitempty"`
	RequestID       string       `json:"request_id" gorm:"index"`
	SessionScope    SessionScope `json:"session_scope"`
	AssuranceMethod string       `json:"assurance_method,omitempty"`
	ChangeIDs       []string     `json:"change_ids" gorm:"serializer:json;type:jsonb"`
}

type SystemHealth struct {
	Components    map[string]string `json:"components"`
	LiveUpdates   map[string]int64  `json:"live_updates,omitempty"`
	SchemaVersion int               `json:"schema_version"`
	BuildVersion  string            `json:"build_version"`
	Status        string            `json:"status"`
	APIVersion    string            `json:"api_version"`
	CheckedAt     time.Time         `json:"checked_at"`
}

type SystemConfiguration struct {
	ServiceConfiguration
	Purge PurgePolicy `json:"purge"`
}

type SystemOverview struct {
	GeneratedAt     time.Time        `json:"generated_at"`
	Health          SystemHealth     `json:"health"`
	Counts          map[string]int64 `json:"counts"`
	LifecycleCounts map[string]int64 `json:"lifecycle_counts"`
	Limits          []Limit          `json:"limits"`
	Warnings        []string         `json:"warnings"`
	RecentActivity  []SystemActivity `json:"recent_activity"`
}

type SystemQuery struct {
	Sort       string     `json:"sort,omitempty"`
	Direction  string     `json:"direction,omitempty"`
	Search     string     `json:"q,omitempty"`
	Status     string     `json:"status,omitempty"`
	OwnerSetup string     `json:"owner_setup,omitempty"`
	LimitState string     `json:"limit_state,omitempty"`
	Cursor     string     `json:"cursor,omitempty"`
	Limit      int        `json:"limit,omitempty"`
	AccountID  AccountID  `json:"account_id,omitempty"`
	Kind       string     `json:"kind,omitempty"`
	Action     string     `json:"action,omitempty"`
	Outcome    string     `json:"outcome,omitempty"`
	ActorID    string     `json:"actor_id,omitempty"`
	RequestID  string     `json:"request_id,omitempty"`
	From       *time.Time `json:"from,omitempty"`
	Till       *time.Time `json:"till,omitempty"`
}

// SystemPage is a page of a system list: the list contract of every other list
// (items, limit, next, total), stamped with when it was read.
type SystemPage[T any] struct {
	Items []T `json:"items"`
	Limit int `json:"limit"`
	// Next continues after this page; absent on the last page.
	Next string `json:"next,omitempty"`
	// Total counts the matching rows, when they could be counted.
	Total       *int64    `json:"total,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
}

func (r *SystemRecord) SystemMetadata() *SystemRecord { return r }

type SystemPolicyAction struct {
	SystemAction
	Configured       *RoleConfiguration `json:"configured,omitempty"`
	HypothesisReview *bool              `json:"hypothesis_review,omitempty"`
	ResultReview     *bool              `json:"result_review,omitempty"`
	Value            *int64             `json:"value,omitempty"`
	PeriodSeconds    *int64             `json:"period_seconds,omitempty"`
}

type TokenResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	TokenType    string       `json:"token_type"`
	Scope        SessionScope `json:"scope"`
	AccountID    AccountID    `json:"account_id,omitempty"`
	ExpiresIn    int64        `json:"expires_in"`
}

type SystemAccountCreate struct {
	Delivery    string `json:"delivery,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnerEmail  string `json:"owner_email"`
	Reason      string `json:"reason"`
	Reference   string `json:"reference,omitempty"`
}

type SystemAccountCreated struct {
	EmailDelivery *InvitationEmailDelivery `json:"email_delivery,omitempty"`
	SystemAccount
	OwnerInvitationID string `json:"owner_invitation_id"`
	InvitationToken   string `json:"token,omitempty"`
}
