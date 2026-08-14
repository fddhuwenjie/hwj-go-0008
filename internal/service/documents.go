package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
)

// CreateDocumentRequest creates a new document in a collection.
type CreateDocumentRequest struct {
	CollectionID string `json:"-"` // set by the transport from the path

	ID             string   `json:"id,omitempty"`
	Title          string   `json:"title"`
	Content        string   `json:"content,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Actor          string   `json:"actor,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

// UpdateDocumentRequest updates a document using optimistic concurrency. Fields
// left nil are not modified, enabling partial updates. Tags, when non-nil, fully
// replace the existing tags.
type UpdateDocumentRequest struct {
	CollectionID string `json:"-"`
	DocumentID   string `json:"-"`

	ExpectedVersion int       `json:"expected_version"`
	Title           *string   `json:"title,omitempty"`
	Content         *string   `json:"content,omitempty"`
	Tags            *[]string `json:"tags,omitempty"`
	Actor           string    `json:"actor,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	IdempotencyKey  string    `json:"idempotency_key,omitempty"`
}

// BatchItem is a single update within a batch.
type BatchItem struct {
	DocumentID      string    `json:"document_id"`
	ExpectedVersion int       `json:"expected_version"`
	Title           *string   `json:"title,omitempty"`
	Content         *string   `json:"content,omitempty"`
	Tags            *[]string `json:"tags,omitempty"`
	Reason          string    `json:"reason,omitempty"`
}

// BatchUpdateRequest applies a set of updates atomically.
type BatchUpdateRequest struct {
	CollectionID   string      `json:"-"`
	Items          []BatchItem `json:"items"`
	Actor          string      `json:"actor,omitempty"`
	IdempotencyKey string      `json:"idempotency_key,omitempty"`
}

// BatchItemResult is the per-item outcome of an applied batch.
type BatchItemResult struct {
	Index    int             `json:"index"`
	Document domain.Document `json:"document"`
}

// BatchUpdateResult is the outcome of a batch update. On conflict, Applied is
// false and Failures lists every conflicting item; Applied is true otherwise.
// A conflicting batch also returns a *BatchConflictError so callers that ignore
// the body still see a failure.
type BatchUpdateResult struct {
	Applied  bool                      `json:"applied"`
	Results  []BatchItemResult         `json:"results,omitempty"`
	Failures []domain.BatchItemFailure `json:"failures,omitempty"`
}

// clone returns a deep copy of the batch result.
func (r *BatchUpdateResult) clone() *BatchUpdateResult {
	if r == nil {
		return nil
	}
	out := &BatchUpdateResult{Applied: r.Applied}
	if r.Results != nil {
		out.Results = make([]BatchItemResult, len(r.Results))
		for i := range r.Results {
			out.Results[i] = BatchItemResult{
				Index:    r.Results[i].Index,
				Document: r.Results[i].Document.Clone(),
			}
		}
	}
	if r.Failures != nil {
		out.Failures = append([]domain.BatchItemFailure(nil), r.Failures...)
	}
	return out
}

// ListFilter selects documents within a collection.
type ListFilter struct {
	// Tags, when non-empty, returns only documents containing ALL the given
	// tags (AND semantics).
	Tags []string
	// Limit bounds the number returned (0 = no limit).
	Limit int
}

// CreateDocument creates a document at version 1.
func (s *Service) CreateDocument(ctx context.Context, req CreateDocumentRequest) (out *domain.Document, err error) {
	if err = checkCtx(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Title) == "" {
		return nil, domain.NewValidationError("title", "must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()

	if req.IdempotencyKey != "" {
		fp := fingerprint("CreateDocument", req.CollectionID, req.ID, req.Title, req.Content, req.Tags)
		if e, ok := s.idem[req.IdempotencyKey]; ok {
			if e.fingerprint != fp {
				return nil, &domain.IdempotencyConflictError{Key: req.IdempotencyKey}
			}
			if e.value == nil {
				return nil, e.err
			}
			return cloneIDemValue(e.value).(*domain.Document), e.err
		}
		defer func() {
			entry := &idemEntry{fingerprint: fp, err: err}
			if out != nil {
				entry.value = cloneIDemValue(out)
			}
			s.idem[req.IdempotencyKey] = entry
		}()
	}

	if !s.collectionExistsUnlocked(req.CollectionID) {
		return nil, domain.ErrCollectionNotFound
	}

	id := req.ID
	if id == "" {
		id = s.idFunc()
	}
	key := documentKey(req.CollectionID, id)
	if _, exists := s.documents[key]; exists {
		return nil, domain.NewValidationError("id", "document already exists")
	}

	tags := normalizeTags(req.Tags)
	doc := &domain.Document{
		ID:           id,
		CollectionID: req.CollectionID,
		Title:        req.Title,
		Content:      req.Content,
		Tags:         tags,
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.documents[key] = doc
	s.history[key] = append(s.history[key], domain.Revision{
		Version:    1,
		Title:      doc.Title,
		Content:    doc.Content,
		Tags:       cloneStrings(doc.Tags),
		Actor:      req.Actor,
		ModifiedAt: now,
	})

	s.appendAudit(domain.AuditEvent{
		Type:           domain.AuditDocumentCreated,
		Timestamp:      now,
		Actor:          req.Actor,
		CollectionID:   req.CollectionID,
		DocumentID:     id,
		IdempotencyKey: req.IdempotencyKey,
		Details: map[string]any{
			"version": 1,
			"tags":    cloneStrings(tags),
		},
	})

	c := doc.Clone()
	out = &c
	return out, nil
}

// GetDocument returns the current state of a document.
func (s *Service) GetDocument(ctx context.Context, collectionID, documentID string) (*domain.Document, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, err := s.getDocumentUnlocked(collectionID, documentID)
	if err != nil {
		return nil, err
	}
	out := doc.Clone()
	return &out, nil
}

// UpdateDocument applies an optimistic-concurrency update. If the document's
// current version does not equal ExpectedVersion, it returns a
// *VersionConflictError and no mutation occurs.
func (s *Service) UpdateDocument(ctx context.Context, req UpdateDocumentRequest) (out *domain.Document, err error) {
	if err = checkCtx(ctx); err != nil {
		return nil, err
	}
	if req.ExpectedVersion < 1 {
		return nil, domain.NewValidationError("expected_version", "must be >= 1")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()

	if req.IdempotencyKey != "" {
		fp := fingerprint("UpdateDocument", req.CollectionID, req.DocumentID, req.ExpectedVersion, req.Title, req.Content, req.Tags)
		if e, ok := s.idem[req.IdempotencyKey]; ok {
			if e.fingerprint != fp {
				return nil, &domain.IdempotencyConflictError{Key: req.IdempotencyKey}
			}
			if e.value == nil {
				return nil, e.err
			}
			return cloneIDemValue(e.value).(*domain.Document), e.err
		}
		defer func() {
			entry := &idemEntry{fingerprint: fp, err: err}
			if out != nil {
				entry.value = cloneIDemValue(out)
			}
			s.idem[req.IdempotencyKey] = entry
		}()
	}

	doc, err := s.getDocumentUnlocked(req.CollectionID, req.DocumentID)
	if err != nil {
		return nil, err
	}

	if doc.Version != req.ExpectedVersion {
		return nil, &domain.VersionConflictError{
			DocumentID: req.DocumentID,
			Expected:   req.ExpectedVersion,
			Actual:     doc.Version,
		}
	}

	updated := s.applyUpdateUnlocked(doc, req, now)
	s.appendAudit(domain.AuditEvent{
		Type:           domain.AuditDocumentUpdated,
		Timestamp:      now,
		Actor:          req.Actor,
		CollectionID:   req.CollectionID,
		DocumentID:     req.DocumentID,
		IdempotencyKey: req.IdempotencyKey,
		Details: map[string]any{
			"from_version": req.ExpectedVersion,
			"to_version":   updated.Version,
			"reason":       req.Reason,
		},
	})

	c := updated.Clone()
	out = &c
	return out, nil
}

// BatchUpdateDocuments applies all items atomically: it validates every item
// first (existence + expected version, plus intra-batch duplicate detection),
// and only if no item conflicts does it apply any. On conflict it returns a
// *BatchConflictError listing every failing item, and no mutation occurs (true
// atomicity — there is never a partial apply).
func (s *Service) BatchUpdateDocuments(ctx context.Context, req BatchUpdateRequest) (out *BatchUpdateResult, err error) {
	if err = checkCtx(ctx); err != nil {
		return nil, err
	}
	if len(req.Items) == 0 {
		return nil, domain.NewValidationError("items", "must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()

	if req.IdempotencyKey != "" {
		fp := fingerprint("BatchUpdateDocuments", req.CollectionID, req.Items)
		if e, ok := s.idem[req.IdempotencyKey]; ok {
			if e.fingerprint != fp {
				return nil, &domain.IdempotencyConflictError{Key: req.IdempotencyKey}
			}
			if e.value == nil {
				return nil, e.err
			}
			return cloneIDemValue(e.value).(*BatchUpdateResult), e.err
		}
		defer func() {
			entry := &idemEntry{fingerprint: fp, err: err}
			if out != nil {
				entry.value = cloneIDemValue(out)
			}
			s.idem[req.IdempotencyKey] = entry
		}()
	}

	if !s.collectionExistsUnlocked(req.CollectionID) {
		return nil, domain.ErrCollectionNotFound
	}

	// Validation phase: collect every conflict before applying anything.
	var failures []domain.BatchItemFailure
	seen := make(map[string]int, len(req.Items)) // documentID -> first index
	for i, item := range req.Items {
		if item.ExpectedVersion < 1 {
			failures = append(failures, domain.BatchItemFailure{
				Index: i, DocumentID: item.DocumentID,
				Reason: "expected_version must be >= 1",
			})
			continue
		}
		if prev, dup := seen[item.DocumentID]; dup {
			failures = append(failures, domain.BatchItemFailure{
				Index: i, DocumentID: item.DocumentID,
				Reason: "duplicate document in batch (first at index " + itoa(prev) + ")",
			})
			continue
		}
		seen[item.DocumentID] = i

		doc, ferr := s.getDocumentUnlocked(req.CollectionID, item.DocumentID)
		if ferr != nil {
			failures = append(failures, domain.BatchItemFailure{
				Index: i, DocumentID: item.DocumentID, Reason: "document not found",
			})
			continue
		}
		if doc.Version != item.ExpectedVersion {
			failures = append(failures, domain.BatchItemFailure{
				Index: i, DocumentID: item.DocumentID,
				Reason: "version conflict: expected " + itoa(item.ExpectedVersion) + ", got " + itoa(doc.Version),
			})
			continue
		}
	}

	if len(failures) > 0 {
		s.appendAudit(domain.AuditEvent{
			Type:           domain.AuditBatchRejected,
			Timestamp:      now,
			Actor:          req.Actor,
			CollectionID:   req.CollectionID,
			IdempotencyKey: req.IdempotencyKey,
			Details: map[string]any{
				"item_count":    len(req.Items),
				"failure_count": len(failures),
			},
		})
		res := &BatchUpdateResult{Applied: false, Failures: failures}
		out = res.clone()
		return out, &domain.BatchConflictError{Failures: failures}
	}

	// Apply phase: all items validated, apply under the same lock.
	results := make([]BatchItemResult, 0, len(req.Items))
	for i, item := range req.Items {
		doc, _ := s.getDocumentUnlocked(req.CollectionID, item.DocumentID)
		updated := s.applyUpdateUnlocked(doc, UpdateDocumentRequest{
			CollectionID:    req.CollectionID,
			DocumentID:      item.DocumentID,
			ExpectedVersion: item.ExpectedVersion,
			Title:           item.Title,
			Content:         item.Content,
			Tags:            item.Tags,
			Actor:           req.Actor,
			Reason:          item.Reason,
		}, now)
		results = append(results, BatchItemResult{Index: i, Document: updated.Clone()})
		s.appendAudit(domain.AuditEvent{
			Type:         domain.AuditDocumentUpdated,
			Timestamp:    now,
			Actor:        req.Actor,
			CollectionID: req.CollectionID,
			DocumentID:   item.DocumentID,
			Details: map[string]any{
				"from_version": item.ExpectedVersion,
				"to_version":   updated.Version,
				"reason":       item.Reason,
				"batch":        true,
			},
		})
	}
	s.appendAudit(domain.AuditEvent{
		Type:           domain.AuditBatchApplied,
		Timestamp:      now,
		Actor:          req.Actor,
		CollectionID:   req.CollectionID,
		IdempotencyKey: req.IdempotencyKey,
		Details:        map[string]any{"item_count": len(req.Items)},
	})

	out = &BatchUpdateResult{Applied: true, Results: results}
	return out.clone(), nil
}

// ListDocuments lists documents in a collection, optionally filtered by tags
// (AND semantics) and limited. Results are ordered by document ID.
func (s *Service) ListDocuments(ctx context.Context, collectionID string, filter ListFilter) ([]domain.Document, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.collectionExistsUnlocked(collectionID) {
		return nil, domain.ErrCollectionNotFound
	}
	want := normalizeTags(filter.Tags)
	out := make([]domain.Document, 0)
	prefix := collectionID + "/"
	for key, doc := range s.documents {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if len(want) > 0 && !hasAllTags(doc.Tags, want) {
			continue
		}
		out = append(out, doc.Clone())
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// GetDocumentHistory returns the full revision history of a document, ordered
// by version ascending.
func (s *Service) GetDocumentHistory(ctx context.Context, collectionID, documentID string) ([]domain.Revision, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := documentKey(collectionID, documentID)
	if _, ok := s.documents[key]; !ok {
		return nil, domain.ErrDocumentNotFound
	}
	return domain.CloneRevisions(s.history[key]), nil
}

// getDocumentUnlocked looks up a document. Caller holds at least a read lock.
func (s *Service) getDocumentUnlocked(collectionID, documentID string) (*domain.Document, error) {
	if !s.collectionExistsUnlocked(collectionID) {
		return nil, domain.ErrCollectionNotFound
	}
	key := documentKey(collectionID, documentID)
	doc, ok := s.documents[key]
	if !ok {
		return nil, domain.ErrDocumentNotFound
	}
	return doc, nil
}

// applyUpdateUnlocked mutates a document in place according to the request and
// appends a revision. Caller holds the write lock. Returns the (same) document
// pointer for convenience.
func (s *Service) applyUpdateUnlocked(doc *domain.Document, req UpdateDocumentRequest, now time.Time) *domain.Document {
	if req.Title != nil {
		doc.Title = *req.Title
	}
	if req.Content != nil {
		doc.Content = *req.Content
	}
	if req.Tags != nil {
		doc.Tags = normalizeTags(*req.Tags)
	}
	doc.Version++
	doc.UpdatedAt = now

	key := documentKey(doc.CollectionID, doc.ID)
	s.history[key] = append(s.history[key], domain.Revision{
		Version:    doc.Version,
		Title:      doc.Title,
		Content:    doc.Content,
		Tags:       cloneStrings(doc.Tags),
		Actor:      req.Actor,
		Reason:     req.Reason,
		ModifiedAt: now,
	})
	return doc
}

// normalizeTags dedupes and sorts tags and returns a non-nil slice (or nil if
// the input is empty after trimming).
func normalizeTags(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// hasAllTags reports whether doc has every tag in want.
func hasAllTags(doc, want []string) bool {
	if len(want) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(doc))
	for _, t := range doc {
		set[t] = struct{}{}
	}
	for _, t := range want {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}

// cloneStrings returns a copy of a string slice.
func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// itoa formats an int as a string.
func itoa(i int) string {
	return strconvItoa(i)
}
