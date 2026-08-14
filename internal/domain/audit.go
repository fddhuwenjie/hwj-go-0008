package domain

import "time"

// AuditEventType enumerates the event types recorded in the audit stream.
type AuditEventType string

const (
	AuditCollectionCreated AuditEventType = "collection.created"
	AuditDocumentCreated   AuditEventType = "document.created"
	AuditDocumentUpdated   AuditEventType = "document.updated"
	AuditBatchApplied      AuditEventType = "document.batch_applied"
	AuditBatchRejected     AuditEventType = "document.batch_rejected"
	AuditLockAcquired      AuditEventType = "lock.acquired"
	AuditLockRenewed       AuditEventType = "lock.renewed"
	AuditLockReleased      AuditEventType = "lock.released"
	AuditLockReaped        AuditEventType = "lock.reaped"
)

// AuditEvent is a single append-only record in the audit stream. Events are
// sequenced with a monotonically increasing Sequence under the service lock, so
// ordering is deterministic and total within a process.
type AuditEvent struct {
	Sequence       int64          `json:"sequence"`
	Timestamp      time.Time      `json:"timestamp"`
	Type           AuditEventType `json:"type"`
	Actor          string         `json:"actor,omitempty"`
	CollectionID   string         `json:"collection_id,omitempty"`
	DocumentID     string         `json:"document_id,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Details        map[string]any `json:"details,omitempty"`
}

// Clone returns a deep copy of the audit event, including its details map.
func (e AuditEvent) Clone() AuditEvent {
	out := AuditEvent{
		Sequence:       e.Sequence,
		Timestamp:      e.Timestamp,
		Type:           e.Type,
		Actor:          e.Actor,
		CollectionID:   e.CollectionID,
		DocumentID:     e.DocumentID,
		IdempotencyKey: e.IdempotencyKey,
	}
	if e.Details != nil {
		out.Details = make(map[string]any, len(e.Details))
		for k, v := range e.Details {
			out.Details[k] = v
		}
	}
	return out
}

// CloneAuditEvents returns a deep copy of an audit-event slice.
func CloneAuditEvents(in []AuditEvent) []AuditEvent {
	if in == nil {
		return nil
	}
	out := make([]AuditEvent, len(in))
	for i := range in {
		out[i] = in[i].Clone()
	}
	return out
}

// AuditFilter selects a subset of audit events. Zero-value fields match all.
type AuditFilter struct {
	CollectionID string
	DocumentID   string
	Actor        string
	Type         AuditEventType
	// MinSequence returns events with Sequence >= MinSequence (for tailing).
	MinSequence int64
	// Limit bounds the number of returned events (most recent first when
	// applied to the full log). A non-positive Limit means no limit.
	Limit int
}
