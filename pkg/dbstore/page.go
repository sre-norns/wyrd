package dbstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
	ErrInvalidCursor = errors.New("invalid page cursor")

	// ErrConflictingPagination is returned when a query asks to continue from a
	// cursor and to skip by offset at once, or orders a cursor listing
	// differently from the order the cursor encodes.
	ErrConflictingPagination = errors.New("conflicting pagination")
)

// Page describes one page of a listing. See [manifest.Page].
type Page = manifest.Page

// cursor is the position after the last row of a page, in the listing's order.
type cursor struct {
	CreatedAt time.Time           `json:"c"`
	UID       manifest.ResourceID `json:"u"`
}

func encodeCursor(c cursor) string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(value string) (cursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor{}, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}

	var c cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return cursor{}, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	if c.UID == "" || c.CreatedAt.IsZero() {
		return cursor{}, fmt.Errorf("%w: incomplete position", ErrInvalidCursor)
	}

	return c, nil
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

	limit := searchQuery.Limit
	if limit == 0 {
		limit = DefaultPageLimit
	}
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}

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

	if searchQuery.Cursor != "" {
		after, err := decodeCursor(searchQuery.Cursor)
		if err != nil {
			return Page{}, err
		}
		// Row-value comparison: strictly after the cursor in descending order.
		tx = tx.Where(clause.Expr{
			SQL:  fmt.Sprintf("(%s, %s) < (?, ?)", s.quote(tx, s.config.CreatedAtColumnName), s.quote(tx, s.config.IDColumnName)),
			Vars: []any{after.CreatedAt, after.UID},
		})
	}

	tx = tx.Order(clause.OrderBy{Columns: []clause.OrderByColumn{
		{Column: clause.Column{Name: s.config.CreatedAtColumnName}, Desc: true},
		{Column: clause.Column{Name: s.config.IDColumnName}, Desc: true},
	}})
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
		page.Next = encodeCursor(last)
	}

	return page, nil
}

func (s *DBStore) quote(tx *gorm.DB, column string) string {
	return tx.Statement.Quote(column)
}

// positionOf reads the cursor position of a model embedding manifest.ObjectMeta.
func positionOf(row reflect.Value) (cursor, error) {
	for row.Kind() == reflect.Pointer {
		row = row.Elem()
	}
	if row.Kind() != reflect.Struct {
		return cursor{}, fmt.Errorf("page rows must be structs embedding manifest.ObjectMeta, got %v", row.Type())
	}

	metaField := row.FieldByName("ObjectMeta")
	if !metaField.IsValid() {
		return cursor{}, fmt.Errorf("page rows must embed manifest.ObjectMeta, %v does not", row.Type())
	}
	meta, ok := metaField.Interface().(manifest.ObjectMeta)
	if !ok || meta.CreatedAt == nil || meta.UID == "" {
		return cursor{}, fmt.Errorf("row of %v has no creation time or UID to continue from", row.Type())
	}

	return cursor{CreatedAt: *meta.CreatedAt, UID: meta.UID}, nil
}
