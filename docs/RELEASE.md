# Releasing Bifrost

Bifrost is currently released as one Go module containing the client and the
versioned protocol package.

## Before tagging

Run from the repository root:

```bash
make check
git diff --check
```

Review the public contract in `protocol/api/openapi.yaml` and update the
protocol version only when a wire-compatible or breaking change requires it.
The module path is:

```text
github.com/CytieqLabs/Bifrost
```

## Tagging a release

Use semantic version tags. The first public preview should be `v0.1.0`.

```bash
git tag -a v0.1.0 -m "Bifrost v0.1.0"
git push origin main --tags
```

The Apache-2.0 `LICENSE` must remain at the repository root. Preserve any
third-party notices required by dependencies when distributing binaries.

## Compatibility

Within protocol `v1`, add optional fields and endpoints compatibly. Removing or
renaming a field, changing its meaning or JSON type, or making an optional
field mandatory requires a new protocol major version and API base path.

## What this release does not promise

The current release is local-first. It does not provide a hosted multi-repo
service, managed worker fleet, billing, Kubernetes orchestration, or an SLA.
