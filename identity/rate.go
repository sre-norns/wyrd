package identity

import (
	"context"
	"slices"
	"time"
)

type requestWindow struct {
	ID        string `gorm:"primaryKey"`
	AccountID string `gorm:"index"`
	StartsAt  time.Time
	Attempts  int64
}

func (s *Service) rateWindow(ctx context.Context, key string) (int64, error) {
	var attempts int64
	err := s.db.WithContext(ctx).Raw(`INSERT INTO request_windows (id, starts_at, attempts)
 VALUES (?, clock_timestamp(), 1)
 ON CONFLICT (id) DO UPDATE SET
 attempts = CASE WHEN request_windows.starts_at < clock_timestamp() - interval '1 minute' THEN 1 ELSE request_windows.attempts + 1 END,
 starts_at = CASE WHEN request_windows.starts_at < clock_timestamp() - interval '1 minute' THEN clock_timestamp() ELSE request_windows.starts_at END
 RETURNING attempts`, digest(key)).Scan(&attempts).Error
	return attempts, err
}

func (s *Service) AllowAuthentication(ctx context.Context, address string) error {
	attempts, err := s.rateWindow(ctx, "oauth:"+address)
	if err != nil {
		return err
	}
	if attempts > s.config.AuthenticationRateLimit {
		return problem(429, "rate-limited", "Authentication request limit reached. Retry after one minute.")
	}
	return nil
}

const providerAttempts = 30

func (s *Service) AllowProviderAuthentication(ctx context.Context, address, provider string) error {
	if !slices.Contains(providerNames, provider) {
		return nil
	}
	attempts, err := s.rateWindow(ctx, "oauth-provider:"+provider+":"+address)
	if err != nil {
		return err
	}
	if attempts > providerAttempts {
		s.observeProvider(ctx, provider, "rate_limited")
		return problem(429, "rate-limited", providerLabel(provider)+" sign-in request limit reached. Retry after one minute or use another sign-in method.")
	}
	return nil
}

func (s *Service) allowRequest(ctx context.Context) error {
	if f := extensions(s.db).AllowRequest; f != nil {
		return f(ctx, database(ctx, s.db))
	}
	return nil
}

func (s *Service) AllowRequest(ctx context.Context) error { return s.allowRequest(ctx) }
