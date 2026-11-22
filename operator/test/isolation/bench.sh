#!/usr/bin/env bash
# Control-plane benchmark (PRODUCT-PLAN §24.6): runs TestBenchmark for each N
# and counts the API requests the operator and Capsule made during the run,
# from their client metrics (rest_client_requests_total); the API server's
# own apiserver_request_total has no user label, so its delta (all clients,
# the backend worker included) is reported apart. Prints one CSV line per N.
#   bench.sh 10 20 50        (KUBE_CONTEXT, default hocbt@kubernetes)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
ctx="${KUBE_CONTEXT:-hocbt@kubernetes}"
k() { kubectl --context "${ctx}" "$@"; }

# A throwaway scraper allowed to get /metrics, outside tenantry-operator
# (that Namespace's ServiceAccounts are Capsule users).
k -n default create serviceaccount optest-bench-scraper --dry-run=client -o yaml | k apply -f - >/dev/null
k create clusterrolebinding optest-bench-scraper --clusterrole=tenantry-operator-metrics-reader \
  --serviceaccount=default:optest-bench-scraper --dry-run=client -o yaml | k apply -f - >/dev/null
cleanup() {
  [[ -n "${pf:-}" ]] && kill "${pf}" 2>/dev/null || true
  k delete clusterrolebinding optest-bench-scraper --ignore-not-found >/dev/null
  k -n default delete serviceaccount optest-bench-scraper --ignore-not-found >/dev/null
}
trap cleanup EXIT
token="$(k -n default create token optest-bench-scraper --duration=2h)"
k -n tenantry-operator port-forward svc/tenantry-operator-metrics 18443:8443 >/dev/null 2>&1 &
pf=$!

sum() { awk '/^rest_client_requests_total\{/ {s += $NF} END {printf "%d", s}'; }
operator_requests() { curl -sk -H "Authorization: Bearer ${token}" https://127.0.0.1:18443/metrics | sum; }
capsule_requests() {
  k get --raw "/api/v1/namespaces/capsule-system/services/http:capsule-controller-manager-metrics-service:8080/proxy/metrics" | sum
}

# The port-forward takes a few seconds; a count of 0 would be a lie.
for _ in $(seq 30); do
  curl -sfk -o /dev/null -H "Authorization: Bearer ${token}" https://127.0.0.1:18443/metrics && break
  sleep 1
done
curl -sfk -o /dev/null -H "Authorization: Bearer ${token}" https://127.0.0.1:18443/metrics ||
  { echo "cannot read the operator's metrics through the port-forward" >&2; exit 1; }

echo "n,ready_p50,ready_p95,ready_max,all_ready,all_deleted,apiserver_requests_all,operator_requests,capsule_requests,operator_cpu_peak,operator_mem_peak,capsule_cpu_peak,capsule_mem_peak"
for n in "$@"; do
  op0="$(operator_requests)" cap0="$(capsule_requests)"
  out="$(KUBE_CONTEXT="${ctx}" BENCH_N="${n}" go test -tags lab -count=1 -v -timeout 40m -run TestBenchmark ./test/isolation 2>&1)" ||
    { echo "${out}" | tail -20 >&2; exit 1; }
  op1="$(operator_requests)" cap1="$(capsule_requests)"
  line="$(grep -o 'BENCH .*' <<<"${out}")"
  val() { sed -E "s/.* $1=([^ ]+).*/\1/" <<<"${line}"; }
  echo "${n},$(val ready_p50),$(val ready_p95),$(val ready_max),$(val all_ready),$(val all_deleted),$(val apiserver_requests),$((op1 - op0)),$((cap1 - cap0)),$(val operator_cpu_peak),$(val operator_mem_peak),$(val capsule_cpu_peak),$(val capsule_mem_peak)"
done
