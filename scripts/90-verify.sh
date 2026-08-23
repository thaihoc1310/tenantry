#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_allowed() {
  local description="$1"
  shift
  if ! "$@" >/dev/null 2>&1; then
    fail "expected allowed: ${description}"
  fi
  echo "PASS allowed: ${description}"
}

assert_denied() {
  local description="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    fail "expected denied: ${description}"
  fi
  echo "PASS denied: ${description}"
}

assert_allowed "Alice lists Solar pods" kubectl --as=alice --as-group=projectcapsule.dev --as-group=solar-owners get pods -n solar-dev
assert_denied "Alice cannot list Water pods" kubectl --as=alice --as-group=projectcapsule.dev --as-group=solar-owners get pods -n water-prod
assert_denied "Alice cannot list kube-system pods" kubectl --as=alice --as-group=projectcapsule.dev --as-group=solar-owners get pods -n kube-system
assert_allowed "Joe edits Solar resources" kubectl --as=joe --as-group=projectcapsule.dev --as-group=solar-developers auth can-i create deployments -n solar-dev
assert_denied "Joe cannot edit Water resources" kubectl --as=joe --as-group=projectcapsule.dev --as-group=solar-developers auth can-i create deployments -n water-prod
assert_denied "Mallory has no tenant access" kubectl --as=mallory --as-group=projectcapsule.dev get pods -n solar-dev
assert_denied "Capsule denies an untrusted registry" kubectl --as=alice --as-group=projectcapsule.dev --as-group=solar-owners create --dry-run=server -f "${ROOT_DIR}/tests/untrusted-pod.yaml"
assert_denied "Capsule denies Burstable pods" kubectl --as=alice --as-group=projectcapsule.dev --as-group=solar-owners create --dry-run=server -f "${ROOT_DIR}/tests/burstable-pod.yaml"
assert_denied "Capsule denies NodePort services" kubectl --as=alice --as-group=projectcapsule.dev --as-group=solar-owners create --dry-run=server -f "${ROOT_DIR}/tests/nodeport-service.yaml"
assert_denied "Solar cannot select a Water compute node" kubectl --as=alice --as-group=projectcapsule.dev --as-group=solar-owners create --dry-run=server -f "${ROOT_DIR}/tests/wrong-node-pod.yaml"
assert_denied "Headlamp ServiceAccount is not cluster-admin" kubectl auth can-i '*' '*' --as=system:serviceaccount:capsule-system:headlamp --all-namespaces
assert_denied "Alice cannot list Nodes through the direct API" kubectl auth can-i list nodes --as=alice --as-group=projectcapsule.dev --as-group=solar-owners
assert_allowed "Headlamp trusts the Keycloak OIDC certificate" \
  kubectl exec -n capsule-system deployment/headlamp -c headlamp -- \
  wget -q -O /dev/null https://keycloak.127.0.0.1.nip.io:8443/realms/capsule/.well-known/openid-configuration

LOCKED_POOL="$(kubectl --as=alice --as-group=projectcapsule.dev --as-group=solar-owners \
  annotate namespace solar-dev cni.projectcalico.org/ipv4pools='["water-ippool"]' \
  --overwrite --dry-run=server \
  -o jsonpath='{.metadata.annotations.cni\.projectcalico\.org/ipv4pools}')"
if [[ "${LOCKED_POOL}" != '["solar-ippool"]' ]]; then
  fail "tenant owner could change the Calico pool: ${LOCKED_POOL}"
fi
echo "PASS locked: Capsule restores the Solar Calico pool annotation"

SOLAR_IP="$(kubectl get pod -n solar-dev -l app=solar-web -o jsonpath='{.items[0].status.podIP}')"
WATER_IP="$(kubectl get pod -n water-prod -l app=water-web -o jsonpath='{.items[0].status.podIP}')"
SOLAR_NODE="$(kubectl get pod -n solar-dev -l app=solar-web -o jsonpath='{.items[0].spec.nodeName}')"
WATER_NODE="$(kubectl get pod -n water-prod -l app=water-web -o jsonpath='{.items[0].spec.nodeName}')"

python3 - "${SOLAR_IP}" "${WATER_IP}" <<'PY'
import ipaddress
import sys

solar = ipaddress.ip_address(sys.argv[1])
water = ipaddress.ip_address(sys.argv[2])
if solar not in ipaddress.ip_network("10.240.0.0/18"):
    raise SystemExit(f"Solar pod IP {solar} is outside solar pool")
if water not in ipaddress.ip_network("10.240.64.0/18"):
    raise SystemExit(f"Water pod IP {water} is outside water pool")
print(f"PASS Solar IPPool: {solar}")
print(f"PASS Water IPPool: {water}")
PY

if [[ "$(kubectl get node "${SOLAR_NODE}" -o jsonpath='{.metadata.labels.capsule\.clastix\.io/tenant}')" != solar ]]; then
  fail "Solar pod is running outside the Solar compute pool: ${SOLAR_NODE}"
fi
echo "PASS Solar compute pool: ${SOLAR_NODE}"

if [[ "$(kubectl get node "${WATER_NODE}" -o jsonpath='{.metadata.labels.capsule\.clastix\.io/tenant}')" != water ]]; then
  fail "Water pod is running outside the Water compute pool: ${WATER_NODE}"
fi
echo "PASS Water compute pool: ${WATER_NODE}"

SOLAR_POD="$(kubectl get pod -n solar-dev -l app=solar-web -o jsonpath='{.items[0].metadata.name}')"
assert_allowed "Solar reaches a service in the same tenant" \
  kubectl exec -n solar-dev "${SOLAR_POD}" -- wget -q -T 3 -O /dev/null http://solar-web.solar-dev.svc.cluster.local
assert_denied "Solar cannot reach a service in the Water tenant" \
  kubectl exec -n solar-dev "${SOLAR_POD}" -- wget -q -T 3 -O /dev/null http://water-web.water-prod.svc.cluster.local

for namespace in solar-dev solar-prod water-dev water-prod; do
  kubectl get limitrange/tenant-default-limits -n "${namespace}" >/dev/null
  kubectl get networkpolicy/tenant-isolation -n "${namespace}" >/dev/null
  kubectl get configmap/tenant-info -n "${namespace}" >/dev/null
  echo "PASS Capsule replication: ${namespace}"
done

kubectl get tenant solar water
kubectl get resourcepool solar-compute water-compute
kubectl get ippools.crd.projectcalico.org solar-ippool water-ippool
echo "All static tenancy and IPPool checks passed."
