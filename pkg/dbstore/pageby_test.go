package dbstore_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// entry is a flat model, as products that predate manifest.ObjectMeta have:
// a text ID and a creation time, nothing embedded.
type entry struct {
	ID        string `gorm:"primaryKey"`
	Name      string
	CreatedAt time.Time
}

var (
	newestFirst = dbstore.Keyset[entry]{
		Descending: true,
		Columns:    []dbstore.KeyColumn{{Expr: "created_at", Kind: dbstore.KeyTime}, {Expr: "id"}},
		Key:        func(e *entry) []any { return []any{e.CreatedAt, e.ID} },
	}
	byName = dbstore.Keyset[entry]{
		Columns: []dbstore.KeyColumn{{Expr: "lower(name)"}, {Expr: "id"}},
		Key:     func(e *entry) []any { return []any{toLower(e.Name), e.ID} },
	}
)

func toLower(s string) string {
	out := []byte(s)
	for i, c := range out {
		if 'A' <= c && c <= 'Z' {
			out[i] = c + 'a' - 'A'
		}
	}
	return string(out)
}

// seedEntries creates entries whose creation times collide in pairs, so the ID
// has to break ties: e-00 and e-01 share a second, e-02 and e-03 the next, ...
func seedEntries(t *testing.T, db *gorm.DB, names ...string) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&entry{}))
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, name := range names {
		require.NoError(t, db.Create(&entry{ID: fmt.Sprintf("e-%02d", i), Name: name, CreatedAt: base.Add(time.Duration(i/2) * time.Second)}).Error)
	}
}

func pageAll(t *testing.T, db *gorm.DB, q manifest.SearchQuery, order dbstore.Keyset[entry], between func()) []string {
	t.Helper()
	var ids []string
	for n := 0; ; n++ {
		rows, page, err := dbstore.PageBy(db.Model(&entry{}), q, order)
		require.NoError(t, err)
		require.LessOrEqual(t, uint(len(rows)), page.Limit)
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		if page.Next == "" {
			return ids
		}
		require.Len(t, rows, int(page.Limit), "only a full page may have a next page")
		q.Cursor = page.Next
		if between != nil {
			between()
		}
		require.Less(t, n, 100, "paging did not terminate")
	}
}

func TestPageByTimeKeyBreaksTiesByID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		seedEntries(t, db, "a", "b", "c", "d", "e")
		got := pageAll(t, db, manifest.SearchQuery{Limit: 2}, newestFirst, nil)
		require.Equal(t, []string{"e-04", "e-03", "e-02", "e-01", "e-00"}, got)
	})
}

func TestPageByTextKeyAscending(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		seedEntries(t, db, "delta", "Alpha", "charlie", "alpha", "Bravo")
		got := pageAll(t, db, manifest.SearchQuery{Limit: 2}, byName, nil)
		// "Alpha" and "alpha" fold to one key; the ID orders them.
		require.Equal(t, []string{"e-01", "e-03", "e-04", "e-02", "e-00"}, got)
	})
}

func TestPageByAppliesTheCallersFilterFirst(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		seedEntries(t, db, "keep", "drop", "keep", "drop", "keep")
		rows, page, err := dbstore.PageBy(db.Model(&entry{}).Where("name = ?", "keep"), manifest.SearchQuery{Limit: 2}, newestFirst)
		require.NoError(t, err)
		require.EqualValues(t, 3, *page.Total, "the total counts the filtered rows, not the table")
		require.Len(t, rows, 2)

		rows, page, err = dbstore.PageBy(db.Model(&entry{}).Where("name = ?", "keep"), manifest.SearchQuery{Limit: 2, Cursor: page.Next}, newestFirst)
		require.NoError(t, err)
		require.Equal(t, []string{"e-00"}, []string{rows[0].ID})
		require.Empty(t, page.Next, "the last page has no next, even though unfiltered rows follow")
	})
}

func TestPageByEmptyAndExactPages(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&entry{}))
		rows, page, err := dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{}, newestFirst)
		require.NoError(t, err)
		require.NotNil(t, rows, "an empty page is an empty list, not null")
		require.Empty(t, rows)
		require.Empty(t, page.Next)
		require.EqualValues(t, 0, *page.Total)
		require.Equal(t, dbstore.DefaultPageLimit, page.Limit)

		seedEntries(t, db, "a", "b")
		rows, page, err = dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{Limit: 2}, newestFirst)
		require.NoError(t, err)
		require.Len(t, rows, 2)
		require.Empty(t, page.Next, "a page exactly as long as what remains is the last one")

		_, page, err = dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{Limit: 1 << 20}, newestFirst)
		require.NoError(t, err)
		require.Equal(t, dbstore.MaxPageLimit, page.Limit)

		noTotal := newestFirst
		noTotal.NoTotal = true
		_, page, err = dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{}, noTotal)
		require.NoError(t, err)
		require.Nil(t, page.Total)
	})
}

