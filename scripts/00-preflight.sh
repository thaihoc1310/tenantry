#!/usr/bin/env bash
set -euo pipefail

EXPECTED_CONTEXT="${EXPECTED_CONTEXT:-hocbt@kubernetes}"
CURRENT_CONTEXT="$(kubectl config current-context)"

if [[ "${CURRENT_CONTEXT}" != "${EXPECTED_CONTEXT}" ]]; then
  echo "Refusing to continue: current context is ${CURRENT_CONTEXT}, expected ${EXPECTED_CONTEXT}" >&2
  exit 1
fi

kubectl auth can-i '*' '*' --all-namespaces | grep -qx yes

if kubectl get ippools.crd.projectcalico.org -o jsonpath='{range .items[*]}{.spec.cidr}{"\n"}{end}' | grep -Eq '^10\.240\.(0|64)\.0/18$'; then
  echo "Tenant IPPool CIDRs already exist; deploy will reconcile them."
fi

if kubectl get services --all-namespaces -o jsonpath='{range .items[*]}{.spec.clusterIP}{"\n"}{end}' | grep -qx '10.96.200.10'; then
  EXISTING_SERVICE="$(kubectl get services --all-namespaces -o jsonpath='{range .items[?(@.spec.clusterIP=="10.96.200.10")]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}')"
  if [[ "${EXISTING_SERVICE}" != "identity-system/keycloak" ]]; then
    echo "ClusterIP 10.96.200.10 is already used by ${EXISTING_SERVICE}" >&2
    exit 1
  fi
fi

kubectl get nodes -o wide
kubectl get ippools.crd.projectcalico.org
echo "Preflight passed for ${CURRENT_CONTEXT}."

