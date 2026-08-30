# Bifrost Protocol

Bifrost Protocol is the stable, implementation-neutral contract between
Bifrost clients, Isha, trusted workers, self-hosted deployments, and the future
Bifrost Server.

It contains only versioned wire types, endpoint paths, error envelopes, event
records, and the canonical OpenAPI document. It does not contain Git access,
workspace management, persistence, authentication policy, or server logic.

Mutation callers may send `Idempotency-Key`; implementations replay the same
response for a repeated key and fingerprint, and reject reuse with a different
request. `GET /v1/events?after=<sequence>` replays ordered events and follows
the live stream using Server-Sent Events.

## Go package

```go
import bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
```

Breaking wire changes require a new versioned package and API base path. Fields
may be added compatibly within `v1`; existing meanings must not change.

## Ownership boundary

- `bifrost-client` implements local execution and may serve the protocol.
- Isha consumes the protocol through HTTP or generated clients.
- The future `bifrost-server` will implement the same protocol for hosted repos.

This repository is licensed under Apache-2.0.
