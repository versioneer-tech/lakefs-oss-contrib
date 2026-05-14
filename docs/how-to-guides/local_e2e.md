# Local E2E

The repository includes a Kind-based e2e script that installs a complete local stack:

- local Docker registry
- Kind cluster
- operator and auth-server images
- MinIO as the lakeFS blockstore backend
- lakeFS with external auth enabled
- `LakeFSUser`, `LakeFSGroup`, `LakeFSCredential`, `LakeFSRole`, `LakeFSRoleBinding`, and `LakeFSRepository`
- S3 authorization matrix through the lakeFS gateway

Run it with:

```bash
make test-e2e-kind
```

Keep the cluster after a successful run:

```bash
E2E_CLEANUP=false make test-e2e-kind
```

The default kept cluster uses:

```text
Kind context: kind-lakefs-oss-contrib-e2e
lakeFS namespace: lakefs
CR namespace: lakefs-oss-e2e
Repositories: repo-a, repo-b, repo-c
Storage namespaces: s3://e2e-bucket/repo-a, s3://e2e-bucket/repo-b, s3://e2e-bucket/repo-c
Credential Secrets: admin-credentials, user-credentials, readonly-user-credentials
```

## Scenario

The e2e setup creates three users:

- `admin-user`
- `user`
- `readonly-user`

It creates three repositories:

- `repo-a`
- `repo-b`
- `repo-c`

The role bindings exercise these permissions:

- `admin-user` has owner permissions on all repositories and can also manage auth and repository CI.
- `user` has `owner` on `repo-a` and can read/write only `repo-a`.
- `readonly-user` has `viewer` on `repo-b` and can read, but not write, `repo-b`.
- `group-c` contains both `user` and `readonly-user`; the group has `owner` on `repo-c`, so both users can read/write `repo-c`.

## Connect to lakeFS

Port-forward lakeFS:

```bash
kubectl port-forward svc/lakefs -n lakefs 18000:80 \
  --context kind-lakefs-oss-contrib-e2e
```

Use the generated credential:

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

Then use the lakeFS S3 gateway:

```bash
aws --endpoint-url http://127.0.0.1:18000 s3 ls
aws --endpoint-url http://127.0.0.1:18000 s3 ls s3://repo-a/main/
aws --endpoint-url http://127.0.0.1:18000 s3 ls s3://repo-b/main/
aws --endpoint-url http://127.0.0.1:18000 s3 ls s3://repo-c/main/
```
