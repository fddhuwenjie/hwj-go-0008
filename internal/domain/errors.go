// Package domain defines the core entities, value objects and errors for the
// document versioning and edit-lock service.
//
// The types in this package are pure data: they hold no mutable shared state
// and perform no I/O. Concurrency safety is the responsibility of the service
// layer (package service), which serializes access to these types. Callers that
// receive values from the service always receive deep copies, so mutating a
// returned value never affects the service's internal state (mutable-data
// isolation).
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

// Sentinel errors. They are wrapped by richer typed errors (see below) so that
// callers can use both errors.Is for category checks and type assertions for
// structured details.
var (
	// ErrInvalidInput is returned when a request fails validation.
	ErrInvalidInput = errors.New("invalid input")

	// ErrCollectionNotFound is returned when a referenced collection does not exist.
	ErrCollectionNotFound = errors.New("collection not found")

	// ErrDocumentNotFound is returned when a referenced document does not exist.
	ErrDocumentNotFound = errors.New("document not found")

	// ErrVersionConflict is returned when an update's expected version does not
	// match the document's current version (optimistic-concurrency failure).
	ErrVersionConflict = errors.New("version conflict")

	// ErrLockHeldByOther is returned when a document is locked by another holder.
	ErrLockHeldByOther = errors.New("lock held by another holder")

	// ErrLockNotHeld is returned when a lock operation targets a document with no
	// active lock held by the requester.
	ErrLockNotHeld = errors.New("lock not held by requester")

	// ErrLockExpired is returned when a lock exists but has already expired.
	ErrLockExpired = errors.New("lock expired")

	// ErrIdempotencyConflict is returned when an idempotency key is reused with a
	// different request payload.
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request")

	// ErrBatchConflict is returned when a batch operation could not be applied
	// atomically because one or more items conflicted.
	ErrBatchConflict = errors.New("batch contained conflicting items")

	// ErrNotFound is a generic not-found error used for resource lookups.
	ErrNotFound = errors.New("not found")
)

// ValidationError wraps ErrInvalidInput with a human-readable reason.
type ValidationError struct {
	Field  string // optional, the offending field
	Reason string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("invalid input: %s: %s", e.Field, e.Reason)
	}
	return fmt.Sprintf("invalid input: %s", e.Reason)
}

// Unwrap allows errors.Is(err, ErrInvalidInput) to match.
func (e *ValidationError) Unwrap() error { return ErrInvalidInput }

// NewValidationError constructs a ValidationError wrapping ErrInvalidInput.
func NewValidationError(field, reason string) *ValidationError {
	return &ValidationError{Field: field, Reason: reason}
}

// VersionConflictError carries the expected and actual versions that caused an
// optimistic-concurrency conflict.
type VersionConflictError struct {
	DocumentID string
	Expected   int
	Actual     int
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version conflict for document %q: expected %d, got %d", e.DocumentID, e.Expected, e.Actual)
}

// Unwrap allows errors.Is(err, ErrVersionConflict) to match.
func (e *VersionConflictError) Unwrap() error { return ErrVersionConflict }

// LockConflictError carries details about which holder currently owns a lock.
type LockConflictError struct {
	DocumentID    string
	CurrentHolder string
	ExpiresAt     string // RFC3339
}

func (e *LockConflictError) Error() string {
	return fmt.Sprintf("document %q is locked by %q until %s", e.DocumentID, e.CurrentHolder, e.ExpiresAt)
}

// Unwrap allows errors.Is(err, ErrLockHeldByOther) to match.
func (e *LockConflictError) Unwrap() error { return ErrLockHeldByOther }

// BatchConflictError lists per-item failures that prevented an atomic batch.
type BatchConflictError struct {
	Failures []BatchItemFailure
}

func (e *BatchConflictError) Error() string {
	return fmt.Sprintf("batch contained %d conflicting item(s)", len(e.Failures))
}

// Unwrap allows errors.Is(err, ErrBatchConflict) to match.
func (e *BatchConflictError) Unwrap() error { return ErrBatchConflict }

// BatchItemFailure describes a single failing item within a batch.
type BatchItemFailure struct {
	Index      int // 0-based position in the batch request
	DocumentID string
	Reason     string // e.g. "version conflict: expected 1, got 2", "document not found"
}

// IdempotencyConflictError indicates a key was reused with a different payload.
type IdempotencyConflictError struct {
	Key string
}

func (e *IdempotencyConflictError) Error() string {
	return fmt.Sprintf("idempotency key %q reused with a different request payload", e.Key)
}

// Unwrap allows errors.Is(err, ErrIdempotencyConflict) to match.
func (e *IdempotencyConflictError) Unwrap() error { return ErrIdempotencyConflict }

// NewID generates a random 16-byte hex identifier (32 characters).
// It panics only if the system CSPRNG is unavailable, which is an environment
// failure outside the service's control. In practice rand.Read never returns an
// error on supported platforms.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback should never be reached on a working system.
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
