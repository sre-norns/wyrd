package dbstore

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrUnknownField is returned when a field selector names an attribute that is
// not selectable. It is reported before any SQL runs: an unknown field silently
// matching everything would read as a filter that worked.
var ErrUnknownField error = manifest.NewStatusError(http.StatusBadRequest, "unknown-field", "field is not selectable")

// FieldColumns maps selectable field paths, as a client writes them, to the
// columns that hold them. See [Fields].
type FieldColumns map[string]string

// metadataFields returns the fields every [manifest.ObjectMeta] model offers.
func metadataFields(config SchemaConfig) FieldColumns {
	fields := FieldColumns{
		"metadata.uid":     config.IDColumnName,
		"metadata.name":    config.NameColumnName,
		"metadata.version": config.VersionColumnName,
	}
	if config.AccountColumnName != "" {
		fields["metadata.account"] = config.AccountColumnName
	}
	if config.ProjectColumnName != "" {
		fields["metadata.project"] = config.ProjectColumnName
	}
	return fields
}

// Fields makes more attributes selectable by a query's field selector, beyond
// the metadata fields every model offers (metadata.uid, .name, .version,
// .account, .project). A product declares one map per kind -- for example
// {"status.phase": "status_phase"} -- and passes it with the kind's list calls.
func Fields(columns FieldColumns) Option {
	return func(a any, tc transactionContext) transactionContext {
		if tc.fields == nil {
			tc.fields = FieldColumns{}
		}
		for field, column := range columns {
			tc.fields[field] = column
		}
		return tc
	}
}

// withFields narrows tx by a field selector. Semantics follow
// [manifest.Requirement.Matches], reading a NULL column as an absent field:
// negative operators admit it, and every other operator does not.
func withFields(tx *gorm.DB, config SchemaConfig, extra FieldColumns, selector manifest.Selector) (*gorm.DB, error) {
	if tx == nil || selector == nil || selector.Empty() {
		return tx, nil
	}

	reqs, ok := selector.Requirements()
	if !ok {
		return nil, manifest.ErrNonSelectableRequirements
	}

	known := metadataFields(config)
	for field, column := range extra {
		known[field] = column
	}

	for _, req := range reqs {
		name, ok := known[req.Key()]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownField, req.Key())
		}
		column := clause.Column{Name: name}

		expr, err := fieldExpression(column, req)
		if err != nil {
			return nil, err
		}
		tx = tx.Where(expr)
	}

	return tx, nil
}

func fieldExpression(column clause.Column, req manifest.Requirement) (clause.Expression, error) {
	isNull := clause.Eq{Column: column, Value: nil}

	switch req.Operator() {
	case manifest.Exists:
		return clause.Neq{Column: column, Value: nil}, nil
	case manifest.DoesNotExist:
		return isNull, nil
	}

	values := req.Values().Slice()
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: no value for field %q", ErrNoRequirementsValueProvided, req.Key())
	}
	set := make([]any, len(values))
	for i, v := range values {
		set[i] = v
	}

	switch req.Operator() {
	case manifest.Equals, manifest.DoubleEquals:
		return clause.Eq{Column: column, Value: values[0]}, nil
	case manifest.In:
		return clause.IN{Column: column, Values: set}, nil
	case manifest.NotEquals:
		return clause.Or(isNull, clause.Neq{Column: column, Value: values[0]}), nil
	case manifest.NotIn:
		return clause.Or(isNull, clause.Not(clause.IN{Column: column, Values: set})), nil
	case manifest.GreaterThan, manifest.LessThan:
		bound, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: field %q needs an integer bound: %v", manifest.ErrNonSelectableRequirements, req.Key(), err)
		}
		if req.Operator() == manifest.GreaterThan {
			return clause.Gt{Column: column, Value: bound}, nil
		}
		return clause.Lt{Column: column, Value: bound}, nil
	default:
		return nil, fmt.Errorf("%w: `%v`", ErrUnexpectedSelectorOperator, req.Operator())
	}
}
