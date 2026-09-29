package identity

import (
	"errors"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

// NewestFirst is the order of every list that offers no other (ADR 0001 §8):
// creation time, then ID, descending. T must be a resource model.
func NewestFirst[T any]() dbstore.Keyset[T] {
	return dbstore.Keyset[T]{
		Descending: true,
		Columns:    []dbstore.KeyColumn{{Expr: "created_at", Kind: dbstore.KeyTime}, {Expr: "id"}},
		Key: func(row *T) []any {
			m := metadata(row)
			return []any{m.CreatedAt, m.ID}
		},
	}
}

// pageError reports a cursor or window the store refused as the client's
// mistake, in the problem vocabulary of this module.
func pageError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, dbstore.ErrInvalidCursor):
		return &Problem{Status: 400, Code: "invalid-cursor", Detail: "The cursor is not valid for this list; start again from the first page."}
	case errors.Is(err, dbstore.ErrConflictingPagination):
		return &Problem{Status: 400, Code: "offset-unsupported", Detail: "Lists page by cursor only."}
	}
	return err
}

// countOf counts the rows tx selects, before an order, a window or an extra
// selected column is added to it.
func countOf(tx *gorm.DB) (*int64, error) {
	var total int64
	if err := tx.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, err
	}
	return &total, nil
}

// systemNewestFirst is [NewestFirst] for system records.
func systemNewestFirst[T any]() dbstore.Keyset[T] {
	return dbstore.Keyset[T]{
		Descending: true,
		Columns:    []dbstore.KeyColumn{{Expr: "created_at", Kind: dbstore.KeyTime}, {Expr: "id"}},
		Key: func(row *T) []any {
			m := any(row).(interface{ SystemMetadata() *e.SystemRecord }).SystemMetadata()
			return []any{m.CreatedAt, m.ID}
		},
	}
}

// systemPageOf pages the rows tx selects for a system list. System lists
// follow the same contract as every other list: newest first unless sorted,
// 100 rows by default, at most 1024.
func systemPageOf[T any](tx *gorm.DB, q e.SystemQuery, keys dbstore.Keyset[T]) ([]T, manifest.Page, error) {
	if q.Limit < 0 {
		return nil, manifest.Page{}, invalid("Limit must not be negative.")
	}
	rows, page, err := dbstore.PageBy(tx, manifest.SearchQuery{Cursor: q.Cursor, Limit: uint(q.Limit)}, keys)
	return rows, page, pageError(err)
}

// systemPage wraps a page of system list items.
func systemPage[T any](db *gorm.DB, items []T, page manifest.Page) (e.SystemPage[T], error) {
	if items == nil {
		items = []T{}
	}
	out := e.SystemPage[T]{Items: items, Limit: int(page.Limit), Next: page.Next, Total: page.Total}
	var err error
	out.GeneratedAt, err = now(db)
	return out, err
}
