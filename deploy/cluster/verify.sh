#!/usr/bin/env bash
# shellcheck disable=SC2317 # helpers below are called through check()
# Check that the foundation does what it is for. Runs every check, prints one
# line per check, and exits non-zero if any failed.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

failed=0
pass() { echo "PASS  $1"; }
fail() { echo "FAIL  $1${2:+: $2}"; failed=1; }

# check NAME COMMAND...: PASS when COMMAND succeeds, FAIL with its output otherwise.
check() {
  local name="$1" out
  shift
  if out="$("$@" 2>&1)"; then pass "${name}"; else fail "${name}" "${out}"; fi
}

field_is() { # resource (kind/name), jsonpath, expected value
  local got
  got="$(kubectl get "$1" -o jsonpath="$2")"
  [[ "${got}" == "$3" ]] || { echo "got '${got}', want '$3'"; return 1; }
}

for node in ${SYSTEM_NODES}; do
  check "${node} is in the system pool" \
    field_is "node/${node}" '{.metadata.labels.tenantry\.io/compute-pool}' system
  check "${node} keeps other Pods off" \
    field_is "node/${node}" '{.spec.taints[?(@.key=="tenantry.io/compute-pool")].effect}' NoSchedule
done
for node in ${SHARED_NODES}; do
  check "${node} is in the shared pool" \
    field_is "node/${node}" '{.metadata.labels.tenantry\.io/compute-pool}' shared-general
done

# Namespace for the probes below, held to the restricted Pod Security level
# like the namespaces of the platform.
ensure_namespace cluster-verify restricted

check "standard is the default StorageClass" \
  field_is storageclass/standard '{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}' true

# Probes run inside `if` (in check), where bash ignores set -e: every step
# that can fail ends with `|| return 1`.
storage_probe() {
  local manifest="${CLUSTER_DIR}/manifests/verify-storage.yaml" out
  kubectl delete -f "${manifest}" --ignore-not-found --wait >/dev/null || return 1 # leftovers of a failed run
  kubectl apply -f "${manifest}" >/dev/null || return 1
  kubectl -n cluster-verify wait pvc/storage-probe --for=jsonpath='{.status.phase}'=Bound --timeout=1m >/dev/null || return 1
  kubectl -n cluster-verify wait pod/storage-probe --for=jsonpath='{.status.phase}'=Succeeded --timeout=2m >/dev/null \
    || { kubectl -n cluster-verify logs storage-probe; return 1; }
  out="$(kubectl -n cluster-verify logs storage-probe)" || return 1
  kubectl delete -f "${manifest}" --wait >/dev/null || return 1
  [[ "${out}" == ok ]] || { echo "read back '${out}'"; return 1; }
}
check "a PVC on standard binds and a non-root Pod writes to it" storage_probe

certificates_ready() {
  local all not_ready
  all="$(kubectl get certificates -A -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}={.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}')" || return 1
  [[ -n "${all}" ]] || { echo "no Certificate at all"; return 1; }
  not_ready="$(grep -v '=True$' <<<"${all}" || true)"
  [[ -z "${not_ready}" ]] || { echo "${not_ready}"; return 1; }
}
check "every Certificate is Ready" certificates_ready

# Staging certificates chain up to Let's Encrypt's staging roots, which no
# system trusts; fetch them so the HTTPS checks can still verify the chain.
ca_args=()
if [[ "${CLUSTER_ISSUER}" == letsencrypt-staging ]]; then
  staging_roots="$(mktemp)"
  trap 'rm -f "${staging_roots}"' EXIT
  for root in letsencrypt-stg-root-x1 letsencrypt-stg-root-x2; do
    curl -fsS "https://letsencrypt.org/certs/staging/${root}.pem" >>"${staging_roots}"
  done
  ca_args=(--cacert "${staging_roots}")
fi

https_ok() { # url, text the body must contain
  local body
  body="$(curl -fsS --max-time 10 "${ca_args[@]}" "$1")" || return 1
  [[ "${body}" == *"$2"* ]] || { echo "body does not contain '$2'"; return 1; }
}
redirects_to_https() { # host
  local got
  got="$(curl -sS --max-time 10 -o /dev/null -w '%{http_code} %{redirect_url}' "http://$1/")" || return 1
  # 301 for GET (308 for other methods): both are permanent redirects.
  [[ "${got}" == "301 https://$1/" ]] || { echo "got '${got}'"; return 1; }
}
check "https://demo.${APPS_DOMAIN} answers with a valid certificate" https_ok "https://demo.${APPS_DOMAIN}/" "Hostname: whoami"
check "http://demo.${APPS_DOMAIN} redirects to HTTPS" redirects_to_https "demo.${APPS_DOMAIN}"

exit "${failed}"
