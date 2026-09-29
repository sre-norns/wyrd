package identity

import (
	"context"
	"reflect"
	"strings"
	"testing"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// directoryFixture is an account owner with one project, plus the principals
// that must be refused: anonymous, a machine identity, the owner acting in
// another account, and the owner's system session.
type directoryFixture struct {
	pagingFixture
	project e.ProjectID
	machine context.Context
	refused []context.Context
}

func newDirectoryFixture(t *testing.T) directoryFixture {
	t.Helper()
	db, s := newPagingService(t)
	f := directoryFixture{pagingFixture: newPagingOwner(t, db, s, "owner@example.test", "acme")}
	project, err := s.Projects().Create(f.owner, e.Project{Resource: e.Resource{Name: "first", AccountID: f.account}})
	check(t, err)
	f.project = e.ProjectID(project.ID)
	machine, err := s.MachineIdentities().Create(f.owner, f.account, e.MachineIdentity{Resource: e.Resource{Name: "runner"}})
	check(t, err)
	token, err := s.MachineTokens().Create(f.owner, e.MachineIdentityID(machine.ID), e.MachineToken{})
	check(t, err)
	p, err := s.Authenticate(context.Background(), token.Token)
	check(t, err)
	f.machine = WithPrincipal(context.Background(), p)
	f.refused = []context.Context{
		context.Background(),
		f.machine,
		WithPrincipal(context.Background(), e.Principal{Type: "user", AccountID: "other-account", UserID: f.userID}),
		WithPrincipal(context.Background(), e.Principal{Type: "user", Scope: e.ScopeSystem, UserID: f.userID}),
	}
	return f
}

// addMember creates a user and their membership of account directly, so
// statuses that the service would never produce can be tested.
func (f directoryFixture) addMember(t *testing.T, id, email, name, membershipStatus, userStatus string, account e.AccountID) {
	t.Helper()
	check(t, f.db.Create(&e.User{ID: id, Email: email, Status: userStatus}).Error)
	check(t, f.db.Create(&e.AccountMembership{Resource: e.Resource{ID: "m-" + id, Name: name, AccountID: account, Status: membershipStatus}, UserID: id, Role: "member"}).Error)
}

func TestProjectMemberCandidates(t *testing.T) {
	f := newDirectoryFixture(t)
	dir := f.s.Directory()
	f.addMember(t, "candidate-1", "alex@example.test", "Alex Research", "active", "active", f.account)
	f.addMember(t, "candidate-2", "alexa@example.test", "Alexa", "active", "active", f.account)
	f.addMember(t, "suspended", "suspended@example.test", "Suspended", "suspended", "active", f.account)
	f.addMember(t, "inactive", "inactive@example.test", "Inactive", "active", "suspended", f.account)
	f.addMember(t, "outside", "outside@example.test", "Outside", "active", "active", "other-account")
	check(t, f.db.Create(&e.ProjectMembership{Resource: e.Resource{ID: "pm-alex", AccountID: f.account, ProjectID: f.project, Status: "suspended"}, UserID: "candidate-1"}).Error)

	for _, term := range []string{"ALEX@", "candidate-1", "RESEARCH"} {
		items, page, err := dir.ProjectMemberCandidates(f.owner, f.project, term, manifest.SearchQuery{})
		check(t, err)
		if *page.Total != 1 || len(items) != 1 || items[0].UserID != "candidate-1" || items[0].ProjectMembershipStatus != "suspended" {
			t.Fatalf("search %q: %+v / %d", term, items, *page.Total)
		}
	}
	first, page, err := dir.ProjectMemberCandidates(f.owner, f.project, "alex", manifest.SearchQuery{Limit: 1})
	check(t, err)
	second, last, err := dir.ProjectMemberCandidates(f.owner, f.project, "alex", manifest.SearchQuery{Limit: 1, Cursor: page.Next})
	check(t, err)
	// The order is the database collation's, which need not be byte order
	// ("alexa@" sorts before "alex@" under en_US): assert both, once each.
	if *page.Total != 2 || len(first) != 1 || len(second) != 1 || first[0].UserID == second[0].UserID || last.Next != "" {
		t.Fatalf("paging by email: %+v then %+v", first, second)
	}
	// Inactive memberships and users, other accounts, and wildcard or SQL text
	// never match.
	for _, term := range []string{"suspended", "inactive", "outside", "%", "_", "' OR 1=1 --"} {
		items, page, err := dir.ProjectMemberCandidates(f.owner, f.project, term, manifest.SearchQuery{})
		check(t, err)
		if *page.Total != 0 || len(items) != 0 {
			t.Fatalf("unexpected match for %q: %+v", term, items)
		}
	}

	// An ordinary account member may not search; a project administrator may,
	// without account administration.
	member := WithPrincipal(context.Background(), e.Principal{Type: "user", AccountID: f.account, UserID: "candidate-2"})
	if _, _, err = dir.ProjectMemberCandidates(member, f.project, "", manifest.SearchQuery{}); err == nil {
		t.Fatal("ordinary member can search")
	}
	check(t, f.db.Create(&e.ProjectMembership{Resource: e.Resource{ID: "pm-alexa", AccountID: f.account, ProjectID: f.project, Status: "active"}, UserID: "candidate-2"}).Error)
	_, _, err = dir.ProjectMemberCandidates(member, f.project, "", manifest.SearchQuery{})
	check(t, err)
	for i, ctx := range f.refused {
		if _, _, err = dir.ProjectMemberCandidates(ctx, f.project, "", manifest.SearchQuery{}); err == nil {
			t.Fatalf("refused principal %d searched", i)
		}
	}
	if _, _, err = dir.ProjectMemberCandidates(f.owner, f.project, strings.Repeat("x", 257), manifest.SearchQuery{}); err == nil {
		t.Fatal("oversized search accepted")
	}
	if _, _, err = dir.ProjectMemberCandidates(f.owner, f.project, "", manifest.SearchQuery{Offset: 1}); err == nil {
		t.Fatal("offset accepted")
	}
}

func TestProjectAgentCandidates(t *testing.T) {
	f := newDirectoryFixture(t)
	dir := f.s.Directory()
	for _, item := range []e.AgentIdentity{
		{Resource: e.Resource{ID: "agent-alpha", AccountID: f.account, Name: "Alpha", Status: "active"}, Description: "Latency research"},
		{Resource: e.Resource{ID: "agent-alpine", AccountID: f.account, Name: "Alpine", Status: "active"}},
		{Resource: e.Resource{ID: "hidden", AccountID: f.account, Name: "Hidden", Status: "suspended"}},
		{Resource: e.Resource{ID: "outside", AccountID: "other-account", Name: "Outside", Status: "active"}},
	} {
		check(t, f.db.Create(&item).Error)
	}
	check(t, f.db.Create(&e.AgentAuthorization{Resource: e.Resource{ID: "alpha-grant", AccountID: f.account, ProjectID: f.project, Status: "suspended"}, AgentID: "agent-alpha", Roles: []e.RoleType{"runner"}}).Error)

	for _, term := range []string{"ALPHA", "agent-alpha", "latency"} {
		items, page, err := dir.ProjectAgentCandidates(f.owner, f.project, term, manifest.SearchQuery{})
		check(t, err)
		if *page.Total != 1 || len(items) != 1 || items[0].AgentID != "agent-alpha" || items[0].AuthorizationStatus != "suspended" {
			t.Fatalf("search %q: %+v", term, items)
		}
	}
	first, page, err := dir.ProjectAgentCandidates(f.owner, f.project, "alp", manifest.SearchQuery{Limit: 1})
	check(t, err)
	second, _, err := dir.ProjectAgentCandidates(f.owner, f.project, "alp", manifest.SearchQuery{Limit: 1, Cursor: page.Next})
	check(t, err)
	if *page.Total != 2 || len(first) != 1 || len(second) != 1 || first[0].AgentID == second[0].AgentID {
		t.Fatalf("paging by name: %+v then %+v", first, second)
	}
	for _, term := range []string{"hidden", "outside", "%", "_"} {
		items, page, err := dir.ProjectAgentCandidates(f.owner, f.project, term, manifest.SearchQuery{})
		check(t, err)
		if *page.Total != 0 || len(items) != 0 {
			t.Fatalf("unexpected candidate for %q: %+v", term, items)
		}
	}

	f.addMember(t, "project-user", "project-user@example.test", "Project user", "active", "active", f.account)
	member := WithPrincipal(context.Background(), e.Principal{Type: "user", AccountID: f.account, UserID: "project-user"})
	if _, _, err = dir.ProjectAgentCandidates(member, f.project, "", manifest.SearchQuery{}); err == nil {
		t.Fatal("ordinary member can search")
	}
	check(t, f.db.Create(&e.ProjectMembership{Resource: e.Resource{ID: "pm", AccountID: f.account, ProjectID: f.project, Status: "active"}, UserID: "project-user"}).Error)
	_, _, err = dir.ProjectAgentCandidates(member, f.project, "", manifest.SearchQuery{})
	check(t, err)
	for i, ctx := range f.refused {
		if _, _, err = dir.ProjectAgentCandidates(ctx, f.project, "", manifest.SearchQuery{}); err == nil {
			t.Fatalf("refused principal %d searched", i)
		}
	}
}

func TestAgentProjectAuthorizations(t *testing.T) {
	f := newDirectoryFixture(t)
	dir := f.s.Directory()
	agent, err := f.s.MachineIdentities().Create(f.owner, f.account, e.MachineIdentity{Resource: e.Resource{Name: "granted"}})
	check(t, err)
	agentID := e.AgentIdentityID(agent.ID)
	for _, p := range []e.Project{
		{Resource: e.Resource{ID: "alpha", AccountID: f.account, Name: "Alpha", Status: "archived"}},
		{Resource: e.Resource{ID: "beta", AccountID: f.account, Name: "Beta", Status: "active"}},
		{Resource: e.Resource{ID: "outside", AccountID: "other-account", Name: "Outside", Status: "active"}},
	} {
		check(t, f.db.Create(&p).Error)
	}
	for _, a := range []e.AgentAuthorization{
		{Resource: e.Resource{ID: "alpha-grant", AccountID: f.account, ProjectID: "alpha", Status: "suspended"}, AgentID: agentID, Roles: []e.RoleType{"runner"}},
		{Resource: e.Resource{ID: "beta-grant", AccountID: f.account, ProjectID: "beta", Status: "revoked"}, AgentID: agentID, Roles: []e.RoleType{"reader"}},
		// A malformed cross-account link must not expose another account's project.
		{Resource: e.Resource{ID: "outside-grant", AccountID: f.account, ProjectID: "outside", Status: "active"}, AgentID: agentID, Roles: []e.RoleType{"runner"}},
	} {
		check(t, f.db.Create(&a).Error)
	}

	items, page, err := dir.AgentProjectAuthorizations(f.owner, agentID, manifest.SearchQuery{Limit: 1})
	check(t, err)
	if *page.Total != 2 || len(items) != 1 || items[0].ProjectID != "alpha" || items[0].ProjectName != "Alpha" || items[0].ProjectStatus != "archived" || items[0].AuthorizationStatus != "suspended" || !reflect.DeepEqual(items[0].Roles, []e.RoleType{"runner"}) {
		t.Fatalf("first page: %d %+v", *page.Total, items)
	}
	items, last, err := dir.AgentProjectAuthorizations(f.owner, agentID, manifest.SearchQuery{Limit: 1, Cursor: page.Next})
	check(t, err)
	if len(items) != 1 || items[0].ProjectID != "beta" || items[0].AuthorizationStatus != "revoked" || last.Next != "" {
		t.Fatalf("second page: %+v", items)
	}

	other, err := f.s.MachineIdentities().Create(f.owner, f.account, e.MachineIdentity{Resource: e.Resource{Name: "unassigned"}})
	check(t, err)
	items, page, err = dir.AgentProjectAuthorizations(f.owner, e.AgentIdentityID(other.ID), manifest.SearchQuery{})
	check(t, err)
	if *page.Total != 0 || items == nil || len(items) != 0 {
		t.Fatal("an unassigned identity must list an empty collection, not null")
	}

	f.addMember(t, "member", "member@example.test", "Member", "active", "active", f.account)
	check(t, f.db.Create(&e.ProjectMembership{Resource: e.Resource{ID: "member-project", AccountID: f.account, ProjectID: f.project, Status: "active"}, UserID: "member"}).Error)
	member := WithPrincipal(context.Background(), e.Principal{Type: "user", AccountID: f.account, UserID: "member"})
	for i, ctx := range append(f.refused, member) {
		if _, _, err = dir.AgentProjectAuthorizations(ctx, agentID, manifest.SearchQuery{}); err == nil {
			t.Fatalf("refused principal %d listed grants", i)
		}
	}
	if _, _, err = dir.AgentProjectAuthorizations(f.owner, "missing", manifest.SearchQuery{}); err == nil {
		t.Fatal("missing identity accepted")
	}
}
