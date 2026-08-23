#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

helm upgrade --install capsule \
  oci://ghcr.io/projectcapsule/charts/capsule \
  --version 0.13.10 \
  --namespace capsule-system \
  --create-namespace \
  --values "${ROOT_DIR}/values/capsule.yaml" \
  --wait \
  --timeout 10m

helm upgrade --install capsule-proxy \
  oci://ghcr.io/projectcapsule/charts/capsule-proxy \
  --version 0.13.9 \
  --namespace capsule-system \
  --values "${ROOT_DIR}/values/capsule-proxy.yaml" \
  --wait \
  --timeout 10m

kubectl wait --for=condition=Available deployment/capsule-controller-manager -n capsule-system --timeout=5m
kubectl wait --for=condition=Available deployment/capsule-proxy -n capsule-system --timeout=5m
