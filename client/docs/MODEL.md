# Bifrost domain model

## Repository

An imported or Bifrost-created Git repository. Bifrost stores the canonical
remote identity and provider metadata but never prevents normal Git access.

## Run

A bounded attempt by an agent to satisfy a task. A run has a base revision,
isolated linked worktree, parent/child lineage, status, and resulting revision.

## Checkpoint

A resumable snapshot of a run. It references a Git tree/worktree plus agent
state and tool progress. Checkpoints are cheap and disposable; approved
promotions remain ordinary Git commits.

## Evidence

Structured test, scan, build, and artifact records associated with a run or
promotion. Evidence is immutable after recording and can be verified by policy.

## Promotion

A request to apply a run's verified patch to a target branch. Promotion checks
the base revision, policy, conflicts, and required evidence before approval.
Each check pins the exact target and result commits and computes a virtual merge
tree, so readiness cannot be confused with an unverified branch mutation.
Apply re-resolves the local target branch and updates it only if it still equals
the pinned target commit. Checked-out branches are rejected by ref-only apply;
remote pushes remain outside the Bifrost local service contract.

Before moving the ref, Bifrost durably records the exact candidate commit and
marks the promotion `applying`. If execution is interrupted, retrying apply
either completes the compare-and-swap or recognizes the already-moved branch
and finalizes the receipt. A third-party branch movement marks the promotion
`stale` instead of overwriting it.

## Local persistence contract (v2)

`.bifrost/state.json` is an atomically replaced JSON document with an explicit
schema version. Mutations acquire `.bifrost/write.lock`; stale locks expire so
an interrupted agent cannot permanently block the repository.

Version 2 replaces the legacy workspace path string with the portable protocol
workspace object. Version 1 state is migrated in memory and written as version
2 on its next mutation.

Every run records an immutable base commit. Child lineage is explicit through
`parent_id`, not inferred from branch names or commit authors. A checkpoint is
an unreferenced Git commit whose parent is the previous checkpoint (or the run's
base commit). This makes a run resumable without creating user-visible refs.

Evidence is append-only in the CLI contract. When an artifact path is supplied,
Bifrost records its repository-relative path and SHA-256 digest so later policy
checks can detect replacement or tampering. Artifact paths are resolved through
symlinks and must remain within the run workspace.

## Workspace contract

The primary worktree owns `.bifrost/state.json`. Each run uses a detached Git
worktree at `.bifrost/workspaces/<run-id>` and reads the primary state even when
a command is invoked from inside that linked checkout. Creating a checkpoint
uses an isolated index and records it only if no concurrent checkpoint advanced
the run. Completed workspaces remain available for inspection until explicitly
removed.

Each successful mutation emits an ordered repository event. Event persistence
is append-only and best-effort relative to the primary mutation; consumers use
the sequence cursor and event ID to resume and deduplicate. API mutation
requests can be retried safely with one stable `Idempotency-Key`.
