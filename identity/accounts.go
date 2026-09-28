package identity

import (
	"context"

	expbench "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type accountsService struct {
	db     *gorm.DB
	config *Config
}

func (a *accountsService) List(ctx context.Context, query manifest.SearchQuery) (result []expbench.Account, total int64, err error) {
	return list[expbench.Account](ctx, a.db, query, "")
}

func (a *accountsService) Get(ctx context.Context, id expbench.AccountID) (resource expbench.Account, exists bool, commError error) {
	return get[expbench.Account](ctx, a.db, string(id))
}

func (a *accountsService) CreateOrUpdate(ctx context.Context, account expbench.Account) (resource expbench.Account, created bool, commError error) {
	if account.ID == "" && a.config.AccountProvisioning == "system-admin-only" {
		if !systemAuthority(ctx, database(ctx, a.db)) {
			return resource, false, forbidden()
		}
		if account.Name == "" || account.OwnerEmail == "" {
			return resource, false, invalid("Account name and owner invitation email are required.")
		}
		commError = mutation(ctx, a.db, func(tx *gorm.DB) error {
			if err := resourceLimit(tx, "", "", "accounts"); err != nil {
				return err
			}
			account.Resource = expbench.Resource{Name: account.Name}
			resource = account
			if err := insert(ctx, tx, &resource); err != nil {
				return err
			}
			if err := materialize(ctx, tx, expbench.AccountID(resource.ID), ""); err != nil {
				return err
			}
			invitation, err := newInvitation(ctx, tx, a.config, expbench.AccountInvitation{Resource: expbench.Resource{AccountID: expbench.AccountID(resource.ID)}, Email: account.OwnerEmail, Role: "owner", Delivery: account.Delivery})
			if err != nil {
				return err
			}
			resource.OwnerInvitation = &invitation
			return nil
		})
		return resource, commError == nil, commError
	}
	return upsert(ctx, a.db, account)
}

type accountMembershipsService struct {
	db *gorm.DB
}

func (a *accountMembershipsService) List(ctx context.Context, accountID expbench.AccountID, query manifest.SearchQuery) (result []expbench.AccountMembership, total int64, err error) {
	return list[expbench.AccountMembership](ctx, a.db, query, "account_id = ?", accountID)
}

func (a *accountMembershipsService) Get(ctx context.Context, id expbench.AccountMembershipID) (resource expbench.AccountMembership, exists bool, commError error) {
	return get[expbench.AccountMembership](ctx, a.db, string(id))
}

func (a *accountMembershipsService) Create(ctx context.Context, accountID expbench.AccountID, membership expbench.AccountMembership) (resource expbench.AccountMembership, commError error) {
	membership.AccountID = accountID
	return create(ctx, a.db, membership)
}

func (a *accountMembershipsService) CreateOrUpdate(ctx context.Context, membership expbench.AccountMembership) (resource expbench.AccountMembership, created bool, commError error) {
	return upsert(ctx, a.db, membership)
}

type accountInvitationsService struct {
	db     *gorm.DB
	config *Config
}

func (a *accountInvitationsService) List(ctx context.Context, accountID expbench.AccountID, query manifest.SearchQuery) (result []expbench.AccountInvitation, total int64, err error) {
	return list[expbench.AccountInvitation](ctx, a.db, query, "account_id = ?", accountID)
}

func (a *accountInvitationsService) Get(ctx context.Context, id expbench.AccountInvitationID) (resource expbench.AccountInvitation, exists bool, commError error) {
	return get[expbench.AccountInvitation](ctx, a.db, string(id))
}

func (a *accountInvitationsService) Create(ctx context.Context, accountID expbench.AccountID, invitation expbench.AccountInvitation) (out expbench.AccountInvitation, err error) {
	if invitation.ID != "" || invitation.EmailDelivery != nil || invitation.AcceptedBy != "" || invitation.MembershipID != "" || invitation.Token != "" {
		return out, invalid("Invitation result fields are read-only.")
	}
	invitation.AccountID = accountID
	err = mutation(ctx, a.db, func(tx *gorm.DB) error {
		if !accountAdmin(ctx, tx, accountID) || (invitation.Role == "owner" && accountRole(ctx, tx, accountID) != "owner") {
			return forbidden()
		}
		var err error
		out, err = newInvitation(ctx, tx, a.config, invitation)
		return err
	})
	return
}

func (a *accountInvitationsService) CreateOrUpdate(ctx context.Context, invitation expbench.AccountInvitation) (out expbench.AccountInvitation, created bool, err error) {
	if invitation.ID == "" {
		out, err = a.Create(ctx, invitation.AccountID, invitation)
		return out, err == nil, err
	}
	if patch := request(ctx).Patch; patch != nil {
		for key := range patch {
			if key != "status" {
				return out, false, invalid("Only invitation revocation is supported.")
			}
		}
	}
	if invitation.Status != "revoked" {
		return out, false, invalid("Only invitation revocation is supported.")
	}
	s := *NewService(a.db)
	if a.config != nil {
		s.config = a.config
	}
	out, err = s.RevokeInvitation(ctx, invitation.ID, expbench.SystemAction{})
	return out, false, err
}

func (a *accountInvitationsService) Accept(ctx context.Context, id expbench.AccountInvitationID) (out expbench.AccountMembership, exists bool, err error) {
	err = mutation(ctx, a.db, func(tx *gorm.DB) error {
		i, err := load[expbench.AccountInvitation](tx, string(id))
		if err != nil {
			return err
		}
		p := principal(ctx)
		if p.Type != "user" {
			return forbidden()
		}
		u, err := load[user](tx, p.UserID)
		if err != nil {
			return err
		}
		var count int64
		if err = tx.Model(&credential{}).Where("owner_id = ? AND kind = 'invitation' AND verifier = ? AND used = false", i.ID, digest(request(ctx).InvitationToken)).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return forbidden()
		}
		out, err = acceptInvitation(ctx, tx, &i, u)
		return err
	})
	return out, err == nil, err
}

func (a *accountInvitationsService) RequestDelivery(ctx context.Context, id expbench.AccountInvitationID) (expbench.AccountInvitation, error) {
	s := *NewService(a.db)
	if a.config != nil {
		s.config = a.config
	}
	return s.RequestInvitationDelivery(ctx, string(id), expbench.SystemAction{})
}
