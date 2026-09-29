package model

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type SessionsService interface {
	List(ctx context.Context, query manifest.SearchQuery) (result []Session, page manifest.Page, err error)
	Get(ctx context.Context, id SessionID) (resource Session, exists bool, commError error)
	CreateOrUpdate(ctx context.Context, session Session) (resource Session, created bool, commError error)
}

type PrincipalService interface {
	Get(ctx context.Context) (resource Principal, exists bool, commError error)
}
