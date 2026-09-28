package identity

import (
	"context"

	"gorm.io/gorm"
)

const ValueConfirmRegister = confirmRegister
const ValueConfirmLink = confirmLink

type StorageProviderMail = providerMail

func (s *Service) SendProviderMail(ctx context.Context, m *providerMail) error {
	return s.sendProviderMail(ctx, m)
}

func (s *Service) PrepareProviderConfirmation(tx *gorm.DB, purpose, userID, to string, t upstreamAuthTransaction, identity providerIdentity, accountName string) (*providerMail, error) {
	return s.prepareProviderConfirmation(tx, purpose, userID, to, t, identity, accountName)
}
func (s *Service) RequestProviderLink(tx *gorm.DB, t upstreamAuthTransaction, identity providerIdentity) (*providerMail, error) {
	return s.requestProviderLink(tx, t, identity)
}
func (s *Service) ProviderRegistration(ctx context.Context, t upstreamAuthTransaction, identity providerIdentity, result ProviderResult) (ProviderResult, error) {
	return s.providerRegistration(ctx, t, identity, result)
}

func (s *Service) RegisterProviderUser(ctx context.Context, tx *gorm.DB, identity providerIdentity, accountName string) (user, error) {
	return s.registerProviderUser(ctx, tx, identity, accountName)
}
