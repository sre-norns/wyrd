package dbstore_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/sre-norns/wyrd/internal/pgtest"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	acme   = manifest.ScopeRef{Account: "acme", Project: "payments"}
	acme2  = manifest.ScopeRef{Account: "acme", Project: "search"}
	globex = manifest.ScopeRef{Account: "globex", Project: "payments"}
	ledger = manifest.ScopeRef{Account: "globex", Project: "ledger"}
)

// forEachDialect runs fn against a fresh SQLite database and, when one is
// configured, a private Postgres schema. Scope and visibility are enforced in
// SQL, so they are asserted on both.
func forEachDialect(t *testing.T, fn func(t *testing.T, db *gorm.DB)) {
	t.Run("sqlite", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{Logger: logger.Discard})
		require.NoError(t, err)
		t.Cleanup(func() {
			sqlDB, _ := db.DB()
			_ = sqlDB.Close()
		})
		fn(t, db)
	})
	t.Run("postgres", func(t *testing.T) {
		fn(t, pgtest.Open(t))
	})
}

func newCrateStore(t *testing.T, db *gorm.DB) *dbstore.DBStore {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&crate{}))
	store, err := dbstore.NewDBStore(db, dbstore.ManifestModel)
	require.NoError(t, err)
	return store
}

func scopedCrate(name string, scope manifest.ScopeRef, labels manifest.Labels) *crate {
	return &crate{ObjectMeta: manifest.ObjectMeta{
		Name:    manifest.ResourceName(name),
		Account: scope.Account,
		Project: scope.Project,
		Labels:  labels,
	}}
}

func crateNames(crates []crate) []string {
	names := make([]string, 0, len(crates))
	for _, c := range crates {
		names = append(names, fmt.Sprintf("%s/%s/%s", c.Account, c.Project, c.Name))
	}
	sort.Strings(names)
	return names
}

// Uniqueness is a Postgres property here. SQLite treats NULLs as distinct in a
// unique index and cannot be told otherwise, and a live row's deleted_at is
// NULL, so SQLite enforces no name uniqueness at all -- no more than it did
// before scoping.
func TestNamesAreUniquePerScope(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		db := pgtest.Open(t)
		store := newCrateStore(t, db)
		ctx := context.Background()

		require.NoError(t, store.Create(ctx, scopedCrate("edge", acme, nil)))
		require.NoError(t, store.Create(ctx, scopedCrate("edge", acme2, nil)), "same name, another project of the same account")
		require.NoError(t, store.Create(ctx, scopedCrate("edge", globex, nil)), "same name, another account")
		require.Error(t, store.Create(ctx, scopedCrate("edge", acme, nil)), "same name in the same scope must be refused by the database")

		// Unscoped kinds keep today's rule: one name per table.
		require.NoError(t, store.Create(ctx, scopedCrate("shared", manifest.ScopeRef{}, nil)))
		require.Error(t, store.Create(ctx, scopedCrate("shared", manifest.ScopeRef{}, nil)))

		// A tombstone frees the name within its scope.
		var gone crate
		ok, err := store.GetByName(ctx, &gone, "edge", dbstore.InScope(acme))
		require.NoError(t, err)
		require.True(t, ok)
		_, err = store.Delete(ctx, &crate{}, gone.UID, 0)
		require.NoError(t, err)
		require.NoError(t, store.Create(ctx, scopedCrate("edge", acme, nil)))
	})
}

