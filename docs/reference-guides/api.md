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

## LakeFSRepository

Represents a lakeFS repository that should exist.

Important fields:

- `spec.endpoint`
- `spec.storageNamespace`
- `spec.defaultBranch`
- `spec.credentialsSecretRef.name`
- `spec.credentialsSecretRef.accessKeyIdKey`
- `spec.credentialsSecretRef.secretAccessKeyKey`

Status:

- `status.conditions[type=Ready]`
- `status.repository`

## Conditions

All resources expose a standard `Ready` condition. The common reasons are:

- `Resolved`: the resource is valid and available
- `Invalid`: the resource spec or referenced data is incomplete
- `Unavailable`: a dependency or external API call is not available
