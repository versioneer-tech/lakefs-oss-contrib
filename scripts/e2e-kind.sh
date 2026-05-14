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
OPERATOR_IMG="${OPERATOR_IMG:-${REGISTRY}/lakefs-oss-contrib:e2e}"
AUTHSERVER_IMG="${AUTHSERVER_IMG:-${REGISTRY}/lakefs-auth-server:e2e}"
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
E2E_GROUP_C="${E2E_GROUP_C:-group-c}"
E2E_S3_BUCKET="${E2E_S3_BUCKET:-e2e-bucket}"
E2E_STORAGE_NAMESPACE_A="${E2E_STORAGE_NAMESPACE_A:-s3://${E2E_S3_BUCKET}/${E2E_REPO_A}}"
E2E_STORAGE_NAMESPACE_B="${E2E_STORAGE_NAMESPACE_B:-s3://${E2E_S3_BUCKET}/${E2E_REPO_B}}"
E2E_STORAGE_NAMESPACE_C="${E2E_STORAGE_NAMESPACE_C:-s3://${E2E_S3_BUCKET}/${E2E_REPO_C}}"
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
  - install lakeFS with stats.enabled=false
  - create three LakeFSUser objects, one LakeFSGroup, credentials, roles, role bindings, and three repositories
  - verify admin, owner, viewer, and group-inherited S3 access through lakeFS

Useful environment variables:
  KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME}
  REGISTRY=${REGISTRY}
  OPERATOR_IMG=${OPERATOR_IMG}
  AUTHSERVER_IMG=${AUTHSERVER_IMG}
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
      -e "s#ghcr.io/versioneer-tech/lakefs-oss-contrib:latest#${OPERATOR_IMG}#g" \
      -e "s#ghcr.io/versioneer-tech/lakefs-auth-server:latest#${AUTHSERVER_IMG}#g" \
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

apply_e2e_resources() {
  log "Resetting previous e2e LakeFS resources"
  kubectl delete \
    "lakefsrepository/${E2E_REPO_A}" \
    "lakefsrepository/${E2E_REPO_B}" \
    "lakefsrepository/${E2E_REPO_C}" \
    -n "${E2E_NAMESPACE}" \
    --context "${KIND_CONTEXT}" \
    --ignore-not-found=true \
    --wait=true \
    --timeout=120s
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
  kubectl delete "lakefsgroup/${E2E_GROUP_C}" lakefsgroup/group-admins \
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
  local access_key_id="$1"
  local secret_name="$2"
  unset AWS_PROFILE AWS_SESSION_TOKEN
  export AWS_ACCESS_KEY_ID="${access_key_id}"
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

write_tmp_object() {
  local message="$1"
  local tmp_object
  tmp_object="$(mktemp)"
  printf '%s\n' "${message}" >"${tmp_object}"
  printf '%s' "${tmp_object}"
}

run_s3_smoke() {
  log "Running S3 authorization matrix through lakeFS"

  local tmp_object
  tmp_object="$(write_tmp_object "hello from lakefs-oss-contrib e2e")"

  use_lakefs_credential "${E2E_ADMIN_ACCESS_KEY_ID}" "${E2E_ADMIN_CREDENTIAL_SECRET}"
  expect_s3_success "admin-user can list all repositories" lakefs_s3 s3 ls
  expect_s3_success "admin-user can write ${E2E_REPO_A}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_A}/main/admin-user/repo-a.txt"
  expect_s3_success "admin-user can write ${E2E_REPO_B}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_B}/main/admin-user/repo-b.txt"
  expect_s3_success "admin-user can read ${E2E_REPO_A}" lakefs_s3 s3 ls "s3://${E2E_REPO_A}/main/admin-user/"
  expect_s3_success "admin-user can read ${E2E_REPO_B}" lakefs_s3 s3 ls "s3://${E2E_REPO_B}/main/admin-user/"

  use_lakefs_credential "${E2E_USER_ACCESS_KEY_ID}" "${E2E_USER_CREDENTIAL_SECRET}"
  expect_s3_success "user can read ${E2E_REPO_A}" lakefs_s3 s3 ls "s3://${E2E_REPO_A}/main/admin-user/"
  expect_s3_success "user can write ${E2E_REPO_A}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_A}/main/user/write.txt"
  expect_s3_failure "user cannot write ${E2E_REPO_B}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_B}/main/user/denied.txt"

  use_lakefs_credential "${E2E_READONLY_ACCESS_KEY_ID}" "${E2E_READONLY_CREDENTIAL_SECRET}"
  expect_s3_success "readonly-user can read ${E2E_REPO_B}" lakefs_s3 s3 ls "s3://${E2E_REPO_B}/main/admin-user/"
  expect_s3_failure "readonly-user cannot write ${E2E_REPO_B}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_B}/main/readonly-user/denied.txt"
  expect_s3_failure "readonly-user cannot write ${E2E_REPO_A}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_A}/main/readonly-user/denied.txt"

  use_lakefs_credential "${E2E_ADMIN_ACCESS_KEY_ID}" "${E2E_ADMIN_CREDENTIAL_SECRET}"
  expect_s3_success "admin-user can write ${E2E_REPO_C}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_C}/main/admin-user/repo-c.txt"

  use_lakefs_credential "${E2E_USER_ACCESS_KEY_ID}" "${E2E_USER_CREDENTIAL_SECRET}"
  expect_s3_success "group-c gives user write access to ${E2E_REPO_C}" lakefs_s3 s3 cp "${tmp_object}" "s3://${E2E_REPO_C}/main/user/group-c.txt"
  expect_s3_success "group-c gives user read access to ${E2E_REPO_C}" lakefs_s3 s3 ls "s3://${E2E_REPO_C}/main/user/"

  use_lakefs_credential "${E2E_READONLY_ACCESS_KEY_ID}" "${E2E_READONLY_CREDENTIAL_SECRET}"
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
  require_tool aws

  cd "${ROOT_DIR}"

  ensure_registry
  ensure_kind_cluster
  configure_kind_registry
  ensure_e2e_namespace
  build_and_push_images
  deploy_operator_stack
  verify_platform_endpoints
  deploy_s3_backend
  install_lakefs
  apply_e2e_resources
  wait_for_lakefs_port_forward

  if ! run_s3_smoke; then
    dump_debug
    exit 1
  fi

  log "lakeFS OSS operator e2e passed"
}

main "$@"
