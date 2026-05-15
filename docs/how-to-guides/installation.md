# Installation

The stack consists of lakeFS, the `lakefs-oss-contrib` operator, and the `lakefs-oss-contrib` auth server.

## Requirements

- Kubernetes cluster
- `kubectl`
- `kustomize` or `kubectl kustomize`
- `oras`, `jq`, and `tar` to extract the packaged OCI base
- `envsubst` to substitute installation values after rendering
- Object storage reachable by lakeFS
- Two images:
  - `ghcr.io/versioneer-tech/lakefs-oss-contrib/operator:latest`
  - `ghcr.io/versioneer-tech/lakefs-oss-contrib/auth-server:latest`

## Packaged Base

For installation, use the reusable Kustomize base published from `versioneer-tech/bases`. The `lakefs-oss-contrib` base installs lakeFS, the auth server, and the operator together in the `lakefs` namespace.

Kustomize consumes a local directory or a Git URL, so first extract the OCI artifact to a filesystem path and then run Kustomize against the unpacked base.

```bash
export LAKEFS_OSS_CONTRIB_BASE_REPOSITORY=ghcr.io/versioneer-tech/bases
export LAKEFS_OSS_CONTRIB_BASE_TAG=lakefs-oss-contrib-<version>
export LAKEFS_OSS_CONTRIB_BASE_REF="${LAKEFS_OSS_CONTRIB_BASE_REPOSITORY}:${LAKEFS_OSS_CONTRIB_BASE_TAG}"

mkdir -p vendor/lakefs-oss-contrib
oras manifest fetch "${LAKEFS_OSS_CONTRIB_BASE_REF}" \
  | jq -r '.layers[0].digest' \
  | xargs -I{} oras blob fetch "${LAKEFS_OSS_CONTRIB_BASE_REPOSITORY}@{}" \
      --output /tmp/lakefs-oss-contrib-base.tar.gz
tar -xzf /tmp/lakefs-oss-contrib-base.tar.gz -C vendor/lakefs-oss-contrib
```

Set the installation values expected by the base, then render and apply it:

```bash
export LAKEFS_BLOCKSTORE_ACCESS_KEY_ID=<object-storage-access-key>
export LAKEFS_BLOCKSTORE_SECRET_ACCESS_KEY=<object-storage-secret-key>
export LAKEFS_BLOCKSTORE_REGION=<object-storage-region>
export LAKEFS_BLOCKSTORE_ENDPOINT=<object-storage-endpoint>
export LAKEFS_INSTALLATION_USER_NAME=admin-user
export LAKEFS_INSTALLATION_ACCESS_KEY_ID=lakefs_ak_admin
export LAKEFS_INSTALLATION_SECRET_ACCESS_KEY=<lakefs-admin-secret-key>

kustomize build vendor/lakefs-oss-contrib/default \
  | envsubst \
  | kubectl apply -f -
```

The packaged base creates:

- CRDs for `LakeFSUser`, `LakeFSGroup`, `LakeFSCredential`, `LakeFSRole`, `LakeFSRoleBinding`, and `LakeFSRepository`
- lakeFS with external auth configured and the initial setup screen skipped
- operator Deployment
- auth-server Deployment and Service
- RBAC for reconciliation and auth-server reads/writes
- `admin-user` `LakeFSUser`, `admin-credentials` `LakeFSCredential`, and `admin-user-all` `LakeFSRoleBinding`
- out-of-the-box `LakeFSRole` objects: `admin`, `owner`, and `viewer`

## Admin Bootstrap

The base sets lakeFS `installation.user_name`, `installation.access_key_id`, and `installation.secret_access_key`. The same values are represented as Kubernetes resources:

```yaml
apiVersion: pkg.internal/v1beta1
kind: LakeFSUser
metadata:
  name: admin-user
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSCredential
metadata:
  name: admin-credentials
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRoleBinding
metadata:
  name: admin-user-all
spec:
  subject:
    kind: LakeFSUser
    name: admin-user
  roleRef:
    name: admin
  repository: "*"
```

## Development Install

For local development of the operator and auth server only, generate and apply the manifests from this repository:

```bash
make deploy \
  IMG=ghcr.io/versioneer-tech/lakefs-oss-contrib/operator:latest \
  AUTHSERVER_IMG=ghcr.io/versioneer-tech/lakefs-oss-contrib/auth-server:latest
```
