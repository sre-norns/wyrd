package identity

import (
	"context"
	"net/url"

	expbench "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

type serviceConfigService struct {
	db     *gorm.DB
	config *Config
}

func (s *serviceConfigService) Get(ctx context.Context) (resource expbench.ServiceConfiguration, exists bool, commError error) {
	return expbench.ServiceConfiguration{APIVersions: []string{"v1"}, OAuthDiscovery: s.config.Issuer + "/.well-known/oauth-authorization-server", Grants: []string{"authorization_code", "urn:ietf:params:oauth:grant-type:device_code", "refresh_token"}, AccountProvisioning: s.config.AccountProvisioning, SignInProviders: enabledProviders(*s.config)}, true, nil
}

func enabledProviders(c Config) []string {
	names := []string{}
	for _, name := range providerNames {
		if _, ok := c.Providers[name]; ok {
			names = append(names, name)
		}
	}
	return names
}

type oauthService struct {
	db     *gorm.DB
	config *Config
}

func (o *oauthService) Metadata(ctx context.Context) (any, error) {
	return o.metadata(), nil
}

func (o *oauthService) Authorize(ctx context.Context) (any, error) {
	return o.approve(ctx, url.Values(request(ctx).OAuthQuery))
}

func (o *oauthService) DeviceAuthorization(ctx context.Context, form url.Values) (any, error) {
	return o.device(ctx, form)
}

func (o *oauthService) Token(ctx context.Context, form url.Values) (any, error) {
	return o.exchange(ctx, form)
}

func (o *oauthService) Revoke(ctx context.Context, form url.Values) (any, error) {
	return o.revoke(ctx, form)
}
