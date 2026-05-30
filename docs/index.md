# lakeFS OSS Contrib

Disclaimer: `lakefs-oss-contrib` is developed by Versioneer as an independent open source contribution to the [lakeFS](https://lakefs.io/) ecosystem. It is not affiliated with, endorsed by, or maintained by lakeFS or Treeverse, the company behind lakeFS. We are grateful to Treeverse for creating lakeFS and making such a great data versioning solution available as open source software.

This project provides an operator and an external auth server so lakeFS identities, credentials, roles, repositories, and authorization can be declared through Kubernetes resources.

lakeFS remains the upstream data versioning system. This project does not replace, fork, or vendor lakeFS; it adds Kubernetes reconciliation and an external authorization integration around lakeFS so platform teams can manage OSS lakeFS installations with the same declarative model they use for the rest of a Kubernetes platform.

The focus is on making the operational contract explicit: Kubernetes custom resources describe the desired lakeFS users, groups, credentials, repositories, roles, and role bindings, while generated Kubernetes Secrets provide the credentials that clients use through the lakeFS API and S3 gateway.

Kubernetes provides the abstraction for managing users, groups, and credentials. Operational setups can be hardened with tools such as [External Secrets Operator (ESO)](https://external-secrets.io/main/) for secret delivery and [Crossplane Compositions](https://docs.crossplane.io/latest/composition/compositions/) for higher-level platform APIs, for example a custom composition backed by the [Crossplane Keycloak provider](https://github.com/crossplane-contrib/provider-keycloak).

## What It Provides

- `LakeFSUser` for lakeFS users.
- `LakeFSGroup` for groups of lakeFS users.
- `LakeFSCredential` for access keys and generated or supplied secret keys.
- `LakeFSRepository` for repository creation through the lakeFS API.
- `LakeFSGCPolicy` for reusable lakeFS garbage-collection retention and CronJob configuration.
- `LakeFSRole` for reusable lakeFS policy templates.
- `LakeFSRoleBinding` for assigning roles to users or groups for a repository, or `*`.
- An auth server implementing the lakeFS external authorization API from Kubernetes resources.

The intended contract is simple: Kubernetes resources and Secrets are the source of truth; lakeFS consumes them through its normal API and external auth hooks.

## Components

The repository builds two binaries plus a prepared GC Spark image:

```text
ghcr.io/versioneer-tech/lakefs-oss-contrib/operator
ghcr.io/versioneer-tech/lakefs-oss-contrib/auth-server
ghcr.io/versioneer-tech/lakefs-oss-contrib/gc-spark
```

The operator reconciles desired lakeFS state from custom resources. The auth server is stateless and can run with multiple replicas; lakeFS calls it for users, credentials, groups, policies, and permissions.
The GC Spark image provides the default runtime for managed lakeFS OSS garbage-collection CronJobs.

## Quick Shape

```yaml
apiVersion: pkg.internal/v1beta1
kind: LakeFSUser
metadata:
  name: user
spec:
  externalId: user
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSCredential
metadata:
  name: user-credentials
spec:
  userRef:
    name: user
  accessKeyId: user-access-key
  secretRef:
    name: user-credentials
    key: secretAccessKey
```

See the how-to guides for installation, local e2e testing, and the CRD contract.

## License

Apache 2.0 (Apache License Version 2.0, January 2004)
<https://www.apache.org/licenses/LICENSE-2.0>
