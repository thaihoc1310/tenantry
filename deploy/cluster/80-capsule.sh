#!/usr/bin/env bash
# Capsule and Capsule Proxy (IMPLEMENTATION-PLAN M1.9), Capsule Proxy public at
# https://KUBE_HOST.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

ensure_namespace capsule-system restricted

helm upgrade --install capsule oci://ghcr.io/projectcapsule/charts/capsule \
  --version "${CAPSULE_VERSION}" \
  --namespace capsule-system \
  --values "${CLUSTER_DIR}/values/capsule.yaml" \
  --wait --timeout 10m

helm upgrade --install capsule-proxy oci://ghcr.io/projectcapsule/charts/capsule-proxy \
  --version "${CAPSULE_PROXY_VERSION}" \
  --namespace capsule-system \
  --values "${CLUSTER_DIR}/values/capsule-proxy.yaml" \
  --wait --timeout 10m

sed -e "s/__KUBE_HOST__/${KUBE_HOST}/g" -e "s/__CLUSTER_ISSUER__/${CLUSTER_ISSUER}/g" \
  "${CLUSTER_DIR}/manifests/kube-a-ingress.yaml" | kubectl apply -f -
kubectl -n capsule-system wait certificate/kube-a-tls --for=condition=Ready --timeout=10m
