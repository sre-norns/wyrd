package dbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/sre-norns/wyrd/pkg/idempotency"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IdempotencyRecord is the table behind [IdempotencyStore]. Migrate it with the
// product's other models.
type IdempotencyRecord struct {
	// Key is stored as idempotency_key: KEY is a reserved word in MySQL.
	Key        string    `gorm:"column:idempotency_key;primaryKey"`
	Digest     string    `gorm:"not null"`
	ReservedAt time.Time `gorm:"not null"`

	// CompletedAt is nil while the request is executing.
	CompletedAt *time.Time `gorm:"index"`
	Status      int
	Header      []byte
	Body        []byte
}

// TableName keeps the table apart from Exp-Bench's own idempotency_records,
// whose shape differs, until Exp-Bench moves onto this one.
func (IdempotencyRecord) TableName() string { return "idempotency_keys" }

// IdempotencyStore is an [idempotency.Store] over the resource database.
type IdempotencyStore struct {
	db    *gorm.DB
	lease time.Duration
}

// NewIdempotencyStore returns a store whose reservations expire after lease: a
// request that neither completes nor releases its key within it is presumed
// dead, and the key can be reserved again.
func NewIdempotencyStore(db *gorm.DB, lease time.Duration) *IdempotencyStore {
	return &IdempotencyStore{db: db, lease: lease}
}

// Reserve implements [idempotency.Store].
//
// The claim is a single INSERT ... ON CONFLICT DO NOTHING, so of two requests
// racing for a new key the database lets exactly one in. Taking over an expired
// reservation is a compare-and-swap on its reservation time, for the same
// reason.
func (s *IdempotencyStore) Reserve(ctx context.Context, key, digest string) (idempotency.Outcome, *idempotency.Record, error) {
	now := time.Now().UTC()
	db := s.db.WithContext(ctx)

	claim := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&IdempotencyRecord{Key: key, Digest: digest, ReservedAt: now})
	if claim.Error != nil {
		return 0, nil, claim.Error
	}
	if claim.RowsAffected == 1 {
		return idempotency.Reserved, nil, nil
	}

	var existing IdempotencyRecord
	if err := db.Where("idempotency_key = ?", key).Take(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Released between the insert and this read: try once more.
			return s.Reserve(ctx, key, digest)
		}
		return 0, nil, err
	}

	if existing.CompletedAt != nil {
		record := &idempotency.Record{
			Digest:   existing.Digest,
			Response: idempotency.Response{Status: existing.Status, Body: existing.Body, Header: http.Header{}},
		}
		if len(existing.Header) > 0 {
			if err := json.Unmarshal(existing.Header, &record.Response.Header); err != nil {
				return 0, nil, fmt.Errorf("corrupt idempotency record %q: %w", key, err)
			}
		}
		return idempotency.Recorded, record, nil
	}

	if now.Sub(existing.ReservedAt) < s.lease {
		return idempotency.InProgress, nil, nil
	}

	takeover := db.Model(&IdempotencyRecord{}).
		Where("idempotency_key = ? AND reserved_at = ? AND completed_at IS NULL", key, existing.ReservedAt).
		Updates(map[string]any{"digest": digest, "reserved_at": now})
	if takeover.Error != nil {
		return 0, nil, takeover.Error
	}
	if takeover.RowsAffected == 1 {
		return idempotency.Reserved, nil, nil
	}

	// Someone else took it over or completed it first.
	return idempotency.InProgress, nil, nil
}

// Complete implements [idempotency.Store].
func (s *IdempotencyStore) Complete(ctx context.Context, key string, response idempotency.Response) error {
	header, err := json.Marshal(response.Header)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&IdempotencyRecord{}).
		Where("idempotency_key = ? AND completed_at IS NULL", key).
		Updates(map[string]any{
			"completed_at": now,
			"status":       response.Status,
			"header":       header,
			"body":         response.Body,
		}).Error
}

// Release implements [idempotency.Store].
func (s *IdempotencyStore) Release(ctx context.Context, key string) error {
	return s.db.WithContext(ctx).
		Where("idempotency_key = ? AND completed_at IS NULL", key).
		Delete(&IdempotencyRecord{}).Error
}

// Sweep deletes records completed before cutoff, and returns how many. Keys are
// only worth keeping for as long as a client may still retry; run this on a
// schedule with a cutoff comfortably longer than any client's retry window.
func (s *IdempotencyStore) Sweep(ctx context.Context, cutoff time.Time) (int64, error) {
	result := s.db.WithContext(ctx).
		Where("completed_at IS NOT NULL AND completed_at < ?", cutoff).
		Delete(&IdempotencyRecord{})
	return result.RowsAffected, result.Error
}
