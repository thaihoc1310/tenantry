#!/usr/bin/env bash
# PostgreSQL for the central plane (IMPLEMENTATION-PLAN M1.6): databases
# tenantry and keycloak on one instance, data on StorageClass standard.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

ensure_namespace tenantry-system restricted
helm upgrade --install postgres "${CLUSTER_DIR}/charts/postgres" \
  --namespace tenantry-system \
  --wait --timeout 10m

# The connection string the tenantry-backend chart reads (database.existingSecret).
# The password goes from one Secret to the other through a pipe and printf, a
# shell builtin: it never lands on a command line or in the output.
password="$(kubectl -n tenantry-system get secret postgres -o jsonpath='{.data.tenantry-password}' | base64 -d)"
printf 'postgres://tenantry:%s@postgres.tenantry-system.svc:5432/tenantry?sslmode=disable' "${password}" |
  kubectl -n tenantry-system create secret generic tenantry-db \
    --from-file=DATABASE_URL=/dev/stdin --dry-run=client -o yaml |
  kubectl apply -f -
unset password
