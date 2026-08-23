#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

for script in \
  00-preflight.sh \
  10-cert-manager.sh \
  15-capsule-core.sh \
  20-monitoring.sh \
  30-capsule.sh \
  40-platform.sh \
  50-keycloak.sh \
  60-enable-oidc.sh \
  70-headlamp.sh \
  80-demo-namespaces.sh \
  90-verify.sh; do
  echo
  echo "==> Running ${script}"
  "${ROOT_DIR}/scripts/${script}"
done

echo
echo "Capsule lab deployed. Run ${ROOT_DIR}/scripts/access.sh to open the UIs."
