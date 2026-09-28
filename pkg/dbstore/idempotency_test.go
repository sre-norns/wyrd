package dbstore_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/internal/pgtest"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/idempotency"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIdempotencyStoreLifecycle(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&dbstore.IdempotencyRecord{}))
		store := dbstore.NewIdempotencyStore(db, time.Minute)
		ctx := context.Background()

		outcome, _, err := store.Reserve(ctx, "k1", "digest-a")
		require.NoError(t, err)
		require.Equal(t, idempotency.Reserved, outcome)

		outcome, _, err = store.Reserve(ctx, "k1", "digest-a")
		require.NoError(t, err)
		require.Equal(t, idempotency.InProgress, outcome)

		response := idempotency.Response{Status: http.StatusCreated, Header: http.Header{"X-Run": {"r1"}}, Body: []byte("created")}
		require.NoError(t, store.Complete(ctx, "k1", response))

		outcome, record, err := store.Reserve(ctx, "k1", "digest-b")
		require.NoError(t, err)
		require.Equal(t, idempotency.Recorded, outcome)
		require.Equal(t, "digest-a", record.Digest, "the record keeps the first request's digest")
		require.Equal(t, response, record.Response)

		// Releasing a completed key does nothing: its outcome stands.
		require.NoError(t, store.Release(ctx, "k1"))
		outcome, _, err = store.Reserve(ctx, "k1", "digest-a")
		require.NoError(t, err)
		require.Equal(t, idempotency.Recorded, outcome)

		// Releasing an executing key frees it.
		_, _, err = store.Reserve(ctx, "k2", "d")
		require.NoError(t, err)
		require.NoError(t, store.Release(ctx, "k2"))
		outcome, _, err = store.Reserve(ctx, "k2", "d")
		require.NoError(t, err)
		require.Equal(t, idempotency.Reserved, outcome)

		// Sweeping removes completed records older than the cutoff only.
		swept, err := store.Sweep(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.EqualValues(t, 1, swept, "k1 is completed; k2 is still executing")
	})
}

// A reservation whose holder died is not held forever.
func TestIdempotencyStoreLeaseExpires(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&dbstore.IdempotencyRecord{}))
		store := dbstore.NewIdempotencyStore(db, time.Minute)
		ctx := context.Background()

		_, _, err := store.Reserve(ctx, "k1", "d")
		require.NoError(t, err)
		require.NoError(t, db.Model(&dbstore.IdempotencyRecord{}).Where("idempotency_key = ?", "k1").
			Update("reserved_at", time.Now().UTC().Add(-2*time.Minute)).Error)

		outcome, _, err := store.Reserve(ctx, "k1", "d")
		require.NoError(t, err)
		require.Equal(t, idempotency.Reserved, outcome)
	})
}

// The property that makes the middleware safe: of many requests racing for one
// new key, exactly one reserves it.
//
// Postgres only: SQLite's shared-cache mode answers concurrent writers with
// "table is locked" rather than serialising them. That is still safe -- an error
// is not a reservation -- but it cannot show the property.
func TestIdempotencyStoreReservationIsExclusive(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		db := pgtest.Open(t)
		require.NoError(t, db.AutoMigrate(&dbstore.IdempotencyRecord{}))
		store := dbstore.NewIdempotencyStore(db, time.Minute)

		const racers = 16
		outcomes := make(chan idempotency.Outcome, racers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				outcome, _, err := store.Reserve(context.Background(), "contended", "d")
				if err != nil {
					t.Error(err)
					return
				}
				outcomes <- outcome
			}()
		}
		close(start)
		wg.Wait()
		close(outcomes)

		reserved := 0
		for outcome := range outcomes {
			if outcome == idempotency.Reserved {
				reserved++
			}
		}
		require.Equal(t, 1, reserved)
	})
}
