package client

import (
	"context"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// projectMembershipsClient is the REST client for the ProjectMembershipsService API group.
type projectMembershipsClient struct {
	*Client
}

func (c *projectMembershipsClient) List(ctx context.Context, projectID model.ProjectID, query manifest.SearchQuery) (result []model.ProjectMembership, page manifest.Page, err error) {
	return Resource[model.ProjectMembership](c.Client).List(ctx, ResourcePath("v1", "projects", string(projectID), "memberships"), query)
}

func (c *projectMembershipsClient) Get(ctx context.Context, id model.ProjectMembershipID) (resource model.ProjectMembership, exists bool, commError error) {
	return Resource[model.ProjectMembership](c.Client).Get(ctx, ResourcePath("v1", "project-memberships", string(id)))
}

func (c *projectMembershipsClient) Create(ctx context.Context, projectID model.ProjectID, membership model.ProjectMembership) (resource model.ProjectMembership, commError error) {
	return Resource[model.ProjectMembership](c.Client).Post(ctx, ResourcePath("v1", "projects", string(projectID), "memberships"), membership)
}

func (c *projectMembershipsClient) CreateOrUpdate(ctx context.Context, membership model.ProjectMembership) (resource model.ProjectMembership, created bool, commError error) {
	return Resource[model.ProjectMembership](c.Client).Patch(ctx, ResourcePath("/v1/project-memberships", membership.ID), membership)
}
