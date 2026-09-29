package dbstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// DefaultPageLimit is the page size of a listing that asks for none.
	DefaultPageLimit uint = 100
	// MaxPageLimit is the largest page a listing returns, whatever it asks for.
	MaxPageLimit uint = 1024
)

var (
	// ErrInvalidCursor is returned for a cursor this store did not produce.
	ErrInvalidCursor error = manifest.NewStatusError(http.StatusBadRequest, "invalid-cursor", "invalid page cursor")

	// ErrConflictingPagination is returned when a query asks to continue from a
	// cursor and to skip by offset at once, or orders a cursor listing
	// differently from the order the cursor encodes.
	ErrConflictingPagination error = manifest.NewStatusError(http.StatusBadRequest, "conflicting-pagination", "conflicting pagination")
)

// Page describes one page of a listing. See [manifest.Page].
type Page = manifest.Page

// KeyKind says how the values of a key column travel in a cursor.
type KeyKind int

const (
	// KeyString is a text column, or anything compared as text.
	KeyString KeyKind = iota
	// KeyTime is a timestamp column.
	KeyTime
)

// KeyColumn is one column of a keyset order.
type KeyColumn struct {
	// Expr is the SQL the rows are ordered by: a quoted column or an
	// expression such as "lower(name)". It must never be NULL, because a row
	// value holding a NULL compares as unknown and the row would never be
	// reached.
	Expr string
	Kind KeyKind
}

// Keyset is the total order of a cursor listing and how to read a row's
// position in it.
//
// The last column must be unique -- an ID -- so that no two rows share a
// position: that is what lets a cursor resume exactly where its page ended.
// Every column is sorted in the same direction, which keeps the continuation a
// single row-value comparison.
type Keyset[T any] struct {
	Columns    []KeyColumn
	Descending bool
	// Key returns the values of Columns for a row, in the same order.
	Key func(row *T) []any
	// NoTotal skips counting the matching rows; the page then has no total.
	NoTotal bool
}

// Cursor returns the cursor continuing after row, for a listing that pages
// through PageBy's rows further -- filtering on something SQL cannot see -- and
// so ends its page on a row other than PageBy's last.
func (k Keyset[T]) Cursor(row *T) (string, error) {
	return keyOrder{columns: k.Columns, descending: k.Descending}.encode(k.Key(row))
}

// keyOrder is the untyped part of a [Keyset]: the order, and the cursor codec.
type keyOrder struct {
	columns    []KeyColumn
	descending bool
}

// signature identifies the order a cursor was issued in, so that a cursor from
// one order is refused by another instead of resuming at a meaningless place.
func (o keyOrder) signature() string {
	h := sha256.New()
	for _, c := range o.columns {
		fmt.Fprintf(h, "%s\x00%d\x00", c.Expr, c.Kind)
	}
	fmt.Fprintf(h, "%t", o.descending)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil)[:6])
}

// cursor is the position after the last row of a page, in the listing's order.
type cursor struct {
	Order string   `json:"o"`
	Key   []string `json:"k"`
}

func (o keyOrder) encode(values []any) (string, error) {
	if len(values) != len(o.columns) {
		return "", fmt.Errorf("a keyset of %d columns got %d key values", len(o.columns), len(values))
	}
	c := cursor{Order: o.signature(), Key: make([]string, len(values))}
	for i, v := range values {
		switch v := v.(type) {
		case time.Time:
			c.Key[i] = v.UTC().Format(time.RFC3339Nano)
		case *time.Time:
			if v == nil {
				return "", fmt.Errorf("key column %q is nil", o.columns[i].Expr)
			}
			c.Key[i] = v.UTC().Format(time.RFC3339Nano)
		default:
			rv := reflect.ValueOf(v)
			if rv.Kind() != reflect.String {
				return "", fmt.Errorf("key column %q has unsupported value type %T", o.columns[i].Expr, v)
			}
			c.Key[i] = rv.String()
		}
	}
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (o keyOrder) decode(value string) ([]any, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	var c cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	if c.Order != o.signature() || len(c.Key) != len(o.columns) {
		return nil, fmt.Errorf("%w: issued for a different order", ErrInvalidCursor)
	}
	values := make([]any, len(c.Key))
	for i, k := range c.Key {
		switch o.columns[i].Kind {
		case KeyTime:
			t, err := time.Parse(time.RFC3339Nano, k)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
			}
			values[i] = t
		default:
			values[i] = k
		}
	}
	return values, nil
}

// window orders tx and, given a cursor, starts it strictly after the cursor.
func (o keyOrder) window(tx *gorm.DB, after string) (*gorm.DB, error) {
	exprs := make([]string, len(o.columns))
	for i, c := range o.columns {
		exprs[i] = c.Expr
	}
	if after != "" {
		values, err := o.decode(after)
		if err != nil {
			return nil, err
		}
		op := ">"
		if o.descending {
			op = "<"
		}
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(values)), ", ")
		tx = tx.Where(clause.Expr{SQL: fmt.Sprintf("(%s) %s (%s)", strings.Join(exprs, ", "), op, marks), Vars: values})
	}
	direction := " ASC"
	if o.descending {
		direction = " DESC"
	}
	return tx.Order(strings.Join(exprs, direction+", ") + direction), nil
}

