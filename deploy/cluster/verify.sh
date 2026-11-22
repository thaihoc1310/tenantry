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

# The roots CLUSTER_ISSUER's certificates chain up to: staging roots are in no
# system trust store, so every HTTPS check names the roots explicitly.
le_roots="$(mktemp)"
trap 'rm -f "${le_roots}"' EXIT
letsencrypt_roots >"${le_roots}"
ca_args=(--cacert "${le_roots}")

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
# keycloak_token [CLIENT SECRET_KEY]: the same for another confidential client.
keycloak_token() {
  local client="${1:-cluster-verify}" key="${2:-CLUSTER_VERIFY_CLIENT_SECRET}" secret
  secret="$(kubectl -n keycloak get secret keycloak-realm -o jsonpath="{.data.${key}}" | base64 -d)" || return 1
  printf 'user = "%s:%s"\n' "${client}" "${secret}" |
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

# The group sync worker's service account (tenantry-sync) holds exactly
# manage-users and query-groups of realm-management, and its token carries
# them with the admin API scope; nothing else (IMPLEMENTATION-PLAN §5.2).
sync_token_rights() {
  local token access scope
  token="$(keycloak_token tenantry-sync TENANTRY_SYNC_CLIENT_SECRET)" || return 1
  access="$(jwt_claim "${token}" resource_access)" scope="$(jwt_claim "${token}" scope)"
  [[ "${access}" == '{"realm-management": {"roles": ["manage-users", "query-groups"]}}' ||
    "${access}" == '{"realm-management": {"roles": ["query-groups", "manage-users"]}}' ]] &&
    [[ " ${scope//\"/} " == *" kc-admin-api "* ]] || { echo "resource_access ${access}, scope ${scope}"; return 1; }
}

check "Keycloak is ready" field_is statefulset/keycloak '{.status.readyReplicas}' 1 keycloak
check "https://${AUTH_HOST} serves the tenantry realm" \
  https_ok "https://${AUTH_HOST}/realms/tenantry/.well-known/openid-configuration" "\"issuer\":\"https://${AUTH_HOST}/realms/tenantry\""
check "the master realm is not reachable from outside" status_is "https://${AUTH_HOST}/realms/master/.well-known/openid-configuration" 404
check "the admin console is not reachable from outside" status_is "https://${AUTH_HOST}/admin/" 404
check "Keycloak issues a token for the kubernetes audience" token_for_kubernetes
check "tenantry-sync may manage users and query groups, nothing more" sync_token_rights
check "Mailpit, where Keycloak sends verification mail, is ready" field_is deployment/mailpit '{.status.availableReplicas}' 1 keycloak

# accepts_keycloak_token SERVER CA_FILE: present a Keycloak token alone (a
# kubeconfig with that server and nothing else, so the admin certificate is
# not sent too) and ask who that is: the answer must be oidc:<sub>.
accepts_keycloak_token() {
  local token sub dir got
  token="$(keycloak_token)" || return 1
  sub="$(jwt_claim "${token}" sub | tr -d '"')"
  dir="$(mktemp -d)" && chmod 700 "${dir}"
  cp "$2" "${dir}/ca.crt"
  cat >"${dir}/kubeconfig" <<KUBECONFIG
apiVersion: v1
kind: Config
clusters: [{name: c, cluster: {server: "$1", certificate-authority: ${dir}/ca.crt}}]
users: [{name: u, user: {token: "${token}"}}]
contexts: [{name: c, context: {cluster: c, user: u}}]
current-context: c
KUBECONFIG
  got="$(KUBECONFIG="${dir}/kubeconfig" command kubectl auth whoami -o jsonpath='{.status.userInfo.username}' 2>&1)"
  rm -rf "${dir}"
  [[ "${got}" == "oidc:${sub}" ]] || { echo "got '${got}', want 'oidc:${sub}'"; return 1; }
}
apiserver_ca="$(mktemp)"
trap 'rm -f "${le_roots}" "${apiserver_ca}"' EXIT
command kubectl config view --raw --minify --context "${KUBE_CONTEXT}" \
  -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 -d >"${apiserver_ca}"
apiserver_url="$(command kubectl config view --minify --context "${KUBE_CONTEXT}" -o jsonpath='{.clusters[0].cluster.server}')"
check "kube-apiserver accepts that token as oidc:<sub>" accepts_keycloak_token "${apiserver_url}" "${apiserver_ca}"

# refused_by MESSAGE SELECTOR: the probe Pod with that label is refused, and
# the refusal names the admission plugin's rule.
refused_by() {
  local out
  if out="$(kubectl apply --dry-run=server -l "$2" -f "${CLUSTER_DIR}/manifests/verify-placement.yaml" 2>&1)"; then
    echo "accepted: ${out}"; return 1
  fi
  [[ "${out}" == *"$1"* ]] || { echo "refused for another reason: ${out}"; return 1; }
}
kubectl apply -l '!probe' -f "${CLUSTER_DIR}/manifests/verify-placement.yaml" >/dev/null
check "PodNodeSelector refuses a Pod that leaves its namespace's nodes" \
  refused_by "conflicts with its namespace node label selector" probe=node-selector
check "PodTolerationRestriction refuses a toleration its namespace does not allow" \
  refused_by "conflict with its namespace whitelist" probe=tolerations
check "PodTolerationRestriction accepts a toleration its namespace allows" \
  kubectl apply --dry-run=server -l probe=own-toleration -f "${CLUSTER_DIR}/manifests/verify-placement.yaml"

capsule_users() {
  local got
  got="$(kubectl get capsuleconfiguration default -o jsonpath='{range .spec.users[*]}{.kind}:{.name}{" "}{end}')" || return 1
  [[ "${got}" == "Group:oidc:tenantry:tenant-users Group:system:serviceaccounts:tenantry-operator " ]] ||
    { echo "users are '${got}'"; return 1; }
}
no_rights() { # subject: must not read Pods or Secrets anywhere
  if kubectl auth can-i list secrets --all-namespaces --as="$1" >/dev/null 2>&1 ||
    kubectl auth can-i list pods --all-namespaces --as="$1" >/dev/null 2>&1; then
    echo "$1 can list Pods or Secrets"; return 1
  fi
}

check "Capsule is ready" field_is deployment/capsule-controller-manager '{.status.availableReplicas}' 1 capsule-system
check "Capsule's users are oidc:tenantry:tenant-users and the operator's ServiceAccounts" capsule_users
check "Capsule Proxy is ready" field_is deployment/capsule-proxy '{.status.availableReplicas}' 1 capsule-system
check "https://${KUBE_HOST} (Capsule Proxy) accepts the token as oidc:<sub>" \
  accepts_keycloak_token "https://${KUBE_HOST}" "${le_roots}"
check "Headlamp is ready" field_is deployment/headlamp '{.status.availableReplicas}' 1 headlamp-system
check "https://${CONSOLE_HOST} (Headlamp) answers with a valid certificate" https_ok "https://${CONSOLE_HOST}/" "Headlamp"
check "Headlamp's ServiceAccount has no Kubernetes rights" no_rights system:serviceaccount:headlamp-system:headlamp

# ---- Workspace Operator (make deploy-operator, IMPLEMENTATION-PLAN M6) ------
inventory_complete() {
  local nodes reported
  nodes="$(kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{" "}{end}')" || return 1
  reported="$(kubectl get platformnodeinventory cluster -o jsonpath='{range .status.nodes[*]}{.name}{" "}{end}')" || return 1
  [[ "${reported}" == "${nodes}" ]] || { echo "reports '${reported}', nodes are '${nodes}'"; return 1; }
}
# can_i SUBJECT_ARGS VERB RESOURCE WANT: kubectl auth can-i in cluster-verify.
can_i() {
  local got
  # shellcheck disable=SC2086 # $1 holds several flags
  got="$(kubectl auth can-i $1 "$2" "$3" -n cluster-verify 2>/dev/null)"
  [[ "${got}" == "$4" ]] || { echo "can-i $2 $3 ($1): got ${got}, want $4"; return 1; }
}
# The Developer and Viewer roles, bound in cluster-verify the way Capsule binds
# them in a Tenant's Namespaces (architecture/06 §8; envtest checks more).
tenant_roles() {
  local dev="--as=oidc:verify-dev --as-group=oidc:verify:dev" view="--as=oidc:verify-view --as-group=oidc:verify:view"
  kubectl -n cluster-verify create rolebinding verify-developer --clusterrole=tenantry:tenant-developer \
    --group=oidc:verify:dev --dry-run=client -o yaml | kubectl apply -f - >/dev/null || return 1
  kubectl -n cluster-verify create rolebinding verify-viewer --clusterrole=tenantry:tenant-viewer \
    --group=oidc:verify:view --dry-run=client -o yaml | kubectl apply -f - >/dev/null || return 1
  local ok=0
  can_i "${dev}" create deployments.apps yes && can_i "${dev}" create pods/exec yes &&
    can_i "${dev}" get secrets no && can_i "${dev}" create rolebindings no && can_i "${dev}" create ingresses no &&
    can_i "${view}" list pods yes && can_i "${view}" get pods/log yes &&
    can_i "${view}" get secrets no && can_i "${view}" create pods/exec no && can_i "${view}" delete pods no || ok=1
  kubectl -n cluster-verify delete rolebinding verify-developer verify-viewer >/dev/null || return 1
  return "${ok}"
}
no_operator_rights() { # subject
  if kubectl auth can-i create certificates.cert-manager.io --all-namespaces --as="$1" >/dev/null 2>&1 ||
    kubectl auth can-i get secrets --all-namespaces --as="$1" >/dev/null 2>&1; then
    echo "$1 can write Certificates or read Secrets"; return 1
  fi
}
check "the Workspace Operator is ready" field_is deployment/tenantry-operator '{.status.availableReplicas}' 1 tenantry-operator
check "PlatformNodeInventory reports every node" inventory_complete
check "Developer and Viewer have the rights of architecture/06 §8, no more" tenant_roles
check "the operator may not touch certificates or Secrets" \
  no_operator_rights system:serviceaccount:tenantry-operator:tenantry-operator

exit "${failed}"
