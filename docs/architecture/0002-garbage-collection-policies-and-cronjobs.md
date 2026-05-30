# ADR-002: Garbage Collection Policies and CronJobs

## Status

Accepted

## Date

2026-05-25

## Implementation

Implemented in 0.2.0.

## Context

lakeFS garbage collection removes two categories of physical objects from the
underlying storage:

- committed objects that were deleted or replaced in lakeFS and have expired
  according to configured retention rules;
- uncommitted objects that are no longer accessible, such as objects deleted
  before they were ever committed.

Retention rules apply to committed objects. Without lakeFS GC retention rules,
the GC job can still remove inaccessible uncommitted objects.

Managed repositories should get this lifecycle automatically while still
allowing reusable, domain-level configuration. Operator process settings stay in
the Deployment; GC policy is repository behavior selected by custom resources.

## Decision

Introduce `LakeFSGCPolicy` as a namespaced policy resource, following ADR-001's
namespace-local reference model. `LakeFSRepository.spec.gc.policyRef.name`
defaults to `default`, and the reference resolves only to a `LakeFSGCPolicy` in
the same namespace.

The repository controller uses the selected policy to manage:

- lakeFS retention configuration for the repository;
- a Kubernetes `CronJob` that runs the lakeFS OSS garbage collection workload.

The `CronJob` runs lakeFS GC with Spark. This project publishes a prepared
default image, `ghcr.io/versioneer-tech/lakefs-oss-contrib/gc-spark`, because
lakeFS documents the `spark-submit` invocation but does not prescribe a Docker
image. Runtime image, command, init containers, and pod volumes live in
`LakeFSGCPolicy.spec.job`; Spark class, jar, packages, configuration, and
positional arguments live in `LakeFSGCPolicy.spec.spark`.

The default managed repository behavior is:

```yaml
spec:
  gc:
    enabled: true
    policyRef:
      name: default
```

The operator creates `LakeFSGCPolicy/default` in the repository namespace on
first use when no default exists. Repositories may select another policy for a
different schedule, retention window, runtime image, resource profile, or
job-level settings.

The managed `CronJob` is owned by the `LakeFSRepository`, created in the same
namespace, and labeled with the repository CR and selected GC policy. The job
uses the repository credentials Secret for lakeFS API access and therefore
requires that Secret to be in the repository namespace when managed GC is
enabled.

Managed GC must preserve upstream lakeFS garbage-collection semantics. This
project only manages retention configuration and the Kubernetes workload that
runs lakeFS GC; it does not redefine what GC may delete.

## Consequences

Every managed repository can receive GC by default while still allowing explicit
repository-level customization.

Using a policy resource keeps schedules and retention choices reusable within a
namespace. It also keeps the `LakeFSRepository` API focused: repositories
select a policy instead of embedding the full GC workload shape each time.

Running GC as a Kubernetes `CronJob` makes Jobs, logs, failures, resource
usage, and schedules observable through standard Kubernetes tools.
