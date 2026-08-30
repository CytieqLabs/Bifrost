# Bifrost Client

Bifrost Client gives coding agents a safe, inspectable place to change a Git
repository. Each task runs in its own linked worktree. The client records what
revision the agent started from, what it produced, which checks passed, and
whether the result may be applied to a branch.

It is a local tool today. It is also the reference client for the Bifrost
Protocol, so the same CLI and SDK can use a compatible hosted service later.

## What it does

- creates isolated workspaces for agent runs;
- keeps parent/child run lineage;
- records checkpoints as real Git commits without moving the user's branch;
- records test, build, scan, review, and artifact evidence;
- checks conflicts and policy requirements before promotion;
- applies a promotion with compare-and-swap protection;
- recovers an interrupted promotion without guessing what happened;
- exposes the local service over HTTP, including idempotent mutations and a
  resumable Server-Sent Events stream.

Bifrost does not replace Git. Normal `clone`, `fetch`, `pull`, `push`, `log`,
and `diff` commands continue to work. Bifrost adds the execution record around
those Git objects.

## Requirements

- Go 1.26 or newer
- Git 2.30 or newer
- an existing repository with at least one commit

Build from this repository:

```bash
go build -o bifrost ./cmd/bifrost
```

During development, the workspace-level `go.work` supplies the sibling
`bifrost-protocol` module. Release builds should use a tagged protocol version.

## Five-minute local run

Run the commands from the primary checkout, not from a Bifrost worktree:

```bash
bifrost init
bifrost run start --task "fix the session race" --agent isha
# copy the run ID and workspace path from the response
bifrost workspace path RUN_ID

# Work in the printed workspace, then return to the primary checkout:
bifrost checkpoint create --run RUN_ID --note "implementation complete"
bifrost evidence add --run RUN_ID --kind test \
  --command "go test ./..." --exit-code 0 --summary "all tests passed"
bifrost run finish RUN_ID
bifrost promotion check --run RUN_ID --target main
bifrost promotion apply PROMOTION_ID
```

`bifrost init` creates `.bifrost/state.json` and registers the repository. The
state file is kept out of Git through the repository's local exclude file; it
does not modify the project's `.gitignore`.

## Command reference

```text
init                              register the current Git repository
status                            show repository and run counts
run start|list|show|finish        manage agent runs
checkpoint create                 snapshot a run workspace
evidence add                      record a check or artifact
workspace list|path|remove        inspect or clean run workspaces
promotion check|apply             validate or apply a result
git <git-arguments...>            run Git in the primary checkout
commit|add|diff|pull|push|log ...  common Git shortcuts
serve                             expose the local HTTP API
profile add|use|list|show|remove   configure a remote protocol endpoint
```

Run `bifrost COMMAND --help` for flags and request fields.

The `git` command and common shortcuts are thin wrappers around the installed
Git binary. They operate in the primary checkout; use ordinary Git for less
common commands.

## How promotion works

Promotion is deliberately a separate step from completing a run. A check pins
the target branch commit and the run result commit. It is `ready` only when the
run completed, has a result checkpoint, includes passing test evidence, shares
the expected base, and has no merge conflicts.

Apply re-checks the target branch and uses an atomic compare-and-swap update.
If another process changed the branch, the promotion becomes `stale`; Bifrost
never overwrites that change. Ref-only apply also refuses a branch checked out
in any worktree, because moving that ref would leave the checkout inconsistent.

## Local HTTP service

Start the service for Isha or another local client:

```bash
bifrost serve
BIFROST_API_TOKEN="change-me" bifrost serve --addr 0.0.0.0:8741
```

The health endpoint is public. Repository endpoints are under `/v1`. A
non-loopback listener requires a bearer token. Request bodies are strict JSON
and capped at 1 MiB.

The canonical wire contract lives in the sibling `protocol/` directory:

- [OpenAPI document](../protocol/api/openapi.yaml)
- [event semantics](../protocol/docs/EVENTS.md)
- [versioning policy](../protocol/docs/VERSIONING.md)

Mutation callers may attach one stable `Idempotency-Key`. Retrying the same
logical request returns the original response; reusing the key with a different
request is rejected. `GET /v1/events?after=N` replays events after sequence N
and then follows the live stream.

## Profiles and remote services

The CLI defaults to the embedded local backend. A profile selects a compatible
remote service without changing command syntax:

```bash
bifrost profile add staging \
  --mode remote \
  --endpoint https://bifrost.example.com \
  --token-env BIFROST_STAGING_TOKEN
bifrost --profile staging run list
bifrost profile use staging
```

Profiles contain endpoints and token sources, never token values. Configuration
is written atomically with mode `0600`; set `BIFROST_CONFIG` to choose a path.
Remote workspaces and filesystem cleanup remain server-owned operations.

## Go SDK

```go
api, err := bifrost.New("http://127.0.0.1:8741", token, nil)
ctx := bifrost.WithIdempotencyKey(context.Background(), "run-create-2026-08-30")
run, err := api.StartRun(ctx, bifrostv1.StartRunRequest{
	Task: "fix the session race",
	Agent: "isha",
})
```

The SDK also provides typed methods for checkpoints, evidence, finishing runs,
promotions, and `StreamEvents` for reconnecting from a sequence cursor.

## Persistence and recovery

Metadata lives in `.bifrost/state.json`. Writes use a short-lived lock and an
atomic replacement. State version 1 is migrated in memory to version 2, which
uses the portable protocol workspace object. Checkpoints are unreferenced Git
commits created with an isolated index, so the user's index, branch, and
worktree are not changed.

See [docs/MODEL.md](docs/MODEL.md) for the data and safety contracts and
[docs/OPERATIONS.md](docs/OPERATIONS.md) for backup, cleanup, and recovery
guidance.

## Project boundaries

Bifrost owns repository truth: revisions, workspaces, lineage, evidence, and
promotion receipts. Isha owns models, orchestration, memory, permissions, and
user interaction. The future Bifrost Server will add multi-repository hosting,
remote workers, and managed execution behind the same protocol; those features
are not part of this local client.

## Development

```bash
go test -race ./...
go vet ./...
go build ./cmd/bifrost
```

Changes are licensed under [Apache-2.0](LICENSE).
