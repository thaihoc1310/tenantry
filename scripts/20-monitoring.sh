#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

helm upgrade --install kube-prometheus-stack \
  oci://ghcr.io/prometheus-community/charts/kube-prometheus-stack \
  --version 88.3.0 \
  --namespace monitoring-system \
  --create-namespace \
  --values "${ROOT_DIR}/values/monitoring.yaml" \
  --wait \
  --timeout 15m