func TestInScopeLimitsReadsAndWrites(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		store := newCrateStore(t, db)
		ctx := context.Background()

		for _, scope := range []manifest.ScopeRef{acme, acme2, globex} {
			require.NoError(t, store.Create(ctx, scopedCrate("edge", scope, nil)))
		}

		var found []crate
		total, err := store.Find(ctx, &found, manifest.SearchQuery{}, dbstore.InScope(acme))
		require.NoError(t, err)
		require.EqualValues(t, 1, total)
		require.Equal(t, []string{"acme/payments/edge"}, crateNames(found))

		// An account-only reference spans the account's projects.
		found = nil
		_, err = store.Find(ctx, &found, manifest.SearchQuery{}, dbstore.InScope(manifest.ScopeRef{Account: "acme"}))
		require.NoError(t, err)
		require.Equal(t, []string{"acme/payments/edge", "acme/search/edge"}, crateNames(found))

		// Get by name resolves within the scope, so the same name in another
		// project is not an ambiguity.
		var got crate
		ok, err := store.GetByName(ctx, &got, "edge", dbstore.InScope(globex))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, globex, got.Scope())

		// A UID from another scope is not found through this one.
		ok, err = store.GetByUID(ctx, &crate{}, got.UID, dbstore.InScope(acme))
		require.NoError(t, err)
		require.False(t, ok)
		existed, err := store.Delete(ctx, &crate{}, got.UID, 0, dbstore.InScope(acme))
		require.NoError(t, err)
		require.False(t, existed, "delete through the wrong scope must not remove the row")

		// Create stamps the scope onto a value that leaves it empty.
		fresh := &crate{ObjectMeta: manifest.ObjectMeta{Name: "fresh"}}
		require.NoError(t, store.Create(ctx, fresh, dbstore.InScope(acme)))
		require.Equal(t, acme, fresh.Scope())

		// ...and refuses one that names another.
		err = store.Create(ctx, scopedCrate("stray", globex, nil), dbstore.InScope(acme))
		require.ErrorIs(t, err, manifest.ErrScopeMismatch)
	})
}

func TestInScopeNeedsScopeColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&crate{}))

	config := dbstore.ManifestModel
	config.AccountColumnName = ""
	store, err := dbstore.NewDBStore(db, config)
	require.NoError(t, err)

	var found []crate
	_, err = store.Find(context.Background(), &found, manifest.SearchQuery{}, dbstore.InScope(acme))
	require.ErrorIs(t, err, dbstore.ErrNoScopeColumns)
}

// projectOnly lets its caller see and write one project, as a tenancy module's
// Visibility would for a member of that project.
type projectOnly struct {
	project manifest.ResourceID
}

var errNotMember = errors.New("not a member of that project")

func (v projectOnly) Filter(_ context.Context, query *gorm.DB, _ any) (*gorm.DB, error) {
	return query.Where("project_id = ?", v.project), nil
}

func (v projectOnly) Admit(_ context.Context, value any) error {
	scoped, ok := value.(interface{ Scope() manifest.ScopeRef })
	if !ok || scoped.Scope().Project != v.project {
		return errNotMember
	}
	return nil
}

