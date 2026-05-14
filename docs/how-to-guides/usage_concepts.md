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

Access keys should use the `lakefs_ak_` prefix and secret keys should use `lakefs_sk_`.

```yaml
apiVersion: pkg.internal/v1beta1
kind: LakeFSCredential
metadata:
  name: user-credentials
  namespace: lakefs
spec:
  userRef:
    name: user
  accessKeyId: lakefs_ak_user
  secretRef:
    name: user-credentials
    key: secretAccessKey
```

The resulting Secret contains:

- `accessKeyId`
- `secretAccessKey`

## Role

A `LakeFSRole` stores one or more lakeFS policy templates. Templates can use `<REPOSITORY>` when the same role should be reused for multiple repositories.

The project ships three out-of-the-box roles: `admin`, `owner`, and `viewer`. These satisfy most installations:

- `admin` grants owner permissions on all repositories and can also manage auth and repository CI.
- `owner` grants read/write access for one repository.
- `viewer` grants read-only access for one repository.

Customizations are possible by adding another `LakeFSRole` CR dynamically.

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

## Role Binding

A `LakeFSRoleBinding` grants a role to either a user or a group. The binding also carries the repository scope. Use `repository: "*"` for global policies, or a concrete repository name when role templates contain `<REPOSITORY>`.

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
```

S3 clients use lakeFS gateway paths in the form:

```text
s3://<repository>/<branch>/<path>
```

For example:

```bash
aws --endpoint-url http://127.0.0.1:18000 \
  s3 cp ./data.txt s3://repo-a/main/data/data.txt
```
