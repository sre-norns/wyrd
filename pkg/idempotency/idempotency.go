// Package idempotency records the outcome of requests by client-chosen key, so
// that a retried request is answered from the record instead of being executed
// again (ADR 0001 §7).
//
// The HTTP half is bark.Idempotent; a store implementation over the resource
// database is dbstore.IdempotencyStore. This package holds only what both need.
package idempotency

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Response is a recorded outcome, replayed verbatim to a retry.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Record is what a store holds for a key whose request has completed.
type Record struct {
	// Digest identifies the request the key was first used with. A retry with
	// the same key and another digest is a different request reusing the key.
	Digest   string
	Response Response
}

// Outcome is the result of reserving a key.
type Outcome int

const (
	// Reserved means the caller now holds the key: it must execute the request
	// and then Complete or Release the key.
	Reserved Outcome = iota
	// Recorded means the key's request already completed; the record is
	// returned for the caller to replay or, if the digest differs, refuse.
	Recorded
	// InProgress means another request holds the key and has not finished.
	InProgress
)

// Store records outcomes by key.
//
// Reservation is what makes concurrent retries safe: two copies of a request
// arriving together cannot both execute, because only one can reserve the key.
// A reservation that is neither completed nor released -- its holder crashed --
// expires after the store's lease, so the key is not locked forever.
type Store interface {
	Reserve(ctx context.Context, key, digest string) (Outcome, *Record, error)
	Complete(ctx context.Context, key string, response Response) error
	Release(ctx context.Context, key string) error
}

// MemoryStore is a Store for tests and single-process tools. Records are never
// evicted.
type MemoryStore struct {
	mu      sync.Mutex
	lease   time.Duration
	now     func() time.Time
	entries map[string]*memoryEntry
}

type memoryEntry struct {
	digest     string
	reservedAt time.Time
	response   *Response
}

// NewMemoryStore returns an empty MemoryStore whose reservations expire after
// lease.
func NewMemoryStore(lease time.Duration) *MemoryStore {
	return &MemoryStore{lease: lease, now: time.Now, entries: map[string]*memoryEntry{}}
}

// Reserve implements Store.
func (s *MemoryStore) Reserve(_ context.Context, key, digest string) (Outcome, *Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	entry, exists := s.entries[key]
	switch {
	case !exists:
	case entry.response != nil:
		return Recorded, &Record{Digest: entry.digest, Response: *entry.response}, nil
	case now.Sub(entry.reservedAt) < s.lease:
		return InProgress, nil, nil
	}

	s.entries[key] = &memoryEntry{digest: digest, reservedAt: now}
	return Reserved, nil, nil
}

// Complete implements Store.
func (s *MemoryStore) Complete(_ context.Context, key string, response Response) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.entries[key]; ok {
		entry.response = &response
	}
	return nil
}

// Release implements Store.
func (s *MemoryStore) Release(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.entries[key]; ok && entry.response == nil {
		delete(s.entries, key)
	}
	return nil
}
