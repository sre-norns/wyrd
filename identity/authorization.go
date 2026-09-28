package identity

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

func accountRole(ctx context.Context, db *gorm.DB, account e.AccountID) string {
	db = db.Session(&gorm.Session{NewDB: true})
	p := principal(ctx)
	if p.Type != "user" || p.AccountID != account {
		return ""
	}
	var m e.AccountMembership
	if db.Where("account_id = ? AND user_id = ? AND status = 'active'", account, p.UserID).First(&m).Error != nil {
		return ""
	}
	return m.Role
}

func accountAdmin(ctx context.Context, db *gorm.DB, account e.AccountID) bool {
	return slices.Contains([]string{"owner", "admin"}, accountRole(ctx, db, account))
}

func projectAdmin(ctx context.Context, db *gorm.DB, project e.ProjectID) bool {
	db = db.Session(&gorm.Session{NewDB: true})
	p := principal(ctx)
	if p.Type != "user" {
		return false
	}
	var n int64
	db.Model(&e.ProjectMembership{}).Where("project_id = ? AND account_id = ? AND user_id = ? AND status = 'active'", project, p.AccountID, p.UserID).Count(&n)
	return n > 0 && accountRole(ctx, db, p.AccountID) != ""
}

func agentGrant(ctx context.Context, db *gorm.DB, project e.ProjectID, role e.RoleType) bool {
	db = db.Session(&gorm.Session{NewDB: true})
	p := principal(ctx)
	if p.Type != "agent" {
		return false
	}
	var a e.AgentAuthorization
	if db.Where("project_id = ? AND account_id = ? AND agent_id = ? AND status = 'active'", project, p.AccountID, p.AgentID).First(&a).Error != nil {
		return false
	}
	return role == "" || slices.Contains(a.Roles, role)
}

func authorize(ctx context.Context, db *gorm.DB, v any, write bool) error {
	p := principal(ctx)
	if p.Type == "" {
		return unauthenticated()
	}
	m := metadata(v)
	k := kind(v)
	if _, ok := extensions(db).Kinds[k]; !ok {
		return missing()
	}
	policy := extensions(db).Kinds[k]
	if k != "Session" && ((policy.Scope == "project" && (m.ProjectID == "" || m.AccountID == "")) || (policy.Scope == "account" && (m.AccountID == "" || m.ProjectID != ""))) {
		return missing()
	}
	if p.Scope == e.ScopeSystem {
		if !systemAuthority(ctx, database(ctx, db)) {
			return forbidden()
		}
		if k == "Session" {
			if m.ID == p.CredentialID {
				return nil
			}
			return missing()
		}
		if m.ProjectID != "" {
			return missing()
		}
		if extensions(db).Kinds[k].SystemManageAccount || (m.AccountID == "" && extensions(db).Kinds[k].SystemRead) {
			return nil
		}
		return missing()
	}
	if k == "Account" {
		if m.ID == "" {
			if p.Type != "user" {
				return forbidden()
			}
			return nil
		}
		if systemAuthority(ctx, database(ctx, db)) {
			return nil
		}
		if write && accountRole(ctx, db, e.AccountID(m.ID)) != "owner" {
			return forbidden()
		}
		if accountRole(ctx, db, e.AccountID(m.ID)) == "" {
			return missing()
		}
		return nil
	}
	if k == "Session" {
		s := v.(*e.Session)
		if s.UserID != p.UserID || s.AccountID != p.AccountID || p.Type != "user" {
			return missing()
		}
		return nil
	}
	if !write {
		if f := extensions(db).Kinds[k].Admit; f != nil && f(ctx, db, v) {
			return nil
		}
	}
	if m.ProjectID != "" {
		project, err := load[e.Project](db, string(m.ProjectID))
		if err != nil {
			return err
		}
		if project.AccountID != m.AccountID || m.AccountID != p.AccountID {
			return missing()
		}
		if k == "ProjectMembership" && accountAdmin(ctx, db, m.AccountID) {
			return nil
		}
		if projectAdmin(ctx, db, m.ProjectID) {
			return nil
		}
		if !write && agentGrant(ctx, db, m.ProjectID, "") && extensions(db).Kinds[k].MachineRead {
			return nil
		}
		return missing()
	}
	if k == "Project" {
		if m.ID == "" && accountAdmin(ctx, db, m.AccountID) {
			return nil
		}
		if projectAdmin(ctx, db, e.ProjectID(m.ID)) {
			return nil
		}
		if !write && (accountAdmin(ctx, db, m.AccountID) || agentGrant(ctx, db, e.ProjectID(m.ID), "")) {
			return nil
		}
		return missing()
	}
	if m.AccountID != "" {
		if m.AccountID != p.AccountID {
			if systemAuthority(ctx, database(ctx, db)) && extensions(db).Kinds[k].SystemManageAccount {
				return nil
			}
			return missing()
		}
		if extensions(db).Kinds[k].SystemManageAccount && write {
			if systemAuthority(ctx, database(ctx, db)) {
				return nil
			}
			return forbidden()
		}
		if k == "AccountMembership" && write {
			mem := v.(*e.AccountMembership)
			if mem.Role == "owner" && accountRole(ctx, db, m.AccountID) != "owner" {
				return forbidden()
			}
		}
		if !accountAdmin(ctx, db, m.AccountID) {
			return forbidden()
		}
		return nil
	}
	if !write && extensions(db).Kinds[k].PublicSystemRead {
		return nil
	}
	if systemAuthority(ctx, database(ctx, db)) {
		return nil
	}
	return forbidden()
}

