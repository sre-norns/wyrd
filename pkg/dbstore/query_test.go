package dbstore_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/internal/pgtest"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type queryRecord struct {
	ID        string `gorm:"primaryKey"`
	Name      string
	AccountID string
	ProjectID string
	Revision  int64
	Labels    manifest.Labels `gorm:"serializer:json;type:jsonb"`
	Status    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

var querySchema = dbstore.SchemaConfig{IDColumnName: "id", NameColumnName: "name", VersionColumnName: "revision", LabelsColumnName: "labels", CreatedAtColumnName: "created_at", AccountColumnName: "account_id", ProjectColumnName: "project_id"}

type queryVisibility struct{ err error }

func (v queryVisibility) Filter(_ context.Context, db *gorm.DB, _ any) (*gorm.DB, error) {
	if v.err != nil {
		return nil, v.err
	}
	return db.Where("account_id = ?", "a"), nil
}
func (v queryVisibility) Admit(context.Context, any) error { return v.err }

func TestFilterQuerySQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	checkFilterQuery(t, db)
}
func TestFilterQueryPostgres(t *testing.T) { checkFilterQuery(t, pgtest.Open(t)) }
func checkFilterQuery(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&queryRecord{}))
	base := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	rows := []queryRecord{
		{ID: "1", Name: "Checkout A", AccountID: "a", ProjectID: "p", Revision: 1, Status: "active", Labels: manifest.Labels{"status": "user-owned"}, CreatedAt: base, ExpiresAt: base.Add(-time.Hour)},
		{ID: "2", Name: "checkout B", AccountID: "a", ProjectID: "p", Revision: 2, Status: "active", CreatedAt: base.Add(time.Hour), ExpiresAt: base.Add(time.Hour)},
		{ID: "3", Name: "CHECKOUT C", AccountID: "a", ProjectID: "p", Revision: 3, Status: "archived", CreatedAt: base.Add(2 * time.Hour), ExpiresAt: base.Add(time.Hour)},
		{ID: "4", Name: "Checkout D", AccountID: "b", ProjectID: "p", Revision: 4, Status: "active", CreatedAt: base.Add(time.Hour), ExpiresAt: base.Add(time.Hour)},
		{ID: "5", Name: "account configuration", AccountID: "a", Status: "active", CreatedAt: base, ExpiresAt: base.Add(time.Hour)},
		{ID: "6", Name: "system configuration", Status: "active", CreatedAt: base, ExpiresAt: base.Add(time.Hour)},
	}
	require.NoError(t, db.Create(&rows).Error)
	parse := func(s string) manifest.Selector {
		q, err := manifest.ParseSelector(s)
		require.NoError(t, err)
		return q
	}
	policy := dbstore.QueryPolicy{
		Visibility: queryVisibility{},
		Scope:      &manifest.ScopeRef{Account: "a", Project: "p"},
		Predicates: map[string]dbstore.FieldPredicate{"status.phase": func(req manifest.Requirement) (clause.Expression, error) {
			return dbstore.CompareField(clause.Expr{SQL: "CASE WHEN status = ? AND expires_at <= ? THEN ? ELSE status END", Vars: []any{"active", base, "expired"}}, req)
		}},
	}
	keyset := dbstore.Keyset[queryRecord]{Columns: []dbstore.KeyColumn{{Expr: "id", Kind: dbstore.KeyString}}, Key: func(r *queryRecord) []any { return []any{r.ID} }}
	find := func(q manifest.SearchQuery, p dbstore.QueryPolicy) ([]queryRecord, manifest.Page) {
		tx, err := dbstore.FilterQuery(context.Background(), db, &queryRecord{}, querySchema, q, p)
		require.NoError(t, err)
		found, page, err := dbstore.PageBy(tx, q, keyset)
		require.NoError(t, err)
		return found, page
	}
	t.Run("name time scope count and continuation", func(t *testing.T) {
		q := manifest.SearchQuery{Name: "cHeCkOuT", FromTime: base, TillTime: base.Add(2 * time.Hour), Limit: 1}
		first, page := find(q, policy)
		require.Len(t, first, 1)
		require.Equal(t, "1", first[0].ID)
		require.EqualValues(t, 2, *page.Total)
		require.NotEmpty(t, page.Next)
		q.Cursor = page.Next
		second, page := find(q, policy)
		require.Len(t, second, 1)
		require.Equal(t, "2", second[0].ID)
		require.Empty(t, page.Next)
	})
	t.Run("computed phase and label namespace", func(t *testing.T) {
		found, _ := find(manifest.SearchQuery{Fields: parse("status.phase=expired"), Selector: parse("status=user-owned")}, policy)
		require.Len(t, found, 1)
		require.Equal(t, "1", found[0].ID)
		found, _ = find(manifest.SearchQuery{Fields: parse("status.phase=active,metadata.version>1")}, policy)
		require.Len(t, found, 1)
		require.Equal(t, "2", found[0].ID)
	})
	t.Run("exact account and system scopes", func(t *testing.T) {
		p := policy
		p.Scope = &manifest.ScopeRef{Account: "a"}
		found, _ := find(manifest.SearchQuery{}, p)
		require.Len(t, found, 1)
		require.Equal(t, "5", found[0].ID)
		p.Scope = &manifest.ScopeRef{}
		p.Visibility = nil // explicit unrestricted service read
		found, _ = find(manifest.SearchQuery{}, p)
		require.Len(t, found, 1)
		require.Equal(t, "6", found[0].ID)
	})
	t.Run("unselectable and malformed filters fail closed", func(t *testing.T) {
		for _, q := range []manifest.SearchQuery{{Fields: parse("password=secret")}, {FromTime: base.Add(time.Hour), TillTime: base}, {Offset: 1}} {
			tx, err := dbstore.FilterQuery(context.Background(), db, &queryRecord{}, querySchema, q, policy)
			require.Error(t, err)
			require.Nil(t, tx)
		}
		p := policy
		p.Scope = &manifest.ScopeRef{Project: "p"}
		_, err := dbstore.FilterQuery(context.Background(), db, &queryRecord{}, querySchema, manifest.SearchQuery{}, p)
		require.ErrorIs(t, err, manifest.ErrInvalidScope)
		p = policy
		p.Visibility = queryVisibility{err: errors.New("permission lookup failed")}
		_, err = dbstore.FilterQuery(context.Background(), db, &queryRecord{}, querySchema, manifest.SearchQuery{}, p)
		require.ErrorContains(t, err, "permission lookup failed")
		p = policy
		p.Predicates = map[string]dbstore.FieldPredicate{"status.phase": func(manifest.Requirement) (clause.Expression, error) { return nil, nil }}
		_, err = dbstore.FilterQuery(context.Background(), db, &queryRecord{}, querySchema, manifest.SearchQuery{Fields: parse("status.phase=active")}, p)
		require.Error(t, err)
	})
	t.Run("existing transaction and predicates survive", func(t *testing.T) {
		sentinel := errors.New("rollback test")
		err := db.Transaction(func(tx *gorm.DB) error {
			r := queryRecord{ID: "7", Name: "transactional", AccountID: "a", ProjectID: "p", CreatedAt: base, ExpiresAt: base}
			require.NoError(t, tx.Create(&r).Error)
			filtered, err := dbstore.FilterQuery(context.Background(), tx.Where("id = ?", "7"), &queryRecord{}, querySchema, manifest.SearchQuery{}, policy)
			require.NoError(t, err)
			var found []queryRecord
			require.NoError(t, filtered.Find(&found).Error)
			require.Len(t, found, 1)
			return sentinel
		})
		require.ErrorIs(t, err, sentinel)
		var count int64
		require.NoError(t, db.Model(&queryRecord{}).Where("id = ?", "7").Count(&count).Error)
		require.Zero(t, count)
	})
	t.Run("bound input cannot broaden ownership", func(t *testing.T) {
		found, _ := find(manifest.SearchQuery{Name: "' OR 1=1 --"}, policy)
		require.Empty(t, found)
	})
}
