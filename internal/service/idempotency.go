package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
)

// idemEntry stores the outcome of an idempotent operation so that a retried
// request with the same key returns the identical result (success or error).
//
// The stored Value is a deep copy made at operation time; on replay the service
// returns a fresh clone so callers still cannot reach internal state. For batch
// operations a non-nil Value may accompany a non-nil Err (the batch result plus
// the conflict error).
type idemEntry struct {
	fingerprint string
	err         error
	value       any
}

// fingerprint returns a stable SHA-256 digest of the given parts, used to detect
// when an idempotency key is reused with a different request payload.
//
// json.Marshal of a slice of strings, ints and []byte inputs is deterministic
// (map keys are sorted by the encoder), so equal inputs always yield equal
// fingerprints.
func fingerprint(parts ...any) string {
	b, err := json.Marshal(parts)
	if err != nil {
		// Marshal can only fail for unsupported types (e.g. chan, func). Our
		// callers only pass primitive, string, int and slice values, so this
		// branch is unreachable in practice. Fall back to a constant marker so
		// that even a marshaling failure is deterministic.
		return "fingerprint-error"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// cloneIDemValue clones a stored idempotent result value according to its
// concrete type so that a replay never shares state with internal storage. It
// is never called with a nil value (callers guard), but nil typed pointers are
// handled defensively.
func cloneIDemValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case *domain.Document:
		if val == nil {
			return nil
		}
		c := val.Clone()
		return &c
	case domain.Document:
		c := val.Clone()
		return c
	case *domain.Collection:
		if val == nil {
			return nil
		}
		c := val.Clone()
		return &c
	case domain.Collection:
		c := val.Clone()
		return c
	case *domain.Lock:
		if val == nil {
			return nil
		}
		c := val.Clone()
		return &c
	case domain.Lock:
		c := val.Clone()
		return c
	case *BatchUpdateResult:
		if val == nil {
			return nil
		}
		return val.clone()
	default:
		// Value types without mutable interior (int, string, ...): return as-is.
		return v
	}
}
