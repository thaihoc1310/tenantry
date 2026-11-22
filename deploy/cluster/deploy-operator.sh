#!/usr/bin/env bash
# Install or upgrade the Workspace Operator on this cluster: its CRDs, its
# ClusterRoles, the cluster's PlatformNodeInventory and the operator itself,
# in its own namespace (architecture/06 §7: Capsule's users include every
# ServiceAccount of that namespace, so nothing else runs there). Needs the
# foundation (make cluster-up) first. Safe to run again: an unchanged image
# keeps its tag, so nothing restarts.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"
ROOT="${CLUSTER_DIR}/../.."
NAMESPACE=tenantry-operator

# ---- image ----------------------------------------------------------------
# As in deploy.sh: no registry yet, so the image goes into the containerd of
# the system nodes over ssh, tagged with a hash of its sources.
tag="src-$({
  cat "${ROOT}/deploy/Dockerfile.operator"
  (cd "${ROOT}/operator" && git ls-files -co --exclude-standard -z | sort -z | xargs -0 sha256sum)
} | sha256sum | cut -c1-12)"
missing=""
for node in ${SYSTEM_NODES}; do
  ssh -o BatchMode=yes "${node}" sudo -n ctr -n k8s.io images ls -q "name==docker.io/tenantry/operator:${tag}" | grep -q . ||
    missing="${missing} ${node}"
done
if [[ -n "${missing}" ]]; then
  docker build -q -f "${ROOT}/deploy/Dockerfile.operator" -t "tenantry/operator:${tag}" -t tenantry/operator:dev "${ROOT}/operator" >/dev/null
  for node in ${missing}; do
    echo "loading tenantry/operator:${tag} into ${node}"
    docker save "tenantry/operator:${tag}" | ssh -o BatchMode=yes "${node}" sudo -n ctr -n k8s.io images import - >/dev/null
  done
fi
echo "image: tenantry/operator:${tag}"

# ---- chart ----------------------------------------------------------------
ensure_namespace "${NAMESPACE}" restricted
helm upgrade --install tenantry-operator "${CLUSTER_DIR}/../charts/tenantry-operator" \
  --namespace "${NAMESPACE}" \
  --values "${CLUSTER_DIR}/values/tenantry-operator.yaml" \
  --set image.tag="${tag}" \
  --wait --timeout 5m
