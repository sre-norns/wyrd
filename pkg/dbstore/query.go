package dbstore

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FieldPredicate translates one requirement into a trusted SQL predicate. A
// product registers these for computed attributes (expiry, inherited policy,
// linked records). Client input must only enter bound values, never SQL text.
type FieldPredicate func(manifest.Requirement) (clause.Expression, error)

// QueryPolicy supplies ownership and product-specific filters to FilterQuery.
// A nil Visibility is reserved for a service operation whose caller has already
// authorized unrestricted access. Scope, when present, matches exactly: an
// account scope does not include project rows. Leave it nil for an authorized
// cross-scope listing; Visibility still constrains those rows.
type QueryPolicy struct {
	Visibility Visibility
	Scope      *manifest.ScopeRef
	Fields     FieldColumns
	Predicates map[string]FieldPredicate
}

// FilterQuery composes an authorized resource query on an existing transaction.
// It does not start/commit a transaction, select a page, count, or change order.
// Pass its result to PageBy so selection and totals share the same predicates.
//
// Names use case-insensitive substring matching (SQL LIKE wildcards are retained).
// Time ranges are [from, till). Labels and fields are separate namespaces.
// Pagination is left to PageBy; offset is rejected to prevent silent first-page
// repeats. Config names and predicates are server-owned, never request options.
func FilterQuery(ctx context.Context, query *gorm.DB, model any, config SchemaConfig, q manifest.SearchQuery, policy QueryPolicy) (*gorm.DB, error) {
	if query == nil {
		return nil, ErrNoDBObject
	}
	if model == nil {
		return nil, fmt.Errorf("resource model is required")
	}
	if q.Offset != 0 {
		return nil, manifest.NewStatusError(http.StatusBadRequest, "offset-unsupported", "lists require cursor pagination")
	}
	if !q.FromTime.IsZero() && !q.TillTime.IsZero() && q.FromTime.After(q.TillTime) {
		return nil, manifest.NewStatusError(http.StatusBadRequest, "invalid-time-range", "time range is reversed")
	}
	tx := query.WithContext(ctx).Model(model)
	var err error
	tx, err = filterVisible(ctx, policy.Visibility, tx, model)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, fmt.Errorf("visibility returned a nil query")
	}
	if policy.Scope != nil {
		scope := manifest.ScopeSystem
		if policy.Scope.Project != "" {
			scope = manifest.ScopeProject
		} else if policy.Scope.Account != "" {
			scope = manifest.ScopeAccount
		}
		if err := policy.Scope.Validate(scope); err != nil {
			return nil, err
		}
		if config.AccountColumnName == "" || config.ProjectColumnName == "" {
			return nil, ErrNoScopeColumns
		}
		tx = tx.Where(clause.Eq{Column: clause.Column{Name: config.AccountColumnName}, Value: policy.Scope.Account}).Where(clause.Eq{Column: clause.Column{Name: config.ProjectColumnName}, Value: policy.Scope.Project})
	}
	if q.Name != "" {
		if config.NameColumnName == "" {
			return nil, fmt.Errorf("name column is not configured")
		}
		tx = tx.Where(clause.Expr{SQL: "LOWER(?) LIKE LOWER(?)", Vars: []any{clause.Column{Name: config.NameColumnName}, "%" + q.Name + "%"}})
	}
	if !q.FromTime.IsZero() || !q.TillTime.IsZero() {
		if config.CreatedAtColumnName == "" {
			return nil, fmt.Errorf("creation timestamp column is not configured")
		}
		column := clause.Column{Name: config.CreatedAtColumnName}
		if !q.FromTime.IsZero() {
			tx = tx.Where(clause.Gte{Column: column, Value: q.FromTime})
		}
		if !q.TillTime.IsZero() {
			tx = tx.Where(clause.Lt{Column: column, Value: q.TillTime})
		}
	}
	if q.Selector != nil && !q.Selector.Empty() && config.LabelsColumnName == "" {
		return nil, fmt.Errorf("labels column is not configured")
	}
	tx, err = withSelector(tx, config.LabelsColumnName, q.Selector)
	if err != nil {
		return nil, err
	}
	return withFieldPredicates(tx, config, policy.Fields, policy.Predicates, q.Fields)
}

// CompareField builds a selector comparison against a server-defined scalar SQL
// expression, for example a CASE expression for an effective lifecycle phase.
// NULL means absent, as in ordinary field selectors. Numeric operators require
// a numeric SQL expression; string-valued expressions should reject them in the
// registered predicate. Values from the requirement are always bound parameters.
func CompareField(value clause.Expression, req manifest.Requirement) (clause.Expression, error) {
	if value == nil {
		return nil, fmt.Errorf("field expression is required")
	}
	return fieldExpression(value, req)
}
