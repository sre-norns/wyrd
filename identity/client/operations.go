package client

import (
	"context"
	"net/url"

	"github.com/sre-norns/wyrd/identity/model"
)

// serviceConfigClient is the REST client for the ServiceConfigService API group.
type serviceConfigClient struct {
	*Client
}

func (c *serviceConfigClient) Get(ctx context.Context) (resource model.ServiceConfiguration, exists bool, commError error) {
	return Resource[model.ServiceConfiguration](c.Client).Get(ctx, "/v1/service-configuration")
}

// oauthClient is the REST client for the OAuthService API group.
type oauthClient struct {
	*Client
}

func (c *oauthClient) Metadata(ctx context.Context) (any, error) {
	return c.oauth(ctx, "GET", "/.well-known/oauth-authorization-server", nil)
}

func (c *oauthClient) Authorize(ctx context.Context) (any, error) {
	return c.oauth(ctx, "GET", "/oauth/authorize", Options(ctx).OAuthQuery)
}

func (c *oauthClient) DeviceAuthorization(ctx context.Context, form url.Values) (any, error) {
	return c.oauth(ctx, "POST", "/oauth/device_authorization", form)
}

func (c *oauthClient) Token(ctx context.Context, form url.Values) (any, error) {
	return c.oauth(ctx, "POST", "/oauth/token", form)
}

func (c *oauthClient) Revoke(ctx context.Context, form url.Values) (any, error) {
	return c.oauth(ctx, "POST", "/oauth/revoke", form)
}
