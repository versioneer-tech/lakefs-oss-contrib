#!/usr/bin/env bash
# Copyright 2026, Versioneer (https://versioneer.at)
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-lakefs-oss-contrib-e2e}"
KIND_CONTEXT="kind-${KIND_CLUSTER_NAME}"
REGISTRY_NAME="${REGISTRY_NAME:-kind-registry}"
REGISTRY_PORT="${REGISTRY_PORT:-5001}"
REGISTRY="${REGISTRY:-localhost:${REGISTRY_PORT}}"
OPERATOR_IMG="${OPERATOR_IMG:-${REGISTRY}/lakefs-oss-contrib/operator:e2e}"
AUTHSERVER_IMG="${AUTHSERVER_IMG:-${REGISTRY}/lakefs-oss-contrib/auth-server:e2e}"
E2E_NAMESPACE="${E2E_NAMESPACE:-lakefs-oss-e2e}"
LAKEFS_NAMESPACE="${LAKEFS_NAMESPACE:-lakefs}"
LAKEFS_RELEASE="${LAKEFS_RELEASE:-lakefs}"
E2E_ADMIN_USER="${E2E_ADMIN_USER:-admin-user}"
E2E_ADMIN_ACCESS_KEY_ID="${E2E_ADMIN_ACCESS_KEY_ID:-lakefs_ak_admin}"
E2E_ADMIN_SECRET_ACCESS_KEY="${E2E_ADMIN_SECRET_ACCESS_KEY:-lakefs_sk_admin_e2e}"
E2E_ADMIN_CREDENTIAL_SECRET="${E2E_ADMIN_CREDENTIAL_SECRET:-admin-credentials}"
E2E_USER="${E2E_USER:-user}"
E2E_USER_ACCESS_KEY_ID="${E2E_USER_ACCESS_KEY_ID:-lakefs_ak_user}"
E2E_USER_SECRET_ACCESS_KEY="${E2E_USER_SECRET_ACCESS_KEY:-lakefs_sk_user_e2e}"
E2E_USER_CREDENTIAL_SECRET="${E2E_USER_CREDENTIAL_SECRET:-user-credentials}"
E2E_READONLY_USER="${E2E_READONLY_USER:-readonly-user}"
E2E_READONLY_ACCESS_KEY_ID="${E2E_READONLY_ACCESS_KEY_ID:-lakefs_ak_readonly}"
E2E_READONLY_SECRET_ACCESS_KEY="${E2E_READONLY_SECRET_ACCESS_KEY:-lakefs_sk_readonly_e2e}"
E2E_READONLY_CREDENTIAL_SECRET="${E2E_READONLY_CREDENTIAL_SECRET:-readonly-user-credentials}"
E2E_REPO_A="${E2E_REPO_A:-repo-a}"
E2E_REPO_B="${E2E_REPO_B:-repo-b}"
E2E_REPO_C="${E2E_REPO_C:-repo-c}"
E2E_REPO_GC="${E2E_REPO_GC:-repo-gc}"
E2E_GC_POLICY="${E2E_GC_POLICY:-e2e-gc-fast}"
E2E_GC_SPARK_IMAGE="${E2E_GC_SPARK_IMAGE:-ghcr.io/versioneer-tech/lakefs-oss-contrib/gc-spark:latest}"
E2E_GC_SPARK_BUILD="${E2E_GC_SPARK_BUILD:-false}"
E2E_GROUP_C="${E2E_GROUP_C:-group-c}"
E2E_S3_BUCKET="${E2E_S3_BUCKET:-e2e-bucket}"
E2E_STORAGE_NAMESPACE_A="${E2E_STORAGE_NAMESPACE_A:-s3://${E2E_S3_BUCKET}/${E2E_REPO_A}}"
E2E_STORAGE_NAMESPACE_B="${E2E_STORAGE_NAMESPACE_B:-s3://${E2E_S3_BUCKET}/${E2E_REPO_B}}"
E2E_STORAGE_NAMESPACE_C="${E2E_STORAGE_NAMESPACE_C:-s3://${E2E_S3_BUCKET}/${E2E_REPO_C}}"
E2E_STORAGE_NAMESPACE_GC="${E2E_STORAGE_NAMESPACE_GC:-s3://${E2E_S3_BUCKET}/${E2E_REPO_GC}}"
E2E_S3_SERVICE="${E2E_S3_SERVICE:-lakefs-e2e-minio}"
E2E_S3_ENDPOINT="${E2E_S3_ENDPOINT:-http://${E2E_S3_SERVICE}.${LAKEFS_NAMESPACE}.svc:9000}"
E2E_S3_ACCESS_KEY="${E2E_S3_ACCESS_KEY:-lakefs_e2e_minio}"
E2E_S3_SECRET_KEY="${E2E_S3_SECRET_KEY:-lakefs_e2e_minio_secret}"
LAKEFS_PORT="${LAKEFS_PORT:-18000}"
E2E_CLEANUP="${E2E_CLEANUP:-false}"

created_cluster=false
created_registry=false
port_forward_pid=""

log() {
  printf '\n==> %s\n' "$*"
}

require_tool() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required tool: $1" >&2
    exit 1
  }
}

usage() {
  cat <<EOF
Usage: scripts/e2e-kind.sh

Runs the local lakeFS OSS operator e2e smoke test:
  - create/reuse Kind cluster and local registry
  - build/push operator and auth-server images locally
  - deploy operator/auth-server
  - create the lakeFS bootstrap admin group as Kubernetes state
  - install lakeFS with stats.enabled=false
  - create three LakeFSUser objects, one LakeFSGroup, credentials, repositories, roles, and role bindings
  - create and delete a timestamped lakeFS repository through LakeFSRepository reconciliation
  - create a custom LakeFSGCPolicy and verify the managed GC CronJob collects deleted uncommitted objects
  - verify admin, owner, viewer, and group-inherited S3 access through lakeFS

Useful environment variables:
  KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME}
  REGISTRY=${REGISTRY}
  OPERATOR_IMG=${OPERATOR_IMG}
  AUTHSERVER_IMG=${AUTHSERVER_IMG}
  E2E_GC_SPARK_IMAGE=${E2E_GC_SPARK_IMAGE}
  E2E_GC_SPARK_BUILD=${E2E_GC_SPARK_BUILD}
  E2E_CLEANUP=${E2E_CLEANUP}
EOF
}

cleanup() {
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi

  if [[ "${E2E_CLEANUP}" == "true" ]]; then
    if [[ "${created_cluster}" == "true" ]]; then
      kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
    fi
    if [[ "${created_registry}" == "true" ]]; then
      docker rm -f "${REGISTRY_NAME}" >/dev/null 2>&1 || true
    fi
  fi
}
trap cleanup EXIT