func TestVisibilityConstrainsEveryOperation(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		system := newCrateStore(t, db)
		ctx := context.Background()

		mine := scopedCrate("mine", acme, manifest.Labels{"team": "pay"})
		theirs := scopedCrate("theirs", globex, manifest.Labels{"secret-team": "ops"})
		require.NoError(t, system.Create(ctx, mine))
		require.NoError(t, system.Create(ctx, theirs))

		store := system.WithVisibility(projectOnly{project: acme.Project})
		// payments exists in both acme and globex; the filter here is by
		// project only, so give globex's crate a project of its own.
		require.NoError(t, db.Model(&crate{}).Where("uid = ?", theirs.UID).Update("project_id", "ledger").Error)

		t.Run("find and count", func(t *testing.T) {
			var found []crate
			total, err := store.Find(ctx, &found, manifest.SearchQuery{})
			require.NoError(t, err)
			require.EqualValues(t, 1, total, "the count must be filtered as the page is")
			require.Equal(t, []string{"acme/payments/mine"}, crateNames(found))
		})

		t.Run("get", func(t *testing.T) {
			ok, err := store.GetByUID(ctx, &crate{}, theirs.UID)
			require.NoError(t, err)
			require.False(t, ok)
			ok, err = store.GetByName(ctx, &crate{}, "theirs")
			require.NoError(t, err)
			require.False(t, ok)
		})

		t.Run("name and label catalogues", func(t *testing.T) {
			names, err := store.FindNames(ctx, &crate{}, manifest.SearchQuery{})
			require.NoError(t, err)
			require.Equal(t, manifest.NewStringSet("mine"), names)

			keys, err := store.FindLabels(ctx, &crate{}, manifest.SearchQuery{})
			require.NoError(t, err)
			require.False(t, keys.Has("secret-team"), "label keys of an invisible resource leak through the catalogue")

			values, err := store.FindLabelValues(ctx, &crate{}, "secret-team", manifest.SearchQuery{})
			require.NoError(t, err)
			require.Empty(t, values)
		})

		t.Run("update and delete", func(t *testing.T) {
			changed := *theirs
			changed.Project = acme.Project
			changed.Labels = manifest.Labels{"owned": "now"}
			ok, err := store.Update(ctx, &changed, theirs.UID)
			require.NoError(t, err)
			require.False(t, ok, "an invisible row must not be updated")

			existed, err := store.Delete(ctx, &crate{}, theirs.UID, 0)
			require.NoError(t, err)
			require.False(t, existed, "an invisible row must not be deleted")
		})

		t.Run("create is admitted", func(t *testing.T) {
			require.ErrorIs(t, store.Create(ctx, scopedCrate("stray", ledger, nil)), errNotMember)
			require.NoError(t, store.Create(ctx, scopedCrate("ok", acme, nil)))
		})

		// The case a filter alone cannot stop: Save falls back to an upsert when
		// its filtered UPDATE matches nothing, and the upsert would overwrite the
		// very row the filter hid.
		t.Run("create-or-update cannot overwrite an invisible row", func(t *testing.T) {
			hijack := scopedCrate("hijacked", acme, nil)
			hijack.UID = theirs.UID
			_, err := store.CreateOrUpdate(ctx, hijack)
			require.ErrorIs(t, err, dbstore.ErrNotVisible)

			var still crate
			ok, err := system.GetByUID(ctx, &still, theirs.UID)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, manifest.ResourceName("theirs"), still.Name)
			require.Equal(t, manifest.ResourceID("ledger"), still.Project)
		})

		t.Run("transactions carry the visibility", func(t *testing.T) {
			tx, err := store.Begin(ctx)
			require.NoError(t, err)
			defer tx.Rollback()

			ok, err := tx.GetByUID(&crate{}, theirs.UID)
			require.NoError(t, err)
			require.False(t, ok)
			require.ErrorIs(t, tx.Create(scopedCrate("stray-tx", ledger, nil)), errNotMember)
		})
	})
}

// Postgres only: the legacy index relied on NULLS NOT DISTINCT, which SQLite
// cannot express, so on SQLite it never refused anything to begin with.
func TestDropLegacyNameIndexesAllowsScopedNames(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		db := pgtest.Open(t)
		store := newCrateStore(t, db)
		ctx := context.Background()

		// What a table migrated by wyrd v0.3.0 still has: a unique name index
		// that knows nothing about scope.
		require.NoError(t, db.Exec("CREATE UNIQUE INDEX idx_crates_deleted_name ON crates (name, deleted_at) NULLS NOT DISTINCT").Error)

		require.NoError(t, store.Create(ctx, scopedCrate("edge", acme, nil)))
		require.Error(t, store.Create(ctx, scopedCrate("edge", globex, nil)),
			"the legacy index must still refuse the name, or this test proves nothing")

		require.NoError(t, dbstore.DropLegacyNameIndexes(db, &crate{}))
		require.NoError(t, store.Create(ctx, scopedCrate("edge", globex, nil)))

		// Running it again is harmless.
		require.NoError(t, dbstore.DropLegacyNameIndexes(db, &crate{}))
	})
}
