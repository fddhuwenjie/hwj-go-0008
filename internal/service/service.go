// Package service implements the concurrency-safe, in-memory domain layer for
// the document versioning and edit-lock service.
//
// All public methods are safe for concurrent use. The service guards its entire
// state with a single sync.RWMutex: write operations take the write lock for
// their full duration (including idempotency checks, mutation, and audit
// recording), and read operations take the read lock. This guarantees that
// compound operations (such as an atomic batch update, or an idempotent
// request) are indivisible with respect to other operations. Because every
// operation completes while holding the lock and Go mutexes are non-reentrant,
// no public method calls another public method while the lock is held;
// instead, each delegates to an "unlocked" helper.
//
// Returned values are always deep copies, so callers can never mutate the
// service's internal state (mutable-data isolation).
package service

import (
	"context"
	"sync"
	"time"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
)

// Service is the concurrency-safe in-memory domain layer. Create one with New.
type Service struct {
	clock  domain.Clock
	idFunc func() string

	mu sync.RWMutex

	collections map[string]*domain.Collection
	documents   map[string]*domain.Document  // keyed by collectionID + "/" + documentID
	history     map[string][]domain.Revision // keyed identically to documents
	locks       map[string]*domain.Lock      // keyed by document key

	audit    []domain.AuditEvent
	auditSeq int64

	idem map[string]*idemEntry
}

// documentKey is the composite key under which a document is stored.
func documentKey(collectionID, documentID string) string {
	return collectionID + "/" + documentID
}

// Options configures a Service. The zero value is invalid; use New with
// functional options or NewWithDefaults.
type Options struct {
	Clock  domain.Clock  // required; use domain.SystemClock{} for production
	IDFunc func() string // optional; defaults to domain.NewID
}

// New constructs a Service. If opts.Clock is nil, SystemClock is used.
func New(opts Options) *Service {
	s := &Service{
		collections: make(map[string]*domain.Collection),
		documents:   make(map[string]*domain.Document),
		history:     make(map[string][]domain.Revision),
		locks:       make(map[string]*domain.Lock),
		idem:        make(map[string]*idemEntry),
	}
	if opts.Clock != nil {
		s.clock = opts.Clock
	} else {
		s.clock = domain.SystemClock{}
	}
	if opts.IDFunc != nil {
		s.idFunc = opts.IDFunc
	} else {
		s.idFunc = domain.NewID
	}
	return s
}

// StartReaper launches a background goroutine that periodically reaps expired
// locks. It stops when ctx is canceled. The returned function is a no-op kept
// for API symmetry; cancellation is driven by ctx. Most tests should call
// ReapExpiredLocks directly for determinism rather than relying on the reaper.
func (s *Service) StartReaper(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_, _ = s.ReapExpiredLocks(ctx)
			}
		}
	}()
}

// checkCtx returns ctx.Err() if the context is already canceled. Operations
// call this at entry to honor cancellation promptly.
func checkCtx(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
