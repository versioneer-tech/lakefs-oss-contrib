# API Reference

All custom resources use:

```text
apiVersion: pkg.internal/v1beta1
```

## LakeFSUser

Represents a lakeFS user.

Important fields:

- `spec.externalId`: external identifier
- `spec.friendlyName`: optional display name

Status:

- `status.conditions[type=Ready]`

## LakeFSCredential

Represents an access key and secret key for a user.

Important fields:

- `spec.userRef.name`
- `spec.accessKeyId`
- `spec.secretRef.name`
- `spec.secretRef.key`
- `spec.revoked`
- `spec.expiresAt`

The operator creates or updates the referenced Secret with `accessKeyId` and the configured secret key field.

Status:

- `status.conditions[type=Ready]`
- `status.secretName`
- `status.secretKey`

## LakeFSRole

Represents one or more lakeFS policy templates.

Important fields:

- `spec.description`
- `spec.policies[].name`
- `spec.policies[].statement[].effect`
- `spec.policies[].statement[].action`
- `spec.policies[].statement[].resource`

Policy templates may include `<REPOSITORY>` in policy names or resources.

Status:

- `status.conditions[type=Ready]`

## LakeFSGroup

Represents a group of lakeFS users.

Important fields:

- `spec.externalId`: external identifier
- `spec.description`
- `spec.users[].name`

Status:

- `status.conditions[type=Ready]`

## LakeFSRoleBinding

Maps a `LakeFSRole` to either a `LakeFSUser` or a `LakeFSGroup`.

Important fields:

- `spec.subject.kind`: `LakeFSUser` or `LakeFSGroup`
- `spec.subject.name`
- `spec.roleRef.name`
- `spec.repository`: repository scope, or `*`

Status:

- `status.conditions[type=Ready]`

## LakeFSGCPolicy

Represents reusable lakeFS garbage-collection retention rules and Kubernetes
CronJob settings. `LakeFSGCPolicy` is namespaced; repositories can select only
policies from their own namespace.

Important fields:

- `spec.schedule`: Cron schedule for the managed GC job
- `spec.retention.defaultRetentionDays`
- `spec.retention.branches[].branchId`
- `spec.retention.branches[].retentionDays`

Optional runtime override fields:

- `spec.spark.className`
- `spec.spark.jarURL`
- `spec.spark.packages[]`
- `spec.spark.conf[].name`
- `spec.spark.conf[].value`
- `spec.spark.args[]`
- `spec.job.image`
- `spec.job.command[]`
- `spec.job.serviceAccountName`
- `spec.job.initContainers[]`
- `spec.job.env[]`
- `spec.job.envFrom[]`
- `spec.job.resources`
- `spec.job.volumeMounts[]`
- `spec.job.volumes[]`

Status:

- `status.conditions[type=Ready]`

## LakeFSRepository

Represents a lakeFS repository that should exist.

Important fields:

- `spec.endpoint`
- `spec.storageNamespace`
- `spec.defaultBranch`
- `spec.credentialsSecretRef.name`
- `spec.credentialsSecretRef.accessKeyIdKey`
- `spec.credentialsSecretRef.secretAccessKeyKey`
- `spec.gc.enabled`
- `spec.gc.policyRef.name`: defaults to `default` in the repository namespace

Managed GC uses the repository credentials Secret as environment variables in
the generated CronJob. The Secret must be in the same namespace as the
`LakeFSRepository` when `spec.gc.enabled` is true.

The controller supplies the default GC runtime. Omit `spec.job.image`,
`spec.job.command`, `spec.spark.className`, and `spec.spark.jarURL` unless a
policy needs to override that prepared runtime.

Status:

- `status.conditions[type=Ready]`
- `status.repository`
- `status.gcPolicy`
- `status.gcCronJob`

## Conditions

All resources expose a standard `Ready` condition. The common reasons are:

- `Resolved`: the resource is valid and available
- `Invalid`: the resource spec or referenced data is incomplete
- `Unavailable`: a dependency or external API call is not available
