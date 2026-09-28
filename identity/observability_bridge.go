package identity

import (
	"context"
)

func (s *Service) RunWorker(ctx context.Context, worker string, iteration func(context.Context) (bool, error)) (bool, error) {
	return s.runWorker(ctx, worker, iteration)
}
func (s *Service) ObserveWorker(ctx context.Context, worker string, err error, worked bool) {
	s.observeWorker(ctx, worker, err, worked)
}
func (s *Service) TelemetryComponents(components map[string]string) {
	s.telemetryComponents(components)
}
func (s *Service) ObserveProvider(ctx context.Context, provider, outcome string) {
	s.observeProvider(ctx, provider, outcome)
}
