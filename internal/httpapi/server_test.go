package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
	"github.com/fddhuwenjie/hwj-go-0008/internal/httpapi"
	"github.com/fddhuwenjie/hwj-go-0008/internal/service"
)

func newServer(t *testing.T) (*httptest.Server, *service.Service, *domain.ManualClock) {
	t.Helper()
	clock := domain.NewManualClock(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))
	svc := service.New(service.Options{Clock: clock})
	srv := httptest.NewServer(httpapi.New(svc))
	t.Cleanup(srv.Close)
	return srv, svc, clock
}

// doJSON performs a request and returns the status and decoded body (into out,
// if non-nil). It also returns the raw body for inspection.
func doJSON(t *testing.T, srv *httptest.Server, method, path string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func mustRegister(t *testing.T, srv *httptest.Server, id, name string) {
	t.Helper()
	code, body := doJSON(t, srv, "POST", "/collections", map[string]string{"id": id, "name": name}, nil)
	if code != http.StatusCreated {
		t.Fatalf("register collection: status %d body %s", code, body)
	}
}

func errCode(t *testing.T, body []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal error body: %v (body=%s)", err, body)
	}
	c, _ := m["code"].(string)
	return c
}

func TestHTTP_RegisterAndListCollections(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "a", "A")
	mustRegister(t, srv, "b", "B")
	code, body := doJSON(t, srv, "GET", "/collections", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d %s", code, body)
	}
	var res struct {
		Collections []map[string]string `json:"collections"`
	}
	json.Unmarshal(body, &res)
	if len(res.Collections) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(res.Collections))
	}
}

func TestHTTP_GetCollection_NotFound(t *testing.T) {
	srv, _, _ := newServer(t)
	code, body := doJSON(t, srv, "GET", "/collections/missing", nil, nil)
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d %s", code, body)
	}
	if errCode(t, body) != "not_found" {
		t.Fatalf("expected not_found, got %s", errCode(t, body))
	}
}

func TestHTTP_CreateDocumentAndGet(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	code, body := doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{
		"id": "d1", "title": "T", "content": "C", "tags": []string{"a", "b"},
	}, nil)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	var doc map[string]any
	json.Unmarshal(body, &doc)
	if doc["version"].(float64) != 1 {
		t.Fatalf("expected version 1, got %v", doc["version"])
	}
	// GET the document.
	code, body = doJSON(t, srv, "GET", "/collections/docs/documents/d1", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("get: %d %s", code, body)
	}
}

func TestHTTP_CreateDocument_Validation(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	code, body := doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{
		"id": "d1", "title": "  ",
	}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", code, body)
	}
	if errCode(t, body) != "invalid_input" {
		t.Fatalf("expected invalid_input, got %s", errCode(t, body))
	}
}

func TestHTTP_CreateDocument_BadJSON(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	req, _ := http.NewRequest("POST", srv.URL+"/collections/docs/documents", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", resp.StatusCode)
	}
}

func TestHTTP_UpdateDocument_VersionConflict(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{
		"id": "d1", "title": "T",
	}, nil)
	// Move to v2.
	code, _ := doJSON(t, srv, "PUT", "/collections/docs/documents/d1", map[string]any{
		"expected_version": 1, "title": "T2",
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("first update: %d", code)
	}
	// Stale update expecting v1.
	code, body := doJSON(t, srv, "PUT", "/collections/docs/documents/d1", map[string]any{
		"expected_version": 1, "title": "T3",
	}, nil)
	if code != http.StatusConflict {
		t.Fatalf("expected 409, got %d %s", code, body)
	}
	if errCode(t, body) != "version_conflict" {
		t.Fatalf("expected version_conflict, got %s", errCode(t, body))
	}
}

func TestHTTP_DocumentHistory(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d1", "title": "T"}, nil)
	doJSON(t, srv, "PUT", "/collections/docs/documents/d1", map[string]any{"expected_version": 1, "title": "T2"}, nil)
	code, body := doJSON(t, srv, "GET", "/collections/docs/documents/d1/history", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("history: %d %s", code, body)
	}
	var res struct {
		Revisions []map[string]any `json:"revisions"`
	}
	json.Unmarshal(body, &res)
	if len(res.Revisions) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(res.Revisions))
	}
}

