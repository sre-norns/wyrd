package model

import (
	"context"
	"net/url"
)

type ServiceConfigService interface {
	Get(ctx context.Context) (resource ServiceConfiguration, exists bool, commError error)
}

type OAuthService interface {
	Metadata(ctx context.Context) (any, error)
	Authorize(ctx context.Context) (any, error)
	DeviceAuthorization(ctx context.Context, form url.Values) (any, error)
	Token(ctx context.Context, form url.Values) (any, error)
	Revoke(ctx context.Context, form url.Values) (any, error)
}
