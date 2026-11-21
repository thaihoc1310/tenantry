# shellcheck shell=bash disable=SC2034 # the variables are used by the scripts that source this file
# Settings shared by every script in deploy/cluster. Sourced, never run.
# Each script sources this first, so running one on its own is safe too.
set -euo pipefail

# The cluster these scripts are written for. Every kubectl and helm call is
# pinned to this context, whatever `kubectl config current-context` says, so
# the scripts cannot hit another cluster.
KUBE_CONTEXT="${KUBE_CONTEXT:-hocbt@kubernetes}"
kubectl() { command kubectl --context "${KUBE_CONTEXT}" "$@"; }
export HELM_KUBECONTEXT="${KUBE_CONTEXT}"

# Node layout (architecture/05-network-and-scheduling.md §1): Traefik on the
# control-plane node, where the public IP lands; central plane and cluster
# components on the system pool; tenant workloads on the shared pool.
INGRESS_NODE="${INGRESS_NODE:-fdp-k8s-cp1}"
SYSTEM_NODES="${SYSTEM_NODES:-fdp-k8s-wk1}"
SHARED_NODES="${SHARED_NODES:-fdp-k8s-wk2 fdp-k8s-wk3}"

# NFS export behind StorageClass standard (05 §9): one VM outside the cluster.
NFS_SERVER="${NFS_SERVER:-172.21.92.161}"
NFS_SHARE="${NFS_SHARE:-/srv/tenantry}"

# Chart versions, pinned: re-running cluster-up must install the same thing.
CSI_DRIVER_NFS_VERSION=4.13.6

CLUSTER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! command kubectl config get-contexts "${KUBE_CONTEXT}" >/dev/null 2>&1; then
  echo "kubeconfig has no context ${KUBE_CONTEXT} (set KUBE_CONTEXT to override)" >&2
  exit 1
fi
