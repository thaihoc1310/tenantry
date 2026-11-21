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

field_is() { # resource (kind/name), jsonpath, expected value[, namespace]
  local got
  got="$(kubectl get "$1" ${4:+-n "$4"} -o jsonpath="$2")"
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

# probe MANIFEST NAMESPACE POD: run a one-shot Pod (with whatever else the
# manifest holds) and pass when it prints "ok". The manifest is deleted again
# either way, leftovers of an interrupted run first.
# Probes run inside `if` (in check), where bash ignores set -e: every step
# that can fail ends with `|| return 1`.
probe() {
  local manifest="${CLUSTER_DIR}/manifests/$1" ns="$2" pod="$3" out phase
  kubectl delete -f "${manifest}" --ignore-not-found --wait >/dev/null || return 1
  kubectl apply -f "${manifest}" >/dev/null || return 1
  kubectl -n "${ns}" wait "pod/${pod}" --for=jsonpath='{.status.phase}'=Succeeded --timeout=2m >/dev/null
  phase="$(kubectl -n "${ns}" get pod "${pod}" -o jsonpath='{.status.phase}' 2>&1)"
  out="$(kubectl -n "${ns}" logs "${pod}" 2>&1)"
  kubectl delete -f "${manifest}" --wait >/dev/null || return 1
  [[ "${phase}" == Succeeded && "${out}" == ok ]] || { echo "Pod ${phase}: ${out}"; return 1; }
}
check "a PVC on standard binds and a non-root Pod writes to it" \
  probe verify-storage.yaml cluster-verify storage-probe

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

check "PostgreSQL is ready" \
  field_is statefulset/postgres '{.status.readyReplicas}' 1 tenantry-system
check "each role opens its own database and only that one" \
  probe verify-postgres.yaml tenantry-system postgres-probe

status_is() { # url, expected HTTP status
  local got
  got="$(curl -sS --max-time 10 "${ca_args[@]}" -o /dev/null -w '%{http_code}' "$1")" || return 1
  [[ "${got}" == "$2" ]] || { echo "got ${got}"; return 1; }
}

# keycloak_token: an access token from the confidential client cluster-verify
# (client credentials). The secret reaches curl on stdin, not on its command line.
keycloak_token() {
  local secret
  secret="$(kubectl -n keycloak get secret keycloak-realm -o jsonpath='{.data.CLUSTER_VERIFY_CLIENT_SECRET}' | base64 -d)" || return 1
  printf 'user = "cluster-verify:%s"\n' "${secret}" |
    curl -fsS --max-time 10 "${ca_args[@]}" --config - -d grant_type=client_credentials \
      "https://${AUTH_HOST}/realms/tenantry/protocol/openid-connect/token" |
    python3 -c 'import json, sys; print(json.load(sys.stdin)["access_token"])'
}
jwt_claim() { # token, claim: the claim of the token's payload, as JSON
  python3 -c 'import base64, json, sys
p = sys.argv[1].split(".")[1]
print(json.dumps(json.loads(base64.urlsafe_b64decode(p + "=" * (-len(p) % 4))).get(sys.argv[2])))' "$1" "$2"
}
token_for_kubernetes() {
  local token iss aud
  token="$(keycloak_token)" || return 1
  iss="$(jwt_claim "${token}" iss)" aud="$(jwt_claim "${token}" aud)"
  [[ "${iss}" == "\"https://${AUTH_HOST}/realms/tenantry\"" && "${aud}" == *'"kubernetes"'* ]] ||
    { echo "iss ${iss}, aud ${aud}"; return 1; }
}

check "Keycloak is ready" field_is statefulset/keycloak '{.status.readyReplicas}' 1 keycloak
check "https://${AUTH_HOST} serves the tenantry realm" \
  https_ok "https://${AUTH_HOST}/realms/tenantry/.well-known/openid-configuration" "\"issuer\":\"https://${AUTH_HOST}/realms/tenantry\""
check "the master realm is not reachable from outside" status_is "https://${AUTH_HOST}/realms/master/.well-known/openid-configuration" 404
check "the admin console is not reachable from outside" status_is "https://${AUTH_HOST}/admin/" 404
check "Keycloak issues a token for the kubernetes audience" token_for_kubernetes

exit "${failed}"
