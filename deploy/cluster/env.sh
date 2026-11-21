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

# Public entry point: the infrastructure NATs this IP to INGRESS_NODE.
PUBLIC_IP="${PUBLIC_IP:-203.29.16.150}"
APPS_DOMAIN="${APPS_DOMAIN:-mvp.tenantry.site}" # tenant Routes: <name>-<workspace>.APPS_DOMAIN
DNS_ZONE="${DNS_ZONE:-tenantry.site}"           # platform hosts live directly under it
# Platform hosts (architecture/01-system-context.md §3). kube-a and console-a
# carry the cluster's letter; a second cluster adds kube-b, console-b.
PORTAL_HOST="portal.${DNS_ZONE}"
AUTH_HOST="auth.${DNS_ZONE}"
KUBE_HOST="kube-a.${DNS_ZONE}"
CONSOLE_HOST="console-a.${DNS_ZONE}"

# Let's Encrypt account (ClusterIssuers letsencrypt-staging and letsencrypt).
# Certificates use CLUSTER_ISSUER; stay on staging until everything works,
# because production limits how many certificates a domain gets per week.
CLUSTER_ISSUER="${CLUSTER_ISSUER:-letsencrypt-staging}"
# API token allowed to edit DNS records of DNS_ZONE only (DNS-01 challenge).
CLOUDFLARE_TOKEN_FILE="${CLOUDFLARE_TOKEN_FILE:-${HOME}/.config/tenantry/cloudflare-token}"

# Chart versions, pinned: re-running cluster-up must install the same thing.
CSI_DRIVER_NFS_VERSION=4.13.6
CERT_MANAGER_VERSION=v1.21.3
TRAEFIK_VERSION=42.3.1 # Traefik v3.7.17
KEYCLOAKX_VERSION=7.4.0 # image pinned in values/keycloak.yaml

CLUSTER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! command kubectl config get-contexts "${KUBE_CONTEXT}" >/dev/null 2>&1; then
  echo "kubeconfig has no context ${KUBE_CONTEXT} (set KUBE_CONTEXT to override)" >&2
  exit 1
fi

# ensure_namespace NAME LEVEL: create the namespace if needed and enforce a
# Pod Security level on it (restricted unless a component cannot run that way).
# Run before installing into it, so the install itself is checked.
ensure_namespace() {
  kubectl create namespace "$1" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl label namespace "$1" "pod-security.kubernetes.io/enforce=$2" --overwrite >/dev/null
}

# ensure_generated_secret NAMESPACE NAME KEY...: create the Secret with a random
# value per key (128 bits, 32 hex characters), only if it does not exist yet; an existing one is
# never touched, so a re-run keeps every password. Values go through printf (a
# shell builtin) into stdin: never on a command line, never printed.
ensure_generated_secret() {
  local ns="$1" name="$2" key
  shift 2
  kubectl -n "${ns}" get secret "${name}" >/dev/null 2>&1 && return 0
  for key in "$@"; do
    printf '%s=%s\n' "${key}" "$(openssl rand -hex 16)"
  done | kubectl -n "${ns}" create secret generic "${name}" --from-env-file=/dev/stdin >/dev/null
  echo "secret/${name} generated in ${ns}"
}

# copy_secret_key FROM_NS FROM_NAME FROM_KEY TO_NS TO_NAME TO_KEY: make TO hold
# the same value, through a pipe.
copy_secret_key() {
  kubectl -n "$1" get secret "$2" -o jsonpath="{.data.${3}}" | base64 -d |
    kubectl -n "$4" create secret generic "$5" --from-file="$6=/dev/stdin" --dry-run=client -o yaml |
    kubectl apply -f - >/dev/null
}
