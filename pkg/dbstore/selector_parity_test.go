package dbstore_test

import (
	"context"
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

type crateSpec struct{ Note string }

type crate manifest.ResourceModel[crateSpec]

// The crates cover every case a requirement has to decide: the key missing,
// present with the value asked about, present with another value, and -- for the
// numeric operators -- values that are not integers at all.
var parityCrates = map[string]manifest.Labels{
	"no-labels":     nil,
	"env-prod":      {"env": "prod"},
	"env-dev":       {"env": "dev"},
	"env-empty":     {"env": ""},
	"size-10":       {"size": "10"},
	"size-2":        {"size": "2"},
	"size-neg":      {"size": "-3"},
	"size-plus":     {"size": "+7"},
	"size-padded":   {"size": "007"},
	"size-word":     {"size": "large"},
	"size-float":    {"size": "1.5"},
	"size-huge":     {"size": "99999999999999999999"},
	"env-prod-size": {"env": "prod", "size": "10"},
}

// The selectors are parsed, as an HTTP caller's would be. The expected set is not
// written down: it is whatever the in-memory selector admits, because that is the
// behaviour the SQL rendering exists to reproduce. (The Kubernetes grammar admits
// no value starting with '-', so a negative bound cannot be expressed.)
var paritySelectors = []string{
	"env",
	"!env",
	"env=prod",
	"env==prod",
	"env!=prod",
	"env in (prod,dev)",
	"env notin (prod)",
	"env notin (prod,dev)",
	"env=",
	"env!=",
	"size>5",
	"size<5",
	"env=prod,size>5",
	"env!=prod,!size",
}

func TestSelectorSQLMatchesInMemorySemanticsSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	checkSelectorParity(t, db, "size-huge")
}

func TestSelectorSQLMatchesInMemorySemanticsPostgres(t *testing.T) {
	checkSelectorParity(t, pgtest.Open(t))
}

// checkSelectorParity asserts that Find returns exactly the crates the parsed
// selector matches in memory. unsupported names crates whose result on this
// dialect is not specified and so are left out of the comparison.
func checkSelectorParity(t *testing.T, db *gorm.DB, unsupported ...string) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&crate{}))

	store, err := dbstore.NewDBStore(db, dbstore.ManifestModel)
	require.NoError(t, err)

	ctx := context.Background()
	for name, labels := range parityCrates {
		c := crate{ObjectMeta: manifest.ObjectMeta{Name: manifest.ResourceName(name), Labels: labels}}
		require.NoError(t, store.Create(ctx, &c))
	}

	skip := manifest.NewStringSet(unsupported...)
	for _, expr := range paritySelectors {
		t.Run(expr, func(t *testing.T) {
			selector, err := manifest.ParseSelector(expr)
			require.NoError(t, err)

			var want []string
			for name, labels := range parityCrates {
				if !skip.Has(name) && selector.Matches(labels) {
					want = append(want, name)
				}
			}

			var found []crate
			_, err = store.Find(ctx, &found, manifest.SearchQuery{Selector: selector})
			require.NoError(t, err, "a selector the parser accepts must not fail the query")

			var got []string
			for _, c := range found {
				if !skip.Has(string(c.Name)) {
					got = append(got, string(c.Name))
				}
			}

			sort.Strings(want)
			sort.Strings(got)
			require.Equal(t, want, got)
		})
	}
}
