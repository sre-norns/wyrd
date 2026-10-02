package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/google/uuid"
	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Problem struct {
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Status    int               `json:"status"`
	Code      string            `json:"code"`
	Detail    string            `json:"detail"`
	Instance  string            `json:"instance,omitempty"`
	RequestID string            `json:"requestId,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

func (p *Problem) Error() string { return p.Detail }

func problem(status int, code, detail string) error {
	return &Problem{Type: "urn:exp-bench:problem:" + code, Title: http.StatusText(status), Status: status, Code: code, Detail: detail}
}

func invalid(detail string) error {
	return &Problem{Type: "urn:exp-bench:problem:validation", Title: "Validation failed", Status: 422, Code: "validation", Detail: detail, Fields: map[string]string{"resource": detail}}
}

func invalidFields(detail string, fields map[string]string) error {
	return &Problem{Type: "urn:exp-bench:problem:validation", Title: "Validation failed", Status: 422, Code: "validation", Detail: detail, Fields: fields}
}

func conflict(code string) error {
	return problem(409, code, "The resource state does not permit this operation.")
}

func missing() error { return problem(404, "not-found", "Resource not found.") }

func forbidden() error {
	return problem(403, "forbidden", "The principal does not have authority for this operation.")
}

func unauthenticated() error {
	return problem(401, "unauthenticated", "Valid authentication is required.")
}

type contextKey int

const (
	principalKey contextKey = iota
	requestKey
	databaseKey
)

type Request struct {
	Sort             string
	Direction        string
	SupportReason    string
	SupportReference string
	ID               string
	Method           string
	Target           string
	IfMatch          string
	Patch            map[string]json.RawMessage
	InvitationToken  string
	OAuthQuery       map[string][]string
	DeviceFlow       bool
	Origin           string
	IPAddress        string
	UserAgent        string
}

func WithPrincipal(ctx context.Context, p e.Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

func WithRequest(ctx context.Context, r Request) context.Context {
	return context.WithValue(ctx, requestKey, r)
}

func principal(ctx context.Context) e.Principal {
	p, _ := ctx.Value(principalKey).(e.Principal)
	return p
}

func request(ctx context.Context) Request { r, _ := ctx.Value(requestKey).(Request); return r }

func database(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(databaseKey).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}
	return db.WithContext(ctx)
}

func metadata(v any) *e.Resource { return v.(interface{ Metadata() *e.Resource }).Metadata() }

func kind(v any) string {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

func newID() string { return uuid.NewString() }

func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func digest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }

func now(db *gorm.DB) (time.Time, error) {
	var t time.Time
	err := db.Raw("SELECT clock_timestamp()").Scan(&t).Error
	return t.UTC(), err
}

func initResource(ctx context.Context, r *e.Resource) {
	r.ID = newID()
	if r.Name == "" {
		r.Name = r.ID
	}
	r.Revision = 1
	r.CreatedAt = time.Now().UTC()
	r.UpdatedAt = r.CreatedAt
	r.Actor = principal(ctx)
	if r.Actor.Type == "" {
		r.Actor = e.Principal{Type: "service", CredentialID: "service"}
	}
	r.Authority = r.Actor.Type
	if r.Actor.Type == "user" {
		if r.ProjectID != "" {
			r.Authority = "project-administrator"
		} else if r.AccountID != "" {
			r.Authority = "account-administrator"
		} else if r.Actor.SystemAdmin {
			r.Authority = "system-administrator"
		}
	}
	if r.Actor.Type == "agent" {
		r.Authority = "research-agent"
	}
	if r.Status == "" {
		r.Status = "active"
	}
	if r.Labels == nil {
		r.Labels = manifest.Labels{}
	}
}

type credential struct {
	ID       string `gorm:"primaryKey"`
	OwnerID  string `gorm:"index"`
	Kind     string `gorm:"index"`
	Verifier string `gorm:"uniqueIndex"`
	Used     bool
}

type user = e.User

func lock(tx *gorm.DB) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(690245812)).Error
}

func mutation(ctx context.Context, db *gorm.DB, fn func(*gorm.DB) error) error {
	err := normalizeError(database(ctx, db).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		return fn(tx)
	}))
	if err != nil {
		if f := extensions(db).Rejected; f != nil {
			if auditErr := f(ctx, database(ctx, db), err); auditErr != nil {
				return auditErr
			}
		}
	}
	return err
}

func insert(ctx context.Context, db *gorm.DB, v any) error {
	m := metadata(v)
	if err := resourceLimit(db, m.AccountID, m.ProjectID, "total-capacity"); err != nil {
		return err
	}
	initResource(ctx, metadata(v))
	if err := db.Create(v).Error; err != nil {
		return err
	}
	return record(ctx, db, v, "create-"+kind(v), metadata(v).ID)
}

func save(ctx context.Context, db *gorm.DB, v any) error {
	m := metadata(v)
	m.Revision++
	m.UpdatedAt = time.Now().UTC()
	m.Actor = principal(ctx)
	if m.Actor.Type == "" {
		m.Actor = e.Principal{Type: "service"}
	}
	if err := db.Save(v).Error; err != nil {
		return err
	}
	return record(ctx, db, v, "update-"+kind(v), m.ID)
}

func load[T any](db *gorm.DB, id string) (v T, err error) {
	if id == "" {
		return v, missing()
	}
	store, err := dbstore.NewDBStore(db, identitySchema)
	if err != nil {
		return v, err
	}
	// Internal load is used before service authorization and locking decisions.
	// It shares the caller's transaction; public collection queries use Visibility.
	found, err := store.GetByUID(db.Statement.Context, &v, manifest.ResourceID(id))
	if err == nil && !found {
		err = missing()
	}
	return
}

func get[T any](ctx context.Context, db *gorm.DB, id string) (v T, found bool, err error) {
	db = database(ctx, db)
	v, err = load[T](db, id)
	if err != nil {
		return v, false, err
	}
	if err = authorize(ctx, db, &v, false); err != nil {
		return v, false, err
	}
	err = decorate(ctx, db, &v)
	if err == nil && (kind(v) == "AgentIdentityToken" || kind(v) == "Session") {
		err = metadataAudit(ctx, db, metadata(&v), "read-credential-metadata")
	}
	return v, err == nil, err
}

// listQuery is the authorised, filtered query behind a list: every row it
// selects may be shown to the caller. Paging is applied on top of it and can
// only narrow it.
func listQuery[T any](ctx context.Context, db *gorm.DB, q manifest.SearchQuery, where string, args ...any) (*gorm.DB, error) {
	db = database(ctx, db)
	var model T
	if len(args) > 0 {
		id := fmt.Sprint(args[0])
		switch where {
		case "project_id = ?":
			project, err := load[e.Project](db, id)
			if err != nil {
				return nil, err
			}
			metadata(&model).AccountID = project.AccountID
			metadata(&model).ProjectID = e.ProjectID(id)
			if err = authorize(ctx, db, &model, false); err != nil {
				return nil, err
			}
		case "account_id = ?", "account_id = ? AND project_id = ''":
			// Project collections use the same membership filter as /projects.
			if kind(model) == "Project" && principal(ctx).Scope != e.ScopeSystem && principal(ctx).AccountID == e.AccountID(id) && (accountRole(ctx, db, e.AccountID(id)) != "" || principal(ctx).Type == "agent") {
				break
			}
			if !(systemAuthority(ctx, database(ctx, db)) && kind(model) == "Limit") && !accountAdmin(ctx, db, e.AccountID(id)) {
				return nil, forbidden()
			}
		}
	}
	if where == "account_id = '' AND project_id = ''" && !systemAuthority(ctx, database(ctx, db)) {
		return nil, forbidden()
	}

	query := db
	if where != "" {
		query = query.Where(where, args...)
	}
	policy, err := identityQueryPolicy(db, &model)
	if err != nil {
		return nil, err
	}
	tx, err := dbstore.FilterQuery(ctx, query, &model, identitySchema, q, policy)
	if err != nil {
		return nil, pageError(err)
	}
	// Case-insensitive user-visible project search over the visible project
	// fields only. It composes with authorization, status filters, ordering, and
	// pagination; it never searches research content.
	if term := projectSearchTerm(ctx); term != "" && kind(model) == "Project" {
		pattern := "%" + term + "%"
		tx = tx.Where("name ILIKE ? OR description ILIKE ? OR target ILIKE ?", pattern, pattern, pattern)
	}
	return tx, nil
}

func list[T any](ctx context.Context, db *gorm.DB, q manifest.SearchQuery, where string, args ...any) ([]T, manifest.Page, error) {
	tx, err := listQuery[T](ctx, db, q, where, args...)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	out, page, err := dbstore.PageBy(tx, q, NewestFirst[T]())
	if err != nil {
		return nil, manifest.Page{}, pageError(err)
	}
	if err = decorated(ctx, db, out); err != nil {
		return nil, manifest.Page{}, err
	}
	return out, page, nil
}

// decorated completes listed rows, and records that credential metadata was
// listed.
func decorated[T any](ctx context.Context, db *gorm.DB, out []T) error {
	db = database(ctx, db)
	for i := range out {
		if err := decorate(ctx, db, &out[i]); err != nil {
			return err
		}
	}
	var model T
	if kind(model) == "AgentIdentityToken" || kind(model) == "Session" {
		r := e.Resource{AccountID: principal(ctx).AccountID}
		if err := metadataAudit(ctx, db, &r, "list-credential-metadata"); err != nil {
			return err
		}
	}
	return nil
}

var identitySchema = dbstore.SchemaConfig{
	IDColumnName: "id", NameColumnName: "name", VersionColumnName: "revision", LabelsColumnName: "labels",
	CreatedAtColumnName: "created_at", UpdatedAtColumnName: "updated_at", AccountColumnName: "account_id", ProjectColumnName: "project_id",
}

func identityQueryPolicy(db *gorm.DB, value any) (dbstore.QueryPolicy, error) {
	policy := dbstore.QueryPolicy{Visibility: Visibility{DB: db}, Predicates: map[string]dbstore.FieldPredicate{}}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(value); err != nil {
		return policy, err
	}
	columns := map[string]string{"metadata.uid": "id", "metadata.name": "name", "metadata.account": "account_id", "metadata.project": "project_id", "status.phase": "status", "metadata.version": "revision"}
	if d, ok := resource.Describe(value); ok {
		for _, f := range d.Fields {
			if f.Section != "input" {
				if field := stmt.Schema.LookUpField(f.GoName); field != nil && field.DBName != "" {
					columns[f.Section+"."+f.Name] = field.DBName
				}
			}
		}
	}
	for path, column := range columns {
		field := stmt.Schema.LookUpField(column)
		if field == nil {
			continue
		}
		if field.DataType != "string" && field.DataType != "int" && field.DataType != "uint" && field.DataType != "bool" {
			continue
		}
		expression := clause.Expression(clause.Expr{SQL: "?", Vars: []any{clause.Column{Name: column}}})
		if path == "status.phase" && kind(value) == "AccountInvitation" {
			expression = clause.Expr{SQL: "CASE WHEN status = 'pending' AND expires_at <= clock_timestamp() THEN 'expired' ELSE status END"}
		}
		numeric := field.DataType == "int" || field.DataType == "uint"
		policy.Predicates[path] = func(req manifest.Requirement) (clause.Expression, error) {
			if !numeric && (req.Operator() == manifest.GreaterThan || req.Operator() == manifest.LessThan) {
				return nil, manifest.NewStatusError(400, "invalid-field-selector", "numeric comparison requires a numeric field")
			}
			return dbstore.CompareField(expression, req)
		}
	}
	return policy, nil
}

func upsert[T any](ctx context.Context, db *gorm.DB, v T) (out T, created bool, err error) {
	out = v
	created = metadata(&out).ID == ""
	err = mutation(ctx, db, func(tx *gorm.DB) error {
		m := metadata(&out)
		if !created {
			old, err := load[T](tx, m.ID)
			if err != nil {
				return err
			}
			if err = authorize(ctx, tx, &old, true); err != nil {
				return err
			}
			if err = precondition(ctx, metadata(&old)); err != nil {
				return err
			}
			if patch := request(ctx).Patch; patch != nil {
				data, _ := json.Marshal(old)
				var fields map[string]json.RawMessage
				_ = json.Unmarshal(data, &fields)
				allowed := mutableFields(kind(out))
				for key, val := range patch {
					if !allowed[key] {
						return invalid("Field cannot be changed: " + key)
					}
					if key == "labels" {
						fields[key], err = mergeLabels(fields[key], val)
						if err != nil {
							return invalid("Invalid label patch.")
						}
					} else {
						fields[key] = val
					}
				}
				data, err = json.Marshal(fields)
				if err != nil {
					return err
				}
				var empty T
				out = empty
				if err = json.Unmarshal(data, &out); err != nil {
					return invalid("Invalid patch.")
				}
			} else {
				if m.AccountID != metadata(&old).AccountID || m.ProjectID != metadata(&old).ProjectID {
					return invalid("Ownership is immutable.")
				}
				m.CreatedAt = metadata(&old).CreatedAt
				m.Revision = metadata(&old).Revision
				m.Actor = metadata(&old).Actor
			}
		}
		if created {
			if err := creationScope(tx, &out); err != nil {
				return err
			}
			if err := authorize(ctx, tx, &out, true); err != nil {
				return err
			}
		}
		if err := prepare(ctx, tx, &out, created); err != nil {
			return err
		}
		if err := authorize(ctx, tx, &out, true); err != nil {
			return err
		}
		if created {
			if err := insert(ctx, tx, &out); err != nil {
				return err
			}
			return afterCreate(ctx, tx, &out)
		}
		return save(ctx, tx, &out)
	})
	if err == nil {
		err = decorate(ctx, database(ctx, db), &out)
	}
	return
}

func create[T any](ctx context.Context, db *gorm.DB, v T) (T, error) {
	if metadata(&v).ID != "" {
		return v, invalid("The server assigns resource identifiers.")
	}
	out, _, err := upsert(ctx, db, v)
	return out, err
}

func precondition(ctx context.Context, r *e.Resource) error {
	match := request(ctx).IfMatch
	if match == "" {
		return problem(428, "precondition-required", "If-Match is required.")
	}
	if match != ETag(r.Revision) {
		return problem(412, "precondition-failed", "The resource has changed.")
	}
	return nil
}

func ETag(revision int64) string { return fmt.Sprintf("\"%d\"", revision) }

func mutableFields(k string) map[string]bool {
	fields := "name labels status"
	switch k {
	case "Account", "AgentIdentity":
		fields += " description"
	case "AccountMembership":
		fields += " role"
	case "Project":
		fields += " description target"
	case "AgentAuthorization":
		fields += " roles"
	case "Session", "AgentIdentityToken", "AccountInvitation":
		fields = "status"
	}
	out := map[string]bool{}
	for _, f := range strings.Fields(fields) {
		out[f] = true
	}
	return out
}

func normalizeError(err error) error {
	if pg, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pg.Code {
		case "23505":
			return conflict("already-exists")
		case "23503":
			return conflict("invalid-reference")
		case "40001", "40P01":
			return conflict("concurrent-update")
		}
	}

	return err
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func metadataAudit(ctx context.Context, db *gorm.DB, r *e.Resource, action string) error {
	return audit(ctx, db, Audit{Principal: principal(ctx), Action: action, Target: r, Outcome: "succeeded", RequestID: request(ctx).ID})
}

type queryContextKey struct{}

func WithSearch(ctx context.Context, q manifest.SearchQuery) context.Context {
	return context.WithValue(ctx, queryContextKey{}, q)
}

func contextSearch(ctx context.Context) manifest.SearchQuery {
	q, _ := ctx.Value(queryContextKey{}).(manifest.SearchQuery)
	return q
}

type projectSearchKey struct{}

func WithProjectSearch(ctx context.Context, term string) context.Context {
	return context.WithValue(ctx, projectSearchKey{}, term)
}

func projectSearchTerm(ctx context.Context) string {
	s, _ := ctx.Value(projectSearchKey{}).(string)
	return s
}

func cachedAccount(db *gorm.DB, path string) e.AccountID {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "v1" || parts[1] != "system" {
		return ""
	}
	if parts[2] == "accounts" {
		return e.AccountID(parts[3])
	}
	var owner struct{ AccountID e.AccountID }
	var query *gorm.DB
	switch parts[2] {
	case "owner-recoveries":
		query = db.Model(&e.OwnerRecovery{}).Select("target_account_id AS account_id")
	case "deletion-requests":
		query = db.Model(&e.AccountDeletionRequest{}).Select("target_account_id AS account_id")
	case "account-invitations":
		query = db.Model(&e.AccountInvitation{}).Select("account_id")
	case "account-memberships":
		query = db.Model(&e.AccountMembership{}).Select("account_id")
	default:
		return ""
	}
	query.Where("id = ?", parts[3]).Scan(&owner)
	return owner.AccountID
}

// mergeLabels implements JSON merge-patch object semantics, including removal.
func mergeLabels(old, patch json.RawMessage) (json.RawMessage, error) {
	if string(patch) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var before map[string]string
	if err := json.Unmarshal(old, &before); err != nil {
		return nil, err
	}
	if before == nil {
		before = map[string]string{}
	}
	var changes map[string]*string
	if err := json.Unmarshal(patch, &changes); err != nil {
		return nil, err
	}
	for key, value := range changes {
		if value == nil {
			delete(before, key)
		} else {
			before[key] = *value
		}
	}
	return json.Marshal(before)
}

// remapProblemField preserves a nested service error while naming its input in
// the enclosing operation (for example an invitation email in account create).
func remapProblemField(err error, from, to string) error {
	var p *Problem
	if !errors.As(err, &p) || p.Fields[from] == "" {
		return err
	}
	copy := *p
	copy.Fields = make(map[string]string, len(p.Fields))
	for key, value := range p.Fields {
		if key == from {
			key = to
		}
		copy.Fields[key] = value
	}
	return &copy
}
