package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
)

// sortCollectionsByID sorts the slice by ID in place for deterministic output.
func sortCollectionsByID(in []domain.Collection) {
	sort.Slice(in, func(i, j int) bool { return in[i].ID < in[j].ID })
}

// RegisterCollectionRequest creates a collection.
type RegisterCollectionRequest struct {
	// ID is optional. If empty, one is generated. If provided and already
	// registered, the existing collection is returned (registration is
	// idempotent on ID).
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Actor       string `json:"actor,omitempty"`

	// IdempotencyKey optionally makes the request idempotent. When set, a
	// repeated request with the same key and payload replays the original
	// result, and a repeated key with a different payload fails.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// RegisterCollection registers a new collection. If the request supplies an ID
// that already exists, the existing collection is returned unchanged (making
// re-registration safe). With an IdempotencyKey, retries replay the original
// result.
func (s *Service) RegisterCollection(ctx context.Context, req RegisterCollectionRequest) (out *domain.Collection, err error) {
	if err = checkCtx(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, domain.NewValidationError("name", "must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()

	// Idempotency: replay a prior result for the same key + payload. The defer
	// is registered only on the cache-miss path, so replays (which return above)
	// never overwrite the stored entry.
	if req.IdempotencyKey != "" {
		fp := fingerprint("RegisterCollection", req.ID, req.Name, req.Description)
		if e, ok := s.idem[req.IdempotencyKey]; ok {
			if e.fingerprint != fp {
				return nil, &domain.IdempotencyConflictError{Key: req.IdempotencyKey}
			}
			if e.value == nil {
				return nil, e.err
			}
			return cloneIDemValue(e.value).(*domain.Collection), e.err
		}
		defer func() {
			entry := &idemEntry{fingerprint: fp, err: err}
			if out != nil {
				entry.value = cloneIDemValue(out)
			}
			s.idem[req.IdempotencyKey] = entry
		}()
	}

	col, existed := s.registerCollectionUnlocked(req, now)
	if !existed {
		s.appendAudit(domain.AuditEvent{
			Type:         domain.AuditCollectionCreated,
			Timestamp:    now,
			Actor:        req.Actor,
			CollectionID: col.ID,
			Details: map[string]any{
				"name":        col.Name,
				"description": col.Description,
			},
			IdempotencyKey: req.IdempotencyKey,
		})
	}

	c := col.Clone()
	out = &c
	return out, nil
}

// registerCollectionUnlocked creates or returns a collection. The caller must
// hold s.mu (write). `now` is the operation timestamp used for CreatedAt.
func (s *Service) registerCollectionUnlocked(req RegisterCollectionRequest, now time.Time) (*domain.Collection, bool) {
	id := req.ID
	if id == "" {
		id = s.idFunc()
	}
	if existing, ok := s.collections[id]; ok {
		return existing, true
	}
	col := &domain.Collection{
		ID:          id,
		Name:        req.Name,
		Description: req.Description,
		CreatedAt:   now,
	}
	s.collections[id] = col
	return col, false
}

// GetCollection returns a collection by ID.
func (s *Service) GetCollection(ctx context.Context, id string) (*domain.Collection, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	col, ok := s.collections[id]
	if !ok {
		return nil, domain.ErrCollectionNotFound
	}
	out := col.Clone()
	return &out, nil
}

// ListCollections returns all collections ordered by ID for determinism.
func (s *Service) ListCollections(ctx context.Context) ([]domain.Collection, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Collection, 0, len(s.collections))
	for _, c := range s.collections {
		out = append(out, c.Clone())
	}
	sortCollectionsByID(out)
	return out, nil
}

// collectionExistsUnlocked reports whether a collection exists. Caller holds at
// least a read lock.
func (s *Service) collectionExistsUnlocked(id string) bool {
	_, ok := s.collections[id]
	return ok
}
