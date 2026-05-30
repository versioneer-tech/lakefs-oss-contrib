# ADR-003: Action-Driven Versioneer Index Generation

## Status

Draft

## Date

2026-05-28

## Implementation

Planned for 0.3.0.

## Context

lakeFS commits should automatically carry machine-readable index files under
`_versioneer/`. The index should be generated from the data being committed and
validated before lakeFS accepts the commit.

lakeFS actions provide the needed hooks: `prepare-commit` can mutate the branch
before finalization, and `pre-commit` can validate the result. Index generation
is expected to need normal libraries, resources, and observability, so it fits a
Kubernetes service better than embedded Lua.

## Decision

Use managed lakeFS actions to automate index generation and validation:

- a `prepare-commit` webhook calls an indexer service running inside the
  Kubernetes cluster;
- the indexer inspects the target lakeFS repository and branch, generates the
  index file, and uploads it back to the same branch before the outer commit is
  finalized;
- a `pre-commit` Lua hook validates that the expected
  `_versioneer/index-*.parquet` file exists and matches the commit
  requirements.

The webhook service must be idempotent. It should not create a separate commit.
The generated index object should become part of the user's original commit.

The Lua hook should stay small and deterministic. Its job is to fail the commit
when the generated index is missing or stale, not to build the index itself.

## Consequences

Index generation stays transparent to users: they can commit data and receive a
matching `_versioneer/` index in the same commit.

The heavy work lives in a Kubernetes service where it can use normal libraries,
metrics, logs, retries, and resource limits. Lua remains reserved for lightweight
commit validation.

The design depends on `prepare-commit` behavior. Implementations must account
for its concurrency caveats and use `pre-commit` validation as the final guard.
