package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
	"gorm.io/gorm"
)

// Kind describes product-owned data. Registration controls visibility and purge.
type Kind struct {
	IDColumn                                  string // Defaults to id; use uid for manifest.ObjectMeta.
	Path, Table, Scope                        string
	MachineRead, SystemRead, PublicSystemRead bool
	// SystemManageAccount allows system administrators to manage account-level policy.
	SystemManageAccount bool
	CredentialOwner     bool
	Admit               func(context.Context, *gorm.DB, any) bool
	MachineFilter       func(context.Context, *gorm.DB) *gorm.DB
	PurgeHook           func(context.Context, *gorm.DB, e.AccountID) error
}

// Audit carries domain policy inputs and a credential-free snapshot in the mutation transaction.
type Audit struct {
	// Snapshot is the canonical, credential-free identity resource for host history.
	// Resource remains a domain value for authorization/policy callbacks.
	Snapshot                      json.RawMessage
	Principal                     e.Principal
	Action                        string
	Target                        *e.Resource
	Resource                      any
	Outcome, RequestID, Aggregate string
	SystemAction                  *e.SystemAction
}
type Auditor interface {
	Record(context.Context, *gorm.DB, Audit) error
}
type AuditorFunc func(context.Context, *gorm.DB, Audit) error

func (f AuditorFunc) Record(ctx context.Context, db *gorm.DB, a Audit) error { return f(ctx, db, a) }

// Extensions supplies product policy without importing a product into identity.
type Extensions struct {
	HTTPRejected func(context.Context, *gorm.DB, error) error

	UncachedPostPaths map[string]bool

	ImpactCounts  func(*gorm.DB, e.AccountID, map[string]int64) error
	ImpactTarget  func(*gorm.DB, e.AccountID, string, string) (int64, error)
	PreviewTables []string

	Kinds              map[string]Kind
	GrantRoles         []e.RoleType
	CredentialPurposes map[string]string
	AuxiliaryTables    []string
	Auditor            Auditor
	Rejected           func(context.Context, *gorm.DB, error) error
	Materialize        func(context.Context, *gorm.DB, e.AccountID, e.ProjectID) error
	ResourceLimit      func(*gorm.DB, e.AccountID, e.ProjectID, string) error
	EffectiveLimit     func(*gorm.DB, *e.Limit) error
	PrepareProject     func(context.Context, *gorm.DB, *e.Project, bool) error
	DecorateGrant      func(context.Context, *gorm.DB, *e.AgentAuthorization) error
	AllowRequest       func(context.Context, *gorm.DB) error
}

func (*Extensions) Name() string              { return "wyrd:identity" }
func (*Extensions) Initialize(*gorm.DB) error { return nil }

