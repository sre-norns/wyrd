package identity

import (
	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

type StorageInvitationDelivery = invitationDelivery

func DecorateInvitation(db *gorm.DB, i *e.AccountInvitation) error { return decorateInvitation(db, i) }
