# Operating Bifrost locally

This document covers the parts that matter when Bifrost is used by an agent or
automation rather than entered by hand.

## State and backups

The control document is `.bifrost/state.json`. It contains repository identity,
run metadata, checkpoints, evidence, promotion receipts, idempotency receipts,
and the event cursor. It is replaced atomically after every successful
mutation. Keep a copy of the `.bifrost` directory if the run history matters;
Git commits remain in the object database independently.

Do not edit `state.json` while Bifrost is running. If a backup must be restored,
stop writers first and restore the complete `.bifrost` directory, including the
workspace directory and lock file only after verifying no process owns it.

## Workspaces

Each run gets a detached linked worktree at `.bifrost/workspaces/<run-id>`.
Agents should write only in that directory. A workspace can be inspected after
a run finishes and can be removed with:

```bash
bifrost workspace remove RUN_ID
```

Removing an active workspace requires `--force`. Removing a workspace does not
remove its checkpoints, evidence, or promotion history.

## Safe automation

Use one idempotency key per logical mutation and reuse it when retrying after a
timeout. Do not generate a new key for every retry. A key is scoped to the
local service state and should not be shared between unrelated operations.

For event consumers, persist the last processed sequence and reconnect with
`/v1/events?after=<sequence>`. Event handlers should be idempotent: a reconnect
may deliver an event that was received immediately before a client crash.

## Interrupted promotions

If the process stops during promotion, run the same apply command again. Bifrost
records the candidate commit before changing the branch. On retry it either
finalizes the already-applied commit or reports that the target moved and marks
the promotion stale. Never force-update the branch to make a stale promotion
look ready; create a new check against the current target.

## Exposing the API

Keep `bifrost serve` bound to loopback for local clients. If it must listen on a
network interface, configure a bearer token and put the service behind the
deployment's TLS and network controls. The built-in token check is an API
authentication boundary, not a complete production edge proxy.

## Cleaning up

Run cleanup only for completed, failed, or cancelled runs. Before removing a
workspace, retain any artifact files that are needed for audit; evidence keeps
their repository-relative path and digest, not a copy of the file. Ordinary
Git garbage collection may eventually remove unreferenced checkpoint commits
once they are no longer reachable from Bifrost state or another ref.
