package client

import (
	"context"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// sessionsClient is the REST client for the SessionsService API group.
type sessionsClient struct {
	*Client
}

func (c *sessionsClient) List(ctx context.Context, query manifest.SearchQuery) (result []model.Session, page manifest.Page, err error) {
	return Resource[model.Session](c.Client).List(ctx, "/v1/sessions", query)
}

func (c *sessionsClient) Get(ctx context.Context, id model.SessionID) (resource model.Session, exists bool, commError error) {
	return Resource[model.Session](c.Client).Get(ctx, ResourcePath("v1", "sessions", string(id)))
}

func (c *sessionsClient) CreateOrUpdate(ctx context.Context, session model.Session) (resource model.Session, created bool, commError error) {
	return Resource[model.Session](c.Client).Patch(ctx, ResourcePath("/v1/sessions", session.ID), session)
}

// principalClient is the REST client for the PrincipalService API group.
type principalClient struct {
	*Client
}

func (c *principalClient) Get(ctx context.Context) (resource model.Principal, exists bool, commError error) {
	return Resource[model.Principal](c.Client).Get(ctx, "/v1/principal")
}
