#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HEADLAMP_CHART="https://github.com/kubernetes-sigs/headlamp/releases/download/headlamp-helm-0.44.0/headlamp-0.44.0.tgz"

CA_CERT="$(kubectl -n identity-system get secret capsule-lab-root-ca -o jsonpath='{.data.tls\.crt}' | base64 -d)"
kubectl -n capsule-system create secret generic capsule-lab-root-ca \
  --from-literal=tls.crt="${CA_CERT}" \
  --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install headlamp "${HEADLAMP_CHART}" \
  --namespace capsule-system \
  --values "${ROOT_DIR}/values/headlamp.yaml" \
  --wait \
  --timeout 10m

kubectl rollout status deployment/headlamp -n capsule-system --timeout=5m
