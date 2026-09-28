package identity

import (
	"context"
	"net/url"
)

type StorageInvitationContinuation = invitationContinuation

func (s *Service) BindInvitationProvider(ctx context.Context, t *upstreamAuthTransaction, form url.Values) error {
	return s.bindInvitationProvider(ctx, t, form)
}

func (s *Service) CurrentProviderInvitation(ctx context.Context, t upstreamAuthTransaction, identity providerIdentity, result ProviderResult) (ProviderResult, error) {
	return s.providerInvitation(ctx, t, identity, result)
}
func (s *Service) JoinWithProvider(ctx context.Context, cookie string) (redirect AuthorizationRedirect, err error) {
	return s.joinWithProvider(ctx, cookie)
}
