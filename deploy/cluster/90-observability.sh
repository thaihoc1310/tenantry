#!/usr/bin/env bash
# A light Prometheus and Grafana on the system pool (IMPLEMENTATION-PLAN M12
# item 1), Namespace observability. Reach Grafana with
#   kubectl -n observability port-forward svc/grafana 3000:80
# and its admin password in the Secret grafana.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

ensure_namespace observability restricted

# Prometheus reads the operator's metrics as a scraper allowed to get /metrics.
kubectl create clusterrolebinding observability-tenantry-operator-metrics \
  --clusterrole=tenantry-operator-metrics-reader --serviceaccount=observability:prometheus-server \
  --dry-run=client -o yaml | kubectl apply -f -

# The dashboard, its data source set to the one provisioned below.
# shellcheck disable=SC2016 # ${DS_PROMETHEUS} is literal text of the JSON
kubectl -n observability create configmap grafana-dashboards-tenantry \
  --from-file=tenantry-operator.json=<(sed 's/\${DS_PROMETHEUS}/prometheus/g' "${CLUSTER_DIR}/../observability/tenantry-operator-dashboard.json") \
  --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install prometheus prometheus --version 29.47.2 \
  --repo https://prometheus-community.github.io/helm-charts \
  --namespace observability --values "${CLUSTER_DIR}/values/prometheus.yaml" --wait --timeout 10m
helm upgrade --install grafana grafana --version 10.5.13 \
  --repo https://grafana.github.io/helm-charts \
  --namespace observability --values "${CLUSTER_DIR}/values/grafana.yaml" --wait --timeout 10m