func pageLimit(limit uint) uint {
	if limit == 0 {
		return DefaultPageLimit
	}
	return min(limit, MaxPageLimit)
}

// PageBy returns one page of the rows tx selects, in order's order, continuing
// from q.Cursor. tx carries the caller's filters -- scope, visibility, search --
// and no order or window of its own; PageBy only narrows it further, so paging
// can never widen what a caller may see. q.Limit is bounded as [FindPage]
// bounds it. An offset is refused: a keyset listing does not skip.
func PageBy[T any](tx *gorm.DB, q manifest.SearchQuery, order Keyset[T]) ([]T, Page, error) {
	if q.Offset > 0 {
		return nil, Page{}, fmt.Errorf("%w: a cursor listing does not skip by offset", ErrConflictingPagination)
	}
	if len(order.Columns) == 0 || order.Key == nil {
		return nil, Page{}, fmt.Errorf("a keyset needs columns and a key")
	}
	if tx.Statement.Model == nil {
		tx = tx.Model(new(T))
	}

	page := Page{Limit: pageLimit(q.Limit)}
	if !order.NoTotal {
		var total int64
		if err := tx.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, Page{}, err
		}
		page.Total = &total
	}

	keys := keyOrder{columns: order.Columns, descending: order.Descending}
	tx, err := keys.window(tx.Session(&gorm.Session{}), q.Cursor)
	if err != nil {
		return nil, Page{}, err
	}

	// One row beyond the page says whether there is a next page, without
	// guessing from a full page or trusting the advisory total.
	rows := []T{}
	if err := tx.Limit(int(page.Limit) + 1).Find(&rows).Error; err != nil {
		return nil, Page{}, err
	}
	if uint(len(rows)) > page.Limit {
		rows = rows[:page.Limit]
		if page.Next, err = keys.encode(order.Key(&rows[len(rows)-1])); err != nil {
			return nil, Page{}, err
		}
	}
	return rows, page, nil
}

// FindPage returns one page of the models matching searchQuery into dest, a
// pointer to a slice of models embedding [manifest.ObjectMeta].
//
// Rows are ordered newest first -- creation time, then UID, descending -- so the
// order is total and a cursor resumes exactly where its page ended, however
// rows before it change in between. That is the difference from offset paging,
// which skips or repeats rows when earlier ones are inserted or deleted.
// searchQuery.Offset is still honoured for callers migrating from it, but not
// together with a cursor.
//
// Ordering options are refused: the order is part of what a cursor means.
func (s *DBStore) FindPage(ctx context.Context, dest any, searchQuery manifest.SearchQuery, options ...Option) (Page, error) {
	tc := compileOptions(s.config, dest, options...)
	if len(tc.Order.OrderColumns) > 0 {
		return Page{}, fmt.Errorf("%w: a cursor listing is ordered by creation time and UID", ErrConflictingPagination)
	}
	if searchQuery.Cursor != "" && searchQuery.Offset > 0 {
		return Page{}, fmt.Errorf("%w: cursor and offset together", ErrConflictingPagination)
	}

	limit := pageLimit(searchQuery.Limit)

	tx, xtx, err := applyContext(s.db.WithContext(ctx), s.config, tc)
	if err != nil {
		return Page{}, err
	}

	// The window is applied here rather than by withQuery, which would page the
	// count query as well.
	unpaged := searchQuery
	unpaged.Offset, unpaged.Limit = 0, 0
	tx, xtx, err = withQuery(tx, xtx, s.config, unpaged, tc.fields)
	if err != nil {
		return Page{}, err
	}
	if tx, err = filterVisible(ctx, s.visibility, tx, dest); err != nil {
		return Page{}, err
	}
	if xtx, err = filterVisible(ctx, s.visibility, xtx, dest); err != nil {
		return Page{}, err
	}

	page := Page{Limit: limit}
	if xtx != nil {
		var total int64
		if err := xtx.Model(dest).Count(&total).Error; err != nil {
			return Page{}, err
		}
		page.Total = &total
	}

	keys := s.createdOrder(tx)
	if tx, err = keys.window(tx, searchQuery.Cursor); err != nil {
		return Page{}, err
	}
	if searchQuery.Offset > 0 {
		tx = tx.Offset(int(searchQuery.Offset))
	}

	// One row beyond the page says whether there is a next page, without
	// guessing from a full page or trusting the advisory total.
	if err := tx.Limit(int(limit) + 1).Find(dest).Error; err != nil {
		return Page{}, err
	}

	rows := reflect.ValueOf(dest).Elem()
	if uint(rows.Len()) > limit {
		rows.SetLen(int(limit))
		last, err := positionOf(rows.Index(int(limit) - 1))
		if err != nil {
			return Page{}, err
		}
		if page.Next, err = keys.encode(last); err != nil {
			return Page{}, err
		}
	}

	return page, nil
}

