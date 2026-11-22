#!/usr/bin/env bash
# Run backend/internal/keycloak's TestLiveKeycloak against this cluster's
# Keycloak, as tenantry-sync with the secret the group sync worker uses: the
# check that the fake Keycloak of the unit tests answers like the real one,
# and that tenantry-sync has the rights the worker needs. Creates and deletes
# one group named tenantry-live-test:<random>; touches no user.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

port=18080
command kubectl --context "${KUBE_CONTEXT}" -n keycloak port-forward svc/keycloak-http "${port}:80" >/dev/null 2>&1 &
pf=$!
trap 'kill "${pf}" 2>/dev/null || true' EXIT
for _ in $(seq 50); do
  curl -fsS -o /dev/null "http://localhost:${port}/realms/tenantry" 2>/dev/null && break
  sleep 0.2
done

# The secret goes into the test's environment, never onto a command line.
KEYCLOAK_TEST_CLIENT_SECRET="$(kubectl -n tenantry-system get secret tenantry-keycloak-sync -o jsonpath='{.data.client-secret}' | base64 -d)"
export KEYCLOAK_TEST_CLIENT_SECRET
export KEYCLOAK_TEST_URL="http://localhost:${port}"
cd "${CLUSTER_DIR}/../../backend" || exit 1
go test -count=1 -run '^TestLiveKeycloak$' -v ./internal/keycloak
