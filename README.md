# doclock — Document Versioning & Edit-Lock Service

A self-contained, **offline-buildable** Go service for versioned documents with
optimistic concurrency, atomic batch updates, temporary edit locks, an audit
stream, and idempotent requests. The standard library is the only dependency;
there are no third-party packages, no code generation, and no network access
required to build, test, or run.

State is held entirely in memory and is lost when the process exits.

## Build & test

```bash
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

All four must pass. None of them touch the network.

## Run

```bash
go run ./cmd/doclock -addr :8080 -reap-interval 5s
```

The server shuts down gracefully on `SIGINT`/`SIGTERM`. A background reaper
removes expired locks every `reap-interval` (set to `0` to disable and rely on
lazy reaping, which also happens on acquire/renew/get).

## Design

```
cmd/doclock/         command-line server entry point
internal/domain/     pure entities, errors, clock, value objects
internal/service/    concurrency-safe in-memory domain layer (the core)
internal/httpapi/    JSON HTTP transport
```

### Concurrency model

The service guards its entire state (collections, documents, revisions, locks,
audit log, idempotency cache) with a single `sync.RWMutex`.

* Write operations hold the **write lock** for their whole duration —
  idempotency check, validation, mutation, audit recording, and idempotency
  recording all happen atomically with respect to other operations. This makes
  compound operations (atomic batch, idempotent retries) indivisible.
* Read operations hold the **read lock** and return deep copies.
* Because Go mutexes are non-reentrant, no public method calls another public
  method while holding the lock; each delegates to an `*Unlocked` helper.

### Mutable-data isolation

Every value returned by the service is a **deep copy** (document tags, revision
slices, audit-event detail maps, etc.). Callers can never mutate the service's
internal state through a returned reference. Conversely, the service copies
slices out of request objects before storing them, so later caller mutation does
not affect stored state.

## HTTP API

All request and response bodies are `application/json`. Timestamps are RFC3339.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/collections` | Register a collection |
| `GET` | `/collections` | List collections |
| `GET` | `/collections/{collectionID}` | Get a collection |
| `POST` | `/collections/{collectionID}/documents` | Create a document (v1) |
| `GET` | `/collections/{collectionID}/documents` | List documents (`?tags=a,b&limit=10`) |
| `POST` | `/collections/{collectionID}/documents/batch` | Atomic batch update |
| `GET` | `/collections/{collectionID}/documents/{documentID}` | Get a document |
| `PUT` | `/collections/{collectionID}/documents/{documentID}` | Update a document (expected version) |
| `GET` | `/collections/{collectionID}/documents/{documentID}/history` | Version history |
| `POST` | `/collections/{collectionID}/documents/{documentID}/lock` | Acquire lock |
| `PUT` | `/collections/{collectionID}/documents/{documentID}/lock` | Renew lock |
| `DELETE` | `/collections/{collectionID}/documents/{documentID}/lock` | Release lock |
| `POST` | `/locks/reap` | Reap expired locks |
| `GET` | `/audit` | Query audit stream |
| `GET` | `/healthz` | Liveness |

### Idempotency

Write operations accept an optional `Idempotency-Key` header (lock acquire/renew
also accept `idempotency_key` in the body). A repeated request with the same key
and an **identical** body replays the original result (success **or** error,
including version conflicts). Reusing a key with a **different** body returns
`409 idempotency_conflict`. Malformed requests (failing validation before the
locked section) do not consume the key, so a client may retry with a corrected
payload.

### Optimistic concurrency & version conflicts

`PUT /documents/{id}` requires `expected_version`. If the document's current
version differs, the response is `409 version_conflict` with `expected`/`actual`
and **no mutation** occurs. This is the primary mechanism for safe concurrent
updates.

### Batch atomic updates

`POST /documents/batch` validates **every** item first (existence, expected
version, and intra-batch duplicate detection). If any item conflicts, **nothing
is applied** and the response is `409 batch_conflict` listing every failing item
(partial-failure reporting without partial application). If all items validate,
they are all applied under the same lock — true atomicity.

### Edit locks

A document may hold one lock at a time. `Acquire` grants a new lock or, if the
same holder already holds it, renews it; a different holder gets `409
lock_conflict`. `Renew` extends expiry (holder-only). `Release` removes it
(holder-only; releasing an absent lock is `404`). Expired locks are reaped lazily
(on acquire/renew/get) and by the background reaper; `POST /locks/reap` reaps on
demand.

**Time boundary:** a lock is valid while `now < ExpiresAt` and expired once
`now >= ExpiresAt`. Expiry is evaluated against an injected `Clock`, so it is
deterministic in tests.

> Locks are an independent advisory coordination mechanism. Updates use
> optimistic concurrency and do **not** require holding a lock; the two can be
> combined by clients that want both pessimistic session control and optimistic
> conflict detection.

### Audit stream

Every mutating operation appends an `AuditEvent` with a monotonic sequence
number, timestamp, type, actor, and scoped IDs. `GET /audit` supports filtering
by `collection_id`, `document_id`, `actor`, `type`, `min_sequence` (for tailing),
and `limit` (most-recent N).

## Error model

| Code | HTTP | Meaning |
|---|---|---|
| `invalid_input` | 400 | Validation failure |
| `not_found` | 404 | Collection/document/lock not found |
| `version_conflict` | 409 | `expected_version` mismatch |
| `lock_conflict` | 409 | Lock held by another holder |
| `lock_expired` | 410 | Lock exists but has expired |
| `idempotency_conflict` | 409 | Key reused with a different payload |
| `batch_conflict` | 409 | Batch contained conflicting items |
| `unavailable` | 503 | Request canceled/timed out |
| `internal_error` | 500 | Unexpected failure |

## Example

```bash
# register a collection
curl -sX POST :8080/collections \
  -H 'Content-Type: application/json' \
  -d '{"id":"docs","name":"Docs"}'

# create a document (v1)
curl -sX POST :8080/collections/docs/documents \
  -H 'Content-Type: application/json' \
  -d '{"id":"d1","title":"Hello","content":"world","tags":["a","b"]}'

# update with expected version 1 -> v2
curl -sX PUT :8080/collections/docs/documents/d1 \
  -H 'Content-Type: application/json' \
  -d '{"expected_version":1,"title":"Hello v2"}'

# acquire a 60s lock
curl -sX POST :8080/collections/docs/documents/d1/lock \
  -H 'Content-Type: application/json' \
  -d '{"holder":"alice","ttl_seconds":60}'

# query audit
curl -s :8080/audit?document_id=d1
```

## Limitations

* In-memory only; state is not persisted.
* Idempotency records live for the process lifetime (no eviction).
* The audit log is unbounded.
* Collection IDs must not contain `/` (used as a composite-key separator).
