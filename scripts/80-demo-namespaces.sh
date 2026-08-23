#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

create_namespace() {
  local user="$1"
  local owner_group="$2"
  local namespace="$3"
  if ! kubectl get namespace "${namespace}" >/dev/null 2>&1; then
    kubectl --as="${user}" \
      --as-group=projectcapsule.dev \
      --as-group="${owner_group}" \
      create namespace "${namespace}"
  fi
}

create_namespace alice solar-owners solar-dev
create_namespace alice solar-owners solar-prod
create_namespace bob water-owners water-dev
create_namespace bob water-owners water-prod

kubectl --as=alice --as-group=projectcapsule.dev --as-group=solar-owners \
  apply -f "${ROOT_DIR}/manifests/solar-claims.yaml"
kubectl --as=bob --as-group=projectcapsule.dev --as-group=water-owners \
  apply -f "${ROOT_DIR}/manifests/water-claims.yaml"

kubectl wait --for=condition=Ready resourcepoolclaim/namespace-budget -n solar-dev --timeout=2m
kubectl wait --for=condition=Ready resourcepoolclaim/namespace-budget -n solar-prod --timeout=2m
kubectl wait --for=condition=Ready resourcepoolclaim/namespace-budget -n water-dev --timeout=2m
kubectl wait --for=condition=Ready resourcepoolclaim/namespace-budget -n water-prod --timeout=2m

kubectl apply -f "${ROOT_DIR}/manifests/demo-workloads.yaml"
kubectl rollout restart deployment/solar-web -n solar-dev
kubectl rollout restart deployment/water-web -n water-prod
kubectl rollout status deployment/solar-web -n solar-dev --timeout=5m
kubectl rollout status deployment/water-web -n water-prod --timeout=5m