func TestHTTP_ListDocuments_TagFilter(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d1", "title": "T", "tags": []string{"a", "b"}}, nil)
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d2", "title": "T", "tags": []string{"b"}}, nil)
	code, body := doJSON(t, srv, "GET", "/collections/docs/documents?tags=a,b", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d %s", code, body)
	}
	var res struct {
		Documents []map[string]any `json:"documents"`
	}
	json.Unmarshal(body, &res)
	if len(res.Documents) != 1 || res.Documents[0]["id"] != "d1" {
		t.Fatalf("expected [d1], got %+v", res.Documents)
	}
}

func TestHTTP_BatchUpdate_Success(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d1", "title": "T1"}, nil)
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d2", "title": "T2"}, nil)
	code, body := doJSON(t, srv, "POST", "/collections/docs/documents/batch", map[string]any{
		"items": []map[string]any{
			{"document_id": "d1", "expected_version": 1, "title": "T1b"},
			{"document_id": "d2", "expected_version": 1, "title": "T2b"},
		},
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("batch: %d %s", code, body)
	}
	var res struct {
		Applied bool             `json:"applied"`
		Results []map[string]any `json:"results"`
	}
	json.Unmarshal(body, &res)
	if !res.Applied || len(res.Results) != 2 {
		t.Fatalf("expected applied batch with 2 results, got %s", body)
	}
}

func TestHTTP_BatchUpdate_Conflict(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d1", "title": "T1"}, nil)
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d2", "title": "T2"}, nil)
	// Move d2 to v2 so the batch conflicts.
	doJSON(t, srv, "PUT", "/collections/docs/documents/d2", map[string]any{"expected_version": 1, "title": "T2b"}, nil)
	code, body := doJSON(t, srv, "POST", "/collections/docs/documents/batch", map[string]any{
		"items": []map[string]any{
			{"document_id": "d1", "expected_version": 1, "title": "X"},
			{"document_id": "d2", "expected_version": 1, "title": "Y"}, // conflict
		},
	}, nil)
	if code != http.StatusConflict {
		t.Fatalf("expected 409, got %d %s", code, body)
	}
	if errCode(t, body) != "batch_conflict" {
		t.Fatalf("expected batch_conflict, got %s", errCode(t, body))
	}
	// d1 must not have been applied.
	_, body2 := doJSON(t, srv, "GET", "/collections/docs/documents/d1", nil, nil)
	var doc map[string]any
	json.Unmarshal(body2, &doc)
	if doc["version"].(float64) != 1 {
		t.Fatalf("d1 was partially applied: %s", body2)
	}
}

func TestHTTP_LockAcquireRenewRelease(t *testing.T) {
	srv, _, clock := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d1", "title": "T"}, nil)

	// Acquire.
	code, body := doJSON(t, srv, "POST", "/collections/docs/documents/d1/lock", map[string]any{
		"holder": "alice", "ttl_seconds": 60,
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("acquire: %d %s", code, body)
	}
	// Another holder conflicts.
	code, body = doJSON(t, srv, "POST", "/collections/docs/documents/d1/lock", map[string]any{
		"holder": "bob", "ttl_seconds": 60,
	}, nil)
	if code != http.StatusConflict || errCode(t, body) != "lock_conflict" {
		t.Fatalf("expected lock_conflict, got %d %s", code, body)
	}
	// Renew by alice.
	code, _ = doJSON(t, srv, "PUT", "/collections/docs/documents/d1/lock", map[string]any{
		"holder": "alice", "ttl_seconds": 60,
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("renew: %d", code)
	}
	// Expire the lock; bob can now acquire.
	clock.Advance(61 * time.Second)
	code, _ = doJSON(t, srv, "POST", "/collections/docs/documents/d1/lock", map[string]any{
		"holder": "bob", "ttl_seconds": 60,
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("bob acquire after expiry: %d", code)
	}
	// Release by alice fails (bob holds it).
	code, _ = doJSON(t, srv, "DELETE", "/collections/docs/documents/d1/lock", map[string]any{
		"holder": "alice",
	}, nil)
	if code != http.StatusConflict {
		t.Fatalf("expected 409 for wrong holder release, got %d", code)
	}
	// Release by bob succeeds.
	code, _ = doJSON(t, srv, "DELETE", "/collections/docs/documents/d1/lock", map[string]any{
		"holder": "bob",
	}, nil)
	if code != http.StatusNoContent {
		t.Fatalf("expected 204 for release, got %d", code)
	}
	// Re-release is not found (duplicate).
	code, _ = doJSON(t, srv, "DELETE", "/collections/docs/documents/d1/lock", map[string]any{
		"holder": "bob",
	}, nil)
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 for duplicate release, got %d", code)
	}
}

func TestHTTP_ReapLocks(t *testing.T) {
	srv, _, clock := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d1", "title": "T"}, nil)
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d2", "title": "T"}, nil)
	doJSON(t, srv, "POST", "/collections/docs/documents/d1/lock", map[string]any{"holder": "alice", "ttl_seconds": 60}, nil)
	doJSON(t, srv, "POST", "/collections/docs/documents/d2/lock", map[string]any{"holder": "bob", "ttl_seconds": 120}, nil)
	clock.Advance(61 * time.Second)
	code, body := doJSON(t, srv, "POST", "/locks/reap", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("reap: %d %s", code, body)
	}
	var res struct {
		Reaped []string `json:"reaped"`
	}
	json.Unmarshal(body, &res)
	if len(res.Reaped) != 1 || res.Reaped[0] != "d1" {
		t.Fatalf("expected [d1] reaped, got %v", res.Reaped)
	}
}

func TestHTTP_AuditQuery(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{"id": "d1", "title": "T"}, nil)
	code, body := doJSON(t, srv, "GET", "/audit?document_id=d1", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("audit: %d %s", code, body)
	}
	var res struct {
		Events []map[string]any `json:"events"`
	}
	json.Unmarshal(body, &res)
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event for d1, got %d", len(res.Events))
	}
	// Filter by type.
	code, body = doJSON(t, srv, "GET", "/audit?document_id=d1&type=document.created", nil, nil)
	json.Unmarshal(body, &res)
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 created event, got %d", len(res.Events))
	}
}

