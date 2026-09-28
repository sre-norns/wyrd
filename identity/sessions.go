package identity

import (
	"context"

	expbench "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type sessionsService struct {
	db *gorm.DB
}

func (s *sessionsService) List(ctx context.Context, query manifest.SearchQuery) (result []expbench.Session, total int64, err error) {
	return list[expbench.Session](ctx, s.db, query, "")
}

func (s *sessionsService) Get(ctx context.Context, id expbench.SessionID) (resource expbench.Session, exists bool, commError error) {
	return get[expbench.Session](ctx, s.db, string(id))
}

func (s *sessionsService) CreateOrUpdate(ctx context.Context, session expbench.Session) (resource expbench.Session, created bool, commError error) {
	return upsert(ctx, s.db, session)
}

type principalService struct {
	db *gorm.DB
}

func (p *principalService) Get(ctx context.Context) (resource expbench.Principal, exists bool, commError error) {
	actor := principal(ctx)
	if actor.Type == "" {
		return actor, false, unauthenticated()
	}
	if actor.Scope == expbench.ScopeSystem {
		if !systemAuthority(ctx, database(ctx, p.db)) {
			return expbench.Principal{}, false, forbidden()
		}
		return expbench.Principal{Scope: expbench.ScopeSystem, Type: "user", UserID: actor.UserID, CredentialID: actor.CredentialID, SystemAdmin: true, SystemEntitled: true}, true, nil
	}
	actor.Scope = expbench.ScopeAccount
	actor.SystemAdmin = false
	actor.SystemEntitled = actor.Type == "user" && entitled(database(ctx, p.db), actor.UserID)
	actor.AccountRole = accountRole(ctx, p.db, actor.AccountID)
	if actor.Type == "user" {
		var memberships []expbench.ProjectMembership
		if err := p.db.Where("account_id = ? AND user_id = ? AND status = 'active'", actor.AccountID, actor.UserID).Find(&memberships).Error; err != nil {
			return actor, false, err
		}
		for _, m := range memberships {
			actor.ProjectIDs = append(actor.ProjectIDs, m.ProjectID)
		}
	} else {
		var grants []expbench.AgentAuthorization
		if err := p.db.Where("agent_id = ? AND status = 'active'", actor.AgentID).Find(&grants).Error; err != nil {
			return actor, false, err
		}
		actor.RoleGrants = map[expbench.ProjectID][]expbench.RoleType{}
		for _, g := range grants {
			actor.RoleGrants[g.ProjectID] = g.Roles
		}
	}
	return actor, true, nil
}
