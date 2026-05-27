# lakeFS OSS Contrib

Disclaimer: `lakefs-oss-contrib` is developed by Versioneer as an independent open source contribution to the [lakeFS](https://lakefs.io/) ecosystem. It is not affiliated with, endorsed by, or maintained by lakeFS or Treeverse, the company behind lakeFS. We are grateful to Treeverse for creating lakeFS and making such a great data versioning solution available as open source software.

This project provides an operator and an external auth server so lakeFS identities, credentials, roles, repositories, and authorization can be declared through Kubernetes resources.

For the full project overview and usage guides, read the [documentation](https://lakefs-oss-contrib.versioneer.at/) or the local [docs home](docs/index.md).

## What It Provides

`lakefs-oss-contrib` ships two images:

```text
ghcr.io/versioneer-tech/lakefs-oss-contrib/operator
ghcr.io/versioneer-tech/lakefs-oss-contrib/auth-server
```

The operator reconciles these Kubernetes resources:

- `LakeFSUser`: a lakeFS user.
- `LakeFSGroup`: a group of lakeFS users.
- `LakeFSCredential`: desired access credentials for a user.
- `LakeFSRepository`: desired lakeFS repositories.
- `LakeFSRole`: lakeFS policy templates.
- `LakeFSRoleBinding`: grants a role to a user or group for a repository, or `*`.

The auth server implements the lakeFS external authorization API from Kubernetes resources and generated Secrets.

The normal flow is:

1. Create a `LakeFSUser`.
2. Create a `LakeFSCredential` for that user.
3. The operator creates a Secret with lakeFS S3/API credentials.
4. Create the `LakeFSRepository` objects.
5. Create one or more `LakeFSRole` objects.
6. Create a `LakeFSRoleBinding` for a user or group and repository.
7. lakeFS calls the auth server for credentials and effective policies.
8. Clients use the lakeFS S3 gateway with the generated credential.

## Local Development

Useful targets:

- `make test`
- `make build`
- `make docker-build`
- `make test-e2e-kind`

```bash
make test-e2e-kind
```

Keep the e2e Kind cluster after a run:

```bash
E2E_CLEANUP=false make test-e2e-kind
```

See the [local e2e guide](docs/how-to-guides/local_e2e.md) for requirements and the full scenario.

## Local lakeFS Access

When the e2e cluster is running, port-forward lakeFS:

```bash
kubectl port-forward svc/lakefs -n lakefs 18000:80 \
  --context kind-lakefs-oss-contrib-e2e
```

In another shell, read both S3 credential fields from the generated Kubernetes Secret:

```bash
unset AWS_PROFILE AWS_SESSION_TOKEN

export AWS_ACCESS_KEY_ID="$(kubectl get secret admin-credentials \
  -n lakefs-oss-e2e \
  --context kind-lakefs-oss-contrib-e2e \
  -o jsonpath='{.data.accessKeyId}' | base64 -d)"
export AWS_SECRET_ACCESS_KEY="$(kubectl get secret admin-credentials \
  -n lakefs-oss-e2e \
  --context kind-lakefs-oss-contrib-e2e \
  -o jsonpath='{.data.secretAccessKey}' | base64 -d)"
export AWS_DEFAULT_REGION=us-east-1
export AWS_EC2_METADATA_DISABLED=true
```

Then use the lakeFS S3 gateway:

```bash
aws --endpoint-url http://127.0.0.1:18000 s3 ls
aws --endpoint-url http://127.0.0.1:18000 s3 cp ./data.txt s3://repo-a/main/data/data.txt
```

## License

Apache 2.0 (Apache License Version 2.0, January 2004)
<https://www.apache.org/licenses/LICENSE-2.0>