var sqlName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Register installs an immutable registry before serving requests.
func Register(db *gorm.DB, x Extensions) error {
	kinds := defaultKinds()
	for name, k := range x.Kinds {
		if k.IDColumn == "" {
			k.IDColumn = "id"
		}
		if !sqlName.MatchString(k.IDColumn) {
			return fmt.Errorf("invalid identity ID column: %s", k.IDColumn)
		}
		if name == "" || !sqlName.MatchString(k.Table) || (k.Scope != "account" && k.Scope != "project" && k.Scope != "system" && k.Scope != "scoped") {
			return fmt.Errorf("invalid identity kind registration: %s", name)
		}
		if _, exists := kinds[name]; exists {
			return fmt.Errorf("identity kind already registered: %s", name)
		}
		kinds[name] = k
	}
	for _, t := range append(slices.Clone(x.AuxiliaryTables), x.PreviewTables...) {
		if !sqlName.MatchString(t) {
			return fmt.Errorf("invalid auxiliary table: %s", t)
		}
	}
	for purpose, table := range x.CredentialPurposes {
		if purpose == "" || !sqlName.MatchString(table) {
			return fmt.Errorf("invalid credential purpose: %s", purpose)
		}
	}
	x.Kinds = kinds
	x.GrantRoles = slices.Clone(x.GrantRoles)
	x.CredentialPurposes = maps.Clone(x.CredentialPurposes)
	x.AuxiliaryTables = slices.Clone(x.AuxiliaryTables)
	x.PreviewTables = slices.Clone(x.PreviewTables)
	x.UncachedPostPaths = maps.Clone(x.UncachedPostPaths)
	return db.Use(&x)
}
func extensions(db *gorm.DB) *Extensions {
	if x, ok := db.Config.Plugins["wyrd:identity"].(*Extensions); ok {
		return x
	}
	return &Extensions{Kinds: defaultKinds()}
}
func defaultKinds() map[string]Kind {
	return map[string]Kind{
		"Account":            {Path: "accounts", Table: "accounts", Scope: "system"},
		"Project":            {Path: "projects", Table: "projects", Scope: "account"},
		"Session":            {Path: "sessions", Table: "sessions", Scope: "account", CredentialOwner: true},
		"AccountMembership":  {Path: "account-memberships", Table: "account_memberships", Scope: "account"},
		"AccountInvitation":  {Path: "account-invitations", Table: "account_invitations", Scope: "account", CredentialOwner: true},
		"AgentIdentity":      {Path: "agent-identities", Table: "agent_identities", Scope: "account"},
		"AgentIdentityToken": {Path: "agent-identity-tokens", Table: "agent_identity_tokens", Scope: "account", CredentialOwner: true},
		"AgentAuthorization": {Path: "agent-authorizations", Table: "agent_authorizations", Scope: "project"},
		"ProjectMembership":  {Path: "project-memberships", Table: "project_memberships", Scope: "project"},
	}
}
func accountTables(db *gorm.DB) []string {
	var out []string
	for _, k := range extensions(db).Kinds {
		if k.Scope != "system" {
			out = append(out, k.Table)
		}
	}
	sort.Strings(out)
	return out
}
func credentialOwnerTables(db *gorm.DB) []string {
	set := map[string]bool{}
	for _, k := range extensions(db).Kinds {
		if k.CredentialOwner {
			set[k.Table] = true
		}
	}
	for _, t := range extensions(db).CredentialPurposes {
		set[t] = true
	}
	out := []string{}
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
func auxiliaryTables(db *gorm.DB) []string {
	return append([]string{"oauth_grants", "request_windows", "invitation_deliveries", "invitation_continuations", "project_access_mails", "idempotency_records"}, extensions(db).AuxiliaryTables...)
}
func audit(ctx context.Context, db *gorm.DB, a Audit) error {
	if resource.IsResource(a.Resource) {
		wire, err := resource.Encode(a.Resource)
		if err != nil {
			return err
		}
		a.Snapshot, err = json.Marshal(wire)
		if err != nil {
			return err
		}
	}

	if f := extensions(db).Auditor; f != nil {
		return f.Record(ctx, db, a)
	}
	return nil
}
func record(ctx context.Context, db *gorm.DB, v any, action, aggregate string) error {
	return audit(ctx, db, Audit{Principal: principal(ctx), Action: action, Target: metadata(v), Resource: v, Outcome: "succeeded", RequestID: request(ctx).ID, Aggregate: aggregate})
}
func materialize(ctx context.Context, db *gorm.DB, a e.AccountID, p e.ProjectID) error {
	if f := extensions(db).Materialize; f != nil {
		return f(ctx, db, a, p)
	}
	return nil
}
func resourceLimit(db *gorm.DB, a e.AccountID, p e.ProjectID, n string) error {
	if f := extensions(db).ResourceLimit; f != nil {
		return f(db, a, p, n)
	}
	return nil
}
func effectiveLimit(db *gorm.DB, l *e.Limit) error {
	if f := extensions(db).EffectiveLimit; f != nil {
		return f(db, l)
	}
	return nil
}
func validGrantRole(db *gorm.DB, r e.RoleType) bool {
	for _, role := range extensions(db).GrantRoles {
		if role == r {
			return true
		}
	}
	return false
}
func agentLabel(db *gorm.DB, id e.AgentIdentityID) string {
	a, err := load[e.AgentIdentity](db, string(id))
	if err != nil {
		return string(id)
	}
	return a.Name
}

func previewTables(db *gorm.DB) []string {
	return append([]string{"accounts", "projects"}, extensions(db).PreviewTables...)
}

func credentialOwnerID(db *gorm.DB, table string) string {
	for _, k := range extensions(db).Kinds {
		if k.Table == table && k.IDColumn != "" {
			return k.IDColumn
		}
	}
	return "id"
}