func visible(ctx context.Context, db *gorm.DB, k string) (*gorm.DB, error) {
	if _, ok := extensions(db).Kinds[k]; !ok {
		return nil, missing()
	}
	p := principal(ctx)
	if p.Scope == e.ScopeSystem {
		if !systemAuthority(ctx, database(ctx, db)) {
			return nil, forbidden()
		}
		if k == "Session" {
			return db.Where("id = ?", p.CredentialID), nil
		}
		if extensions(db).Kinds[k].SystemManageAccount {
			return db.Where("project_id = ''"), nil
		}
		if extensions(db).Kinds[k].SystemRead {
			return db.Where("account_id = '' AND project_id = ''"), nil
		}
		return nil, missing()
	}
	if p.Type == "" {
		return nil, unauthenticated()
	}
	switch k {
	case "Session":
		if p.Type != "user" {
			return nil, forbidden()
		}
		return db.Where("account_id = ? AND user_id = ?", p.AccountID, p.UserID), nil
	case "Account":
		if systemAuthority(ctx, database(ctx, db)) {
			return db, nil
		}
		return db.Where("id = ? AND id IN (SELECT account_id FROM account_memberships WHERE user_id = ? AND status = 'active')", p.AccountID, p.UserID), nil
	case "Project":
		if accountAdmin(ctx, db, p.AccountID) {
			return db.Where("account_id = ?", p.AccountID), nil
		}
		if p.Type == "agent" {
			return db.Where("account_id = ? AND id IN (SELECT project_id FROM agent_authorizations WHERE agent_id = ? AND status = 'active')", p.AccountID, p.AgentID), nil
		}
		return db.Where("account_id = ? AND id IN (SELECT project_id FROM project_memberships WHERE user_id = ? AND status = 'active')", p.AccountID, p.UserID), nil
	}
	if extensions(db).Kinds[k].SystemManageAccount && systemAuthority(ctx, database(ctx, db)) {
		return db, nil
	}
	projectSub := db.Session(&gorm.Session{NewDB: true}).Model(&e.ProjectMembership{}).Select("project_id").Where("user_id = ? AND account_id = ? AND status = 'active'", p.UserID, p.AccountID)
	if p.Type == "agent" {
		policy, ok := extensions(db).Kinds[k]
		if !ok {
			return nil, missing()
		}
		if policy.MachineFilter != nil {
			return policy.MachineFilter(ctx, db), nil
		}
		if !policy.MachineRead {
			return db.Where("FALSE"), nil
		}
		projectSub = db.Session(&gorm.Session{NewDB: true}).Model(&e.AgentAuthorization{}).Select("project_id").Where("agent_id = ? AND status = 'active'", p.AgentID)
	}
	accountAccess := accountAdmin(ctx, db, p.AccountID)
	systemAccess := systemAuthority(ctx, database(ctx, db)) || extensions(db).Kinds[k].PublicSystemRead
	return db.Where("(account_id = ? AND (project_id IN (?) OR (project_id = '' AND ?))) OR (account_id = '' AND project_id = '' AND ?)", p.AccountID, projectSub, accountAccess, systemAccess), nil
}

