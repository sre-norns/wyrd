package identity

import (
	"strings"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

func SystemInvitationProjection(i e.AccountInvitation) e.SystemInvitation {
	if i.Status == "pending" && !i.ExpiresAt.After(time.Now()) {
		i.Status = "expired"
	}
	return e.SystemInvitation{SystemRecord: systemRecord(i.Resource), AccountID: i.AccountID, Email: i.Email, Role: i.Role, ExpiresAt: i.ExpiresAt, Delivery: i.Delivery, EmailDelivery: i.EmailDelivery}
}

const invitationStatusSQL = "CASE WHEN status = 'pending' AND expires_at <= CURRENT_TIMESTAMP THEN 'expired' ELSE status END"

func invitationSortColumn(field string) (string, error) {
	switch field {
	case "id", "email", "role", "created_at", "expires_at":
		return field, nil
	case "", "created":
		return "created_at", nil
	case "status":
		return "(" + invitationStatusSQL + ")", nil
	case "email_status":
		return "COALESCE((SELECT state FROM invitation_deliveries WHERE invitation_id = account_invitations.id ORDER BY created_at DESC, id DESC LIMIT 1), 'not-requested')", nil
	case "last_attempt_at":
		return "COALESCE((SELECT last_attempt_at FROM invitation_deliveries WHERE invitation_id = account_invitations.id ORDER BY created_at DESC, id DESC LIMIT 1), 'epoch'::timestamptz)", nil
	default:
		return "", invalid("Unsupported invitation sort field.")
	}
}

// invitationRow carries the value an invitation list is sorted by when that
// value is computed rather than stored, so the cursor can record it.
type invitationRow struct {
	e.AccountInvitation
	SortText string    `gorm:"->;column:sort_text"`
	SortTime time.Time `gorm:"->;column:sort_time"`
}

// invitationKeyset is the order of an invitation list sorted by field. Every
// order ends in the ID, so it is total. With no field the list is newest
// first, like every other list; a named field defaults to ascending, except
// creation time, whose natural reading is newest first.
func invitationKeyset(field, direction string) (keys dbstore.Keyset[invitationRow], selectAs string, err error) {
	column, err := invitationSortColumn(field)
	if err != nil {
		return keys, "", err
	}
	switch direction {
	case "":
		keys.Descending = column == "created_at"
	case "asc", "desc":
		keys.Descending = direction == "desc"
	default:
		return keys, "", invalid("Sort direction must be asc or desc.")
	}

	id := dbstore.KeyColumn{Expr: "account_invitations.id"}
	var key func(r *invitationRow) any
	kind := dbstore.KeyString
	switch field {
	case "id":
		keys.Columns = []dbstore.KeyColumn{id}
		keys.Key = func(r *invitationRow) []any { return []any{r.ID} }
		return keys, "", nil
	case "email":
		key = func(r *invitationRow) any { return r.Email }
	case "role":
		key = func(r *invitationRow) any { return r.Role }
	case "expires_at":
		kind, key = dbstore.KeyTime, func(r *invitationRow) any { return r.ExpiresAt }
	case "status", "email_status":
		selectAs, key = "sort_text", func(r *invitationRow) any { return r.SortText }
	case "last_attempt_at":
		kind, selectAs, key = dbstore.KeyTime, "sort_time", func(r *invitationRow) any { return r.SortTime }
	default:
		kind, key = dbstore.KeyTime, func(r *invitationRow) any { return r.CreatedAt }
	}
	if !strings.HasPrefix(column, "(") && !strings.HasPrefix(column, "COALESCE") {
		column = "account_invitations." + column
	}
	keys.Columns = []dbstore.KeyColumn{{Expr: column, Kind: kind}, id}
	keys.Key = func(r *invitationRow) []any { return []any{key(r), r.ID} }
	return keys, selectAs, nil
}

// invitationPage pages the invitations tx selects, sorted by field. tx is
// already authorised and filtered.
func invitationPage(tx *gorm.DB, q manifest.SearchQuery, field, direction string) ([]e.AccountInvitation, manifest.Page, error) {
	keys, selectAs, err := invitationKeyset(field, direction)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	total, err := countOf(tx)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	keys.NoTotal = true
	// Selected explicitly: scanning into invitationRow would otherwise select
	// its computed columns from the table.
	if selectAs != "" {
		tx = tx.Select("account_invitations.*, " + keys.Columns[0].Expr + " AS " + selectAs)
	} else {
		tx = tx.Select("account_invitations.*")
	}
	rows, page, err := dbstore.PageBy(tx, q, keys)
	if err != nil {
		return nil, manifest.Page{}, pageError(err)
	}
	page.Total = total
	out := make([]e.AccountInvitation, len(rows))
	for i := range rows {
		out[i] = rows[i].AccountInvitation
	}
	return out, page, nil
}
