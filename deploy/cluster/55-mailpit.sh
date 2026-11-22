#!/usr/bin/env bash
# Mailpit, the fake mailbox Keycloak sends verification mail to
# (IMPLEMENTATION-PLAN M3.7). Before Keycloak, whose realm points SMTP at it.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

ensure_namespace keycloak restricted
kubectl apply -f "${CLUSTER_DIR}/manifests/mailpit.yaml"
kubectl -n keycloak rollout status deployment/mailpit --timeout=3m
