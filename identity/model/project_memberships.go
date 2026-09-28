package model

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type ProjectMembershipsService interface {
	List(ctx context.Context, projectID ProjectID, query manifest.SearchQuery) (result []ProjectMembership, total int64, err error)
	Get(ctx context.Context, id ProjectMembershipID) (resource ProjectMembership, exists bool, commError error)
	Create(ctx context.Context, projectID ProjectID, membership ProjectMembership) (resource ProjectMembership, commError error)
	CreateOrUpdate(ctx context.Context, membership ProjectMembership) (resource ProjectMembership, created bool, commError error)
}
