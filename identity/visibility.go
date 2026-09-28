package identity

import (
	"context"
	"reflect"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"gorm.io/gorm"
)

type servicePrincipalKey struct{}

// WithServicePrincipal marks an internal control loop. A system administrator
// session is not this principal and never gains project access through it.
func WithServicePrincipal(ctx context.Context) context.Context {
	return context.WithValue(ctx, servicePrincipalKey{}, true)
}
func servicePrincipal(ctx context.Context) bool {
	v, _ := ctx.Value(servicePrincipalKey{}).(bool)
	return v
}

// Visibility applies the identity registry to wyrd stores, including resources
// that embed manifest.ObjectMeta rather than the legacy identity Resource.
type Visibility struct{ DB *gorm.DB }

var _ dbstore.Visibility = Visibility{}

func modelName(v any) string {
	t := reflect.TypeOf(v)
	if t == nil {
		return ""
	}
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	return t.Name()
}
func (v Visibility) Filter(ctx context.Context, q *gorm.DB, model any) (*gorm.DB, error) {
	name := modelName(model)
	if _, ok := extensions(v.DB).Kinds[name]; !ok {
		return nil, missing()
	}
	if servicePrincipal(ctx) {
		return q, nil
	}
	return visible(ctx, q, name)
}
func (v Visibility) Admit(ctx context.Context, value any) error {
	return Authorize(ctx, v.DB, value, true)
}

// Authorize checks either legacy resource metadata or manifest.ObjectMeta.
func Authorize(ctx context.Context, db *gorm.DB, value any, write bool) error {
	name := modelName(value)
	k, ok := extensions(db).Kinds[name]
	if !ok {
		return missing()
	}
	if servicePrincipal(ctx) {
		return nil
	}
	if _, ok := value.(interface{ Metadata() *e.Resource }); ok {
		return authorize(ctx, db, value, write)
	}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(value); err != nil {
		return err
	}
	field := func(column string) string {
		f := stmt.Schema.FieldsByDBName[column]
		if f == nil {
			return ""
		}
		x, _ := f.ValueOf(ctx, reflect.ValueOf(value))
		r := reflect.ValueOf(x)
		if r.IsValid() && r.Kind() == reflect.String {
			return r.String()
		}
		return ""
	}
	return authorizeScope(ctx, db, k, e.AccountID(field("account_id")), e.ProjectID(field("project_id")), write)
}
func authorizeScope(ctx context.Context, db *gorm.DB, k Kind, a e.AccountID, p e.ProjectID, write bool) error {
	actor := principal(ctx)
	if actor.Type == "" {
		return unauthenticated()
	}
	if actor.Scope == e.ScopeSystem {
		if k.Scope == "system" && !write && k.SystemRead && systemAuthority(ctx, db) {
			return nil
		}
		return missing()
	}
	if a == "" || actor.AccountID != a {
		return missing()
	}
	if k.Scope == "account" {
		if p != "" {
			return missing()
		}
		if accountAdmin(ctx, db, a) {
			return nil
		}
		return forbidden()
	}
	if k.Scope != "project" || p == "" {
		return missing()
	}
	project, err := load[e.Project](db, string(p))
	if err != nil {
		return err
	}
	if project.AccountID != a {
		return missing()
	}
	if projectAdmin(ctx, db, p) {
		return nil
	}
	if !write && k.MachineRead && agentGrant(ctx, db, p, "") {
		return nil
	}
	return missing()
}
