#!/usr/bin/env bash
# Run backend/internal/keycloak's TestLiveKeycloak against this cluster's
# Keycloak, as tenantry-sync with the secret the group sync worker uses: the
# check that the fake Keycloak of the unit tests answers like the real one,
# and that tenantry-sync has the rights the worker needs, and no more
# (TestLiveKeycloakNarrowed: password resets, role mappings, impersonation,
# Platform Admins, clients and the realm all refused). Creates and deletes
# groups named tenantry-live-test:<random>; turns the demo user mallory on
# (she is on already). First checks the realm the way keycloak-fgap.py does:
# no group carries a role mapping, every platform-admin is protected.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

port=18080
command kubectl --context "${KUBE_CONTEXT}" -n keycloak port-forward svc/keycloak-http "${port}:80" >/dev/null 2>&1 &
pf=$!
trap 'kill "${pf}" 2>/dev/null || true' EXIT
for _ in $(seq 50); do
  curl -fsS -o /dev/null "http://localhost:${port}/realms/tenantry" 2>/dev/null && break
  sleep 0.2
done

"${CLUSTER_DIR}/keycloak-fgap.py" check | kubectl -n keycloak exec -i keycloak-0 -- bash -s

# Ids (not secrets) the narrowed test needs, read with the bootstrap admin in the Pod.
ids="$(kubectl -n keycloak exec -i keycloak-0 -- bash -s <<'SH'
set -euo pipefail
kcadm=/opt/keycloak/bin/kcadm.sh; cfg=$(mktemp); trap 'rm -f "$cfg"' EXIT
$kcadm config credentials --config "$cfg" --server http://localhost:8080 --realm master \
  --user "$KC_BOOTSTRAP_ADMIN_USERNAME" --password "$KC_BOOTSTRAP_ADMIN_PASSWORD" >/dev/null
k() { $kcadm "$@" --config "$cfg" -r tenantry; }
marker=""
while IFS=, read -r i n; do [ "$n" = "tenantry:platform-admins" ] && marker=$i; done <<< "$(k get groups -q search=tenantry:platform-admins -q exact=true --fields id,name --format csv --noquotes)"
echo "$(k get users -q username=mallory -q exact=true --fields id --format csv --noquotes) $(k get users -q username=admin -q exact=true --fields id --format csv --noquotes) $(k get roles/platform-admin --fields id --format csv --noquotes) ${marker}"
SH
)"
read -r KEYCLOAK_TEST_USER_ID KEYCLOAK_TEST_ADMIN_ID KEYCLOAK_TEST_ADMIN_ROLE_ID KEYCLOAK_TEST_MARKER_ID <<< "${ids}"
export KEYCLOAK_TEST_USER_ID KEYCLOAK_TEST_ADMIN_ID KEYCLOAK_TEST_ADMIN_ROLE_ID KEYCLOAK_TEST_MARKER_ID
# The narrowed checks run once the permissions are on (none in the rollback state).
[ -z "${KEYCLOAK_TEST_MARKER_ID}" ] || export KEYCLOAK_TEST_FGAP=1

# The secret goes into the test's environment, never onto a command line.
KEYCLOAK_TEST_CLIENT_SECRET="$(kubectl -n tenantry-system get secret tenantry-keycloak-sync -o jsonpath='{.data.client-secret}' | base64 -d)"
export KEYCLOAK_TEST_CLIENT_SECRET
export KEYCLOAK_TEST_URL="http://localhost:${port}"
cd "${CLUSTER_DIR}/../../backend" || exit 1
go test -count=1 -run '^TestLiveKeycloak(Narrowed)?$' -v ./internal/keycloak
