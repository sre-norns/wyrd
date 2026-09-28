package dbstore_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedCrates creates n crates, each a second older than the next, so the
// newest-first order is known: crate-(n-1) first, crate-0 last.
func seedCrates(t *testing.T, db *gorm.DB, store *dbstore.DBStore, n int, scope manifest.ScopeRef) {
	t.Helper()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		c := scopedCrate(fmt.Sprintf("crate-%02d", i), scope, nil)
		require.NoError(t, store.Create(context.Background(), c))
		require.NoError(t, db.Model(&crate{}).Where("uid = ?", c.UID).UpdateColumn("created_at", base.Add(time.Duration(i)*time.Second)).Error)
	}
}

func plainNames(crates []crate) []string {
	names := make([]string, 0, len(crates))
	for _, c := range crates {
		names = append(names, string(c.Name))
	}
	return names
}

// listAll pages through everything, returning names in the order received.
func listAll(t *testing.T, store *dbstore.DBStore, query manifest.SearchQuery, between func(page int), options ...dbstore.Option) []string {
	t.Helper()
	var names []string
	for page := 0; ; page++ {
		var batch []crate
		result, err := store.FindPage(context.Background(), &batch, query, options...)
		require.NoError(t, err)
		require.LessOrEqual(t, uint(len(batch)), result.Limit)
		names = append(names, plainNames(batch)...)
		if result.Next == "" {
			return names
		}
		require.Len(t, batch, int(result.Limit), "only a full page may have a next page")
		query.Cursor = result.Next
		if between != nil {
			between(page)
		}
		require.Less(t, page, 100, "paging did not terminate")
	}
}

func TestFindPageVisitsEveryRowOnceNewestFirst(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		store := newCrateStore(t, db)
		seedCrates(t, db, store, 7, acme)

		want := []string{"crate-06", "crate-05", "crate-04", "crate-03", "crate-02", "crate-01", "crate-00"}
		for _, limit := range []uint{1, 2, 3, 7, 8} {
			got := listAll(t, store, manifest.SearchQuery{Limit: limit}, nil)
			require.Equal(t, want, got, "limit %d", limit)
		}

		// A last page that is exactly full still has no next page: the store
		// looked one row further and found none.
		var batch []crate
		page, err := store.FindPage(context.Background(), &batch, manifest.SearchQuery{Limit: 7})
		require.NoError(t, err)
		require.Len(t, batch, 7)
		require.Empty(t, page.Next)
		require.NotNil(t, page.Total)
		require.EqualValues(t, 7, *page.Total)
	})
}

// The reason for cursors. With offsets, a row inserted ahead of the reader
// shifts the window and the next page repeats a row; a row deleted ahead of it
// shifts the other way and one is skipped. A cursor names the last row seen.
func TestFindPageIsStableUnderConcurrentWrites(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		store := newCrateStore(t, db)
		seedCrates(t, db, store, 6, acme)
		ctx := context.Background()

		got := listAll(t, store, manifest.SearchQuery{Limit: 2}, func(page int) {
			switch page {
			case 0:
				// A new row arrives, newest of all: it belongs before the
				// cursor, so this listing does not see it and nothing shifts.
				require.NoError(t, store.Create(ctx, scopedCrate("latecomer", acme, nil)))
			case 1:
				// A row already returned is deleted.
				var gone crate
				ok, err := store.GetByName(ctx, &gone, "crate-05")
				require.NoError(t, err)
				require.True(t, ok)
				_, err = store.Delete(ctx, &crate{}, gone.UID, 0)
				require.NoError(t, err)
			}
		})

		require.Equal(t, []string{"crate-05", "crate-04", "crate-03", "crate-02", "crate-01", "crate-00"}, got,
			"every row that existed when paging began is seen exactly once")
	})
}

