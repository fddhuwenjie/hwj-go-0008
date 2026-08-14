package service

import (
	"context"
	"time"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
)

// AcquireLockRequest acquires an edit lock on a document.
type AcquireLockRequest struct {
	CollectionID   string        `json:"-"`
	DocumentID     string        `json:"-"`
	Holder         string        `json:"holder"`
	TTL            time.Duration `json:"ttl_seconds"` // seconds; <=0 means default
	Actor          string        `json:"actor,omitempty"`
	IdempotencyKey string        `json:"idempotency_key,omitempty"`
}

// RenewLockRequest extends an existing lock held by Holder.
type RenewLockRequest struct {
	CollectionID   string        `json:"-"`
	DocumentID     string        `json:"-"`
	Holder         string        `json:"holder"`
	TTL            time.Duration `json:"ttl_seconds"`
	Actor          string        `json:"actor,omitempty"`
	IdempotencyKey string        `json:"idempotency_key,omitempty"`
}

// ReleaseLockRequest releases a lock held by Holder.
type ReleaseLockRequest struct {
	CollectionID   string `json:"-"`
	DocumentID     string `json:"-"`
	Holder         string `json:"holder"`
	Actor          string `json:"actor,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// AcquireLock acquires an edit lock for a document.
//
// Semantics:
//   - If no active lock exists (none, or a prior one has expired), a new lock is
//     granted to Holder.
//   - If an active lock is already held by the same Holder, it is renewed
//     (equivalent to RenewLock) — this makes acquire idempotent for the holder.
//   - If an active lock is held by a different holder, a *LockConflictError is
//     returned and no change is made.
func (s *Service) AcquireLock(ctx context.Context, req AcquireLockRequest) (out *domain.Lock, err error) {
	if err = checkCtx(ctx); err != nil {
		return nil, err
	}
	if req.Holder == "" {
		return nil, domain.NewValidationError("holder", "must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	ttl := domain.NormalizeTTL(req.TTL)

	if req.IdempotencyKey != "" {
		fp := fingerprint("AcquireLock", req.CollectionID, req.DocumentID, req.Holder, req.TTL)
		if e, ok := s.idem[req.IdempotencyKey]; ok {
			if e.fingerprint != fp {
				return nil, &domain.IdempotencyConflictError{Key: req.IdempotencyKey}
			}
			if e.value == nil {
				return nil, e.err
			}
			return cloneIDemValue(e.value).(*domain.Lock), e.err
		}
		defer func() {
			entry := &idemEntry{fingerprint: fp, err: err}
			if out != nil {
				entry.value = cloneIDemValue(out)
			}
			s.idem[req.IdempotencyKey] = entry
		}()
	}

	if _, err = s.getDocumentUnlocked(req.CollectionID, req.DocumentID); err != nil {
		return nil, err
	}

	key := documentKey(req.CollectionID, req.DocumentID)
	existing, ok := s.locks[key]
	if ok {
		if existing.IsExpired(now) {
			// Stale lock: reclaim silently.
			delete(s.locks, key)
			ok = false
		} else if existing.Holder != req.Holder {
			return nil, &domain.LockConflictError{
				DocumentID:    req.DocumentID,
				CurrentHolder: existing.Holder,
				ExpiresAt:     existing.ExpiresAt.UTC().Format(time.RFC3339),
			}
		}
		// else: same holder, active — fall through to renew.
	}

	var lock *domain.Lock
	if ok {
		// Renew existing same-holder lock.
		renewedAt := existing.ExpiresAt
		existing.AcquiredAt = renewedAt
		existing.ExpiresAt = renewedAt.Add(ttl)
		existing.RenewCount++
		lock = existing
		s.appendAudit(domain.AuditEvent{
			Type:           domain.AuditLockRenewed,
			Timestamp:      now,
			Actor:          req.Actor,
			CollectionID:   req.CollectionID,
			DocumentID:     req.DocumentID,
			IdempotencyKey: req.IdempotencyKey,
			Details:        map[string]any{"ttl_seconds": int(ttl.Seconds()), "renew_count": lock.RenewCount},
		})
	} else {
		lock = &domain.Lock{
			DocumentID: req.DocumentID,
			Holder:     req.Holder,
			AcquiredAt: now,
			ExpiresAt:  now.Add(ttl),
			RenewCount: 0,
		}
		s.locks[key] = lock
		s.appendAudit(domain.AuditEvent{
			Type:           domain.AuditLockAcquired,
			Timestamp:      now,
			Actor:          req.Actor,
			CollectionID:   req.CollectionID,
			DocumentID:     req.DocumentID,
			IdempotencyKey: req.IdempotencyKey,
			Details:        map[string]any{"ttl_seconds": int(ttl.Seconds()), "holder": lock.Holder},
		})
	}

	c := lock.Clone()
	out = &c
	return out, nil
}

// RenewLock extends an existing lock held by Holder. If the lock is held by
// another holder, or has expired, or does not exist, an error is returned.
func (s *Service) RenewLock(ctx context.Context, req RenewLockRequest) (out *domain.Lock, err error) {
	if err = checkCtx(ctx); err != nil {
		return nil, err
	}
	if req.Holder == "" {
		return nil, domain.NewValidationError("holder", "must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	ttl := domain.NormalizeTTL(req.TTL)

	if req.IdempotencyKey != "" {
		fp := fingerprint("RenewLock", req.CollectionID, req.DocumentID, req.Holder, req.TTL)
		if e, ok := s.idem[req.IdempotencyKey]; ok {
			if e.fingerprint != fp {
				return nil, &domain.IdempotencyConflictError{Key: req.IdempotencyKey}
			}
			if e.value == nil {
				return nil, e.err
			}
			return cloneIDemValue(e.value).(*domain.Lock), e.err
		}
		defer func() {
			entry := &idemEntry{fingerprint: fp, err: err}
			if out != nil {
				entry.value = cloneIDemValue(out)
			}
			s.idem[req.IdempotencyKey] = entry
		}()
	}

	if _, err = s.getDocumentUnlocked(req.CollectionID, req.DocumentID); err != nil {
		return nil, err
	}

	key := documentKey(req.CollectionID, req.DocumentID)
	existing, ok := s.locks[key]
	if !ok || existing.IsExpired(now) {
		if ok {
			delete(s.locks, key)
		}
		return nil, domain.ErrLockNotHeld
	}
	if existing.Holder != req.Holder {
		return nil, &domain.LockConflictError{
			DocumentID:    req.DocumentID,
			CurrentHolder: existing.Holder,
			ExpiresAt:     existing.ExpiresAt.UTC().Format(time.RFC3339),
		}
	}

	existing.ExpiresAt = now.Add(ttl)
	existing.RenewCount++
	s.appendAudit(domain.AuditEvent{
		Type:           domain.AuditLockRenewed,
		Timestamp:      now,
		Actor:          req.Actor,
		CollectionID:   req.CollectionID,
		DocumentID:     req.DocumentID,
		IdempotencyKey: req.IdempotencyKey,
		Details:        map[string]any{"ttl_seconds": int(ttl.Seconds()), "renew_count": existing.RenewCount},
	})

	c := existing.Clone()
	out = &c
	return out, nil
}

// ReleaseLock releases a lock. Only the current holder may release it; a release
// by another holder is a conflict. Releasing an already-absent lock is a
// not-held error (so callers can detect duplicate releases).
func (s *Service) ReleaseLock(ctx context.Context, req ReleaseLockRequest) (err error) {
	if err = checkCtx(ctx); err != nil {
		return err
	}
	if req.Holder == "" {
		return domain.NewValidationError("holder", "must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()

	if req.IdempotencyKey != "" {
		fp := fingerprint("ReleaseLock", req.CollectionID, req.DocumentID, req.Holder)
		if e, ok := s.idem[req.IdempotencyKey]; ok {
			if e.fingerprint != fp {
				return &domain.IdempotencyConflictError{Key: req.IdempotencyKey}
			}
			return e.err
		}
		defer func() {
			s.idem[req.IdempotencyKey] = &idemEntry{fingerprint: fp, err: err}
		}()
	}

	if _, err = s.getDocumentUnlocked(req.CollectionID, req.DocumentID); err != nil {
		return err
	}

	key := documentKey(req.CollectionID, req.DocumentID)
	existing, ok := s.locks[key]
	if !ok || existing.IsExpired(now) {
		if ok {
			delete(s.locks, key)
		}
		return domain.ErrLockNotHeld
	}
	if existing.Holder != req.Holder {
		return &domain.LockConflictError{
			DocumentID:    req.DocumentID,
			CurrentHolder: existing.Holder,
			ExpiresAt:     existing.ExpiresAt.UTC().Format(time.RFC3339),
		}
	}

	delete(s.locks, key)
	s.appendAudit(domain.AuditEvent{
		Type:           domain.AuditLockReleased,
		Timestamp:      now,
		Actor:          req.Actor,
		CollectionID:   req.CollectionID,
		DocumentID:     req.DocumentID,
		IdempotencyKey: req.IdempotencyKey,
		Details:        map[string]any{"holder": existing.Holder, "renew_count": existing.RenewCount},
	})
	return nil
}

// GetLock returns the active lock for a document, if any. Expired locks are
// reported as not held.
func (s *Service) GetLock(ctx context.Context, collectionID, documentID string) (*domain.Lock, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := documentKey(collectionID, documentID)
	existing, ok := s.locks[key]
	if !ok {
		return nil, domain.ErrLockNotHeld
	}
	if existing.IsExpired(s.clock.Now()) {
		return nil, domain.ErrLockExpired
	}
	out := existing.Clone()
	return &out, nil
}

// ReapExpiredLocks removes all expired locks and returns the document IDs that
// were reaped. It is safe to call concurrently and is also invoked by the
// background reaper.
func (s *Service) ReapExpiredLocks(ctx context.Context) ([]string, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()

	var reaped []string
	for key, l := range s.locks {
		if l.IsExpired(now) {
			delete(s.locks, key)
			reaped = append(reaped, l.DocumentID)
			s.appendAudit(domain.AuditEvent{
				Type:         domain.AuditLockReaped,
				Timestamp:    now,
				CollectionID: collectionIDFromKey(key),
				DocumentID:   l.DocumentID,
				Details:      map[string]any{"holder": l.Holder, "expired_at": l.ExpiresAt.UTC().Format(time.RFC3339)},
			})
		}
	}
	return reaped, nil
}

// collectionIDFromKey splits a "collectionID/documentID" composite key back into
// its collection component. The split is on the first "/", so collection IDs
// must not contain "/".
func collectionIDFromKey(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i]
		}
	}
	return key
}
