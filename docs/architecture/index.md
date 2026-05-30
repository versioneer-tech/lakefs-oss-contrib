# Architecture

`lakefs-oss-contrib` makes lakeFS OSS manageable through Kubernetes-native
resources. lakeFS remains the data versioning system; this project adds an
operator and an external auth server that reconcile Kubernetes API state into
lakeFS repositories, identities, credentials, roles, and authorization answers.

## Control Plane Shape

```text
Kubernetes API
  |
  +-- LakeFSUser
  +-- LakeFSGroup
  +-- LakeFSCredential
  +-- LakeFSRepository
  +-- LakeFSGCPolicy
  +-- LakeFSRole
  +-- LakeFSRoleBinding
  |
  +--> operator -----------> lakeFS API
  |       |
  |       +-- repository creation
  |       +-- credential Secret materialization
  |
  +--> auth server <-------- lakeFS external auth callbacks
          |
          +-- dynamic user, group, role, and policy resolution
```

The main contract is declarative: Kubernetes resources and Secrets are the
source of truth, while lakeFS consumes the resulting state through its normal
API and external authorization interface.

## Current Resources

lakeFS control-plane state is modeled with namespaced custom resources:

- `LakeFSUser` describes a lakeFS user.
- `LakeFSGroup` describes a group and its user membership.
- `LakeFSCredential` describes a user's lakeFS access key and Secret contract.
- `LakeFSRepository` describes a lakeFS repository that should exist.
- `LakeFSGCPolicy` describes namespace-local lakeFS GC retention and CronJob policy.
- `LakeFSRole` describes reusable lakeFS policy templates.
- `LakeFSRoleBinding` grants a role to a user or group for a repository scope.

`LakeFSRole` templates may use `<REPOSITORY>` in policy names and resources.
`LakeFSRoleBinding.spec.repository` supplies the concrete value, so roles such
as `owner` and `viewer` can be reused across repositories without generating
static per-repository manifests.

## Managed Extensions

Garbage collection follows the same control-plane model. It is declared through
Kubernetes resources and reconciled into lakeFS retention rules and Kubernetes
workloads. Index generation is planned to follow the same pattern through
managed lakeFS action files.

```text
LakeFSGCPolicy
  |
  +-- namespace-local default policy
  +-- namespace-local repository override by LakeFSRepository
  |
  +--> retention setup in lakeFS
  +--> Kubernetes CronJob for lakeFS GC

LakeFSRepository
  |
  +-- managed lakeFS actions
  |
  +--> prepare-commit webhook to Kubernetes indexer
  +--> pre-commit Lua validator for _versioneer/index-*.parquet
```

## Architecture Decision Records

Architecture Decision Records capture the durable reasoning behind
`lakefs-oss-contrib` design choices. Each record includes an implementation
status so readers can distinguish implemented behavior from planned work.

### Status Legend

| Status | Meaning |
| --- | --- |
| `Draft` | The idea is being sketched and is not yet a decision. |
| `Accepted` | This is the current architectural direction. |
| `Proposed` | The decision is still being evaluated. |
| `Superseded` | A later ADR replaces this decision. |
| `Deprecated` | The decision remains historical context only. |

### Records

| ADR | Decision | Status | Implementation |
| --- | --- | --- | --- |
| [ADR-001](0001-kubernetes-native-lakefs-control-plane.md) | Use Kubernetes custom resources for lakeFS users, groups, credentials, repositories, roles, and role bindings. | Accepted | Implemented in v0.1.0 |
| [ADR-002](0002-garbage-collection-policies-and-cronjobs.md) | Manage lakeFS GC with reusable policies, repository defaults, retention setup, and Kubernetes CronJobs. | Accepted | Implemented in v0.2.0 |
| [ADR-003](0003-action-driven-versioneer-index-generation.md) | Generate Versioneer indexes through lakeFS actions backed by a Kubernetes indexer and Lua validation. | Draft | Not implemented |
