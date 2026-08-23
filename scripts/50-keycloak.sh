#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

kubectl apply --server-side -f "${ROOT_DIR}/manifests/keycloak.yaml"
kubectl wait --for=condition=Ready certificate/capsule-lab-root-ca -n identity-system --timeout=5m
kubectl wait --for=condition=Ready certificate/keycloak-tls -n identity-system --timeout=5m
kubectl rollout status deployment/keycloak -n identity-system --timeout=10m

kubectl run oidc-connectivity-test \
  --namespace identity-system \
  --image=curlimages/curl:8.16.0 \
  --restart=Never \
  --rm \
  --attach \
  --overrides='{"spec":{"automountServiceAccountToken":false,"volumes":[{"name":"ca","secret":{"secretName":"capsule-lab-root-ca"}}],"containers":[{"name":"oidc-connectivity-test","image":"curlimages/curl:8.16.0","args":["--fail","--silent","--show-error","--cacert","/ca/tls.crt","https://10.96.200.10:8443/realms/capsule/.well-known/openid-configuration"],"volumeMounts":[{"name":"ca","mountPath":"/ca","readOnly":true}]}]}}'