// A value keyset remembers where the page ended, not which row ended it: the
// row may go and the listing still continues after where it was.
func TestPageByContinuesPastADeletedCursorRow(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		seedEntries(t, db, "a", "b", "c", "d", "e")
		rows, page, err := dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{Limit: 2}, newestFirst)
		require.NoError(t, err)
		require.NoError(t, db.Delete(&entry{}, "id = ?", rows[1].ID).Error)

		rows, _, err = dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{Limit: 10, Cursor: page.Next}, newestFirst)
		require.NoError(t, err)
		ids := []string{}
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		require.Equal(t, []string{"e-02", "e-01", "e-00"}, ids)
	})
}

func TestPageByRefusesForeignCursorsAndOffsets(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		seedEntries(t, db, "a", "b", "c")
		_, page, err := dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{Limit: 1}, newestFirst)
		require.NoError(t, err)
		require.NotEmpty(t, page.Next)

		_, _, err = dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{Cursor: page.Next}, byName)
		require.ErrorIs(t, err, dbstore.ErrInvalidCursor, "a cursor from another order would resume at a meaningless place")

		oldest := newestFirst
		oldest.Descending = false
		_, _, err = dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{Cursor: page.Next}, oldest)
		require.ErrorIs(t, err, dbstore.ErrInvalidCursor, "reversing the direction is a different order")

		for _, bad := range []string{"not-base64!", "e30", "eyJ1IjoieCJ9", page.Next[:len(page.Next)-2]} {
			_, _, err = dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{Cursor: bad}, newestFirst)
			require.ErrorIs(t, err, dbstore.ErrInvalidCursor, bad)
		}

		_, _, err = dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{Offset: 1}, newestFirst)
		require.ErrorIs(t, err, dbstore.ErrConflictingPagination)
	})
}

func TestPageSliceSortsAndContinuesLikePageBy(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var rows []entry
	for i := 0; i < 7; i++ {
		// Shuffled, with creation times colliding in pairs.
		j := (i * 3) % 7
		rows = append(rows, entry{ID: fmt.Sprintf("e-%02d", j), CreatedAt: base.Add(time.Duration(j/2) * time.Second)})
	}

	var got []string
	q := manifest.SearchQuery{Limit: 3}
	for n := 0; ; n++ {
		out, page, err := dbstore.PageSlice(rows, q, newestFirst)
		require.NoError(t, err)
		require.EqualValues(t, 7, *page.Total)
		for _, r := range out {
			got = append(got, r.ID)
		}
		if page.Next == "" {
			break
		}
		q.Cursor = page.Next
		require.Less(t, n, 10)
	}
	require.Equal(t, []string{"e-06", "e-05", "e-04", "e-03", "e-02", "e-01", "e-00"}, got)

	// The cursor names a position, not a row: with the row gone, the listing
	// still continues after where it was.
	_, page, err := dbstore.PageSlice(rows, manifest.SearchQuery{Limit: 2}, newestFirst)
	require.NoError(t, err)
	rest := slices.DeleteFunc(slices.Clone(rows), func(e entry) bool { return e.ID == "e-05" })
	out, _, err := dbstore.PageSlice(rest, manifest.SearchQuery{Limit: 2, Cursor: page.Next}, newestFirst)
	require.NoError(t, err)
	require.Equal(t, "e-04", out[0].ID)

	_, _, err = dbstore.PageSlice(rows, manifest.SearchQuery{Cursor: page.Next}, byName)
	require.ErrorIs(t, err, dbstore.ErrInvalidCursor)
	_, _, err = dbstore.PageSlice(rows, manifest.SearchQuery{Offset: 1}, newestFirst)
	require.ErrorIs(t, err, dbstore.ErrConflictingPagination)

	out, page, err = dbstore.PageSlice[entry](nil, manifest.SearchQuery{}, newestFirst)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Empty(t, page.Next)
}

// A listing that filters PageBy's rows further ends its page on a row of its
// choosing; the cursor for that row continues right after it.
func TestKeysetCursorContinuesAfterAnyRow(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		seedEntries(t, db, "a", "b", "c", "d", "e")
		rows, _, err := dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{}, newestFirst)
		require.NoError(t, err)
		cursor, err := newestFirst.Cursor(&rows[1])
		require.NoError(t, err)

		rows, _, err = dbstore.PageBy(db.Model(&entry{}), manifest.SearchQuery{Limit: 1, Cursor: cursor}, newestFirst)
		require.NoError(t, err)
		require.Equal(t, "e-02", rows[0].ID)
	})
}
