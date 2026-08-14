package domain

import "time"

// Lock is a temporary exclusive edit lock on a document. A document may hold at
// most one lock at a time. Locks are identified by document ID and are owned by
// a holder string.
//
// Time boundary: a lock is considered expired (and therefore reclaimable) when
// now >= ExpiresAt. IsExpired uses !now.Before(ExpiresAt), so a lock is valid
// only while now < ExpiresAt. This boundary is evaluated against the injected
// Clock, making expiry deterministic in tests.
type Lock struct {
	DocumentID string    `json:"document_id"`
	Holder     string    `json:"holder"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	RenewCount int       `json:"renew_count"`
}

// IsExpired reports whether the lock has expired as of now. A lock expires at
// the instant now equals ExpiresAt (the boundary is inclusive of expiry).
func (l Lock) IsExpired(now time.Time) bool {
	return !now.Before(l.ExpiresAt)
}

// Clone returns a copy of the lock. Lock has no slice/map fields, so this is a
// value copy, provided for symmetry and safety.
func (l Lock) Clone() Lock {
	return Lock{
		DocumentID: l.DocumentID,
		Holder:     l.Holder,
		AcquiredAt: l.AcquiredAt,
		ExpiresAt:  l.ExpiresAt,
		RenewCount: l.RenewCount,
	}
}

// LockTTL is the default time-to-live for a newly acquired or renewed lock when
// the request does not specify one.
const DefaultLockTTL = 30 * time.Second

// MaxLockTTL bounds how far into the future a lock may be extended, to prevent
// unbounded reservations.
const MaxLockTTL = 1 * time.Hour

// NormalizeTTL returns a non-negative TTL within the allowed maximum. A
// non-positive ttl is replaced by DefaultLockTTL.
func NormalizeTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return DefaultLockTTL
	}
	if ttl > MaxLockTTL {
		return MaxLockTTL
	}
	return ttl
}
