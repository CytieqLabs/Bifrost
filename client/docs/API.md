# Local API guide

The local service implements the versioned Bifrost Protocol under `/v1`.
Authentication, request and response schemas, and event names are defined by
the repository's `protocol/` module. The OpenAPI document is the source of
truth; this page gives the operational shape.

## Start the service

```bash
bifrost serve --addr 127.0.0.1:8741
```

With authentication:

```bash
BIFROST_API_TOKEN="$BIFROST_TOKEN" bifrost serve --addr 127.0.0.1:8741
```

Send `Authorization: Bearer <token>` on repository requests when a token is
configured. `GET /healthz` does not require authentication.

## Request rules

- `Content-Type: application/json` is required for JSON mutations.
- Unknown JSON fields are rejected.
- Request bodies are limited to 1 MiB.
- Mutation responses can be replayed with `Idempotency-Key`.
- Errors use the protocol error envelope and stable error codes.

## Typical sequence

```text
POST /v1/runs
POST /v1/runs/{runId}/checkpoints
POST /v1/runs/{runId}/evidence
POST /v1/runs/{runId}/finish
POST /v1/runs/{runId}/promotions/check
POST /v1/promotions/{promotionId}/apply
```

The exact request bodies and response schemas are in `api/openapi.yaml`.

## Event consumption

`GET /v1/events?after=N` returns Server-Sent Events. Each event includes an
`id`, an event type, and JSON data containing the complete protocol event. The
sequence is monotonic within one repository state file. Store the last applied
sequence and pass it as `after` after reconnecting.

The stream is intentionally repository-local in this implementation. Hosted
fan-out, retention policy, and cross-process event delivery belong to the future
Bifrost Server.
