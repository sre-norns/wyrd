package identity

import (
	"context"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

func ConsumeStepUp(ctx context.Context, tx *gorm.DB, account e.AccountID, action, id string, maxAge int64) (out e.StepUpAuthorization, err error) {
	return consumeStepUp(ctx, tx, account, action, id, maxAge)
}
