package model

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type AgentAuthorizationsService interface {
	List(ctx context.Context, projectID ProjectID, query manifest.SearchQuery) (result []AgentAuthorization, page manifest.Page, err error)
	Get(ctx context.Context, id AgentAuthorizationID) (resource AgentAuthorization, exists bool, commError error)
	Create(ctx context.Context, projectID ProjectID, authorization AgentAuthorization) (resource AgentAuthorization, commError error)
	CreateOrUpdate(ctx context.Context, authorization AgentAuthorization) (resource AgentAuthorization, created bool, commError error)
}
