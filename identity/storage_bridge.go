package identity

import (
	"context"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

func CurrentProblem(status int, code, detail string) error { return problem(status, code, detail) }
func Invalid(detail string) error                          { return invalid(detail) }
func InvalidFields(detail string, fields map[string]string) error {
	return invalidFields(detail, fields)
}
func Conflict(code string) error { return conflict(code) }
func Missing() error             { return missing() }
func Forbidden() error           { return forbidden() }
func Unauthenticated() error     { return unauthenticated() }

type StorageContextKey = contextKey

func Principal(ctx context.Context) e.Principal          { return principal(ctx) }
func CurrentRequest(ctx context.Context) Request         { return request(ctx) }
func Database(ctx context.Context, db *gorm.DB) *gorm.DB { return database(ctx, db) }

type StorageCredential = credential
type StorageUser = user

func MetadataAudit(ctx context.Context, db *gorm.DB, r *e.Resource, action string) error {
	return metadataAudit(ctx, db, r, action)
}

func ContextSearch(ctx context.Context) manifest.SearchQuery { return contextSearch(ctx) }

func ProjectSearchTerm(ctx context.Context) string { return projectSearchTerm(ctx) }
