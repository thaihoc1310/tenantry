#!/usr/bin/env bash
# Install or upgrade the Tenantry charts on this cluster, with the values in
# values/. Needs the foundation (make cluster-up) first. Not numbered: up.sh
# builds the foundation only.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

# The chart's pre-install/pre-upgrade hook migrates the database before the
# new API and worker Pods start.
helm upgrade --install tenantry "${CLUSTER_DIR}/../charts/tenantry-backend" \
  --namespace tenantry-system \
  --values "${CLUSTER_DIR}/values/tenantry-backend.yaml" \
  --wait --timeout 5m
