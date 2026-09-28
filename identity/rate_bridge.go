package identity

import (
	"context"
)

type StorageRequestWindow = requestWindow

func (s *Service) RateWindow(ctx context.Context, key string) (int64, error) {
	return s.rateWindow(ctx, key)
}

const ValueProviderAttempts = providerAttempts
