package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
	"github.com/fddhuwenjie/hwj-go-0008/internal/service"
)

// newTestService returns a service backed by a manual clock set to a fixed
// instant, so that lock expiry and audit timestamps are deterministic.
func newTestService() (*service.Service, *domain.ManualClock) {
	clock := domain.NewManualClock(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))
	return service.New(service.Options{Clock: clock}), clock
}

// mustRegisterCollection registers a collection, failing the test on error.
func mustRegisterCollection(t *testing.T, s *service.Service, id, name string) {
	t.Helper()
	if _, err := s.RegisterCollection(context.Background(), service.RegisterCollectionRequest{
		ID: id, Name: name, Actor: "tester",
	}); err != nil {
		t.Fatalf("RegisterCollection: %v", err)
	}
}

// mustCreateDocument creates a document, failing the test on error.
func mustCreateDocument(t *testing.T, s *service.Service, colID, docID, title string, tags []string) *domain.Document {
	t.Helper()
	doc, err := s.CreateDocument(context.Background(), service.CreateDocumentRequest{
		CollectionID: colID, ID: docID, Title: title, Content: "c", Tags: tags, Actor: "tester",
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	return doc
}

func ptr[T any](v T) *T { return &v }

// ---------- Collections ----------

func TestRegisterCollection_New(t *testing.T) {
	s, _ := newTestService()
	col, err := s.RegisterCollection(context.Background(), service.RegisterCollectionRequest{
		ID: "docs", Name: "Docs", Description: "stuff",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if col.ID != "docs" || col.Name != "Docs" {
		t.Fatalf("unexpected collection: %+v", col)
	}
}

func TestRegisterCollection_GeneratesID(t *testing.T) {
	s, _ := newTestService()
	col, err := s.RegisterCollection(context.Background(), service.RegisterCollectionRequest{Name: "Docs"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if col.ID == "" {
		t.Fatal("expected generated ID")
	}
}

func TestRegisterCollection_ReregisterByID(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	// Re-registering the same ID returns the existing collection unchanged.
	col, err := s.RegisterCollection(context.Background(), service.RegisterCollectionRequest{
		ID: "docs", Name: "Different",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if col.Name != "Docs" {
		t.Fatalf("expected original name Docs, got %q", col.Name)
	}
}

func TestRegisterCollection_Validation(t *testing.T) {
	s, _ := newTestService()
	_, err := s.RegisterCollection(context.Background(), service.RegisterCollectionRequest{Name: "  "})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestRegisterCollection_Idempotent(t *testing.T) {
	s, _ := newTestService()
	req := service.RegisterCollectionRequest{
		ID: "docs", Name: "Docs", IdempotencyKey: "k1",
	}
	col1, err := s.RegisterCollection(context.Background(), req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	col2, err := s.RegisterCollection(context.Background(), req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if col1.ID != col2.ID || col1.Name != col2.Name {
		t.Fatalf("idempotent replay mismatch: %+v vs %+v", col1, col2)
	}
	// Reusing the key with a different payload conflicts.
	_, err = s.RegisterCollection(context.Background(), service.RegisterCollectionRequest{
		ID: "docs", Name: "Other", IdempotencyKey: "k1",
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestGetCollection_NotFound(t *testing.T) {
	s, _ := newTestService()
	_, err := s.GetCollection(context.Background(), "missing")
	if !errors.Is(err, domain.ErrCollectionNotFound) {
		t.Fatalf("expected ErrCollectionNotFound, got %v", err)
	}
}

func TestListCollections(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "b", "B")
	mustRegisterCollection(t, s, "a", "A")
	cols, err := s.ListCollections(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 2 || cols[0].ID != "a" || cols[1].ID != "b" {
		t.Fatalf("expected sorted [a b], got %+v", cols)
	}
}

// ---------- Documents ----------

func TestCreateDocument(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	doc, err := s.CreateDocument(context.Background(), service.CreateDocumentRequest{
		CollectionID: "docs", ID: "d1", Title: "T", Content: "C", Tags: []string{"b", "a", "a"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Version != 1 {
		t.Fatalf("expected version 1, got %d", doc.Version)
	}
	// Tags are deduped and sorted.
	want := []string{"a", "b"}
	if !equalSlice(doc.Tags, want) {
		t.Fatalf("tags: got %v want %v", doc.Tags, want)
	}
}

func TestCreateDocument_Validation(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	_, err := s.CreateDocument(context.Background(), service.CreateDocumentRequest{
		CollectionID: "docs", Title: "  ",
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestCreateDocument_CollectionNotFound(t *testing.T) {
	s, _ := newTestService()
	_, err := s.CreateDocument(context.Background(), service.CreateDocumentRequest{
		CollectionID: "nope", Title: "T",
	})
	if !errors.Is(err, domain.ErrCollectionNotFound) {
		t.Fatalf("expected ErrCollectionNotFound, got %v", err)
	}
}

func TestCreateDocument_DuplicateID(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	_, err := s.CreateDocument(context.Background(), service.CreateDocumentRequest{
		CollectionID: "docs", ID: "d1", Title: "T2",
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for duplicate id, got %v", err)
	}
}

func TestCreateDocument_Idempotent(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	req := service.CreateDocumentRequest{
		CollectionID: "docs", ID: "d1", Title: "T", Content: "C", IdempotencyKey: "k1",
	}
	d1, err := s.CreateDocument(context.Background(), req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	d2, err := s.CreateDocument(context.Background(), req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if d1.ID != d2.ID || d1.Version != d2.Version {
		t.Fatalf("idempotent mismatch: %+v vs %+v", d1, d2)
	}
	// Reusing key with different body conflicts.
	req2 := req
	req2.Title = "Different"
	_, err = s.CreateDocument(context.Background(), req2)
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestCreateDocument_IdempotentReplaysError(t *testing.T) {
	s, _ := newTestService()
	// No collection registered: first call fails, and the error is replayed.
	req := service.CreateDocumentRequest{
		CollectionID: "docs", Title: "T", IdempotencyKey: "k1",
	}
	_, err := s.CreateDocument(context.Background(), req)
	if !errors.Is(err, domain.ErrCollectionNotFound) {
		t.Fatalf("first: expected ErrCollectionNotFound, got %v", err)
	}
	_, err = s.CreateDocument(context.Background(), req)
	if !errors.Is(err, domain.ErrCollectionNotFound) {
		t.Fatalf("replay: expected ErrCollectionNotFound, got %v", err)
	}
}

func TestGetDocument_NotFound(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	_, err := s.GetDocument(context.Background(), "docs", "missing")
	if !errors.Is(err, domain.ErrDocumentNotFound) {
		t.Fatalf("expected ErrDocumentNotFound, got %v", err)
	}
}

func TestUpdateDocument_Success(t *testing.T) {
	s, clock := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)

	clock.Advance(time.Second)
	newTitle := "T2"
	doc, err := s.UpdateDocument(context.Background(), service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Title: &newTitle, Actor: "u", Reason: "r",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Version != 2 || doc.Title != "T2" {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	// Unchanged content preserved.
	if doc.Content != "c" {
		t.Fatalf("content changed unexpectedly: %q", doc.Content)
	}
}

func TestUpdateDocument_PartialTags(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", []string{"a"})
	// Replace tags with a new set; title/content unchanged (nil pointers).
	newTags := []string{"x", "y"}
	doc, err := s.UpdateDocument(context.Background(), service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Tags: &newTags,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !equalSlice(doc.Tags, []string{"x", "y"}) {
		t.Fatalf("tags: got %v", doc.Tags)
	}
	if doc.Title != "T" {
		t.Fatalf("title changed: %q", doc.Title)
	}
	// Setting tags to an empty slice clears them.
	emptyTags := []string{}
	doc, err = s.UpdateDocument(context.Background(), service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 2, Tags: &emptyTags,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doc.Tags) != 0 {
		t.Fatalf("expected empty tags, got %v", doc.Tags)
	}
}

func TestUpdateDocument_VersionConflict(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	newTitle := "T2"
	// Move to v2.
	if _, err := s.UpdateDocument(context.Background(), service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Title: &newTitle,
	}); err != nil {
		t.Fatalf("first update: %v", err)
	}
	// Stale update expecting v1 must conflict.
	_, err := s.UpdateDocument(context.Background(), service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Title: &newTitle,
	})
	var vce *domain.VersionConflictError
	if !errors.As(err, &vce) {
		t.Fatalf("expected VersionConflictError, got %v", err)
	}
	if vce.Expected != 1 || vce.Actual != 2 {
		t.Fatalf("conflict details wrong: expected %d actual %d", vce.Expected, vce.Actual)
	}
	// Document state unchanged by the failed update.
	doc, _ := s.GetDocument(context.Background(), "docs", "d1")
	if doc.Version != 2 {
		t.Fatalf("version changed after conflict: %d", doc.Version)
	}
}

func TestUpdateDocument_Idempotent(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	newTitle := "T2"
	req := service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Title: &newTitle, IdempotencyKey: "k1",
	}
	d1, err := s.UpdateDocument(context.Background(), req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if d1.Version != 2 {
		t.Fatalf("expected v2, got %d", d1.Version)
	}
	// Replay returns the original v2 result even though current version is 2.
	d2, err := s.UpdateDocument(context.Background(), req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if d2.Version != 2 || d2.Title != "T2" {
		t.Fatalf("replay mismatch: %+v", d2)
	}
	// Current version is still 2 (no double-increment).
	doc, _ := s.GetDocument(context.Background(), "docs", "d1")
	if doc.Version != 2 {
		t.Fatalf("version should still be 2, got %d", doc.Version)
	}
}

func TestUpdateDocument_IdempotentReplaysConflict(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	newTitle := "T2"
	if _, err := s.UpdateDocument(context.Background(), service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Title: &newTitle,
	}); err != nil {
		t.Fatalf("first update: %v", err)
	}
	// Stale update with idempotency key; first attempt conflicts.
	req := service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Title: &newTitle, IdempotencyKey: "kc",
	}
	_, err := s.UpdateDocument(context.Background(), req)
	if !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	// Replay returns the same conflict.
	_, err = s.UpdateDocument(context.Background(), req)
	if !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("expected conflict on replay, got %v", err)
	}
}

func TestListDocuments_TagFilter(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T1", []string{"a", "b"})
	mustCreateDocument(t, s, "docs", "d2", "T2", []string{"b"})
	mustCreateDocument(t, s, "docs", "d3", "T3", []string{"a"})
	// AND filter: a and b -> only d1.
	docs, err := s.ListDocuments(context.Background(), "docs", service.ListFilter{Tags: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "d1" {
		t.Fatalf("expected [d1], got %+v", docs)
	}
	// Single tag b -> d1, d2 (sorted).
	docs, _ = s.ListDocuments(context.Background(), "docs", service.ListFilter{Tags: []string{"b"}})
	if len(docs) != 2 || docs[0].ID != "d1" || docs[1].ID != "d2" {
		t.Fatalf("expected [d1 d2], got %+v", docs)
	}
}

func TestListDocuments_Limit(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	mustCreateDocument(t, s, "docs", "d2", "T", nil)
	mustCreateDocument(t, s, "docs", "d3", "T", nil)
	docs, err := s.ListDocuments(context.Background(), "docs", service.ListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2, got %d", len(docs))
	}
}

func TestGetDocumentHistory(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", []string{"a"})
	newTitle := "T2"
	if _, err := s.UpdateDocument(context.Background(), service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Title: &newTitle,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	revs, err := s.GetDocumentHistory(context.Background(), "docs", "d1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(revs))
	}
	if revs[0].Version != 1 || revs[1].Version != 2 {
		t.Fatalf("revision order wrong: %+v", revs)
	}
	if revs[1].Title != "T2" {
		t.Fatalf("revision 2 title: %q", revs[1].Title)
	}
}

// ---------- Batch ----------

func TestBatchUpdate_AtomicSuccess(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T1", nil)
	mustCreateDocument(t, s, "docs", "d2", "T2", nil)
	t1, t2 := "T1b", "T2b"
	res, err := s.BatchUpdateDocuments(context.Background(), service.BatchUpdateRequest{
		CollectionID: "docs",
		Items: []service.BatchItem{
			{DocumentID: "d1", ExpectedVersion: 1, Title: &t1},
			{DocumentID: "d2", ExpectedVersion: 1, Title: &t2},
		},
		Actor: "batcher",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Applied || len(res.Results) != 2 {
		t.Fatalf("expected applied batch with 2 results, got %+v", res)
	}
	// Both documents advanced to v2.
	d1, _ := s.GetDocument(context.Background(), "docs", "d1")
	d2, _ := s.GetDocument(context.Background(), "docs", "d2")
	if d1.Version != 2 || d2.Version != 2 {
		t.Fatalf("expected both v2, got %d %d", d1.Version, d2.Version)
	}
}

func TestBatchUpdate_ConflictNoPartialApply(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T1", nil)
	mustCreateDocument(t, s, "docs", "d2", "T2", nil)
	// Move d2 to v2 so the batch item targeting it conflicts.
	t2 := "T2b"
	if _, err := s.UpdateDocument(context.Background(), service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d2", ExpectedVersion: 1, Title: &t2,
	}); err != nil {
		t.Fatalf("pre-update: %v", err)
	}
	t1 := "T1b"
	_, err := s.BatchUpdateDocuments(context.Background(), service.BatchUpdateRequest{
		CollectionID: "docs",
		Items: []service.BatchItem{
			{DocumentID: "d1", ExpectedVersion: 1, Title: &t1}, // ok
			{DocumentID: "d2", ExpectedVersion: 1, Title: &t2}, // conflict (d2 is v2)
		},
	})
	if !errors.Is(err, domain.ErrBatchConflict) {
		t.Fatalf("expected ErrBatchConflict, got %v", err)
	}
	// d1 must NOT have been updated (atomicity).
	d1, _ := s.GetDocument(context.Background(), "docs", "d1")
	if d1.Version != 1 || d1.Title != "T1" {
		t.Fatalf("d1 was partially applied: %+v", d1)
	}
}

func TestBatchUpdate_ReportsAllFailures(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T1", nil)
	t1 := "T1b"
	res, err := s.BatchUpdateDocuments(context.Background(), service.BatchUpdateRequest{
		CollectionID: "docs",
		Items: []service.BatchItem{
			{DocumentID: "missing", ExpectedVersion: 1, Title: &t1}, // not found
			{DocumentID: "d1", ExpectedVersion: 5, Title: &t1},      // version conflict
			{DocumentID: "d1", ExpectedVersion: 1, Title: &t1},      // duplicate
		},
	})
	if !errors.Is(err, domain.ErrBatchConflict) {
		t.Fatalf("expected ErrBatchConflict, got %v", err)
	}
	if res.Applied {
		t.Fatal("expected not applied")
	}
	if len(res.Failures) != 3 {
		t.Fatalf("expected 3 failures, got %d: %+v", len(res.Failures), res.Failures)
	}
}

func TestBatchUpdate_DuplicateDetection(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T1", nil)
	t1 := "T1b"
	_, err := s.BatchUpdateDocuments(context.Background(), service.BatchUpdateRequest{
		CollectionID: "docs",
		Items: []service.BatchItem{
			{DocumentID: "d1", ExpectedVersion: 1, Title: &t1},
			{DocumentID: "d1", ExpectedVersion: 1, Title: &t1},
		},
	})
	if !errors.Is(err, domain.ErrBatchConflict) {
		t.Fatalf("expected ErrBatchConflict, got %v", err)
	}
}

func TestBatchUpdate_Idempotent(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T1", nil)
	t1 := "T1b"
	req := service.BatchUpdateRequest{
		CollectionID: "docs", IdempotencyKey: "bk1",
		Items: []service.BatchItem{{DocumentID: "d1", ExpectedVersion: 1, Title: &t1}},
	}
	r1, err := s.BatchUpdateDocuments(context.Background(), req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	r2, err := s.BatchUpdateDocuments(context.Background(), req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !r1.Applied || !r2.Applied {
		t.Fatal("both should be applied")
	}
	if r1.Results[0].Document.Version != r2.Results[0].Document.Version {
		t.Fatalf("replay version mismatch")
	}
	// No double-application.
	d1, _ := s.GetDocument(context.Background(), "docs", "d1")
	if d1.Version != 2 {
		t.Fatalf("expected v2, got %d", d1.Version)
	}
}

func TestBatchUpdate_Empty(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	_, err := s.BatchUpdateDocuments(context.Background(), service.BatchUpdateRequest{
		CollectionID: "docs", Items: nil,
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

// ---------- Locks ----------

func TestAcquireLock_Success(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	lock, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lock.Holder != "alice" || lock.RenewCount != 0 {
		t.Fatalf("unexpected lock: %+v", lock)
	}
}

func TestAcquireLock_ConflictOtherHolder(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("alice acquire: %v", err)
	}
	_, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "bob", TTL: 60 * time.Second,
	})
	if !errors.Is(err, domain.ErrLockHeldByOther) {
		t.Fatalf("expected ErrLockHeldByOther, got %v", err)
	}
}

func TestAcquireLock_SameHolderRenews(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	lock, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 120 * time.Second,
	})
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if lock.RenewCount != 1 {
		t.Fatalf("expected renew count 1, got %d", lock.RenewCount)
	}
}

func TestAcquireLock_ExpiredReclaimed(t *testing.T) {
	s, clock := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("alice acquire: %v", err)
	}
	// Advance past expiry.
	clock.Advance(61 * time.Second)
	// Bob can now acquire because alice's lock expired.
	lock, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "bob", TTL: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("bob acquire after expiry: %v", err)
	}
	if lock.Holder != "bob" {
		t.Fatalf("expected bob, got %s", lock.Holder)
	}
}

func TestAcquireLock_TimeBoundary(t *testing.T) {
	s, clock := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// At exactly the expiry instant, the lock is considered expired (boundary
	// inclusive of expiry).
	clock.Advance(60 * time.Second)
	lock, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "bob", TTL: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected reclamation at exact boundary, got %v", err)
	}
	if lock.Holder != "bob" {
		t.Fatalf("expected bob at boundary, got %s", lock.Holder)
	}
}

func TestRenewLock_Success(t *testing.T) {
	s, clock := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	clock.Advance(30 * time.Second)
	lock, err := s.RenewLock(context.Background(), service.RenewLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	// New expiry is now+60s = 30s+60s after acquire time.
	if !lock.ExpiresAt.Equal(clock.Now().Add(60 * time.Second)) {
		t.Fatalf("renew expiry wrong: got %v want %v", lock.ExpiresAt, clock.Now().Add(60*time.Second))
	}
}

func TestRenewLock_WrongHolder(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_, err := s.RenewLock(context.Background(), service.RenewLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "bob", TTL: 60 * time.Second,
	})
	if !errors.Is(err, domain.ErrLockHeldByOther) {
		t.Fatalf("expected ErrLockHeldByOther, got %v", err)
	}
}

func TestRenewLock_ExpiredOrAbsent(t *testing.T) {
	s, clock := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	clock.Advance(61 * time.Second)
	_, err := s.RenewLock(context.Background(), service.RenewLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	})
	if !errors.Is(err, domain.ErrLockNotHeld) {
		t.Fatalf("expected ErrLockNotHeld for expired renew, got %v", err)
	}
	// Renew on a doc with no lock at all.
	mustCreateDocument(t, s, "docs", "d2", "T", nil)
	_, err = s.RenewLock(context.Background(), service.RenewLockRequest{
		CollectionID: "docs", DocumentID: "d2", Holder: "alice", TTL: 60 * time.Second,
	})
	if !errors.Is(err, domain.ErrLockNotHeld) {
		t.Fatalf("expected ErrLockNotHeld for absent renew, got %v", err)
	}
}

func TestReleaseLock_Success(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := s.ReleaseLock(context.Background(), service.ReleaseLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice",
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Another holder can now acquire.
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "bob", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("bob acquire after release: %v", err)
	}
}

func TestReleaseLock_WrongHolder(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	err := s.ReleaseLock(context.Background(), service.ReleaseLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "bob",
	})
	if !errors.Is(err, domain.ErrLockHeldByOther) {
		t.Fatalf("expected ErrLockHeldByOther, got %v", err)
	}
	// Lock still held by alice.
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "carol", TTL: 60 * time.Second,
	}); !errors.Is(err, domain.ErrLockHeldByOther) {
		t.Fatalf("expected lock still held, got %v", err)
	}
}

func TestReleaseLock_Duplicate(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := s.ReleaseLock(context.Background(), service.ReleaseLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice",
	}); err != nil {
		t.Fatalf("first release: %v", err)
	}
	// Releasing again is a not-held error (duplicate operation detection).
	err := s.ReleaseLock(context.Background(), service.ReleaseLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice",
	})
	if !errors.Is(err, domain.ErrLockNotHeld) {
		t.Fatalf("expected ErrLockNotHeld on duplicate release, got %v", err)
	}
}

func TestReleaseLock_Idempotent(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	req := service.ReleaseLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", IdempotencyKey: "rk1",
	}
	if err := s.ReleaseLock(context.Background(), req); err != nil {
		t.Fatalf("first release: %v", err)
	}
	// Replay returns nil (the original success).
	if err := s.ReleaseLock(context.Background(), req); err != nil {
		t.Fatalf("replay release: %v", err)
	}
}

func TestReapExpiredLocks(t *testing.T) {
	s, clock := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	mustCreateDocument(t, s, "docs", "d2", "T", nil)
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("acquire d1: %v", err)
	}
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d2", Holder: "bob", TTL: 120 * time.Second,
	}); err != nil {
		t.Fatalf("acquire d2: %v", err)
	}
	// d1 expires (60s), d2 does not (120s).
	clock.Advance(61 * time.Second)
	reaped, err := s.ReapExpiredLocks(context.Background())
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if len(reaped) != 1 || reaped[0] != "d1" {
		t.Fatalf("expected [d1] reaped, got %v", reaped)
	}
	// d2's lock remains; alice can re-acquire d1.
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d1", Holder: "alice", TTL: 60 * time.Second,
	}); err != nil {
		t.Fatalf("re-acquire d1: %v", err)
	}
	if _, err := s.AcquireLock(context.Background(), service.AcquireLockRequest{
		CollectionID: "docs", DocumentID: "d2", Holder: "alice", TTL: 60 * time.Second,
	}); !errors.Is(err, domain.ErrLockHeldByOther) {
		t.Fatalf("d2 should still be locked by bob, got %v", err)
	}
}

// ---------- Audit ----------

func TestAuditEvents_OrderedAndFiltered(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	newTitle := "T2"
	if _, err := s.UpdateDocument(context.Background(), service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Title: &newTitle, Actor: "u1",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	events, err := s.ListAuditEvents(context.Background(), domain.AuditFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect: collection.created, document.created, document.updated.
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	for i := 1; i < len(events); i++ {
		if events[i].Sequence <= events[i-1].Sequence {
			t.Fatalf("events not ordered by sequence: %v", events)
		}
	}
	// Filter by document_id and type.
	upd, _ := s.ListAuditEvents(context.Background(), domain.AuditFilter{
		DocumentID: "d1", Type: domain.AuditDocumentUpdated,
	})
	if len(upd) != 1 || upd[0].Type != domain.AuditDocumentUpdated {
		t.Fatalf("expected 1 update event for d1, got %+v", upd)
	}
}

func TestAuditEvents_MinSequenceAndLimit(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)
	// min_sequence=2 excludes the first event (collection.created has seq 1).
	tail, _ := s.ListAuditEvents(context.Background(), domain.AuditFilter{MinSequence: 2})
	if len(tail) != 1 {
		t.Fatalf("expected 1 event with seq>=2, got %d", len(tail))
	}
	// limit=1 returns the most recent.
	lim, _ := s.ListAuditEvents(context.Background(), domain.AuditFilter{Limit: 1})
	if len(lim) != 1 || lim[0].Type != domain.AuditDocumentCreated {
		t.Fatalf("expected most recent = document.created, got %+v", lim)
	}
}

// ---------- Mutable isolation ----------

func TestMutableIsolation_ReturnedDocumentNotShared(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	doc, _ := s.CreateDocument(context.Background(), service.CreateDocumentRequest{
		CollectionID: "docs", ID: "d1", Title: "T", Content: "C", Tags: []string{"a"},
	})
	// Mutate the returned document.
	doc.Title = "MUTATED"
	doc.Tags[0] = "MUTATED"
	doc.Content = "MUTATED"
	// The stored document must be unaffected.
	got, _ := s.GetDocument(context.Background(), "docs", "d1")
	if got.Title != "T" || got.Content != "C" || got.Tags[0] != "a" {
		t.Fatalf("internal state leaked: %+v", got)
	}
}

func TestMutableIsolation_ReturnedRevisionsNotShared(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", []string{"a"})
	revs, _ := s.GetDocumentHistory(context.Background(), "docs", "d1")
	revs[0].Title = "MUTATED"
	revs[0].Tags[0] = "MUTATED"
	again, _ := s.GetDocumentHistory(context.Background(), "docs", "d1")
	if again[0].Title != "T" || again[0].Tags[0] != "a" {
		t.Fatalf("revision state leaked: %+v", again[0])
	}
}

func TestMutableIsolation_RequestTagsNotShared(t *testing.T) {
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	tags := []string{"a", "b"}
	doc, _ := s.CreateDocument(context.Background(), service.CreateDocumentRequest{
		CollectionID: "docs", ID: "d1", Title: "T", Tags: tags,
	})
	// Mutate the caller's slice after creation.
	tags[0] = "MUTATED"
	if doc.Tags[0] != "a" {
		t.Fatalf("request slice leaked into storage: %v", doc.Tags)
	}
}

// ---------- Context cancellation ----------

func TestContextCanceled(t *testing.T) {
	s, _ := newTestService()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.ListCollections(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// equalSlice reports whether two string slices are equal.
func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
