# Installation

The installation deploys a complete lakeFS OSS stack on Kubernetes:

- the upstream lakeFS server
- the upstream lakeFS garbage-collection spark-submit job
- the `lakefs-oss-contrib` operator
- the `lakefs-oss-contrib` auth server
- CRDs for `LakeFSUser`, `LakeFSGroup`, `LakeFSCredential`, `LakeFSRepository`, `LakeFSRole`, and `LakeFSRoleBinding`
- RBAC for reconciliation and auth-server reads/writes
- `admin-user` `LakeFSUser`, `admin-credentials` `LakeFSCredential`, and `admin-user-all` `LakeFSRoleBinding`
- out-of-the-box `LakeFSRole` objects: `admin`, `owner`, and `viewer`

For installation, use the reusable [OCI bundle](https://github.com/versioneer-tech/bases/pkgs/container/bases) published from `versioneer-tech/bases`. The `lakefs-oss-contrib` base installs these components in a Kubernetes cluster and configures lakeFS to use the auth server for external authorization.

## Deployment Approaches

Two rollout approaches are supported.

### GitOps Rollout

Use this approach when cluster state is reconciled from Git, for example with FluxCD.

Requirements:

- Kubernetes cluster connected to a Git repository through FluxCD

Commit Flux manifests like the following to the repository path reconciled by Flux:

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: lakefs-oss-contrib-base
  namespace: flux-system
spec:
  interval: 10m
  url: oci://ghcr.io/versioneer-tech/bases
  ref:
    tag: lakefs-oss-contrib-<version>
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: lakefs-oss-contrib-install-values
  namespace: flux-system
data:
  LAKEFS_BLOCKSTORE_REGION: <object-storage-region>
  LAKEFS_BLOCKSTORE_ENDPOINT: <object-storage-endpoint>
  LAKEFS_INSTALLATION_USER_NAME: admin-user
---
apiVersion: v1
kind: Secret
metadata:
  name: lakefs-oss-contrib-install-secrets
  namespace: flux-system
type: Opaque
stringData:
  LAKEFS_BLOCKSTORE_ACCESS_KEY_ID: <object-storage-access-key>
  LAKEFS_BLOCKSTORE_SECRET_ACCESS_KEY: <object-storage-secret-key>
  LAKEFS_INSTALLATION_ACCESS_KEY_ID: <lakefs-admin-access-key-id>
  LAKEFS_INSTALLATION_SECRET_ACCESS_KEY: <lakefs-admin-secret-key>
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: lakefs-oss-contrib
  namespace: flux-system
spec:
  interval: 10m
  path: ./default
  prune: true
  wait: true
  timeout: 5m
  sourceRef:
    kind: OCIRepository
    name: lakefs-oss-contrib-base
  postBuild:
    substituteFrom:
    - kind: ConfigMap
      name: lakefs-oss-contrib-install-values
    - kind: Secret
      name: lakefs-oss-contrib-install-secrets
```

For production GitOps repositories, manage `lakefs-oss-contrib-install-secrets` with the cluster's secret management approach, for example SOPS, External Secrets Operator, or Sealed Secrets. The Flux `Kustomization` only needs the resulting Secret to exist in the same namespace as the `Kustomization`.

### Scripted Rollout

Use this approach when rendering and applying the manifests from a shell.

Requirements:

- Kubernetes cluster with an active kubeconfig context
- `kubectl`
- `kustomize` or `kubectl kustomize`
- `oras`, `jq`, and `tar` to extract the packaged OCI base
- `envsubst` to substitute installation values after rendering

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
export LAKEFS_INSTALLATION_ACCESS_KEY_ID=<lakefs-admin-access-key-id>
export LAKEFS_INSTALLATION_SECRET_ACCESS_KEY=<lakefs-admin-secret-key>

kustomize build vendor/lakefs-oss-contrib/default \
  | envsubst \
  | kubectl apply -f -
```

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
