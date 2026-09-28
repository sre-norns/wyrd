package manifest_test

import (
	"fmt"
	"testing"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type catSpec struct{ Whiskers int }
type dogSpec struct{ Bark string }

type cat manifest.ResourceModel[catSpec]
type dog manifest.ResourceModel[dogSpec]

// Every model embeds ObjectMeta, so an index name fixed in its tags is shared by
// every table. SQLite rejects the second CREATE INDEX outright; Postgres, whose
// index names are also schema-wide, issues CREATE INDEX IF NOT EXISTS and so
// silently leaves every table but the first without a name index.
func TestObjectMetaIndexesArePerTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	require.NoError(t, db.AutoMigrate(&cat{}, &dog{}))

	for _, model := range []any{&cat{}, &dog{}} {
		indexes, err := db.Migrator().GetIndexes(model)
		require.NoError(t, err)

		var nameIndexed, nameUnique bool
		for _, idx := range indexes {
			cols := idx.Columns()
			if len(cols) == 1 && cols[0] == "name" {
				nameIndexed = true
			}
			if unique, _ := idx.Unique(); unique && len(cols) == 2 {
				nameUnique = true
			}
		}
		require.True(t, nameIndexed, "%T: no index on name", model)
		require.True(t, nameUnique, "%T: no unique (name, deleted_at) index", model)
	}
}
