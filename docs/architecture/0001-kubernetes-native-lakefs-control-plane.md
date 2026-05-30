# ADR-001: Kubernetes-Native lakeFS Control Plane

## Status

Accepted

## Date

2026-05-14

## Implementation

Implemented in 0.1.0.

## Context

lakeFS OSS already provides repository management, object versioning, S3 gateway
access, and a pluggable authorization API. Platform teams still need a
Kubernetes-native way to declare lakeFS users, groups, credentials,
repositories, roles, and role bindings without treating lakeFS authorization as
a separate manual setup step.

The project should work with GitOps, Kubernetes RBAC, Secrets, status
conditions, and higher-level platform APIs such as Crossplane Compositions. It
should not fork lakeFS or replace its API surface.

## Decision

Use Kubernetes custom resources as the source of truth for lakeFS operational
state:

- `LakeFSUser` stores the desired lakeFS user identity.
- `LakeFSGroup` stores group membership.
- `LakeFSCredential` stores the desired access key contract and points at a
  Kubernetes Secret for the secret key.
- `LakeFSRepository` stores the desired repository, backing storage namespace,
  default branch, lakeFS endpoint, and administrative credential reference.
- `LakeFSRole` stores one or more reusable lakeFS policy templates.
- `LakeFSRoleBinding` assigns a role to a user or group for a repository scope,
  or for `*`.

The operator reconciles resource status, creates or completes credential
Secrets, and creates or verifies lakeFS repositories through the lakeFS API.

Custom resources are namespaced, and references between them resolve within the
same namespace unless an API explicitly adds a namespace field. This keeps the
Kubernetes namespace as the tenancy and ownership boundary for lakeFS state.

The auth server implements lakeFS external authorization by resolving the
current Kubernetes custom resources dynamically. When lakeFS asks for users,
groups, credentials, policies, or permissions, the auth server derives the
answer from `LakeFSUser`, `LakeFSGroup`, `LakeFSCredential`, `LakeFSRole`, and
`LakeFSRoleBinding` resources.

Role policies are templated at the Kubernetes layer. `LakeFSRole` policy names
and resources may contain `<REPOSITORY>`. `LakeFSRoleBinding.spec.repository`
provides the value used to render the effective lakeFS policy. For example, a
single `owner` role can render `repo-a-owner`, `repo-b-owner`, and their
repository-scoped ARN resources depending on the binding. Bindings with
`repository: "*"` represent global policies.

## Consequences

lakeFS authorization and repository setup become declarative Kubernetes state.
Changes are dynamic: applying or updating a CR changes the desired state without
rebuilding static lakeFS configuration.

The design keeps lakeFS upstream behavior intact. lakeFS remains responsible
for versioning and object access, while this project provides Kubernetes
reconciliation and an external authorization integration around it.

The `<REPOSITORY>` template keeps common roles small and reusable. It avoids
duplicating per-repository policy manifests while still producing lakeFS-native
policy names and resources at authorization time.