func TestHTTP_Health(t *testing.T) {
	srv, _, _ := newServer(t)
	code, body := doJSON(t, srv, "GET", "/healthz", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("health: %d %s", code, body)
	}
}

// TestHTTP_Idempotency_Header verifies the Idempotency-Key header replays the
// original response for an identical retry.
func TestHTTP_Idempotency_Header(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	payload := map[string]any{"id": "d1", "title": "T", "content": "C"}
	hdr := map[string]string{"Idempotency-Key": "ik-1"}

	code1, body1 := doJSON(t, srv, "POST", "/collections/docs/documents", payload, hdr)
	code2, body2 := doJSON(t, srv, "POST", "/collections/docs/documents", payload, hdr)
	if code1 != http.StatusCreated || code2 != http.StatusCreated {
		t.Fatalf("expected 201 both, got %d %d", code1, code2)
	}
	if !bytes.Equal(body1, body2) {
		t.Fatalf("idempotent bodies differ:\n%s\n%s", body1, body2)
	}
	// Only one document exists.
	_, body3 := doJSON(t, srv, "GET", "/collections/docs/documents", nil, nil)
	var res struct {
		Documents []map[string]any `json:"documents"`
	}
	json.Unmarshal(body3, &res)
	if len(res.Documents) != 1 {
		t.Fatalf("expected 1 document after idempotent retries, got %d", len(res.Documents))
	}

	// Reuse the key with a different payload -> conflict.
	code4, body4 := doJSON(t, srv, "POST", "/collections/docs/documents", map[string]any{
		"id": "d2", "title": "Other",
	}, hdr)
	if code4 != http.StatusConflict || errCode(t, body4) != "idempotency_conflict" {
		t.Fatalf("expected idempotency_conflict, got %d %s", code4, body4)
	}
}

// TestHTTP_Idempotency_ReplaysError verifies that a failing idempotent request
// replays the same error.
func TestHTTP_Idempotency_ReplaysError(t *testing.T) {
	srv, _, _ := newServer(t)
	mustRegister(t, srv, "docs", "Docs")
	// Update on a non-existent document with an idempotency key.
	hdr := map[string]string{"Idempotency-Key": "ik-err"}
	payload := map[string]any{"expected_version": 1, "title": "T"}
	code1, _ := doJSON(t, srv, "PUT", "/collections/docs/documents/missing", payload, hdr)
	if code1 != http.StatusNotFound {
		t.Fatalf("expected 404 first, got %d", code1)
	}
	code2, _ := doJSON(t, srv, "PUT", "/collections/docs/documents/missing", payload, hdr)
	if code2 != http.StatusNotFound {
		t.Fatalf("expected 404 on replay, got %d", code2)
	}
}

func TestHTTP_RunGracefulShutdown(t *testing.T) {
	svc := service.New(service.Options{Clock: domain.SystemClock{}})
	srv := httpapi.New(svc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, "127.0.0.1:0") }()
	// Give it a moment to start, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not shut down in time")
	}
}
