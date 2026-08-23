#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

SOLAR_NODES="${SOLAR_NODES:-fdp-k8s-wk1 fdp-k8s-wk2}"
WATER_NODES="${WATER_NODES:-fdp-k8s-wk3}"

for node in ${SOLAR_NODES}; do
  kubectl get node "${node}" >/dev/null
  kubectl label node "${node}" capsule.clastix.io/tenant=solar --overwrite
done

for node in ${WATER_NODES}; do
  kubectl get node "${node}" >/dev/null
  kubectl label node "${node}" capsule.clastix.io/tenant=water --overwrite
done

prepare_existing_tenant_namespaces() {
  local tenant="$1"
  local selector="capsule.clastix.io/tenant=${tenant}"

  if ! kubectl get tenant "${tenant}" >/dev/null 2>&1; then
    return
  fi

  if [[ -n "$(kubectl get tenant "${tenant}" -o jsonpath='{.spec.nodeSelector}')" ]]; then
    kubectl patch tenant "${tenant}" --type=merge -p '{"spec":{"nodeSelector":null}}'
    kubectl wait --for=condition=Ready "tenant/${tenant}" --timeout=5m
  fi

  while IFS= read -r namespace; do
    [[ -z "${namespace}" ]] && continue
    kubectl annotate namespace "${namespace}" \
      scheduler.alpha.kubernetes.io/node-selector="${selector}" --overwrite
  done < <(kubectl get namespace -l "capsule.clastix.io/tenant=${tenant}" \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
}

prepare_existing_tenant_namespaces solar
prepare_existing_tenant_namespaces water

kubectl apply --server-side -f "${ROOT_DIR}/manifests/calico-pools.yaml"
kubectl apply --server-side -f "${ROOT_DIR}/manifests/tenants.yaml"
kubectl apply --server-side -f "${ROOT_DIR}/manifests/tenant-resources.yaml"

kubectl wait --for=condition=Ready tenant/solar --timeout=5m
kubectl wait --for=condition=Ready tenant/water --timeout=5m
