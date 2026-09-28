// Package pgtest opens an isolated Postgres database for a test.
//
// Several store properties -- selector semantics in SQL, scoped uniqueness,
// row visibility -- are properties of a real Postgres, not of SQLite, so they are
// tested against one. The server comes from WYRD_TEST_POSTGRES_URL; a test that
// needs it skips when the variable is unset, and fails when it is set but the
// server cannot be reached, so a misconfigured CI cannot pass by skipping.
//
// Every call creates its own schema and drops it on cleanup. Tests are parallel
// safe and never see each other's tables, and pointing the variable at a
// database that holds other data does not destroy it.
package pgtest

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// EnvURL names the variable holding the Postgres server URL.
const EnvURL = "WYRD_TEST_POSTGRES_URL"

// Open returns a gorm handle whose search_path is a schema private to t.
func Open(t testing.TB) *gorm.DB {
	t.Helper()

	serverURL := os.Getenv(EnvURL)
	if serverURL == "" {
		t.Skipf("%s is not set; skipping a test that needs Postgres", EnvURL)
	}

	admin, err := gorm.Open(postgres.Open(serverURL), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("%s is set but Postgres is unreachable: %v", EnvURL, err)
	}

	schema := "wyrd_test_" + randomSuffix(t)
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}

	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("%s is not a URL: %v", EnvURL, err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("failed to open test schema: %v", err)
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func randomSuffix(t testing.TB) string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("failed to generate a schema name: %v", err)
	}
	return hex.EncodeToString(b[:])
}