func prepare(ctx context.Context, db *gorm.DB, v any, creating bool) error {
	m := metadata(v)
	if err := m.Labels.Validate(); err != nil {
		return invalid(err.Error())
	}
	if m.ProjectID != "" {
		p, err := load[e.Project](db, string(m.ProjectID))
		if err != nil {
			return err
		}
		if m.AccountID != "" && m.AccountID != p.AccountID {
			return invalid("Account does not own project.")
		}
		m.AccountID = p.AccountID
	}
	if creating && m.AccountID != "" {
		a, err := load[e.Account](db, string(m.AccountID))
		if err != nil {
			return err
		}
		if a.Status != "active" {
			return conflict("account-inactive")
		}
	}
	if !slices.Contains([]string{"", "active", "inactive", "suspended", "revoked", "released", "pending"}, m.Status) {
		return invalid("Unsupported lifecycle status.")
	}
	if !creating {
		switch r := v.(type) {
		case *e.AgentAuthorization:
			old, err := load[e.AgentAuthorization](db, r.ID)
			if err != nil {
				return err
			}
			if r.AgentID != old.AgentID {
				return invalid("Agent identity is immutable.")
			}
		case *e.AgentIdentityToken:
			old, err := load[e.AgentIdentityToken](db, r.ID)
			if err != nil {
				return err
			}
			if r.AgentID != old.AgentID {
				return invalid("Token identity is immutable.")
			}
		case *e.Project:
			old, err := load[e.Project](db, r.ID)
			if err != nil {
				return err
			}
			if r.CurrentContextID != old.CurrentContextID {
				return invalid("Create a context version to change project context.")
			}
		case *e.ProjectMembership:
			old, err := load[e.ProjectMembership](db, r.ID)
			if err != nil {
				return err
			}
			if r.UserID != old.UserID {
				return invalid("Membership identity is immutable.")
			}
		}
	}
	switch r := v.(type) {
	case *e.Account:
		r.Name = strings.TrimSpace(r.Name)
		if !creating {
			old, err := load[e.Account](db, r.ID)
			if err != nil {
				return err
			}
			if r.Status != old.Status {
				return invalid("Use the confirmed account lifecycle operation.")
			}
		}
		if r.Name == "" || len(r.Name) > 200 {
			return invalid("Enter an account name of 1 to 200 bytes.")
		}
		var collisions int64
		if err := db.Model(&e.Account{}).Where("lower(btrim(name)) = lower(?) AND id <> ?", r.Name, r.ID).Count(&collisions).Error; err != nil {
			return err
		}
		if collisions > 0 {
			return problem(409, "account-name-in-use", "This account name is already in use. Choose another name.")
		}
		if creating {
			r.AccountID = ""
			r.ProjectID = ""
			if err := resourceLimit(db, "", "", "accounts"); err != nil {
				return err
			}
			r.Status = "active"
		}
	case *e.Project:
		if r.Name == "" {
			return invalid("Project name is required.")
		}
		if creating {
			r.Status = "active"
			r.CurrentContextID = ""
			if err := resourceLimit(db, r.AccountID, "", "projects"); err != nil {
				return err
			}
		}
		if fn := extensions(db).PrepareProject; fn != nil {
			return fn(ctx, db, r, creating)
		}
	case *e.AccountMembership:
		if !slices.Contains([]string{"owner", "admin", "member"}, r.Role) || r.UserID == "" {
			return invalid("A user and supported account role are required.")
		}
		if _, err := load[user](db, r.UserID); err != nil {
			return err
		}
		if !creating {
			old, err := load[e.AccountMembership](db, r.ID)
			if err != nil {
				return err
			}
			if old.UserID != r.UserID {
				return invalid("Membership identity is immutable.")
			}
			if old.Role == "owner" && accountRole(ctx, db, r.AccountID) != "owner" {
				return forbidden()
			}
			if old.Role == "owner" && old.Status == "active" && (r.Role != "owner" || r.Status != "active") {
				var n int64
				db.Model(&e.AccountMembership{}).Where("account_id = ? AND role = 'owner' AND status = 'active'", r.AccountID).Count(&n)
				if n <= 1 {
					return conflict("last-owner")
				}
			}
		}
	case *e.ProjectMembership:
		var n int64
		db.Model(&e.AccountMembership{}).Where("account_id = ? AND user_id = ? AND status = 'active'", r.AccountID, r.UserID).Count(&n)
		if n == 0 {
			return invalid("Project membership requires an active account member.")
		}
	case *e.AgentIdentity:
		if r.Name == "" {
			return invalid("Identity name is required.")
		}
		if creating {
			return resourceLimit(db, r.AccountID, "", "agent-identities")
		}
	case *e.AgentIdentityToken:
		a, err := load[e.AgentIdentity](db, string(r.AgentID))
		if err != nil {
			return err
		}
		r.AccountID = a.AccountID
		if creating && a.Status != "active" {
			return conflict("identity-inactive")
		}
		if r.ExpiresAt != nil && !r.ExpiresAt.After(time.Now()) {
			return invalid("Token expiry must be in the future.")
		}
		if !creating && r.Status != "revoked" {
			return invalid("Only token revocation is supported.")
		}
	case *e.AgentAuthorization:
		if creating {
			if err := resourceLimit(db, r.AccountID, r.ProjectID, "agent-authorizations"); err != nil {
				return err
			}
		}
		a, err := load[e.AgentIdentity](db, string(r.AgentID))
		if err != nil {
			return err
		}
		if a.AccountID != r.AccountID {
			return missing()
		}
		if len(r.Roles) == 0 {
			return invalid("At least one role is required.")
		}
		for _, role := range r.Roles {
			if !validGrantRole(db, role) {
				return invalid("Unsupported role.")
			}
		}
	case *e.Session:
		if creating || r.Status != "revoked" {
			return invalid("Only session revocation is supported.")
		}
	default:
		return invalid(fmt.Sprintf("Direct mutation of %s is unsupported.", kind(v)))
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

func afterCreate(ctx context.Context, db *gorm.DB, v any) error {
	switch r := v.(type) {
	case *e.Account:
		if err := materialize(ctx, db, e.AccountID(r.ID), ""); err != nil {
			return err
		}
		m := e.AccountMembership{UserID: principal(ctx).UserID, Role: "owner"}
		m.AccountID = e.AccountID(r.ID)
		return insert(ctx, db, &m)
	case *e.Project:
		if err := materialize(ctx, db, r.AccountID, e.ProjectID(r.ID)); err != nil {
			return err
		}
		m := e.ProjectMembership{UserID: principal(ctx).UserID}
		m.AccountID = r.AccountID
		m.ProjectID = e.ProjectID(r.ID)
		return insert(ctx, db, &m)
	case *e.AgentIdentityToken:
		r.Token = secret()
		return db.Create(&credential{ID: newID(), OwnerID: r.ID, Kind: "agent", Verifier: digest(r.Token)}).Error
	}
	return nil
}

func creationScope(db *gorm.DB, v any) error {
	m := metadata(v)
	switch kind(v) {
	case "Account":
		if m.AccountID != "" || m.ProjectID != "" {
			return invalid("An account cannot have a parent scope.")
		}
	case "Project", "AccountMembership", "AccountInvitation", "AgentIdentity", "AgentIdentityToken", "Session":
		if m.ProjectID != "" {
			return invalid("This resource has account scope.")
		}
	}
	if r, ok := v.(*e.AgentIdentityToken); ok {
		a, err := load[e.AgentIdentity](db, string(r.AgentID))
		if err != nil {
			return err
		}
		if r.AccountID != "" && r.AccountID != a.AccountID {
			return missing()
		}
		r.AccountID = a.AccountID
	}
	if m.ProjectID != "" {
		p, err := load[e.Project](db, string(m.ProjectID))
		if err != nil {
			return err
		}
		if m.AccountID != "" && m.AccountID != p.AccountID {
			return missing()
		}
		m.AccountID = p.AccountID
	}
	return nil
}
