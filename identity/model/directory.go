package model

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ProjectMemberCandidate contains only identity data needed to select a project administrator.
type ProjectMemberCandidate struct {
	UserID                  string `json:"user_id"`
	Email                   string `json:"email"`
	Name                    string `json:"name"`
	ProjectMembershipStatus string `json:"project_membership_status"`
}

// ProjectAgentCandidate contains safe identity data for machine-identity selection.
type ProjectAgentCandidate struct {
	AgentID             AgentIdentityID `json:"agent_id"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	AuthorizationStatus string          `json:"authorization_status"`
}

// AgentProjectAuthorization contains account administration metadata, not project content.
type AgentProjectAuthorization struct {
	ProjectID           ProjectID  `json:"project_id"`
	ProjectName         string     `json:"project_name"`
	ProjectStatus       string     `json:"project_status"`
	AuthorizationStatus string     `json:"authorization_status"`
	Roles               []RoleType `json:"roles" gorm:"serializer:json;type:jsonb"`
}

// DirectoryService is the read models behind project access management: who
// may be added to a project, and where a machine identity is granted. Each is a
// read over identity records; none mutates state.
type DirectoryService interface {
	// ProjectMemberCandidates searches active members of the project's account.
	ProjectMemberCandidates(ctx context.Context, projectID ProjectID, term string, query manifest.SearchQuery) (items []ProjectMemberCandidate, page manifest.Page, err error)
	// ProjectAgentCandidates searches active machine identities of the project's account.
	ProjectAgentCandidates(ctx context.Context, projectID ProjectID, term string, query manifest.SearchQuery) (items []ProjectAgentCandidate, page manifest.Page, err error)
	// AgentProjectAuthorizations lists a machine identity's project grants for an account administrator.
	AgentProjectAuthorizations(ctx context.Context, agentID AgentIdentityID, query manifest.SearchQuery) (items []AgentProjectAuthorization, page manifest.Page, err error)
}
