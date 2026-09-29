package identity

import (
	"context"
	"strings"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type directoryService struct {
	db *gorm.DB
}

// searchTerm bounds a candidate search. strpos, not LIKE, matches it, so
// wildcard characters are literal search text.
func searchTerm(term string) (string, error) {
	term = strings.TrimSpace(term)
	if len(term) > 256 {
		return "", invalid("Search text must contain at most 256 bytes.")
	}
	return term, nil
}

// projectAccess authorises reading a project's access: whoever may read its
// memberships may search candidates for it. Project administrators receive
// identity labels, not account administration data.
func projectAccess(ctx context.Context, db *gorm.DB, projectID e.ProjectID) (e.Project, error) {
	project, err := load[e.Project](db, string(projectID))
	if err != nil {
		return project, err
	}
	return project, authorize(ctx, db, &e.ProjectMembership{Resource: e.Resource{AccountID: project.AccountID, ProjectID: projectID}}, false)
}

func (d *directoryService) ProjectMemberCandidates(ctx context.Context, projectID e.ProjectID, term string, q manifest.SearchQuery) ([]e.ProjectMemberCandidate, manifest.Page, error) {
	db := database(ctx, d.db)
	project, err := projectAccess(ctx, db, projectID)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	if term, err = searchTerm(term); err != nil {
		return nil, manifest.Page{}, err
	}
	query := db.Table("account_memberships AS m").
		Joins("JOIN users AS u ON u.id = m.user_id").
		Joins("LEFT JOIN project_memberships AS pm ON pm.user_id = m.user_id AND pm.project_id = ? AND pm.account_id = m.account_id", projectID).
		Where("m.account_id = ? AND m.status = 'active' AND u.status = 'active'", project.AccountID)
	if term != "" {
		query = query.Where("strpos(lower(u.email), lower(?)) > 0 OR strpos(lower(u.id), lower(?)) > 0 OR strpos(lower(m.name), lower(?)) > 0", term, term, term)
	}
	return PageByText[e.ProjectMemberCandidate](query, q, "u.id AS user_id, u.email, m.name, COALESCE(pm.status, '') AS project_membership_status", "lower(u.email)", "u.id")
}

func (d *directoryService) ProjectAgentCandidates(ctx context.Context, projectID e.ProjectID, term string, q manifest.SearchQuery) ([]e.ProjectAgentCandidate, manifest.Page, error) {
	db := database(ctx, d.db)
	project, err := projectAccess(ctx, db, projectID)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	if term, err = searchTerm(term); err != nil {
		return nil, manifest.Page{}, err
	}
	query := db.Table("agent_identities AS a").
		Joins("LEFT JOIN agent_authorizations AS pa ON pa.agent_id = a.id AND pa.project_id = ? AND pa.account_id = a.account_id", projectID).
		Where("a.account_id = ? AND a.status = 'active'", project.AccountID)
	if term != "" {
		query = query.Where("strpos(lower(a.name), lower(?)) > 0 OR strpos(lower(a.id), lower(?)) > 0 OR strpos(lower(a.description), lower(?)) > 0", term, term, term)
	}
	return PageByText[e.ProjectAgentCandidate](query, q, "a.id AS agent_id, a.name, a.description, COALESCE(pa.status, '') AS authorization_status", "lower(a.name)", "a.id")
}

// AgentProjectAuthorizations exposes grant metadata to whoever may read the
// machine identity. Project content and grant changes keep their own checks.
func (d *directoryService) AgentProjectAuthorizations(ctx context.Context, agentID e.AgentIdentityID, q manifest.SearchQuery) ([]e.AgentProjectAuthorization, manifest.Page, error) {
	db := database(ctx, d.db)
	agent, err := load[e.AgentIdentity](db, string(agentID))
	if err != nil {
		return nil, manifest.Page{}, err
	}
	if err = authorize(ctx, db, &agent, false); err != nil {
		return nil, manifest.Page{}, err
	}
	query := db.Table("agent_authorizations AS a").
		Joins("JOIN projects AS p ON p.id = a.project_id AND p.account_id = a.account_id").
		Where("a.agent_id = ? AND a.account_id = ?", agentID, agent.AccountID)
	return PageByText[e.AgentProjectAuthorization](query, q, "p.id AS project_id, p.name AS project_name, p.status AS project_status, a.status AS authorization_status, a.roles", "lower(p.name)", "p.id")
}
