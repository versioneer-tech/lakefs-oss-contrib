# Installation

The stack consists of lakeFS, the `lakefs-oss-contrib` operator, and the `lakefs-oss-contrib` auth server.

## Requirements

- Kubernetes cluster
- `kubectl`
- Flux when consuming the packaged OCI base
- Object storage reachable by lakeFS
- Two images:
  - `ghcr.io/versioneer-tech/lakefs-oss-contrib:latest`
  - `ghcr.io/versioneer-tech/lakefs-auth-server:latest`

## Packaged Base

For installation, use the reusable Kustomize base published from `versioneer-tech/bases`. The `lakefs-oss-contrib` base installs lakeFS, the auth server, and the operator together in the `lakefs` namespace.

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: lakefs-oss-contrib
  namespace: flux-system
spec:
  interval: 5m
  url: oci://ghcr.io/versioneer-tech/bases
  ref:
    tag: lakefs-oss-contrib-<sha12>
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: lakefs-oss-contrib
  namespace: flux-system
spec:
  interval: 10m
  prune: true
  wait: true
  timeout: 10m
  sourceRef:
    kind: OCIRepository
    name: lakefs-oss-contrib
  path: ./default
  postBuild:
    substitute:
      LAKEFS_BLOCKSTORE_ACCESS_KEY_ID: <object-storage-access-key>
      LAKEFS_BLOCKSTORE_SECRET_ACCESS_KEY: <object-storage-secret-key>
      LAKEFS_BLOCKSTORE_REGION: <object-storage-region>
      LAKEFS_BLOCKSTORE_ENDPOINT: <object-storage-endpoint>
      LAKEFS_INSTALLATION_USER_NAME: admin-user
      LAKEFS_INSTALLATION_ACCESS_KEY_ID: lakefs_ak_admin
      LAKEFS_INSTALLATION_SECRET_ACCESS_KEY: <lakefs-admin-secret-key>
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
  IMG=ghcr.io/versioneer-tech/lakefs-oss-contrib:latest \
  AUTHSERVER_IMG=ghcr.io/versioneer-tech/lakefs-auth-server:latest
```
