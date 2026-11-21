#!/usr/bin/env bash
# Stop before changing anything if the tools, the permissions or the nodes are not there.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

for tool in kubectl helm openssl curl python3; do
  command -v "${tool}" >/dev/null || { echo "missing tool: ${tool}" >&2; exit 1; }
done

if [[ "$(kubectl auth can-i '*' '*' --all-namespaces)" != yes ]]; then
  echo "the current user is not cluster-admin" >&2
  exit 1
fi

for node in ${INGRESS_NODE} ${SYSTEM_NODES} ${SHARED_NODES}; do
  kubectl get node "${node}" >/dev/null
done

echo "context ${KUBE_CONTEXT}: ingress ${INGRESS_NODE}, system ${SYSTEM_NODES}, shared ${SHARED_NODES}"
