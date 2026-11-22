#!/usr/bin/env bash
# Keycloak with the tenantry realm (IMPLEMENTATION-PLAN M1.7-8), on the
# keycloak database of the PostgreSQL from 50-postgres.sh.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

ensure_namespace keycloak restricted

# Generated once, kept on every re-run (the realm is only imported once, so a
# new value would no longer match what Keycloak stored).
ensure_generated_secret keycloak keycloak-admin password
kubectl -n keycloak patch secret keycloak-admin --type=merge -p '{"stringData":{"username":"admin"}}' >/dev/null
ensure_generated_secret keycloak keycloak-realm \
  PORTAL_CLIENT_SECRET HEADLAMP_CLUSTER_A_CLIENT_SECRET CLUSTER_VERIFY_CLIENT_SECRET \
  DEMO_USER_PASSWORD PLATFORM_ADMIN_PASSWORD

copy_secret_key tenantry-system postgres keycloak-password keycloak keycloak-db password

kubectl -n keycloak create configmap keycloak-realm \
  --from-file=realm-tenantry.json="${CLUSTER_DIR}/../keycloak/realm-tenantry.json" \
  --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install keycloak keycloakx \
  --repo https://codecentric.github.io/helm-charts \
  --version "${KEYCLOAKX_VERSION}" \
  --namespace keycloak \
  --values "${CLUSTER_DIR}/values/keycloak.yaml" \
  --set-string tenantry.authHost="${AUTH_HOST}" \
  --set-string tenantry.portalHost="${PORTAL_HOST}" \
  --set-string tenantry.consoleHost="${CONSOLE_HOST}" \
  --set-string tenantry.clusterIssuer="${CLUSTER_ISSUER}" \
  --wait --timeout 10m

kubectl -n keycloak wait certificate/keycloak-tls --for=condition=Ready --timeout=10m

# The master realm (where the Keycloak admin logs in) is not exposed, so its
# own address is the port-forward one: without this the admin console, opened
# on localhost, would send the login to https://AUTH_HOST/realms/master, which
# Traefik does not route. KC_HOSTNAME_ADMIN alone only moves the console page.
# The bootstrap admin's credentials are already in the container's environment.
# shellcheck disable=SC2016 # expanded inside the container, on purpose
kubectl -n keycloak exec keycloak-0 -- bash -c '
  kcadm=/opt/keycloak/bin/kcadm.sh; cfg=/tmp/kcadm.config
  $kcadm config credentials --config $cfg --server http://localhost:8080 --realm master \
    --user "$KC_BOOTSTRAP_ADMIN_USERNAME" --password "$KC_BOOTSTRAP_ADMIN_PASSWORD" >/dev/null &&
  $kcadm update realms/master --config $cfg -s attributes.frontendUrl=http://localhost:8080; rc=$?
  rm -f $cfg; exit $rc'

# Keycloak imports realm-tenantry.json only when the realm does not exist;
# later changes of the file are applied to the running realm here: realm
# settings, roles and clients. Users, groups, memberships and client secrets
# are left as they are (realm-sync.py).
PORTAL_HOST="${PORTAL_HOST}" CONSOLE_HOST="${CONSOLE_HOST}" \
  "${CLUSTER_DIR}/realm-sync.py" "${CLUSTER_DIR}/../keycloak/realm-tenantry.json" |
  kubectl -n keycloak exec -i keycloak-0 -- bash -s
