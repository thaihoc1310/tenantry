#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

helm upgrade --install capsule \
  oci://ghcr.io/projectcapsule/charts/capsule \
  --version 0.13.10 \
  --namespace capsule-system \
  --create-namespace \
  --values "${ROOT_DIR}/values/capsule-bootstrap.yaml" \
  --wait \
  --timeout 10m

helm upgrade --install capsule-proxy \
  oci://ghcr.io/projectcapsule/charts/capsule-proxy \
  --version 0.13.9 \
  --namespace capsule-system \
  --values "${ROOT_DIR}/values/capsule-proxy-bootstrap.yaml" \
  --wait \
  --timeout 10m