// createdOrder is FindPage's order: newest first, the UID breaking ties.
func (s *DBStore) createdOrder(tx *gorm.DB) keyOrder {
	return keyOrder{descending: true, columns: []KeyColumn{
		{Expr: tx.Statement.Quote(s.config.CreatedAtColumnName), Kind: KeyTime},
		{Expr: tx.Statement.Quote(s.config.IDColumnName), Kind: KeyString},
	}}
}

// positionOf reads the cursor position of a model embedding manifest.ObjectMeta.
func positionOf(row reflect.Value) ([]any, error) {
	for row.Kind() == reflect.Pointer {
		row = row.Elem()
	}
	if row.Kind() != reflect.Struct {
		return nil, fmt.Errorf("page rows must be structs embedding manifest.ObjectMeta, got %v", row.Type())
	}

	metaField := row.FieldByName("ObjectMeta")
	if !metaField.IsValid() {
		return nil, fmt.Errorf("page rows must embed manifest.ObjectMeta, %v does not", row.Type())
	}
	meta, ok := metaField.Interface().(manifest.ObjectMeta)
	if !ok || meta.CreatedAt == nil || meta.UID == "" {
		return nil, fmt.Errorf("row of %v has no creation time or UID to continue from", row.Type())
	}

	return []any{*meta.CreatedAt, meta.UID}, nil
}

// PageSlice is [PageBy] for rows already in memory -- a listing assembled from
// several queries, or computed rather than stored. It sorts rows by order's key
// itself, so the order and the continuation cannot disagree, and it issues and
// accepts the same cursors PageBy does for that order. Text keys compare
// byte-wise here, where a database compares by collation: a keyset shared with
// PageBy should use keys on which the two agree, such as IDs.
func PageSlice[T any](rows []T, q manifest.SearchQuery, order Keyset[T]) ([]T, Page, error) {
	if q.Offset > 0 {
		return nil, Page{}, fmt.Errorf("%w: a cursor listing does not skip by offset", ErrConflictingPagination)
	}
	if len(order.Columns) == 0 || order.Key == nil {
		return nil, Page{}, fmt.Errorf("a keyset needs columns and a key")
	}
	keys := keyOrder{columns: order.Columns, descending: order.Descending}

	type keyed struct {
		row T
		key []any
	}
	sorted := make([]keyed, len(rows))
	for i := range rows {
		sorted[i] = keyed{row: rows[i], key: normalizeKey(order.Key(&rows[i]))}
	}
	slices.SortStableFunc(sorted, func(a, b keyed) int { return keys.compare(a.key, b.key) })

	page := Page{Limit: pageLimit(q.Limit)}
	if !order.NoTotal {
		total := int64(len(rows))
		page.Total = &total
	}

	start := 0
	if q.Cursor != "" {
		after, err := keys.decode(q.Cursor)
		if err != nil {
			return nil, Page{}, err
		}
		after = normalizeKey(after)
		start, _ = slices.BinarySearchFunc(sorted, after, func(k keyed, target []any) int {
			if keys.compare(k.key, target) <= 0 {
				return -1
			}
			return 1
		})
	}

	end := min(start+int(page.Limit), len(sorted))
	out := make([]T, 0, end-start)
	for _, k := range sorted[start:end] {
		out = append(out, k.row)
	}
	if end < len(sorted) {
		var err error
		if page.Next, err = keys.encode(sorted[end-1].key); err != nil {
			return nil, Page{}, err
		}
	}
	return out, page, nil
}

// normalizeKey turns key values into time.Time and string, the two kinds a
// keyset compares.
func normalizeKey(values []any) []any {
	out := make([]any, len(values))
	for i, v := range values {
		switch v := v.(type) {
		case time.Time:
			out[i] = v
		case *time.Time:
			if v != nil {
				out[i] = *v
			}
		default:
			if rv := reflect.ValueOf(v); rv.Kind() == reflect.String {
				out[i] = rv.String()
			} else {
				out[i] = v
			}
		}
	}
	return out
}

// compare orders two normalized keys in the listing's direction.
func (o keyOrder) compare(a, b []any) int {
	for i := range min(len(a), len(b)) {
		var c int
		switch x := a[i].(type) {
		case time.Time:
			y, _ := b[i].(time.Time)
			c = x.Compare(y)
		case string:
			y, _ := b[i].(string)
			c = strings.Compare(x, y)
		}
		if c != 0 {
			if o.descending {
				return -c
			}
			return c
		}
	}
	return 0
}