func TestFindPageBoundsAndConflicts(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		store := newCrateStore(t, db)
		seedCrates(t, db, store, 3, acme)
		ctx := context.Background()

		var batch []crate
		page, err := store.FindPage(ctx, &batch, manifest.SearchQuery{})
		require.NoError(t, err)
		require.Equal(t, dbstore.DefaultPageLimit, page.Limit, "no limit means the default, never unbounded")

		page, err = store.FindPage(ctx, &batch, manifest.SearchQuery{Limit: 1 << 20})
		require.NoError(t, err)
		require.Equal(t, dbstore.MaxPageLimit, page.Limit)

		page, err = store.FindPage(ctx, &batch, manifest.SearchQuery{Limit: 1}, dbstore.Count(false))
		require.NoError(t, err)
		require.Nil(t, page.Total, "counting disabled is reported as no total, not as zero")
		require.NotEmpty(t, page.Next)

		_, err = store.FindPage(ctx, &batch, manifest.SearchQuery{Cursor: page.Next, Offset: 1})
		require.ErrorIs(t, err, dbstore.ErrConflictingPagination)

		_, err = store.FindPage(ctx, &batch, manifest.SearchQuery{}, dbstore.OrderByCreatedAt(dbstore.OrderAscending))
		require.ErrorIs(t, err, dbstore.ErrConflictingPagination)

		for _, bad := range []string{"not-base64!", "e30", "eyJ1IjoieCJ9"} {
			_, err = store.FindPage(ctx, &batch, manifest.SearchQuery{Cursor: bad})
			require.ErrorIs(t, err, dbstore.ErrInvalidCursor, bad)
		}
	})
}

func TestFindPageRespectsScopeAndVisibility(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		store := newCrateStore(t, db)
		seedCrates(t, db, store, 3, acme)
		seedCrates(t, db, store, 3, ledger)

		got := listAll(t, store, manifest.SearchQuery{Limit: 2}, nil, dbstore.InScope(ledger))
		require.Len(t, got, 3)

		visible := store.WithVisibility(projectOnly{project: acme.Project})
		var batch []crate
		page, err := visible.FindPage(context.Background(), &batch, manifest.SearchQuery{})
		require.NoError(t, err)
		require.EqualValues(t, 3, *page.Total)
		for _, c := range batch {
			require.Equal(t, acme.Project, c.Project)
		}
	})
}

func TestFieldSelectors(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		store := newCrateStore(t, db)
		ctx := context.Background()
		for _, c := range []*crate{
			{ObjectMeta: manifest.ObjectMeta{Name: "a", Account: "acme", Project: "payments"}, Spec: crateSpec{Note: "fragile"}},
			{ObjectMeta: manifest.ObjectMeta{Name: "b", Account: "acme", Project: "search"}, Spec: crateSpec{Note: "heavy"}},
			{ObjectMeta: manifest.ObjectMeta{Name: "c", Account: "globex", Project: "ledger"}},
		} {
			require.NoError(t, store.Create(ctx, c))
		}
		// Absent, not merely empty: the case negative operators must admit.
		require.NoError(t, db.Model(&crate{}).Where("name = ?", "c").UpdateColumn("note", nil).Error)

		noteField := dbstore.Fields(dbstore.FieldColumns{"spec.note": "note"})

		cases := map[string][]string{
			"metadata.project=payments":             {"a"},
			"metadata.account=acme":                 {"a", "b"},
			"metadata.project in (search,ledger)":   {"b", "c"},
			"metadata.name notin (a)":               {"b", "c"},
			"spec.note=fragile":                     {"a"},
			"spec.note!=fragile":                    {"b", "c"},
			"spec.note notin (fragile,heavy)":       {"c"},
			"spec.note":                             {"a", "b"},
			"!spec.note":                            {"c"},
			"metadata.account=acme,spec.note=heavy": {"b"},
		}
		for expr, want := range cases {
			fields, err := manifest.ParseSelector(expr)
			require.NoError(t, err)

			var found []crate
			_, err = store.Find(ctx, &found, manifest.SearchQuery{Fields: fields}, noteField)
			require.NoError(t, err, expr)
			require.ElementsMatch(t, want, plainNames(found), expr)
		}

		// The same query, without the product declaring the field, is refused
		// rather than ignored.
		fields, err := manifest.ParseSelector("spec.note=fragile")
		require.NoError(t, err)
		var found []crate
		_, err = store.Find(ctx, &found, manifest.SearchQuery{Fields: fields})
		require.ErrorIs(t, err, dbstore.ErrUnknownField)

		// A label called like a field is still a label.
		labelled := &crate{ObjectMeta: manifest.ObjectMeta{Name: "d", Labels: manifest.Labels{"metadata.project": "payments"}}}
		require.NoError(t, store.Create(ctx, labelled))
		labels, err := manifest.ParseSelector("metadata.project=payments")
		require.NoError(t, err)
		found = nil
		_, err = store.Find(ctx, &found, manifest.SearchQuery{Selector: labels})
		require.NoError(t, err)
		require.Equal(t, []string{"d"}, plainNames(found))
	})
}
