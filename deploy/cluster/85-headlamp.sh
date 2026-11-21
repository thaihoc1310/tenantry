#!/usr/bin/env bash
# Headlamp at https://CONSOLE_HOST (IMPLEMENTATION-PLAN M1.9), signing users in
# with the Keycloak client headlamp-cluster-a.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

ensure_namespace headlamp-system restricted

# Roots of the certificates Headlamp has to trust: Keycloak's (OIDC) and
# Capsule Proxy's (the Kubernetes API). Rewritten when CLUSTER_ISSUER changes.
letsencrypt_roots |
  kubectl -n headlamp-system create configmap letsencrypt-roots --from-file=ca.crt=/dev/stdin \
    --dry-run=client -o yaml | kubectl apply -f -

# OIDC settings for the chart's externalSecret. The client secret is the one
# the realm was imported with; it goes from Secret to Secret through a pipe.
{
  printf 'OIDC_CLIENT_ID=headlamp-cluster-a\n'
  printf 'OIDC_ISSUER_URL=https://%s/realms/tenantry\n' "${AUTH_HOST}"
  printf 'OIDC_CLIENT_SECRET=%s\n' "$(kubectl -n keycloak get secret keycloak-realm \
    -o jsonpath='{.data.HEADLAMP_CLUSTER_A_CLIENT_SECRET}' | base64 -d)"
} | kubectl -n headlamp-system create secret generic headlamp-oidc --from-env-file=/dev/stdin \
  --dry-run=client -o yaml | kubectl apply -f -

ingress_ip="$(kubectl get node "${INGRESS_NODE}" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')"

# Restart when the CA bundle or the OIDC settings change: Headlamp reads them at start.
checksum="$(kubectl -n headlamp-system get configmap/letsencrypt-roots secret/headlamp-oidc -o jsonpath='{.items[*].data}' | sha256sum | cut -c1-16)"

helm upgrade --install headlamp headlamp \
  --repo https://kubernetes-sigs.github.io/headlamp/ \
  --version "${HEADLAMP_VERSION}" \
  --namespace headlamp-system \
  --values "${CLUSTER_DIR}/values/headlamp.yaml" \
  --values - --wait --timeout 5m <<YAML
config:
  extraArgs:
    - -oidc-callback-url=https://${CONSOLE_HOST}/oidc-callback
    - -oidc-scopes=profile,email
    - -oidc-use-pkce
    - -oidc-ca-file=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
podAnnotations:
  checksum/config: "${checksum}"
env:
  - name: KUBERNETES_SERVICE_HOST
    value: ${KUBE_HOST}
  - name: KUBERNETES_SERVICE_PORT
    value: "443"
hostAliases:
  - ip: ${ingress_ip}
    hostnames: [${KUBE_HOST}]
ingress:
  enabled: true
  ingressClassName: platform-shared
  annotations:
    cert-manager.io/cluster-issuer: ${CLUSTER_ISSUER}
  hosts:
    - host: ${CONSOLE_HOST}
      paths:
        - path: /
          type: Prefix
  tls:
    - secretName: console-a-tls
      hosts: [${CONSOLE_HOST}]
YAML

kubectl -n headlamp-system wait certificate/console-a-tls --for=condition=Ready --timeout=10m
