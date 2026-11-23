#!/usr/bin/env bash
# End-to-end tests of the portal on this cluster (IMPLEMENTATION-PLAN M3.8):
# Playwright in web/e2e against https://PORTAL_HOST, the real Keycloak, and
# Mailpit through a port-forward for the verification mail. Each run signs up
# new test accounts named e2e<random> and leaves them behind; each spec
# removes its Organization at its end (one whose address blocks are in
# quarantine is left to make demo-purge-test-data); tests that put something
# on the cluster (a Workspace) check it with kubectl on KUBE_CONTEXT and
# remove it. Extra arguments go to `playwright test`.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

# The port-forward is opened again whenever it drops: a run takes about 20
# minutes, and one dropped forward (a network blip, the API server closing
# the stream) failed every later sign-up with "no verification mail".
port=18025
(
  while true; do
    command kubectl --context "${KUBE_CONTEXT}" -n keycloak port-forward svc/mailpit "${port}:8025" >/dev/null 2>&1
    echo "e2e.sh: $(date +%T) the Mailpit port-forward dropped; opening it again" >&2
    sleep 1
  done
) &
forward=$!
trap 'pkill -P "${forward}" 2>/dev/null; kill "${forward}" 2>/dev/null' EXIT
for _ in $(seq 30); do curl -fsS "http://127.0.0.1:${port}/readyz" >/dev/null 2>&1 && break; sleep 0.5; done

cd "${CLUSTER_DIR}/../../web"
E2E_PORTAL="https://${PORTAL_HOST}" \
E2E_MAILPIT="http://127.0.0.1:${port}" \
E2E_KUBE_CONTEXT="${KUBE_CONTEXT}" \
E2E_ADMIN_PASSWORD="$(kubectl -n keycloak get secret keycloak-realm -o jsonpath='{.data.PLATFORM_ADMIN_PASSWORD}' | base64 -d)" \
  npx playwright test "$@"
