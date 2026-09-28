package identity

import (
	"context"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

func decorate(ctx context.Context, db *gorm.DB, v any) error {
	m := metadata(v)
	if path := extensions(db).Kinds[kind(v)].Path; path != "" {
		m.Links = map[string]string{"self": "/v1/" + path + "/" + m.ID}
	}

	switch r := v.(type) {
	case *e.AccountInvitation:
		if err := decorateInvitation(db, r); err != nil {
			return err
		}
		if r.Status == "pending" {
			t, err := now(db)
			if err != nil {
				return err
			}
			if !r.ExpiresAt.After(t) {
				r.Status = "expired"
			}
		}
	case *e.AgentAuthorization:
		if r.Name == "" {
			r.Name = agentLabel(db, r.AgentID)
		}
		// Active-work state is reported separately from the authorized roles.
		if f := extensions(db).DecorateGrant; f != nil {
			return f(ctx, db, r)
		}
	case *e.AccountMembership:
		email, name, err := userDisplay(db, r.UserID)
		if err != nil {
			return err
		}
		r.Email, r.DisplayName = email, name
	case *e.ProjectMembership:
		email, name, err := userDisplay(db, r.UserID)
		if err != nil {
			return err
		}
		r.Email, r.DisplayName = email, name
	}
	return nil
}

func userDisplay(db *gorm.DB, userID string) (string, *string, error) {
	if userID == "" {
		return "", nil, nil
	}
	u, err := load[user](db, userID)
	if err != nil {
		return "", nil, nil
	}
	return u.Email, u.DisplayName, nil
}
