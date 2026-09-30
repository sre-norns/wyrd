package client

import (
	"context"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// accountsClient is the REST client for the AccountsService API group.
type accountsClient struct {
	*Client
}

func (c *accountsClient) List(ctx context.Context, query manifest.SearchQuery) (result []model.Account, page manifest.Page, err error) {
	return Resource[model.Account](c.Client).List(ctx, "/v1/accounts", query)
}

func (c *accountsClient) Get(ctx context.Context, id model.AccountID) (resource model.Account, exists bool, commError error) {
	return Resource[model.Account](c.Client).Get(ctx, ResourcePath("v1", "accounts", string(id)))
}

func (c *accountsClient) CreateOrUpdate(ctx context.Context, account model.Account) (resource model.Account, created bool, commError error) {
	if account.ID == "" {
		return Resource[model.Account](c.Client).PostStatus(ctx, "/v1/accounts", account)
	}
	return Resource[model.Account](c.Client).Patch(ctx, ResourcePath("/v1/accounts", account.ID), account)
}

// accountMembershipsClient is the REST client for the AccountMembershipsService API group.
type accountMembershipsClient struct {
	*Client
}

func (c *accountMembershipsClient) List(ctx context.Context, accountID model.AccountID, query manifest.SearchQuery) (result []model.AccountMembership, page manifest.Page, err error) {
	return Resource[model.AccountMembership](c.Client).List(ctx, ResourcePath("v1", "accounts", string(accountID), "memberships"), query)
}

func (c *accountMembershipsClient) Get(ctx context.Context, id model.AccountMembershipID) (resource model.AccountMembership, exists bool, commError error) {
	return Resource[model.AccountMembership](c.Client).Get(ctx, ResourcePath("v1", "account-memberships", string(id)))
}

func (c *accountMembershipsClient) Create(ctx context.Context, accountID model.AccountID, membership model.AccountMembership) (resource model.AccountMembership, commError error) {
	return Resource[model.AccountMembership](c.Client).Post(ctx, ResourcePath("v1", "accounts", string(accountID), "memberships"), membership)
}

func (c *accountMembershipsClient) CreateOrUpdate(ctx context.Context, membership model.AccountMembership) (resource model.AccountMembership, created bool, commError error) {
	return Resource[model.AccountMembership](c.Client).Patch(ctx, ResourcePath("/v1/account-memberships", membership.ID), membership)
}

// accountInvitationsClient is the REST client for the AccountInvitationsService API group.
type accountInvitationsClient struct {
	*Client
}

func (c *accountInvitationsClient) List(ctx context.Context, accountID model.AccountID, query manifest.SearchQuery) (result []model.AccountInvitation, page manifest.Page, err error) {
	return Resource[model.AccountInvitation](c.Client).List(ctx, ResourcePath("v1", "accounts", string(accountID), "invitations"), query)
}

func (c *accountInvitationsClient) Get(ctx context.Context, id model.AccountInvitationID) (resource model.AccountInvitation, exists bool, commError error) {
	return Resource[model.AccountInvitation](c.Client).Get(ctx, ResourcePath("v1", "account-invitations", string(id)))
}

func (c *accountInvitationsClient) Create(ctx context.Context, accountID model.AccountID, invitation model.AccountInvitation) (resource model.AccountInvitation, commError error) {
	return Resource[model.AccountInvitation](c.Client).Post(ctx, ResourcePath("v1", "accounts", string(accountID), "invitations"), map[string]any{"email": invitation.Email, "role": invitation.Role, "delivery": invitation.Delivery})
}

func (c *accountInvitationsClient) CreateOrUpdate(ctx context.Context, invitation model.AccountInvitation) (resource model.AccountInvitation, created bool, commError error) {
	return Resource[model.AccountInvitation](c.Client).Patch(ctx, ResourcePath("/v1/account-invitations", invitation.ID), invitation)
}

func (c *accountInvitationsClient) Accept(ctx context.Context, id model.AccountInvitationID) (resource model.AccountMembership, exists bool, commError error) {
	return Resource[model.AccountMembership](c.Client).PostFound(ctx, ResourcePath("v1", "account-invitations", string(id), "acceptance"), nil)
}

func (c *accountInvitationsClient) RequestDelivery(ctx context.Context, id model.AccountInvitationID) (model.AccountInvitation, error) {
	return Resource[model.AccountInvitation](c.Client).Post(ctx, ResourcePath("v1/account-invitations", string(id), "deliveries"), map[string]any{})
}
