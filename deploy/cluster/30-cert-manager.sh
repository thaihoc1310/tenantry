#!/usr/bin/env bash
# cert-manager and the Let's Encrypt ClusterIssuers (05 §7).
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

ensure_namespace cert-manager restricted
helm upgrade --install cert-manager cert-manager \
  --repo https://charts.jetstack.io \
  --version "${CERT_MANAGER_VERSION}" \
  --namespace cert-manager \
  --values "${CLUSTER_DIR}/values/cert-manager.yaml" \
  --wait --timeout 5m

# The Cloudflare token goes from the file straight into the Secret through a
# pipe: never on a command line, in the repo or in the output. The trailing
# newline an editor leaves would break the Authorization header.
if [[ ! -s "${CLOUDFLARE_TOKEN_FILE}" ]]; then
  echo "missing ${CLOUDFLARE_TOKEN_FILE}: Cloudflare API token with Zone.DNS edit on ${DNS_ZONE} (see README)" >&2
  exit 1
fi
tr -d '\r\n' <"${CLOUDFLARE_TOKEN_FILE}" |
  kubectl -n cert-manager create secret generic cloudflare-api-token \
    --from-file=api-token=/dev/stdin --dry-run=client -o yaml |
  kubectl apply -f -

sed "s/__DNS_ZONE__/${DNS_ZONE}/g" "${CLUSTER_DIR}/manifests/cluster-issuers.yaml" | kubectl apply -f -
# Registering the ACME account is what proves the issuer works.
kubectl wait clusterissuer/letsencrypt-staging clusterissuer/letsencrypt --for=condition=Ready --timeout=2m
