# Usage & Concepts

`lakefs-oss-contrib` treats lakeFS authorization and repository setup as Kubernetes API state.

## User

A `LakeFSUser` represents a lakeFS user that can own credentials.

```yaml
apiVersion: pkg.internal/v1beta1
kind: LakeFSUser
metadata:
  name: user
  namespace: lakefs
spec:
  externalId: user
  friendlyName: Example User
```

## Credential

A `LakeFSCredential` binds an access key to a user. If the referenced Secret is missing or does not contain `secretAccessKey`, the operator creates or completes it with a generated secret key.

```yaml
apiVersion: pkg.internal/v1beta1
kind: LakeFSCredential
metadata:
  name: user-credentials
  namespace: lakefs
spec:
  userRef:
    name: user
  accessKeyId: user-access-key
  secretRef:
    name: user-credentials
    key: secretAccessKey
```

The resulting Secret contains:

- `accessKeyId`
- `secretAccessKey`

## Group

A `LakeFSGroup` stores a list of users. Groups do not own credentials; they are used as role binding subjects.

```yaml
apiVersion: pkg.internal/v1beta1
kind: LakeFSGroup
metadata:
  name: group-c
  namespace: lakefs
spec:
  externalId: group-c
  users:
  - name: user
  - name: readonly-user
```

## Repository

A `LakeFSRepository` creates or verifies a lakeFS repository through the lakeFS API.

```yaml
apiVersion: pkg.internal/v1beta1
kind: LakeFSRepository
metadata:
  name: repo-a
  namespace: lakefs
spec:
  endpoint: http://lakefs.lakefs.svc
  storageNamespace: s3://e2e-bucket/repo-a
  defaultBranch: main
  credentialsSecretRef:
    name: admin-credentials
  gc:
    enabled: true
    policyRef:
      name: default
```

Managed garbage collection is enabled by default. If `spec.gc` is omitted, the
operator uses `policyRef.name: default`. The operator looks for that
`LakeFSGCPolicy` in the same namespace as the `LakeFSRepository`, and creates
the namespace-local `default` policy when a repository first needs it and no
custom default exists yet.

S3 clients use lakeFS gateway paths in the form:

```text
s3://<repository>/<branch>/<path>
```

For example:

```bash
aws --endpoint-url http://127.0.0.1:18000 \
  s3 cp ./data.txt s3://repo-a/main/data/data.txt
```

## Garbage Collection Policy

A `LakeFSGCPolicy` stores reusable lakeFS retention rules and Kubernetes
`CronJob` settings for managed lakeFS OSS garbage collection.

```yaml
apiVersion: pkg.internal/v1beta1
kind: LakeFSGCPolicy
metadata:
  name: default
  namespace: lakefs
spec:
  schedule: "0 3 * * *"
  retention:
    defaultRetentionDays: 14
```

The default image is prepared by this project and already contains a compatible
Spark runtime, lakeFS Spark client assembly, and S3A dependencies. The default
`spec.job.image`, `spec.job.command`, `spec.spark.className`, and
`spec.spark.jarURL` values can be omitted unless a policy needs a custom
runtime.

Policies may add branch-specific retention, provider-specific Spark
configuration, extra Spark packages, job environment variables, init
containers, volumes, and resource settings when the default runtime is not
enough. When `spec.spark.packages` is set, the operator injects
`spark.jars.ivy=/tmp/.ivy2` so Spark can resolve packages in a writable
location inside the GC pod. Custom images must use a lakeFS Spark client
assembly that matches the selected Spark runtime.

The repository controller applies the selected retention rules to lakeFS and
creates a repository-owned `CronJob` in the repository namespace. The CronJob
uses the repository's `credentialsSecretRef` for lakeFS API credentials, so the
Secret must be in the same namespace as the `LakeFSRepository` when managed GC
is enabled.

Disable managed GC for a repository with:

```yaml
spec:
  gc:
    enabled: false
```

## Role

A `LakeFSRole` stores one or more lakeFS policy templates. Templates can use `<REPOSITORY>` when the same role should be reused for multiple repositories.

The project ships three out-of-the-box roles: `admin`, `owner`, and `viewer`. These satisfy most installations:

- `admin` grants owner permissions on all repositories and can also manage auth and repository CI.
- `owner` grants read/write access for one repository.
- `viewer` grants read-only access for one repository.

Customizations are possible by adding another `LakeFSRole` CR dynamically.

```yaml
apiVersion: pkg.internal/v1beta1
kind: LakeFSRole
metadata:
  name: owner
  namespace: lakefs
spec:
  description: Owner
  policies:
  - name: <REPOSITORY>-owner
    statement:
    - effect: allow
      action:
      - fs:*
      resource: arn:lakefs:fs:::repository/<REPOSITORY>
    - effect: allow
      action:
      - fs:*
      resource: arn:lakefs:fs:::repository/<REPOSITORY>/branch/*
    - effect: allow
      action:
      - fs:*
      resource: arn:lakefs:fs:::repository/<REPOSITORY>/object/*
    - effect: allow
      action:
      - fs:ListRepositories
      resource: "*"
```

## Role Binding

A `LakeFSRoleBinding` grants a role to either a user or a group. The binding also carries the repository scope. Use `repository: "*"` for global policies, or a concrete repository name when role templates contain `<REPOSITORY>`.

Create the referenced `LakeFSRepository` before applying repository-scoped role bindings so examples and GitOps applies have a concrete repository to reference.

```yaml
apiVersion: pkg.internal/v1beta1
kind: LakeFSRoleBinding
metadata:
  name: group-c-repo-c-owner
  namespace: lakefs
spec:
  subject:
    kind: LakeFSGroup
    name: group-c
  roleRef:
    name: owner
  repository: repo-c
```

When lakeFS asks for a user's policies, the auth server evaluates role bindings for the user and all groups that contain that user.

## Example Scenario

The e2e setup demonstrates a compact authorization model with three users and three repositories:

- `admin-user` has owner permissions on `repo-a`, `repo-b`, and `repo-c`, and can also manage auth and repository CI.
- `user` is bound to `owner` on `repo-a`, so it can read/write `repo-a`.
- `readonly-user` is bound to `viewer` on `repo-b`, so it can read but not write `repo-b`.
- `group-c` contains `user` and `readonly-user`, and is bound to `owner` on `repo-c`, so both users can read/write `repo-c`.
