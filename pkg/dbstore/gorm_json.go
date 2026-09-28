package dbstore

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type sqlOp string

const (
	equals      = sqlOp(" = ")
	notEquals   = sqlOp(" <> ")
	notNull     = sqlOp(" IS NOT NULL ")
	isNull      = sqlOp(" IS NULL ")
	greaterThan = sqlOp(" > ")
	lessThan    = sqlOp(" < ")

	isIn    = sqlOp(" IN ")
	isNotIn = sqlOp(" NOT IN ")
)

// jsonQueryExpression json query expression, implements clause.Expression interface to use as querier
type jsonQueryExpression struct {
	asType  string
	column  string
	keys    []string
	hasKeys bool

	keysOp      sqlOp
	op          sqlOp
	equalsValue any

	groupOp       bool
	groupValueSet manifest.StringSet
}

// jsonQuery query column as json
func jsonQuery(column string) *jsonQueryExpression {
	return &jsonQueryExpression{column: column}
}

// HasKey returns clause.Expression
func (jsonQuery *jsonQueryExpression) HasKey(keys ...string) *jsonQueryExpression {
	jsonQuery.keys = keys
	jsonQuery.hasKeys = true
	jsonQuery.keysOp = notNull
	return jsonQuery
}

func (jsonQuery *jsonQueryExpression) HasNoKey(keys ...string) *jsonQueryExpression {
	jsonQuery.keys = keys
	jsonQuery.hasKeys = true
	jsonQuery.keysOp = isNull
	return jsonQuery
}

func (jsonQuery *jsonQueryExpression) setOp(inOp sqlOp, value any, keys ...string) *jsonQueryExpression {
	jsonQuery.keys = keys
	jsonQuery.op = inOp
	jsonQuery.equalsValue = value
	return jsonQuery
}

func (jsonQuery *jsonQueryExpression) Equals(value any, keys ...string) *jsonQueryExpression {
	return jsonQuery.setOp(equals, value, keys...)
}

func (jsonQuery *jsonQueryExpression) NotEquals(value any, keys ...string) *jsonQueryExpression {
	return jsonQuery.setOp(notEquals, value, keys...)
}

func (jsonQuery *jsonQueryExpression) GreaterThan(value any, keys ...string) *jsonQueryExpression {
	jsonQuery.asType = "int"
	return jsonQuery.setOp(greaterThan, value, keys...)
}

func (jsonQuery *jsonQueryExpression) LessThan(value any, keys ...string) *jsonQueryExpression {
	jsonQuery.asType = "int"
	return jsonQuery.setOp(lessThan, value, keys...)
}

func (jsonQuery *jsonQueryExpression) KeyIn(key string, values manifest.StringSet) *jsonQueryExpression {
	jsonQuery.keys = []string{key}
	jsonQuery.op = isIn
	jsonQuery.groupValueSet = values
	jsonQuery.groupOp = true

	return jsonQuery
}

func (jsonQuery *jsonQueryExpression) KeyNotIn(key string, values manifest.StringSet) *jsonQueryExpression {
	jsonQuery.keys = []string{key}
	jsonQuery.op = isNotIn
	jsonQuery.groupValueSet = values
	jsonQuery.groupOp = true

	return jsonQuery
}

const prefixDotless = "$"

func jsonPathKey(key string) string {
	return "\"" + key + "\""
}

func jsonQueryJoin(keys []string) string {
	if len(keys) == 1 {
		return prefixDotless + "." + jsonPathKey(keys[0])
	}

	n := len(prefixDotless) + len(keys)
	for i := 0; i < len(keys); i++ {
		n += len(keys[i]) + 2
	}

	var b strings.Builder
	b.Grow(n)
	b.WriteString(prefixDotless)
	for _, key := range keys {
		b.WriteString(".")
		b.WriteString(jsonPathKey(key))
	}

	return b.String()
}

// Build implements clause.Expression.
//
// The SQL must select exactly what [manifest.Requirement.Matches] admits, because
// callers filter the same resources both ways and expect the same answer. Two
// rules make that true, and plain SQL comparison gets both wrong:
//
//   - a negative requirement (`!=`, `notin`) matches a resource without the key.
//     A missing JSON key extracts as NULL, and NULL <> 'x' is not true, so the
//     comparison alone drops exactly those resources;
//   - a numeric requirement (`gt`, `lt`) matches only an integer label value in
//     int64 range. Anything else is a non-match. It is never an error that fails
//     the whole query (Postgres' cast), and never a zero that happens to satisfy
//     the bound (SQLite's cast).
func (jsonQuery *jsonQueryExpression) Build(builder clause.Builder) {
	stmt, ok := builder.(*gorm.Statement)
	if !ok {
		return
	}

	dialect := stmt.Dialector.Name()

	if jsonQuery.hasKeys {
		if len(jsonQuery.keys) == 0 {
			return
		}
		switch dialect {
		case "mysql", "sqlite":
			jsonQuery.writeExtract(stmt, dialect)
		case "postgres":
			// A key's presence is tested with ->> / #>> rather than
			// json_extract_path_text, which is equivalent for a flat label map.
			stmt.WriteQuoted(jsonQuery.column)
			stmt.WriteString("::json")
			if len(jsonQuery.keys) == 1 {
				stmt.WriteString(" ->> ")
				stmt.AddVar(builder, jsonQuery.keys[0])
			} else {
				stmt.WriteString(" #>> {")
				for idx, key := range jsonQuery.keys {
					if idx > 0 {
						builder.WriteByte(',')
					}
					stmt.AddVar(builder, key)
				}
				stmt.WriteString("}")
			}
		default:
			return
		}
		builder.WriteString(string(jsonQuery.keysOp))
		return
	}

	if len(jsonQuery.op) == 0 || len(jsonQuery.keys) == 0 {
		return
	}

	switch jsonQuery.op {
	case notEquals, isNotIn:
		builder.WriteString("(")
		jsonQuery.writeExtract(stmt, dialect)
		builder.WriteString(" IS NULL OR ")
		jsonQuery.writeExtract(stmt, dialect)
		builder.WriteString(string(jsonQuery.op))
		jsonQuery.writeOperand(stmt, dialect)
		builder.WriteString(")")
	case greaterThan, lessThan:
		jsonQuery.writeInteger(stmt, dialect)
		builder.WriteString(string(jsonQuery.op))
		stmt.AddVar(builder, jsonQuery.equalsValue)
	default:
		jsonQuery.writeExtract(stmt, dialect)
		builder.WriteString(string(jsonQuery.op))
		jsonQuery.writeOperand(stmt, dialect)
	}
}

