package identity

import (
	"context"
	"net/url"
)

const ValuePurposeLogin = purposeLogin

const ValuePurposeDevice = purposeDevice
const ValuePurposeRegister = purposeRegister
const ValuePurposeInvitation = purposeInvitation

func (s *Service) ProviderFailed(ctx context.Context, provider string, err error) error {
	return s.providerFailed(ctx, provider, err)
}
func (s *Service) ValidOuterAuthorization(form url.Values, webOnly bool) bool {
	return s.validOuterAuthorization(form, webOnly)
}

func (s *Service) ProviderLogin(ctx context.Context, t upstreamAuthTransaction, identity providerIdentity, result ProviderResult) (ProviderResult, error) {
	return s.providerLogin(ctx, t, identity, result)
}
