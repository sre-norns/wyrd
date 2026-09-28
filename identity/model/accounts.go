package model

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type AccountsService interface {
	List(ctx context.Context, query manifest.SearchQuery) (result []Account, total int64, err error)
	Get(ctx context.Context, id AccountID) (resource Account, exists bool, commError error)
	CreateOrUpdate(ctx context.Context, account Account) (resource Account, created bool, commError error)
}

type AccountMembershipsService interface {
	List(ctx context.Context, accountID AccountID, query manifest.SearchQuery) (result []AccountMembership, total int64, err error)
	Get(ctx context.Context, id AccountMembershipID) (resource AccountMembership, exists bool, commError error)
	Create(ctx context.Context, accountID AccountID, membership AccountMembership) (resource AccountMembership, commError error)
	CreateOrUpdate(ctx context.Context, membership AccountMembership) (resource AccountMembership, created bool, commError error)
}

type AccountInvitationsService interface {
	List(ctx context.Context, accountID AccountID, query manifest.SearchQuery) (result []AccountInvitation, total int64, err error)
	Get(ctx context.Context, id AccountInvitationID) (resource AccountInvitation, exists bool, commError error)
	Create(ctx context.Context, accountID AccountID, invitation AccountInvitation) (resource AccountInvitation, commError error)
	CreateOrUpdate(ctx context.Context, invitation AccountInvitation) (resource AccountInvitation, created bool, commError error)
	RequestDelivery(ctx context.Context, id AccountInvitationID) (resource AccountInvitation, err error)
	Accept(ctx context.Context, id AccountInvitationID) (resource AccountMembership, exists bool, commError error)
}
