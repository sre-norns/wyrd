package identity

import (
	"context"
	"maps"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

const (
	PurgeWorker             = "purge"
	InvitationMailWorker    = "invitation-mail"
	ProjectAccessMailWorker = "project-access-mail"
	ChangeListenerWorker    = "change-listener"
)

func (s *Service) runWorker(ctx context.Context, worker string, iteration func(context.Context) (bool, error)) (bool, error) {
	ctx, span := otel.Tracer("github.com/sre-norns/wyrd/identity").Start(ctx, "worker."+worker)
	worked, err := iteration(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "worker iteration failed")
	}
	span.End()
	s.observeWorker(ctx, worker, err, worked)
	return worked, err
}

func (s *Service) observeWorker(ctx context.Context, worker string, err error, worked bool) {
	if s.config.ObserveWorker != nil {
		s.config.ObserveWorker(ctx, worker, err, worked)
	}
}

func (s *Service) telemetryComponents(components map[string]string) {
	if s.config.TelemetryStatus != nil {
		maps.Copy(components, s.config.TelemetryStatus())
	}
	// Provider availability is optional metadata. It never changes readiness
	// or the overall health status.
	for _, name := range enabledProviders(*s.config) {
		status := "configured"
		if value, ok := s.providerStatus.Load(name); ok {
			status = value.(string)
		}
		components["provider_"+name] = status
	}
}

func (s *Service) observeProvider(ctx context.Context, provider, outcome string) {
	switch outcome {
	case providerUnavailable, providerExchangeFailed:
		s.providerStatus.Store(provider, "degraded")
	case ProviderAuthenticated, ProviderAccountName, ProviderConfirmationSent, ProviderInvitation, ProviderExistingUser:
		s.providerStatus.Store(provider, "available")
	}
	if s.config.ObserveProvider != nil {
		s.config.ObserveProvider(ctx, provider, outcome)
	}
}
