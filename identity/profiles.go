package identity

import (
	"context"
	"strings"
	"unicode/utf8"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type personalProfileService struct {
	db *gorm.DB
}

func profileUser(ctx context.Context, db *gorm.DB) (user, error) {
	actor := principal(ctx)
	if actor.Type == "" {
		return user{}, unauthenticated()
	}
	if actor.Type != "user" || actor.UserID == "" {
		return user{}, forbidden()
	}
	var value user
	if err := database(ctx, db).Where("id = ?", actor.UserID).First(&value).Error; err != nil || value.Status != "active" {
		return user{}, unauthenticated()
	}
	return value, nil
}

func publicProfile(value user) e.PersonalProfile {
	return e.PersonalProfile{
		UserID:         value.ID,
		LastModifiedBy: value.LastModifiedBy,
		Email:          value.Email,
		DisplayName:    value.DisplayName,
		Status:         value.Status,
		Revision:       value.Revision,
	}
}

func (p *personalProfileService) Get(ctx context.Context) (resource e.PersonalProfile, exists bool, commError error) {
	value, err := profileUser(ctx, p.db)
	if err != nil {
		return resource, false, err
	}
	return publicProfile(value), true, nil
}

// profileAccountOrder lists a user's accounts by name, as a switcher shows
// them; the account ID breaks ties between accounts of the same name.
var profileAccountOrder = dbstore.Keyset[e.ProfileAccount]{
	Columns: []dbstore.KeyColumn{{Expr: "accounts.name"}, {Expr: "account_memberships.account_id"}},
	Key:     func(a *e.ProfileAccount) []any { return []any{a.Name, a.AccountID} },
}

func (p *personalProfileService) AccessibleAccounts(ctx context.Context, q manifest.SearchQuery) ([]e.ProfileAccount, manifest.Page, error) {
	u, err := profileUser(ctx, p.db)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	query := database(ctx, p.db).
		Table("account_memberships").
		Joins("JOIN accounts ON accounts.id = account_memberships.account_id").
		Where("account_memberships.user_id = ? AND account_memberships.status = 'active' AND accounts.status = 'active'", u.ID)
	total, err := countOf(query)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	keys := profileAccountOrder
	keys.NoTotal = true
	accounts, page, err := dbstore.PageBy(
		query.Select("account_memberships.account_id AS account_id, accounts.name AS name, account_memberships.role AS role, accounts.status AS status"),
		q, keys)
	if err != nil {
		return nil, manifest.Page{}, pageError(err)
	}
	page.Total = total
	return accounts, page, nil
}

func profilePrecondition(ctx context.Context, revision int64) error {
	match := request(ctx).IfMatch
	if match == "" {
		return problem(428, "precondition-required", "If-Match is required.")
	}
	if match != ETag(revision) {
		return problem(412, "precondition-failed", "The resource has changed.")
	}
	return nil
}

func validDisplayName(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" || !utf8.ValidString(trimmed) || len([]byte(trimmed)) > 200 {
		return nil, invalidFields("The display name must contain 1 to 200 UTF-8 bytes.", map[string]string{
			"display_name": "Enter a display name of 1 to 200 UTF-8 bytes, or clear the field.",
		})
	}
	return &trimmed, nil
}

func (p *personalProfileService) Update(ctx context.Context, profile e.PersonalProfile) (resource e.PersonalProfile, commError error) {
	if _, err := profileUser(ctx, p.db); err != nil {
		return resource, err
	}
	displayName, err := validDisplayName(profile.DisplayName)
	if err != nil {
		return resource, err
	}
	err = mutation(ctx, p.db, func(tx *gorm.DB) error {
		current, err := profileUser(ctx, tx)
		if err != nil {
			return err
		}
		if err := profilePrecondition(ctx, current.Revision); err != nil {
			return err
		}
		result := tx.Model(&user{}).
			Where("id = ? AND revision = ?", current.ID, current.Revision).
			Updates(systemMutation(ctx, map[string]any{"display_name": displayName, "revision": current.Revision + 1}))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return problem(412, "precondition-failed", "The resource has changed.")
		}
		return nil
	})
	if err != nil {
		return resource, err
	}
	value, err := profileUser(ctx, p.db)
	if err != nil {
		return resource, err
	}
	return publicProfile(value), nil
}
