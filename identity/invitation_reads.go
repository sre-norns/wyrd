package identity

import (
	"encoding/base64"
	"strings"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
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

func invitationOrder(field, direction string) (string, error) {
	column, err := invitationSortColumn(field)
	if err != nil {
		return "", err
	}
	if direction == "" {
		direction = "asc"
	}
	if direction != "asc" && direction != "desc" {
		return "", invalid("Sort direction must be asc or desc.")
	}
	return column + " " + strings.ToUpper(direction) + ", id " + strings.ToUpper(direction), nil
}

func invitationPageQuery(db *gorm.DB, q e.SystemQuery) (*gorm.DB, int, error) {
	limit := q.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return nil, 0, invalid("Limit must be between 1 and 100.")
	}
	if q.Sort == "" {
		q.Sort = "id"
	} // Preserve existing system pagination.
	order, err := invitationOrder(q.Sort, q.Direction)
	if err != nil {
		return nil, 0, err
	}
	column, _ := invitationSortColumn(q.Sort)
	if q.Status != "" {
		db = db.Where("("+invitationStatusSQL+") = ?", q.Status)
	}
	if q.Cursor != "" {
		id, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || len(id) > 128 {
			return nil, 0, invalid("Invalid cursor.")
		}
		op := ">"
		if q.Direction == "desc" {
			op = "<"
		}
		// A unique ID breaks ties for every supported sort value.
		db = db.Where("("+column+", id) "+op+" (SELECT "+column+", id FROM account_invitations WHERE id = ?)", string(id))
	}
	return db.Order(order), limit, nil
}
