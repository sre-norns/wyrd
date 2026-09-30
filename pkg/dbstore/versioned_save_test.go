package dbstore_test

import (
	"context"
	"testing"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// CreateOrUpdate with WithVersion is optimistic concurrency: it writes only
// over the version it names. gorm's Save falls back to an upsert when its
// UPDATE matches no row, which would turn a stale write into an overwrite.
func TestCreateOrUpdateWithVersionRefusesAStaleWrite(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		store := newCrateStore(t, db)
		ctx := context.Background()

		original := scopedCrate("box", acme, nil)
		original.Spec = crateSpec{Note: "first"}
		require.NoError(t, store.Create(ctx, original))
		read := original.Version

		stale := *original
		current := *original
		current.Spec.Note = "second"
		exists, err := store.CreateOrUpdate(ctx, &current, dbstore.WithVersion(read))
		require.NoError(t, err)
		require.True(t, exists, "a write over the current version succeeds")
		require.Greater(t, current.Version, read)

		stale.Spec.Note = "stale"
		exists, err = store.CreateOrUpdate(ctx, &stale, dbstore.WithVersion(read))
		require.NoError(t, err)
		require.False(t, exists, "a write over a superseded version must be refused")

		var stored crate
		found, err := store.GetByUID(ctx, &stored, original.UID)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, "second", stored.Spec.Note, "the refused write must change nothing")
		require.Equal(t, current.Version, stored.Version)
	})
}

// A versioned save still writes zero values -- the reason callers save rather
// than Update, which drops them.
func TestCreateOrUpdateWithVersionWritesZeroValues(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		store := newCrateStore(t, db)
		ctx := context.Background()

		original := scopedCrate("box", acme, nil)
		original.Spec = crateSpec{Note: "set"}
		require.NoError(t, store.Create(ctx, original))

		cleared := *original
		cleared.Spec.Note = ""
		exists, err := store.CreateOrUpdate(ctx, &cleared, dbstore.WithVersion(original.Version))
		require.NoError(t, err)
		require.True(t, exists)

		var stored crate
		_, err = store.GetByUID(ctx, &stored, original.UID)
		require.NoError(t, err)
		require.Empty(t, stored.Spec.Note)
	})
}