ensure_registry() {
  local running
  if running="$(docker inspect -f '{{.State.Running}}' "${REGISTRY_NAME}" 2>/dev/null)"; then
    if [[ "${running}" == "true" ]]; then
      log "Using existing local registry ${REGISTRY_NAME} on ${REGISTRY}"
      return
    fi

    log "Starting existing local registry ${REGISTRY_NAME}"
    docker start "${REGISTRY_NAME}" >/dev/null
    log "Using existing local registry ${REGISTRY_NAME} on ${REGISTRY}"
    return
  fi

  log "Starting local registry ${REGISTRY_NAME} on ${REGISTRY}"
  docker run -d --restart=always \
    -p "127.0.0.1:${REGISTRY_PORT}:5000" \
    --name "${REGISTRY_NAME}" \
    registry:2 >/dev/null
  created_registry=true
}

ensure_kind_cluster() {
  if kind get clusters | grep -qx "${KIND_CLUSTER_NAME}"; then
    log "Using existing Kind cluster ${KIND_CLUSTER_NAME}"
    return
  fi

  log "Creating Kind cluster ${KIND_CLUSTER_NAME}"
  kind create cluster --name "${KIND_CLUSTER_NAME}"
  created_cluster=true
}

configure_kind_registry() {
  log "Configuring Kind node local registry access"
  local node
  for node in $(kind get nodes --name "${KIND_CLUSTER_NAME}"); do
    docker exec "${node}" mkdir -p "/etc/containerd/certs.d/${REGISTRY}"
    tmp_hosts="$(mktemp)"
    printf '[host."http://%s:5000"]\n' "${REGISTRY_NAME}" >"${tmp_hosts}"
    docker cp "${tmp_hosts}" "${node}:/etc/containerd/certs.d/${REGISTRY}/hosts.toml"
    rm -f "${tmp_hosts}"
  done

  if ! docker inspect -f '{{json .NetworkSettings.Networks.kind}}' "${REGISTRY_NAME}" | grep -qv '^null$'; then
    docker network connect kind "${REGISTRY_NAME}"
  fi

  kubectl apply --context "${KIND_CONTEXT}" -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "${REGISTRY}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
}

prepare_gc_spark_image() {
  log "Preparing GC Spark image ${E2E_GC_SPARK_IMAGE}"
  if [[ "${E2E_GC_SPARK_BUILD}" == "true" ]]; then
    docker build -f "${ROOT_DIR}/Dockerfile.gc-spark" -t "${E2E_GC_SPARK_IMAGE}" "${ROOT_DIR}"
  elif ! docker image inspect "${E2E_GC_SPARK_IMAGE}" >/dev/null 2>&1; then
    docker pull "${E2E_GC_SPARK_IMAGE}"
  fi

  log "Loading GC Spark image ${E2E_GC_SPARK_IMAGE} into Kind"
  kind load docker-image "${E2E_GC_SPARK_IMAGE}" --name "${KIND_CLUSTER_NAME}"
}

ensure_e2e_namespace() {
  kubectl create namespace "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --dry-run=client \
    -o yaml | kubectl apply --context "${KIND_CONTEXT}" -f -
}

