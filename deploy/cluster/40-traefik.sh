#!/usr/bin/env bash
# Traefik on the control-plane node with the wildcard certificate as its
# default certificate, and the demo app behind it (05 §7).
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

# hostPort is forbidden by both the baseline and the restricted Pod Security
# levels, so this namespace alone is privileged. Traefik itself still runs as
# non-root with every capability dropped.
ensure_namespace ingress-system privileged

# Traefik mounts the certificate's Secret, so it must exist first. The first
# issue takes a minute or two: cert-manager publishes a TXT record on
# Cloudflare and waits until public resolvers see it.
sed -e "s/__APPS_DOMAIN__/${APPS_DOMAIN}/g" -e "s/__CLUSTER_ISSUER__/${CLUSTER_ISSUER}/g" \
  "${CLUSTER_DIR}/manifests/wildcard-certificate.yaml" | kubectl apply -f -
kubectl -n ingress-system wait certificate/mvp-wildcard --for=condition=Ready --timeout=10m

helm upgrade --install traefik traefik \
  --repo https://traefik.github.io/charts \
  --version "${TRAEFIK_VERSION}" \
  --namespace ingress-system \
  --skip-crds \
  --values "${CLUSTER_DIR}/values/traefik.yaml" \
  --set-string "nodeSelector.kubernetes\.io/hostname=${INGRESS_NODE}" \
  --set-string "providers.kubernetesIngress.ingressEndpoint.ip=${PUBLIC_IP}" \
  --wait --timeout 5m

ensure_namespace demo restricted
sed "s/__APPS_DOMAIN__/${APPS_DOMAIN}/g" "${CLUSTER_DIR}/manifests/demo.yaml" | kubectl apply -f -
kubectl -n demo rollout status deployment/whoami --timeout=2m