// writeExtract writes the expression reading the label as text, NULL if absent.
func (jsonQuery *jsonQueryExpression) writeExtract(stmt *gorm.Statement, dialect string) {
	switch dialect {
	case "mysql", "sqlite":
		stmt.WriteString("JSON_EXTRACT(")
		stmt.WriteQuoted(jsonQuery.column)
		stmt.WriteByte(',')
		stmt.AddVar(stmt, jsonQueryJoin(jsonQuery.keys))
		stmt.WriteString(")")
	case "postgres":
		stmt.WriteString(fmt.Sprintf("json_extract_path_text(%v::json,", stmt.Quote(jsonQuery.column)))
		for idx, key := range jsonQuery.keys {
			if idx > 0 {
				stmt.WriteByte(',')
			}
			stmt.AddVar(stmt, key)
		}
		stmt.WriteString(")")
	}
}

// writeInteger writes the label as an integer, or NULL when it is not one.
//
// Postgres gets nested CASEs rather than one condition joined by AND: it does not
// promise to evaluate an AND's operands in order, so a regular-expression guard
// beside the cast would not stop the cast from failing the query.
func (jsonQuery *jsonQueryExpression) writeInteger(stmt *gorm.Statement, dialect string) {
	switch dialect {
	case "postgres":
		stmt.WriteString("(CASE WHEN ")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(" ~ '^[-+]?[0-9]+$' THEN CASE WHEN CAST(")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(" AS numeric) BETWEEN -9223372036854775808 AND 9223372036854775807 THEN CAST(")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(" AS bigint) END END)")
	case "sqlite":
		// Sign stripped, the rest must be all digits. At most 18 significant
		// digits are admitted, which stays inside int64 without a range check
		// SQLite cannot express; a 19-digit value is a non-match here, where Go
		// would compare it.
		stmt.WriteString("(CASE WHEN typeof(")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(") = 'text' AND ltrim(")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(", '+-') <> '' AND ltrim(")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(", '+-') NOT GLOB '*[^0-9]*' AND length(")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(") - length(ltrim(")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(", '+-')) <= 1 AND length(ltrim(ltrim(")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(", '+-'), '0')) <= 18 THEN CAST(")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(" AS INTEGER) END)")
	default:
		// MySQL keeps its previous rendering. It has no test server here, so
		// its semantics are not claimed.
		stmt.WriteString("cast(")
		jsonQuery.writeExtract(stmt, dialect)
		stmt.WriteString(" as signed)")
	}
}

// writeOperand writes the value or value set a comparison is against.
func (jsonQuery *jsonQueryExpression) writeOperand(stmt *gorm.Statement, dialect string) {
	if jsonQuery.groupOp {
		// StringSet.Slice is sorted, so a selector always renders the same SQL.
		stmt.WriteString("(")
		for idx, v := range jsonQuery.groupValueSet.Slice() {
			if idx > 0 {
				stmt.WriteByte(',')
			}
			stmt.AddVar(stmt, v)
		}
		stmt.WriteString(")")
		return
	}

	switch dialect {
	case "postgres":
		if _, ok := jsonQuery.equalsValue.(string); ok {
			stmt.AddVar(stmt, jsonQuery.equalsValue)
		} else {
			stmt.AddVar(stmt, fmt.Sprint(jsonQuery.equalsValue))
		}
	default:
		if value, ok := jsonQuery.equalsValue.(bool); ok {
			stmt.WriteString(strconv.FormatBool(value))
		} else {
			stmt.AddVar(stmt, jsonQuery.equalsValue)
		}
	}
}

type jsonExtractExpression struct {
	column string
}

// JSONExtract extracts key,values as text from a Column holding JSON
func JSONExtract(column string) *jsonExtractExpression {
	return &jsonExtractExpression{column: column}
}

// Build implements GORM Expression interface
func (jsonQuery *jsonExtractExpression) Build(builder clause.Builder) {
	stmt, ok := builder.(*gorm.Statement)
	if !ok {
		return
	}

	switch stmt.Dialector.Name() {
	case "mysql", "sqlite":
		builder.WriteString(", json_each(")
		builder.WriteQuoted(jsonQuery.column)
		builder.WriteByte(')')
	case "postgres":
		builder.WriteString(", json_each_text(")
		builder.WriteQuoted(jsonQuery.column)
		builder.WriteByte(')')
	}
}
