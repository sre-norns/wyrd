package identity

import (
	"context"
	"encoding/json"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func mutationActor(ctx context.Context) e.Principal {
	actor := principal(ctx)
	if actor.Type == "" {
		actor = e.Principal{Type: "service"}
	}
	return actor
}
func publicActor(p e.Principal) e.ResourceActor {
	actor := e.ResourceActor{Type: p.Type}
	switch p.Type {
	case "user":
		actor.UserID = p.UserID
	case "agent":
		actor.AgentID = p.AgentID
	}
	return actor
}
func actorJSON(value any) clause.Expr {
	data, _ := json.Marshal(value)
	return gorm.Expr("?::jsonb", string(data))
}
func resourceMutation(ctx context.Context, changes map[string]any) map[string]any {
	changes["actor"] = actorJSON(mutationActor(ctx))
	return changes
}
func systemMutation(ctx context.Context, changes map[string]any) map[string]any {
	changes["last_modified_by"] = actorJSON(publicActor(mutationActor(ctx)))
	return changes
}
func touchSystem(ctx context.Context, r *e.SystemRecord, at time.Time) {
	r.Revision++
	r.UpdatedAt = at
	r.LastModifiedBy = publicActor(mutationActor(ctx))
}
