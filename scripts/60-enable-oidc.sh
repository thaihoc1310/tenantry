#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CA_CERT="$(kubectl -n identity-system get secret capsule-lab-root-ca -o jsonpath='{.data.tls\.crt}' | base64 -d)"

AUTH_CONFIG="$(printf '%s\n' \
  'apiVersion: apiserver.config.k8s.io/v1' \
  'kind: AuthenticationConfiguration' \
  'jwt:' \
  '- issuer:' \
  '    url: https://keycloak.127.0.0.1.nip.io:8443/realms/capsule' \
  '    discoveryURL: https://10.96.200.10:8443/realms/capsule/.well-known/openid-configuration' \
  '    certificateAuthority: |' \
  "$(printf '%s\n' "${CA_CERT}" | sed 's/^/      /')" \
  '    audiences:' \
  '      - headlamp' \
  '      - kubernetes' \
  '    audienceMatchPolicy: MatchAny' \
  '  claimMappings:' \
  '    username:' \
  '      claim: preferred_username' \
  '      prefix: ""' \
  '    groups:' \
  '      claim: groups' \
  '      prefix: ""' \
  '  userValidationRules:' \
  '    - expression: "!user.username.startsWith(\"system:\")"' \
  '      message: username cannot use the reserved system prefix')"

kubectl -n kube-system create configmap capsule-lab-apiserver-auth \
  --from-literal=authentication-config.yaml="${AUTH_CONFIG}" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl delete job capsule-lab-enable-oidc -n kube-system --ignore-not-found
kubectl apply -f "${ROOT_DIR}/manifests/apiserver-patcher.yaml"
kubectl wait --for=condition=Complete job/capsule-lab-enable-oidc -n kube-system --timeout=5m

APISERVER_READY=false
for attempt in $(seq 1 30); do
  if kubectl get --raw=/readyz >/dev/null 2>&1; then
    APISERVER_READY=true
    break
  fi
  sleep 2
done

if [[ "${APISERVER_READY}" != true ]]; then
  echo "kube-apiserver did not become ready in time; inspect the control-plane static pod logs." >&2
  exit 1
fi

# The operator can enter backoff if it starts during the short API outage.
if ! kubectl rollout status deployment/capsule-controller-manager \
  -n capsule-system --timeout=2m; then
  kubectl delete pod -n capsule-system -l app.kubernetes.io/name=capsule
  kubectl rollout status deployment/capsule-controller-manager \
    -n capsule-system --timeout=5m
fi

echo "kube-apiserver is ready with OIDC and PodNodeSelector configuration."
