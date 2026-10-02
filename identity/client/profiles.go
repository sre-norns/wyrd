package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type personalProfileClient struct {
	*Client
}

func (c *personalProfileClient) Get(ctx context.Context) (resource model.PersonalProfile, exists bool, commError error) {
	return Resource[model.PersonalProfile](c.Client).Get(ctx, "/v1/profile")
}

func (c *personalProfileClient) Update(ctx context.Context, profile model.PersonalProfile) (resource model.PersonalProfile, commError error) {
	options := Options(ctx)
	if options.IfMatch == "" && profile.Revision > 0 {
		options.IfMatch = fmt.Sprintf("\"%d\"", profile.Revision)
	}
	resource, _, commError = Resource[model.PersonalProfile](c.Client).Request(
		WithRequestOptions(ctx, options),
		http.MethodPatch,
		"/v1/profile",
		nil,
		map[string]any{"spec": map[string]any{"displayName": profile.DisplayName}},
	)
	return
}

func (c *personalProfileClient) AccessibleAccounts(ctx context.Context, query manifest.SearchQuery) (accounts []model.ProfileAccount, page manifest.Page, commError error) {
	return Resource[model.ProfileAccount](c.Client).List(ctx, "/v1/profile/accounts", query)
}

type signInMethodClient struct {
	*Client
}

func (c *signInMethodClient) List(ctx context.Context) (methods []model.SignInMethod, commError error) {
	methods, _, commError = Resource[model.SignInMethod](c.Client).List(ctx, "/v1/profile/sign-in-methods", manifest.SearchQuery{})
	return
}

func (c *signInMethodClient) Get(ctx context.Context, id string) (resource model.SignInMethod, exists bool, commError error) {
	return Resource[model.SignInMethod](c.Client).Get(ctx, "/v1/profile/sign-in-methods/"+url.PathEscape(id))
}

func (c *signInMethodClient) Revoke(ctx context.Context, method model.SignInMethod) (resource model.SignInMethod, commError error) {
	options := Options(ctx)
	if options.IfMatch == "" && method.Revision > 0 {
		options.IfMatch = fmt.Sprintf("\"%d\"", method.Revision)
	}
	resource, _, commError = Resource[model.SignInMethod](c.Client).Request(
		WithRequestOptions(ctx, options),
		http.MethodPatch,
		"/v1/profile/sign-in-methods/"+url.PathEscape(method.ID),
		nil,
		map[string]any{"operation": "revoke"},
	)
	return
}
