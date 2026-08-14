package service_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
	"github.com/fddhuwenjie/hwj-go-0008/internal/service"
)

// TestConcurrentUpdate_ExactlyOneWins launches many concurrent updates all
// expecting version 1. Exactly one must succeed and advance the version; the
// rest must get a version conflict. This validates optimistic concurrency under
// the race detector.
func TestConcurrentUpdate_ExactlyOneWins(t *testing.T) {
	t.Parallel()
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)

	const n = 50
	var wg sync.WaitGroup
	var success, conflicts atomic.Int32
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			title := "T2"
			req := service.UpdateDocumentRequest{
				CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Title: &title,
			}
			<-start // release all goroutines at once
			_, err := s.UpdateDocument(context.Background(), req)
			_ = i
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, domain.ErrVersionConflict):
				conflicts.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if success.Load() != 1 {
		t.Fatalf("expected exactly 1 success, got %d", success.Load())
	}
	if conflicts.Load() != int32(n-1) {
		t.Fatalf("expected %d conflicts, got %d", n-1, conflicts.Load())
	}
	doc, _ := s.GetDocument(context.Background(), "docs", "d1")
	if doc.Version != 2 {
		t.Fatalf("expected final version 2, got %d", doc.Version)
	}
}

// TestConcurrentLockAcquire_ExactlyOneWins launches many concurrent acquires
// for the same document by different holders; exactly one must win.
func TestConcurrentLockAcquire_ExactlyOneWins(t *testing.T) {
	t.Parallel()
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)

	const n = 50
	var wg sync.WaitGroup
	var success, conflicts atomic.Int32
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			holder := "h" + itoaFmt(i)
			req := service.AcquireLockRequest{
				CollectionID: "docs", DocumentID: "d1", Holder: holder, TTL: 60 * time.Second,
			}
			<-start
			_, err := s.AcquireLock(context.Background(), req)
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, domain.ErrLockHeldByOther):
				conflicts.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if success.Load() != 1 {
		t.Fatalf("expected exactly 1 lock success, got %d", success.Load())
	}
	if conflicts.Load() != int32(n-1) {
		t.Fatalf("expected %d conflicts, got %d", n-1, conflicts.Load())
	}
}

// TestConcurrentIdempotent_SameResult fires the same idempotent create request
// from many goroutines. All must observe the same resulting document (no
// duplicate creation, no errors).
func TestConcurrentIdempotent_SameResult(t *testing.T) {
	t.Parallel()
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")

	const n = 30
	var wg sync.WaitGroup
	results := make([]*domain.Document, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := service.CreateDocumentRequest{
				CollectionID: "docs", ID: "d1", Title: "T", Content: "C", IdempotencyKey: "ik1",
			}
			<-start
			results[i], errs[i] = s.CreateDocument(context.Background(), req)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d unexpected error: %v", i, err)
		}
		if results[i] == nil || results[i].ID != "d1" || results[i].Version != 1 {
			t.Fatalf("goroutine %d unexpected result: %+v err=%v", i, results[i], errs[i])
		}
	}
	// Exactly one document exists.
	docs, _ := s.ListDocuments(context.Background(), "docs", service.ListFilter{})
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
}

// TestConcurrentBatch_NoInterleave runs a batch and concurrent single updates
// to ensure the batch is applied atomically (no version jumps mid-batch).
func TestConcurrentBatch_NoInterleave(t *testing.T) {
	t.Parallel()
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T1", nil)
	mustCreateDocument(t, s, "docs", "d2", "T2", nil)

	// Batch expects both at v1 and advances both to v2.
	t1, t2 := "T1b", "T2b"
	batchReq := service.BatchUpdateRequest{
		CollectionID: "docs",
		Items: []service.BatchItem{
			{DocumentID: "d1", ExpectedVersion: 1, Title: &t1},
			{DocumentID: "d2", ExpectedVersion: 1, Title: &t2},
		},
	}
	// A concurrent single update also expects v1; it must either win (batch
	// conflicts) or lose (single conflicts). Either way, consistency holds.
	single := service.UpdateDocumentRequest{
		CollectionID: "docs", DocumentID: "d1", ExpectedVersion: 1, Title: ptr("X"),
	}

	var wg sync.WaitGroup
	var batchErr, singleErr error
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, batchErr = s.BatchUpdateDocuments(context.Background(), batchReq)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, singleErr = s.UpdateDocument(context.Background(), single)
	}()
	close(start)
	wg.Wait()

	// If the batch applied, d1 must be at v2 (batch's update) and the single
	// update must have conflicted. If the single update applied, the batch must
	// have conflicted (d1 at v2, batch rejected). In no case should d1 exceed v2.
	d1, _ := s.GetDocument(context.Background(), "docs", "d1")
	if d1.Version > 2 {
		t.Fatalf("d1 version exceeded expected: %d", d1.Version)
	}
	if batchErr == nil && singleErr == nil {
		t.Fatalf("expected one of batch/single to conflict; both succeeded (d1=%d)", d1.Version)
	}
}

// TestConcurrentReadsDuringWrites hammers reads while writers update, mainly to
// exercise the race detector over the read/write split.
func TestConcurrentReadsDuringWrites(t *testing.T) {
	t.Parallel()
	s, _ := newTestService()
	mustRegisterCollection(t, s, "docs", "Docs")
	mustCreateDocument(t, s, "docs", "d1", "T", nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	// Readers.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = s.GetDocument(context.Background(), "docs", "d1")
					_, _ = s.GetDocumentHistory(context.Background(), "docs", "d1")
					_, _ = s.ListDocuments(context.Background(), "docs", service.ListFilter{Tags: []string{"a"}})
				}
			}
		}()
	}
	// One writer advancing versions until stopped.
	wg.Add(1)
	go func() {
		defer wg.Done()
		v := 1
		for {
			select {
			case <-stop:
				return
			default:
				title := "v" + itoaFmt(v)
				if _, err := s.UpdateDocument(context.Background(), service.UpdateDocumentRequest{
					CollectionID: "docs", DocumentID: "d1", ExpectedVersion: v, Title: &title,
				}); err == nil {
					v++
				}
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// itoaFmt formats i without importing strconv in the test (keeps imports tidy
// and avoids an extra import line).
func itoaFmt(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
