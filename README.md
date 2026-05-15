# lakeFS OSS Contrib

Disclaimer: `lakefs-oss-contrib` is developed by Versioneer as an independent open source contribution to the [lakeFS](https://lakefs.io/) ecosystem. It is not affiliated with, endorsed by, or maintained by lakeFS or Treeverse, the company behind lakeFS. We are grateful to Treeverse for creating lakeFS and making such a great data versioning solution available as open source software.

This project provides an operator and an external auth server so lakeFS identities, credentials, roles, repositories, and authorization can be declared through Kubernetes resources.

## Goals

`lakefs-oss-contrib` has two components in one Go module:

- **Operator**: reconciles lakeFS-related Kubernetes resources.
- **Auth server**: implements the lakeFS external authorization API.

The operator watches custom resources and turns desired state into real lakeFS state:

- `LakeFSUser`: a lakeFS user.
- `LakeFSGroup`: a group of lakeFS users.
- `LakeFSCredential`: desired access credentials for a user.
- `LakeFSRole`: lakeFS policy templates.
- `LakeFSRoleBinding`: grants a role to a user or group for a repository, or `*`.
- `LakeFSRepository`: desired lakeFS repositories.

The auth server is stateless and can run with multiple replicas. lakeFS calls it to resolve:

- users and groups
- access keys and secret keys
- policies and effective permissions

Kubernetes resources, including generated Kubernetes Secrets, are the source of truth. This gives lakeFS a stable interface today and still leaves room for deeper operations later, such as External Secrets Operator with Vault, OpenBao, or another secrets manager behind the same Secret shape.

## How It Works

The repo builds two binaries:

```text
cmd/operator/main.go
cmd/authserver/main.go
```

And two images:

```text
ghcr.io/versioneer-tech/lakefs-oss-contrib-operator
ghcr.io/versioneer-tech/lakefs-oss-contrib-auth-server
```

The normal flow is:

1. Create a `LakeFSUser`.
2. Create a `LakeFSCredential` for that user.
3. The operator creates a Secret with lakeFS S3/API credentials.
4. Create one or more `LakeFSRole` objects.
5. Create a `LakeFSRoleBinding` for a user or group and repository.
6. Create a `LakeFSRepository`.
7. lakeFS calls the auth server for credentials and effective policies.
8. Clients use the lakeFS S3 gateway with the generated credential.

S3 paths follow the lakeFS convention:

```text
s3://<repository>/<branch>/<path>
```

Example:

```bash
aws --endpoint-url http://127.0.0.1:18000 \
  s3 cp ./data.txt s3://repo-a/main/data/data.txt
```

## Health and Metrics

The operator exposes:

- `/healthz` on `:8081`
- `/readyz` on `:8081`
- `/metrics` on `:8443` through `controller-manager-metrics-service`

The auth server exposes:

- lakeFS auth API on `:8080`, including `/healthcheck` for lakeFS compatibility
- `/metrics` on `:8081` through the auth-server Service port named `metrics`
- Kubernetes probes `/healthz` and `/readyz` on `:8082`

## Local Development

Install these tools locally before running the development and e2e targets:

- Go 1.25.7
- Docker with BuildKit/buildx support
- kubectl 1.33 or newer
- Kind 0.31 or newer
- Helm 3.18 or newer
- AWS CLI 2.15 or newer

Run unit tests:

```bash
make test
```

This project is built on the [Kubebuilder](https://kubebuilder.io/) framework; to add another CRD, add the API type and controller under `api/v1beta1` and `internal/controller`, then run `make manifests generate`.

Build both binaries:

```bash
make build
```

Build both images:

```bash
make docker-build \
  IMG=localhost:5001/lakefs-oss-contrib-operator:dev \
  AUTHSERVER_IMG=localhost:5001/lakefs-oss-contrib-auth-server:dev
```

For local end-to-end work, use a dedicated Kind cluster and a local registry. The e2e script does this for you:

```bash
make test-e2e-kind
```

By default this uses:

```text
Kind cluster: lakefs-oss-contrib-e2e
Registry: localhost:5001
Operator image: localhost:5001/lakefs-oss-contrib-operator:e2e
Auth server image: localhost:5001/lakefs-oss-contrib-auth-server:e2e
```

To keep the cluster after a run:

```bash
E2E_CLEANUP=false make test-e2e-kind
```

To clean up after CI-style runs:

```bash
E2E_CLEANUP=true make test-e2e-kind
```

## Local lakeFS Access

When the e2e cluster is running, port-forward lakeFS:

```bash
kubectl port-forward svc/lakefs -n lakefs 18000:80 \
  --context kind-lakefs-oss-contrib-e2e
```

Use the generated test credential:

```bash
unset AWS_PROFILE AWS_SESSION_TOKEN

export AWS_ACCESS_KEY_ID=lakefs_ak_admin
export AWS_SECRET_ACCESS_KEY="$(kubectl get secret admin-credentials \
  -n lakefs-oss-e2e \
  --context kind-lakefs-oss-contrib-e2e \
  -o jsonpath='{.data.secretAccessKey}' | base64 -d)"
export AWS_DEFAULT_REGION=us-east-1
export AWS_EC2_METADATA_DISABLED=true
```

Then list repositories through the lakeFS S3 gateway:

```bash
aws --endpoint-url http://127.0.0.1:18000 s3 ls
aws --endpoint-url http://127.0.0.1:18000 s3 ls s3://repo-a/main/
aws --endpoint-url http://127.0.0.1:18000 s3 ls s3://repo-b/main/
aws --endpoint-url http://127.0.0.1:18000 s3 ls s3://repo-c/main/
```

## Testing Strategy

The test pyramid is:

- **Go unit/envtest tests** with `make test`.
- **Manifest validation** with `make manifests` and `kustomize build config/default`.
- **Local Kind e2e** with `make test-e2e-kind`.
- **Pull request e2e** through GitHub Actions.

The e2e test creates a fresh local platform:

1. Start Kind and a local Docker registry.
2. Build and push both local images.
3. Deploy the operator and auth server.
4. Create `LakeFSUser`, `LakeFSGroup`, `LakeFSCredential`, `LakeFSRole`, `LakeFSRoleBinding`, and `LakeFSRepository`.
5. Verify auth server health and metrics.
6. Verify the S3 authorization matrix through lakeFS.

The pull request workflow runs the same script so local and CI behavior stay close.

## License

Apache 2.0 (Apache License Version 2.0, January 2004)  
<https://www.apache.org/licenses/LICENSE-2.0>
