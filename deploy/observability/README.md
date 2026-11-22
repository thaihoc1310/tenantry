# Observability

`tenantry-operator-dashboard.json` is a Grafana dashboard of the Workspace Operator's metrics. Import it in
Grafana and pick the Prometheus data source. It shows:
- Workspaces by phase;
- provision duration;
- reconcile rate, errors and duration;
- work queue depth;
- each Workspace's IP usage;
- shared pool capacity;
- Routes not ready;
- Capsule Proxy probe results;
- the operator's API requests.

The operator serves the metrics over HTTPS on port 8443, behind the Service `tenantry-operator-metrics`,
when the chart's `metrics.enabled` is true. A scraper needs a token of a ServiceAccount bound to the
ClusterRole `tenantry-operator-metrics-reader`, and must skip TLS verification: the certificate is
self-signed.
