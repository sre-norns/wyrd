package manifest

import (
	"fmt"
	"net/http"
)

// Scope says where the resources of a kind live, and so which of
// [ObjectMeta.Account] and [ObjectMeta.Project] they carry.
type Scope string

const (
	// ScopeSystem resources belong to the installation, not to any tenant. It is
	// the scope of a kind registered without one, so existing kinds are unchanged.
	ScopeSystem Scope = "system"
	// ScopeAccount resources belong to one account and to no project in it.
	ScopeAccount Scope = "account"
	// ScopeProject resources belong to one project, and through it to its account.
	ScopeProject Scope = "project"
)

var (
	// ErrInvalidScope is returned when a scope reference does not fit the scope
	// of the kind it is used with: a project-scoped resource without a project,
	// or a system-scoped one naming an account.
	ErrInvalidScope error = NewStatusError(http.StatusBadRequest, "invalid-scope", "invalid resource scope")

	// ErrScopeMismatch is returned when a resource already names a scope other
	// than the one it is being placed in. The scope comes from the request, and
	// a body naming a different one is refused rather than silently corrected:
	// correcting it would make applying a manifest copied from another project
	// appear to succeed while writing somewhere its author did not mean.
	ErrScopeMismatch error = NewStatusError(http.StatusBadRequest, "scope-mismatch", "resource scope does not match the request")
)

// ScopeRef identifies the account and project a resource belongs to.
type ScopeRef struct {
	Account ResourceID `form:"account,omitempty" json:"account,omitempty" yaml:"account,omitempty"`
	Project ResourceID `form:"project,omitempty" json:"project,omitempty" yaml:"project,omitempty"`
}

// IsZero reports whether the reference names no scope at all.
func (r ScopeRef) IsZero() bool {
	return r.Account == "" && r.Project == ""
}

// Validate reports whether the reference fits resources of the given scope.
func (r ScopeRef) Validate(scope Scope) error {
	switch scope {
	case ScopeSystem, "":
		if !r.IsZero() {
			return fmt.Errorf("%w: a system resource names no account or project", ErrInvalidScope)
		}
	case ScopeAccount:
		if r.Account == "" || r.Project != "" {
			return fmt.Errorf("%w: an account resource names an account and no project", ErrInvalidScope)
		}
	case ScopeProject:
		if r.Account == "" || r.Project == "" {
			return fmt.Errorf("%w: a project resource names both its account and project", ErrInvalidScope)
		}
	default:
		return fmt.Errorf("%w: unknown scope %q", ErrInvalidScope, scope)
	}

	return nil
}

// Scope returns the account and project the resource belongs to.
func (m ObjectMeta) Scope() ScopeRef {
	return ScopeRef{Account: m.Account, Project: m.Project}
}

// ApplyScope places the resource in ref.
//
// A field the resource leaves empty is filled from ref. A field it already sets
// must equal ref's, or [ErrScopeMismatch] is returned and nothing changes.
func (m *ObjectMeta) ApplyScope(ref ScopeRef) error {
	if m.Account != "" && m.Account != ref.Account {
		return fmt.Errorf("%w: account %q, request is for %q", ErrScopeMismatch, m.Account, ref.Account)
	}
	if m.Project != "" && m.Project != ref.Project {
		return fmt.Errorf("%w: project %q, request is for %q", ErrScopeMismatch, m.Project, ref.Project)
	}

	m.Account = ref.Account
	m.Project = ref.Project

	return nil
}

// KindOption adjusts how a kind is registered.
type KindOption func(*KindSpec)

// WithScope declares the scope of a kind's resources.
func WithScope(scope Scope) KindOption {
	return func(k *KindSpec) {
		k.Scope = scope
	}
}

// ResourceScope returns the scope of the kind's resources: [ScopeSystem] when
// the kind was registered without one.
func (k KindSpec) ResourceScope() Scope {
	if k.Scope == "" {
		return ScopeSystem
	}
	return k.Scope
}

// ScopeOf returns the scope of a registered kind.
func ScopeOf(kind Kind) (Scope, bool) {
	spec, known := LookupKind(kind)
	if !known {
		return "", false
	}
	return spec.ResourceScope(), true
}