deploy_s3_backend() {
  log "Deploying e2e S3 backend"
  kubectl create namespace "${LAKEFS_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --dry-run=client \
    -o yaml | kubectl apply --context "${KIND_CONTEXT}" -f -

  kubectl delete "deployment/${E2E_S3_SERVICE}" "service/${E2E_S3_SERVICE}" \
    -n "${LAKEFS_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s

  kubectl apply --context "${KIND_CONTEXT}" -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${E2E_S3_SERVICE}
  namespace: ${LAKEFS_NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: ${E2E_S3_SERVICE}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: ${E2E_S3_SERVICE}
    spec:
      containers:
      - name: minio
        image: minio/minio:RELEASE.2025-09-07T16-13-09Z
        args:
        - server
        - /data
        env:
        - name: MINIO_ROOT_USER
          value: ${E2E_S3_ACCESS_KEY}
        - name: MINIO_ROOT_PASSWORD
          value: ${E2E_S3_SECRET_KEY}
        ports:
        - name: s3
          containerPort: 9000
        readinessProbe:
          httpGet:
            path: /minio/health/ready
            port: s3
        volumeMounts:
        - name: data
          mountPath: /data
      volumes:
      - name: data
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: ${E2E_S3_SERVICE}
  namespace: ${LAKEFS_NAMESPACE}
spec:
  selector:
    app.kubernetes.io/name: ${E2E_S3_SERVICE}
  ports:
  - name: s3
    port: 9000
    targetPort: s3
EOF

  kubectl rollout status "deployment/${E2E_S3_SERVICE}" \
    -n "${LAKEFS_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --timeout=180s

  kubectl run e2e-create-bucket \
    --context "${KIND_CONTEXT}" \
    --namespace "${LAKEFS_NAMESPACE}" \
    --restart=Never \
    --rm \
    -i \
    --image=minio/mc:RELEASE.2025-08-13T08-35-41Z \
    --command -- sh -ec "
      mc alias set e2e ${E2E_S3_ENDPOINT} ${E2E_S3_ACCESS_KEY} ${E2E_S3_SECRET_KEY}
      mc mb --ignore-existing e2e/${E2E_S3_BUCKET}
      mc rm --recursive --force e2e/${E2E_S3_BUCKET}/${E2E_REPO_A} || true
      mc rm --recursive --force e2e/${E2E_S3_BUCKET}/${E2E_REPO_B} || true
      mc rm --recursive --force e2e/${E2E_S3_BUCKET}/${E2E_REPO_C} || true
      mc rm --recursive --force e2e/${E2E_S3_BUCKET}/${E2E_REPO_GC} || true
    "
}

build_and_push_images() {
  log "Building operator and auth-server images"
  docker build --build-arg TARGET=operator -t "${OPERATOR_IMG}" "${ROOT_DIR}"
  docker build --build-arg TARGET=authserver -t "${AUTHSERVER_IMG}" "${ROOT_DIR}"

  log "Pushing images to ${REGISTRY}"
  docker push "${OPERATOR_IMG}"
  docker push "${AUTHSERVER_IMG}"
}

deploy_operator_stack() {
  log "Deploying operator and auth server"
  make -C "${ROOT_DIR}" manifests kustomize
  "${ROOT_DIR}/bin/kustomize" build "${ROOT_DIR}/config/default" \
    | sed \
      -e "s#ghcr.io/versioneer-tech/lakefs-oss-contrib/operator:latest#${OPERATOR_IMG}#g" \
      -e "s#ghcr.io/versioneer-tech/lakefs-oss-contrib/auth-server:latest#${AUTHSERVER_IMG}#g" \
      -e "s#--default-user-namespace=\$(POD_NAMESPACE)#--default-user-namespace=${E2E_NAMESPACE}#g" \
    | kubectl apply --context "${KIND_CONTEXT}" -f -

  kubectl rollout restart deployment/lakefs-oss-contrib-controller-manager \
    -n lakefs-oss-contrib-system \
    --context "${KIND_CONTEXT}"
  kubectl rollout restart deployment/lakefs-oss-contrib-auth-server \
    -n lakefs-oss-contrib-system \
    --context "${KIND_CONTEXT}"

  kubectl rollout status deployment/lakefs-oss-contrib-controller-manager \
    -n lakefs-oss-contrib-system \
    --context "${KIND_CONTEXT}" \
    --timeout=180s
  kubectl rollout status deployment/lakefs-oss-contrib-auth-server \
    -n lakefs-oss-contrib-system \
    --context "${KIND_CONTEXT}" \
    --timeout=180s
}

verify_platform_endpoints() {
  log "Verifying health and metrics endpoints"
  kubectl run endpoint-check \
    --context "${KIND_CONTEXT}" \
    --namespace lakefs-oss-contrib-system \
    --restart=Never \
    --rm \
    -i \
    --image=curlimages/curl:latest \
    --command -- sh -ec '
      curl -fsS http://lakefs-oss-contrib-auth-server/healthcheck >/dev/null
      curl -fsS http://lakefs-oss-contrib-auth-server:8081/metrics | grep -q "^go_"
    '
}

ensure_lakefs_bootstrap_group() {
  log "Ensuring lakeFS bootstrap admin group"
  kubectl apply --context "${KIND_CONTEXT}" -f - <<EOF
apiVersion: pkg.internal/v1beta1
kind: LakeFSGroup
metadata:
  name: group-admins
  namespace: ${E2E_NAMESPACE}
spec:
  externalId: Admins
  description: lakeFS bootstrap admins
EOF

  kubectl wait lakefsgroup/group-admins \
    -n "${E2E_NAMESPACE}" \
    --for=condition=Ready \
    --context "${KIND_CONTEXT}" \
    --timeout=120s
}

install_lakefs() {
  log "Installing lakeFS with external auth and stats disabled"
  tmp_values="$(mktemp)"
  cat >"${tmp_values}" <<EOF
lakefsConfig: |
  logging:
    level: DEBUG
  stats:
    enabled: false

  database:
    type: local
  blockstore:
    type: s3
    s3:
      region: us-east-1
      endpoint: ${E2E_S3_ENDPOINT}
      discover_bucket_region: false
      force_path_style: true

  auth:
    ui_config:
      rbac: simplified
    api:
      endpoint: http://lakefs-oss-contrib-auth-server.lakefs-oss-contrib-system.svc/auth/v1

  installation:
    user_name: ${E2E_ADMIN_USER}
    access_key_id: ${E2E_ADMIN_ACCESS_KEY_ID}
    secret_access_key: ${E2E_ADMIN_SECRET_ACCESS_KEY}
extraEnvVars:
- name: AWS_ACCESS_KEY_ID
  value: ${E2E_S3_ACCESS_KEY}
- name: AWS_SECRET_ACCESS_KEY
  value: ${E2E_S3_SECRET_KEY}
EOF

  helm repo add lakefs https://charts.lakefs.io >/dev/null 2>&1 || true
  helm repo update lakefs
  helm upgrade --install "${LAKEFS_RELEASE}" lakefs/lakefs \
    -n "${LAKEFS_NAMESPACE}" \
    --create-namespace \
    -f "${tmp_values}" \
    --kube-context "${KIND_CONTEXT}"
  rm -f "${tmp_values}"

  kubectl rollout restart "deployment/${LAKEFS_RELEASE}" \
    -n "${LAKEFS_NAMESPACE}" \
    --context "${KIND_CONTEXT}"

  kubectl rollout status "deployment/${LAKEFS_RELEASE}" \
    -n "${LAKEFS_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --timeout=240s
}

delete_lakefs_repository_resource() {
  local repository_name="$1"

  if kubectl delete "lakefsrepository/${repository_name}" \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s; then
    return 0
  fi

  log "Force-clearing stale finalizers for LakeFSRepository/${repository_name}"
  kubectl patch "lakefsrepository/${repository_name}" \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --type=merge \
    -p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1 || true

  kubectl delete "lakefsrepository/${repository_name}" \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=60s
}

apply_e2e_resources() {
  log "Resetting previous e2e LakeFS resources"
  delete_lakefs_repository_resource "${E2E_REPO_A}"
  delete_lakefs_repository_resource "${E2E_REPO_B}"
  delete_lakefs_repository_resource "${E2E_REPO_C}"
  kubectl delete lakefscredential --all \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s
  kubectl delete \
    lakefsrolebinding/admin-user-all \
    lakefsrolebinding/user-repo-a-owner \
    lakefsrolebinding/readonly-user-repo-b-viewer \
    lakefsrolebinding/group-c-repo-c-owner \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s
  kubectl delete "lakefsgroup/${E2E_GROUP_C}" \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s
  kubectl delete lakefsrole/admin lakefsrole/owner lakefsrole/viewer \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s
  kubectl delete \
    "lakefsuser/${E2E_ADMIN_USER}" \
    "lakefsuser/${E2E_USER}" \
    "lakefsuser/${E2E_READONLY_USER}" \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s
  kubectl delete secret --all \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s

  log "Applying e2e LakeFS resources"
  kubectl apply --context "${KIND_CONTEXT}" -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${E2E_NAMESPACE}
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSUser
metadata:
  name: ${E2E_ADMIN_USER}
  namespace: ${E2E_NAMESPACE}
spec:
  externalId: ${E2E_ADMIN_USER}
  friendlyName: E2E Admin
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSUser
metadata:
  name: ${E2E_USER}
  namespace: ${E2E_NAMESPACE}
spec:
  externalId: ${E2E_USER}
  friendlyName: E2E User
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSUser
metadata:
  name: ${E2E_READONLY_USER}
  namespace: ${E2E_NAMESPACE}
spec:
  externalId: ${E2E_READONLY_USER}
  friendlyName: E2E Readonly User
---
apiVersion: v1
kind: Secret
metadata:
  name: ${E2E_ADMIN_CREDENTIAL_SECRET}
  namespace: ${E2E_NAMESPACE}
type: Opaque
stringData:
  accessKeyId: ${E2E_ADMIN_ACCESS_KEY_ID}
  secretAccessKey: ${E2E_ADMIN_SECRET_ACCESS_KEY}
---
apiVersion: v1
kind: Secret
metadata:
  name: ${E2E_USER_CREDENTIAL_SECRET}
  namespace: ${E2E_NAMESPACE}
type: Opaque
stringData:
  accessKeyId: ${E2E_USER_ACCESS_KEY_ID}
  secretAccessKey: ${E2E_USER_SECRET_ACCESS_KEY}
---
apiVersion: v1
kind: Secret
metadata:
  name: ${E2E_READONLY_CREDENTIAL_SECRET}
  namespace: ${E2E_NAMESPACE}
type: Opaque
stringData:
  accessKeyId: ${E2E_READONLY_ACCESS_KEY_ID}
  secretAccessKey: ${E2E_READONLY_SECRET_ACCESS_KEY}
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSCredential
metadata:
  name: ${E2E_ADMIN_CREDENTIAL_SECRET}
  namespace: ${E2E_NAMESPACE}
spec:
  userRef:
    name: ${E2E_ADMIN_USER}
  accessKeyId: ${E2E_ADMIN_ACCESS_KEY_ID}
  secretRef:
    name: ${E2E_ADMIN_CREDENTIAL_SECRET}
    key: secretAccessKey
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSCredential
metadata:
  name: ${E2E_USER_CREDENTIAL_SECRET}
  namespace: ${E2E_NAMESPACE}
spec:
  userRef:
    name: ${E2E_USER}
  accessKeyId: ${E2E_USER_ACCESS_KEY_ID}
  secretRef:
    name: ${E2E_USER_CREDENTIAL_SECRET}
    key: secretAccessKey
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSCredential
metadata:
  name: ${E2E_READONLY_CREDENTIAL_SECRET}
  namespace: ${E2E_NAMESPACE}
spec:
  userRef:
    name: ${E2E_READONLY_USER}
  accessKeyId: ${E2E_READONLY_ACCESS_KEY_ID}
  secretRef:
    name: ${E2E_READONLY_CREDENTIAL_SECRET}
    key: secretAccessKey
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSGroup
metadata:
  name: ${E2E_GROUP_C}
  namespace: ${E2E_NAMESPACE}
spec:
  externalId: ${E2E_GROUP_C}
  description: Users with owner access to ${E2E_REPO_C}
  users:
  - name: ${E2E_USER}
  - name: ${E2E_READONLY_USER}
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRepository
metadata:
  name: ${E2E_REPO_A}
  namespace: ${E2E_NAMESPACE}
spec:
  endpoint: http://lakefs.${LAKEFS_NAMESPACE}.svc
  storageNamespace: ${E2E_STORAGE_NAMESPACE_A}
  defaultBranch: main
  credentialsSecretRef:
    name: ${E2E_ADMIN_CREDENTIAL_SECRET}
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRepository
metadata:
  name: ${E2E_REPO_B}
  namespace: ${E2E_NAMESPACE}
spec:
  endpoint: http://lakefs.${LAKEFS_NAMESPACE}.svc
  storageNamespace: ${E2E_STORAGE_NAMESPACE_B}
  defaultBranch: main
  credentialsSecretRef:
    name: ${E2E_ADMIN_CREDENTIAL_SECRET}
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRepository
metadata:
  name: ${E2E_REPO_C}
  namespace: ${E2E_NAMESPACE}
spec:
  endpoint: http://lakefs.${LAKEFS_NAMESPACE}.svc
  storageNamespace: ${E2E_STORAGE_NAMESPACE_C}
  defaultBranch: main
  credentialsSecretRef:
    name: ${E2E_ADMIN_CREDENTIAL_SECRET}
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRole
metadata:
  name: admin
  namespace: ${E2E_NAMESPACE}
spec:
  description: Administrator
  policies:
  - name: admin
    statement:
    - effect: allow
      action:
      - fs:*
      - auth:*
      - ci:*
      - retention:*
      - branches:*
      resource: "*"
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRole
metadata:
  name: owner
  namespace: ${E2E_NAMESPACE}
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
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRole
metadata:
  name: viewer
  namespace: ${E2E_NAMESPACE}
spec:
  description: Viewer
  policies:
  - name: <REPOSITORY>-viewer
    statement:
    - effect: allow
      action:
      - fs:ListObjects
      - fs:ReadObject
      resource: arn:lakefs:fs:::repository/<REPOSITORY>
    - effect: allow
      action:
      - fs:ListObjects
      - fs:ReadObject
      resource: arn:lakefs:fs:::repository/<REPOSITORY>/branch/*
    - effect: allow
      action:
      - fs:ListObjects
      - fs:ReadObject
      resource: arn:lakefs:fs:::repository/<REPOSITORY>/object/*
    - effect: allow
      action:
      - fs:ListRepositories
      resource: "*"
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRoleBinding
metadata:
  name: admin-user-all
  namespace: ${E2E_NAMESPACE}
spec:
  subject:
    kind: LakeFSUser
    name: ${E2E_ADMIN_USER}
  roleRef:
    name: admin
  repository: "*"
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRoleBinding
metadata:
  name: user-repo-a-owner
  namespace: ${E2E_NAMESPACE}
spec:
  subject:
    kind: LakeFSUser
    name: ${E2E_USER}
  roleRef:
    name: owner
  repository: ${E2E_REPO_A}
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRoleBinding
metadata:
  name: readonly-user-repo-b-viewer
  namespace: ${E2E_NAMESPACE}
spec:
  subject:
    kind: LakeFSUser
    name: ${E2E_READONLY_USER}
  roleRef:
    name: viewer
  repository: ${E2E_REPO_B}
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRoleBinding
metadata:
  name: group-c-repo-c-owner
  namespace: ${E2E_NAMESPACE}
spec:
  subject:
    kind: LakeFSGroup
    name: ${E2E_GROUP_C}
  roleRef:
    name: owner
  repository: ${E2E_REPO_C}
EOF

  kubectl wait \
    "lakefsuser/${E2E_ADMIN_USER}" \
    "lakefsuser/${E2E_USER}" \
    "lakefsuser/${E2E_READONLY_USER}" \
    -n "${E2E_NAMESPACE}" \
    --for=condition=Ready \
    --context "${KIND_CONTEXT}" \
    --timeout=120s
  kubectl wait \
    "lakefscredential/${E2E_ADMIN_CREDENTIAL_SECRET}" \
    "lakefscredential/${E2E_USER_CREDENTIAL_SECRET}" \
    "lakefscredential/${E2E_READONLY_CREDENTIAL_SECRET}" \
    -n "${E2E_NAMESPACE}" \
    --for=condition=Ready \
    --context "${KIND_CONTEXT}" \
    --timeout=120s
  kubectl wait "lakefsgroup/${E2E_GROUP_C}" \
    -n "${E2E_NAMESPACE}" \
    --for=condition=Ready \
    --context "${KIND_CONTEXT}" \
    --timeout=120s
  kubectl wait lakefsrole/admin \
    -n "${E2E_NAMESPACE}" \
    --for=condition=Ready \
    --context "${KIND_CONTEXT}" \
    --timeout=120s
  kubectl wait lakefsrole/owner \
    -n "${E2E_NAMESPACE}" \
    --for=condition=Ready \
    --context "${KIND_CONTEXT}" \
    --timeout=120s
  kubectl wait lakefsrole/viewer \
    -n "${E2E_NAMESPACE}" \
    --for=condition=Ready \
    --context "${KIND_CONTEXT}" \
    --timeout=120s
  kubectl wait \
    lakefsrolebinding/admin-user-all \
    lakefsrolebinding/user-repo-a-owner \
    lakefsrolebinding/readonly-user-repo-b-viewer \
    lakefsrolebinding/group-c-repo-c-owner \
    -n "${E2E_NAMESPACE}" \
    --for=condition=Ready \
    --context "${KIND_CONTEXT}" \
    --timeout=120s
  kubectl wait \
    "lakefsrepository/${E2E_REPO_A}" \
    "lakefsrepository/${E2E_REPO_B}" \
    "lakefsrepository/${E2E_REPO_C}" \
    -n "${E2E_NAMESPACE}" \
    --for=condition=Ready \
    --context "${KIND_CONTEXT}" \
    --timeout=180s
}

wait_for_lakefs_port_forward() {
  log "Port-forwarding lakeFS to 127.0.0.1:${LAKEFS_PORT}"
  kubectl port-forward "svc/${LAKEFS_RELEASE}" \
    -n "${LAKEFS_NAMESPACE}" \
    "${LAKEFS_PORT}:80" \
    --context "${KIND_CONTEXT}" >/tmp/lakefs-port-forward.log 2>&1 &
  port_forward_pid="$!"

  for _ in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:${LAKEFS_PORT}/_health" >/dev/null 2>&1; then
      return
    fi
    sleep 2
  done

  echo "lakeFS port-forward did not become ready" >&2
  cat /tmp/lakefs-port-forward.log >&2 || true
  exit 1
}

use_lakefs_credential() {
  local secret_name="$1"
  unset AWS_PROFILE AWS_SESSION_TOKEN
  export AWS_ACCESS_KEY_ID
  AWS_ACCESS_KEY_ID="$(kubectl get secret "${secret_name}" \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    -o jsonpath='{.data.accessKeyId}' | base64 -d)"
  export AWS_SECRET_ACCESS_KEY
  AWS_SECRET_ACCESS_KEY="$(kubectl get secret "${secret_name}" \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    -o jsonpath='{.data.secretAccessKey}' | base64 -d)"
  export AWS_DEFAULT_REGION=us-east-1
  export AWS_EC2_METADATA_DISABLED=true
}

lakefs_s3() {
  aws --endpoint-url "http://127.0.0.1:${LAKEFS_PORT}" "$@"
}

lakefs_object_physical_address() {
  local repository_name="$1"
  local object_path="$2"
  local body_file
  local status
  local physical_address

  body_file="$(mktemp)"
  if ! status="$(lakefs_api_status "GET" "/repositories/${repository_name}/refs/main/objects/stat?path=${object_path}" "${body_file}")"; then
    echo "lakeFS object stat request failed for ${repository_name}:${object_path}" >&2
    cat "${body_file}" >&2 || true
    rm -f "${body_file}"
    return 1
  fi

  if [[ "${status}" != "200" ]]; then
    echo "expected HTTP 200 from object stat for ${repository_name}:${object_path}, got ${status}" >&2
    cat "${body_file}" >&2 || true
    rm -f "${body_file}"
    return 1
  fi

  physical_address="$(jq -r '.physical_address // empty' "${body_file}")"
  rm -f "${body_file}"
  if [[ -z "${physical_address}" ]]; then
    echo "object stat for ${repository_name}:${object_path} did not include physical_address" >&2
    return 1
  fi

  printf '%s' "${physical_address}"
}

minio_object_exists() {
  local physical_address="$1"
  local object_key="${physical_address#s3://}"

  kubectl exec "deployment/${E2E_S3_SERVICE}" \
    -n "${LAKEFS_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    -- sh -ec "
      mc alias set e2e ${E2E_S3_ENDPOINT} ${E2E_S3_ACCESS_KEY} ${E2E_S3_SECRET_KEY} >/dev/null
      mc stat 'e2e/${object_key}' >/dev/null 2>&1
    " >/dev/null
}

wait_minio_object_state() {
  local label="$1"
  local physical_address="$2"
  local expected="$3"
  local timeout_seconds="${4:-180}"
  local deadline
  local exists

  log "${label}"
  deadline=$((SECONDS + timeout_seconds))
  while true; do
    exists="false"
    if minio_object_exists "${physical_address}"; then
      exists="true"
    fi

    if [[ "${exists}" == "${expected}" ]]; then
      return 0
    fi

    if (( SECONDS >= deadline )); then
      echo "expected MinIO object ${physical_address} existence to be ${expected}, got ${exists}" >&2
      return 1
    fi

    sleep 5
  done
}

latest_gc_job_name() {
  local repository_name="$1"
  kubectl get jobs \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    -l "lakefs.versioneer.at/repo-cr=${repository_name}" \
    --sort-by=.metadata.creationTimestamp \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | tail -n 1
}

wait_for_gc_job_complete() {
  local repository_name="$1"
  local timeout_seconds="${2:-240}"
  local deadline
  local job_name

  log "Waiting for managed GC CronJob to run for ${repository_name}"
  deadline=$((SECONDS + timeout_seconds))
  while true; do
    job_name="$(latest_gc_job_name "${repository_name}")"
    if [[ -n "${job_name}" ]]; then
      break
    fi

    if (( SECONDS >= deadline )); then
      echo "GC CronJob did not create a Job for ${repository_name} within ${timeout_seconds}s" >&2
      return 1
    fi

    sleep 5
  done

  kubectl wait "job/${job_name}" \
    -n "${E2E_NAMESPACE}" \
    --for=condition=Complete \
    --context "${KIND_CONTEXT}" \
    --timeout="${timeout_seconds}s"
}

wait_for_gc_cronjob_suspend_state() {
  local repository_name="$1"
  local expected="$2"
  local timeout_seconds="${3:-120}"
  local deadline
  local observed

  deadline=$((SECONDS + timeout_seconds))
  while true; do
    observed="$(kubectl get "cronjob/${repository_name}-gc" \
      -n "${E2E_NAMESPACE}" \
      --context "${KIND_CONTEXT}" \
      -o jsonpath='{.spec.suspend}')"
    if [[ "${observed}" == "${expected}" ]]; then
      return 0
    fi

    if (( SECONDS >= deadline )); then
      echo "expected GC CronJob suspend state ${expected}, got ${observed}" >&2
      return 1
    fi

    sleep 2
  done
}

expect_s3_success() {
  local label="$1"
  shift
  log "${label}"
  if ! "$@"; then
    echo "expected command to succeed: $*" >&2
    return 1
  fi
}

expect_s3_failure() {
  local label="$1"
  shift
  log "${label}"
  if "$@"; then
    echo "expected command to fail: $*" >&2
    return 1
  fi
}

lakefs_api_status() {
  local method="$1"
  local path="$2"
  local body_file="$3"

  curl -sS \
    -o "${body_file}" \
    -w "%{http_code}" \
    -u "${AWS_ACCESS_KEY_ID}:${AWS_SECRET_ACCESS_KEY}" \
    -X "${method}" \
    "http://127.0.0.1:${LAKEFS_PORT}/api/v1${path}"
}

expect_lakefs_api_status() {
  local label="$1"
  local method="$2"
  local path="$3"
  local expected_status="$4"
  local body_file
  local status

  log "${label}"
  body_file="$(mktemp)"
  if ! status="$(lakefs_api_status "${method}" "${path}" "${body_file}")"; then
    echo "lakeFS API request failed: ${method} ${path}" >&2
    cat "${body_file}" >&2 || true
    rm -f "${body_file}"
    return 1
  fi

  if [[ "${status}" != "${expected_status}" ]]; then
    echo "expected HTTP ${expected_status} from ${method} ${path}, got ${status}" >&2
    cat "${body_file}" >&2 || true
    rm -f "${body_file}"
    return 1
  fi

  rm -f "${body_file}"
}

wait_lakefs_api_status() {
  local label="$1"
  local method="$2"
  local path="$3"
  local expected_status="$4"
  local timeout_seconds="${5:-30}"
  local body_file
  local deadline
  local status

  log "${label}"
  body_file="$(mktemp)"
  deadline=$((SECONDS + timeout_seconds))

  while true; do
    if ! status="$(lakefs_api_status "${method}" "${path}" "${body_file}")"; then
      echo "lakeFS API request failed: ${method} ${path}" >&2
      cat "${body_file}" >&2 || true
      rm -f "${body_file}"
      return 1
    fi

    if [[ "${status}" == "${expected_status}" ]]; then
      rm -f "${body_file}"
      return 0
    fi

    if (( SECONDS >= deadline )); then
      echo "expected HTTP ${expected_status} from ${method} ${path} within ${timeout_seconds}s, got ${status}" >&2
      cat "${body_file}" >&2 || true
      rm -f "${body_file}"
      return 1
    fi

    sleep 2
  done
}

write_tmp_object() {
  local message="$1"
  local tmp_object
  tmp_object="$(mktemp)"
  printf '%s\n' "${message}" >"${tmp_object}"
  printf '%s' "${tmp_object}"
}

cleanup_lifecycle_repository() {
  local repository_name="$1"

  delete_lakefs_repository_resource "${repository_name}" >/dev/null 2>&1 || true

  if use_lakefs_credential "${E2E_ADMIN_CREDENTIAL_SECRET}" >/dev/null 2>&1; then
    lakefs_api_status "DELETE" "/repositories/${repository_name}?force=true" /dev/null >/dev/null 2>&1 || true
  fi
}

cleanup_gc_repository() {
  delete_lakefs_repository_resource "${E2E_REPO_GC}" >/dev/null 2>&1 || true
  kubectl delete "lakefsgcpolicy/${E2E_GC_POLICY}" \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s >/dev/null 2>&1 || true
  kubectl delete jobs \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    -l "lakefs.versioneer.at/repo-cr=${E2E_REPO_GC}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s >/dev/null 2>&1 || true

  if use_lakefs_credential "${E2E_ADMIN_CREDENTIAL_SECRET}" >/dev/null 2>&1; then
    lakefs_api_status "DELETE" "/repositories/${E2E_REPO_GC}?force=true" /dev/null >/dev/null 2>&1 || true
  fi

  kubectl run e2e-clean-gc-storage \
    --context "${KIND_CONTEXT}" \
    --namespace "${LAKEFS_NAMESPACE}" \
    --restart=Never \
    --rm \
    -i \
    --image=minio/mc:RELEASE.2025-08-13T08-35-41Z \
    --command -- sh -ec "
      mc alias set e2e ${E2E_S3_ENDPOINT} ${E2E_S3_ACCESS_KEY} ${E2E_S3_SECRET_KEY}
      mc rm --recursive --force e2e/${E2E_S3_BUCKET}/${E2E_REPO_GC} || true
    " >/dev/null 2>&1 || true
}

run_repository_lifecycle_smoke() {
  local timestamp
  local repository_name
  local status=0

  timestamp="$(date -u +%Y%m%d%H%M%S)"
  repository_name="repo-lifecycle-${timestamp}-$$"

  log "Running repository lifecycle smoke for ${repository_name}"
  kubectl apply --context "${KIND_CONTEXT}" -f - <<EOF || status=$?
apiVersion: pkg.internal/v1beta1
kind: LakeFSRepository
metadata:
  name: ${repository_name}
  namespace: ${E2E_NAMESPACE}
spec:
  endpoint: http://lakefs.${LAKEFS_NAMESPACE}.svc
  storageNamespace: s3://${E2E_S3_BUCKET}/${repository_name}
  defaultBranch: main
  credentialsSecretRef:
    name: ${E2E_ADMIN_CREDENTIAL_SECRET}
EOF

  if [[ "${status}" -eq 0 ]]; then
    kubectl wait "lakefsrepository/${repository_name}" \
      -n "${E2E_NAMESPACE}" \
      --for=condition=Ready \
      --context "${KIND_CONTEXT}" \
      --timeout=180s || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    use_lakefs_credential "${E2E_ADMIN_CREDENTIAL_SECRET}" || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    expect_lakefs_api_status "timestamped repository exists" \
      "GET" "/repositories/${repository_name}" "200" || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    log "Deleting timestamped LakeFSRepository resource through the controller"
    kubectl delete "lakefsrepository/${repository_name}" \
      -n "${E2E_NAMESPACE}" \
      --context "${KIND_CONTEXT}" \
      --wait=true \
      --timeout=120s || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    wait_lakefs_api_status "controller-deleted timestamped repository returns 404" \
      "GET" "/repositories/${repository_name}" "404" 30 || status=$?
  fi

  cleanup_lifecycle_repository "${repository_name}"
  return "${status}"
}

run_gc_smoke() {
  local timestamp
  local keep_one
  local keep_two
  local delete_one
  local keep_one_file
  local keep_two_file
  local delete_one_file
  local keep_one_physical
  local keep_two_physical
  local delete_one_physical
  local status=0

  timestamp="$(date -u +%Y%m%d%H%M%S)-$$"
  keep_one="gc_keep_one_${timestamp}"
  keep_two="gc_keep_two_${timestamp}"
  delete_one="gc_delete_one_${timestamp}"

  log "Running managed GC smoke for ${E2E_REPO_GC}"
  cleanup_gc_repository

  kubectl apply --context "${KIND_CONTEXT}" -f - <<EOF || status=$?
apiVersion: pkg.internal/v1beta1
kind: LakeFSGCPolicy
metadata:
  name: ${E2E_GC_POLICY}
  namespace: ${E2E_NAMESPACE}
spec:
  schedule: "* * * * *"
  suspend: true
  retention:
    defaultRetentionDays: 1
  job:
    image: ${E2E_GC_SPARK_IMAGE}
    successfulJobsHistoryLimit: 1
    failedJobsHistoryLimit: 1
    backoffLimit: 0
    ttlSecondsAfterFinished: 300
    env:
    - name: S3_ACCESS_KEY
      value: ${E2E_S3_ACCESS_KEY}
    - name: S3_SECRET_KEY
      value: ${E2E_S3_SECRET_KEY}
  spark:
    conf:
    - name: spark.hadoop.fs.s3a.endpoint
      value: ${E2E_S3_ENDPOINT}
    - name: spark.hadoop.fs.s3a.path.style.access
      value: "true"
    - name: spark.hadoop.fs.s3a.connection.ssl.enabled
      value: "false"
    - name: spark.hadoop.fs.s3a.access.key
      value: \$(S3_ACCESS_KEY)
    - name: spark.hadoop.fs.s3a.secret.key
      value: \$(S3_SECRET_KEY)
    - name: spark.hadoop.fs.s3a.aws.credentials.provider
      value: org.apache.hadoop.fs.s3a.SimpleAWSCredentialsProvider
    - name: spark.hadoop.fs.s3a.impl
      value: org.apache.hadoop.fs.s3a.S3AFileSystem
    - name: spark.hadoop.fs.s3.impl
      value: org.apache.hadoop.fs.s3a.S3AFileSystem
    - name: spark.hadoop.lakefs.debug.gc.uncommitted_min_age_seconds
      value: "1"
    args:
    - us-east-1
---
apiVersion: pkg.internal/v1beta1
kind: LakeFSRepository
metadata:
  name: ${E2E_REPO_GC}
  namespace: ${E2E_NAMESPACE}
spec:
  endpoint: http://lakefs.${LAKEFS_NAMESPACE}.svc
  storageNamespace: ${E2E_STORAGE_NAMESPACE_GC}
  defaultBranch: main
  credentialsSecretRef:
    name: ${E2E_ADMIN_CREDENTIAL_SECRET}
  gc:
    enabled: true
    policyRef:
      name: ${E2E_GC_POLICY}
EOF

  if [[ "${status}" -eq 0 ]]; then
    kubectl wait "lakefsgcpolicy/${E2E_GC_POLICY}" \
      -n "${E2E_NAMESPACE}" \
      --for=condition=Ready \
      --context "${KIND_CONTEXT}" \
      --timeout=120s || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    kubectl wait "lakefsrepository/${E2E_REPO_GC}" \
      -n "${E2E_NAMESPACE}" \
      --for=condition=Ready \
      --context "${KIND_CONTEXT}" \
      --timeout=180s || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    local schedule
    local suspend
    schedule="$(kubectl get "cronjob/${E2E_REPO_GC}-gc" \
      -n "${E2E_NAMESPACE}" \
      --context "${KIND_CONTEXT}" \
      -o jsonpath='{.spec.schedule}')"
    if [[ "${schedule}" != "* * * * *" ]]; then
      echo "expected GC CronJob schedule '* * * * *', got '${schedule}'" >&2
      status=1
    fi
    suspend="$(kubectl get "cronjob/${E2E_REPO_GC}-gc" \
      -n "${E2E_NAMESPACE}" \
      --context "${KIND_CONTEXT}" \
      -o jsonpath='{.spec.suspend}')"
    if [[ "${suspend}" != "true" ]]; then
      echo "expected GC CronJob to start suspended, got '${suspend}'" >&2
      status=1
    fi
  fi

  if [[ "${status}" -eq 0 ]]; then
    kubectl delete jobs \
      -n "${E2E_NAMESPACE}" \
      --context "${KIND_CONTEXT}" \
      -l "lakefs.versioneer.at/repo-cr=${E2E_REPO_GC}" \
      --ignore-not-found=true \
      --wait=true \
      --timeout=120s || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    use_lakefs_credential "${E2E_ADMIN_CREDENTIAL_SECRET}" || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    keep_one_file="$(write_tmp_object "${keep_one}")"
    keep_two_file="$(write_tmp_object "${keep_two}")"
    delete_one_file="$(write_tmp_object "${delete_one}")"

    expect_s3_success "write uncommitted keep-one object" lakefs_s3 s3 cp "${keep_one_file}" "s3://${E2E_REPO_GC}/main/gc/keep-one.txt" || status=$?
    expect_s3_success "write uncommitted keep-two object" lakefs_s3 s3 cp "${keep_two_file}" "s3://${E2E_REPO_GC}/main/gc/keep-two.txt" || status=$?
    expect_s3_success "write uncommitted delete-one object" lakefs_s3 s3 cp "${delete_one_file}" "s3://${E2E_REPO_GC}/main/gc/delete-one.txt" || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    keep_one_physical="$(lakefs_object_physical_address "${E2E_REPO_GC}" "gc/keep-one.txt")" || status=$?
    keep_two_physical="$(lakefs_object_physical_address "${E2E_REPO_GC}" "gc/keep-two.txt")" || status=$?
    delete_one_physical="$(lakefs_object_physical_address "${E2E_REPO_GC}" "gc/delete-one.txt")" || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    expect_s3_success "delete one uncommitted object through lakeFS" lakefs_s3 s3 rm "s3://${E2E_REPO_GC}/main/gc/delete-one.txt" || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    wait_minio_object_state "deleted backing object exists before GC" "${delete_one_physical}" "true" 60 || status=$?
    wait_minio_object_state "kept backing object exists before GC" "${keep_one_physical}" "true" 60 || status=$?
    wait_minio_object_state "second kept backing object exists before GC" "${keep_two_physical}" "true" 60 || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    kubectl delete jobs \
      -n "${E2E_NAMESPACE}" \
      --context "${KIND_CONTEXT}" \
      -l "lakefs.versioneer.at/repo-cr=${E2E_REPO_GC}" \
      --ignore-not-found=true \
      --wait=true \
      --timeout=120s || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    sleep 2
  fi

  if [[ "${status}" -eq 0 ]]; then
    log "Enabling managed GC CronJob for ${E2E_REPO_GC}"
    kubectl patch "lakefsgcpolicy/${E2E_GC_POLICY}" \
      -n "${E2E_NAMESPACE}" \
      --context "${KIND_CONTEXT}" \
      --type=merge \
      -p '{"spec":{"suspend":false}}' || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    kubectl wait "lakefsgcpolicy/${E2E_GC_POLICY}" \
      -n "${E2E_NAMESPACE}" \
      --for=condition=Ready \
      --context "${KIND_CONTEXT}" \
      --timeout=120s || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    wait_for_gc_cronjob_suspend_state "${E2E_REPO_GC}" "false" 120 || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    wait_for_gc_job_complete "${E2E_REPO_GC}" 600 || status=$?
  fi

  if [[ "${status}" -eq 0 ]]; then
    wait_minio_object_state "deleted backing object collected by GC" "${delete_one_physical}" "false" 180 || status=$?
    wait_minio_object_state "kept backing object remains after GC" "${keep_one_physical}" "true" 60 || status=$?
    wait_minio_object_state "second kept backing object remains after GC" "${keep_two_physical}" "true" 60 || status=$?
    expect_s3_success "kept uncommitted objects remain readable through lakeFS" lakefs_s3 s3 ls "s3://${E2E_REPO_GC}/main/gc/" || status=$?
  fi

  rm -f "${keep_one_file:-}" "${keep_two_file:-}" "${delete_one_file:-}"
  cleanup_gc_repository
  return "${status}"
}

run_s3_smoke() {
  log "Running S3 authorization matrix through lakeFS"

  local tmp_object
  tmp_object="$(write_tmp_object "hello from lakefs-oss-contrib e2e")"

  use_lakefs_credential "${E2E_ADMIN_CREDENTIAL_SECRET}"
  expect_s3_success "admin-user can list all repositories" lakefs_s3 s3 ls
  expect_s3_success "admin-user can write ${E2E_REPO_A}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_A}/main/admin-user/repo-a.txt"
  expect_s3_success "admin-user can write ${E2E_REPO_B}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_B}/main/admin-user/repo-b.txt"
  expect_s3_success "admin-user can read ${E2E_REPO_A}" lakefs_s3 s3 ls "s3://${E2E_REPO_A}/main/admin-user/"
  expect_s3_success "admin-user can read ${E2E_REPO_B}" lakefs_s3 s3 ls "s3://${E2E_REPO_B}/main/admin-user/"

  use_lakefs_credential "${E2E_USER_CREDENTIAL_SECRET}"
  expect_s3_success "user can read ${E2E_REPO_A}" lakefs_s3 s3 ls "s3://${E2E_REPO_A}/main/admin-user/"
  expect_s3_success "user can write ${E2E_REPO_A}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_A}/main/user/write.txt"
  expect_s3_failure "user cannot write ${E2E_REPO_B}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_B}/main/user/denied.txt"

  use_lakefs_credential "${E2E_READONLY_CREDENTIAL_SECRET}"
  expect_s3_success "readonly-user can read ${E2E_REPO_B}" lakefs_s3 s3 ls "s3://${E2E_REPO_B}/main/admin-user/"
  expect_s3_failure "readonly-user cannot write ${E2E_REPO_B}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_B}/main/readonly-user/denied.txt"
  expect_s3_failure "readonly-user cannot write ${E2E_REPO_A}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_A}/main/readonly-user/denied.txt"

  use_lakefs_credential "${E2E_ADMIN_CREDENTIAL_SECRET}"
  expect_s3_success "admin-user can write ${E2E_REPO_C}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_C}/main/admin-user/repo-c.txt"

  use_lakefs_credential "${E2E_USER_CREDENTIAL_SECRET}"
  expect_s3_success "group-c gives user write access to ${E2E_REPO_C}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_C}/main/user/group-c.txt"
  expect_s3_success "group-c gives user read access to ${E2E_REPO_C}" lakefs_s3 s3 ls "s3://${E2E_REPO_C}/main/user/"

  use_lakefs_credential "${E2E_READONLY_CREDENTIAL_SECRET}"
  expect_s3_success "group-c gives readonly-user write access to ${E2E_REPO_C}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_C}/main/readonly-user/group-c.txt"
  expect_s3_success "group-c gives readonly-user read access to ${E2E_REPO_C}" lakefs_s3 s3 ls "s3://${E2E_REPO_C}/main/readonly-user/"

  rm -f "${tmp_object}"
}

dump_debug() {
  log "Cluster state"
  kubectl get pods -A --context "${KIND_CONTEXT}" || true
  log "Operator logs"
  kubectl logs deployment/lakefs-oss-contrib-controller-manager \
    -n lakefs-oss-contrib-system \
    --context "${KIND_CONTEXT}" \
    --tail=120 || true
  log "Auth server logs"
  kubectl logs deployment/lakefs-oss-contrib-auth-server \
    -n lakefs-oss-contrib-system \
    --context "${KIND_CONTEXT}" \
    --tail=120 || true
  log "lakeFS logs"
  kubectl logs "deployment/${LAKEFS_RELEASE}" \
    -n "${LAKEFS_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --tail=120 || true
  log "GC jobs"
  kubectl get cronjobs,jobs,pods \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    -l "lakefs.versioneer.at/repo-cr=${E2E_REPO_GC}" || true
}

main() {
  case "${1:-}" in
    -h|--help)
      usage
      exit 0
      ;;
  esac

  require_tool docker
  require_tool kind
  require_tool kubectl
  require_tool helm
  require_tool curl
  require_tool jq
  require_tool aws

  cd "${ROOT_DIR}"

  ensure_registry
  ensure_kind_cluster
  configure_kind_registry
  prepare_gc_spark_image
  ensure_e2e_namespace
  build_and_push_images
  deploy_operator_stack
  verify_platform_endpoints
  deploy_s3_backend
  ensure_lakefs_bootstrap_group
  install_lakefs
  apply_e2e_resources
  wait_for_lakefs_port_forward

  if ! run_repository_lifecycle_smoke; then
    dump_debug
    exit 1
  fi

  if ! run_gc_smoke; then
    dump_debug
    exit 1
  fi

  if ! run_s3_smoke; then
    dump_debug
    exit 1
  fi

  log "lakeFS OSS operator e2e passed"
}

main "$@"
