#!/usr/bin/env bash
# shellcheck disable=SC2317 # helpers below are called through check()
# Check that the foundation does what it is for. Runs every check, prints one
# line per check, and exits non-zero if any failed.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

failed=0
pass() { echo "PASS  $1"; }
fail() { echo "FAIL  $1${2:+: $2}"; failed=1; }

# check NAME COMMAND...: PASS when COMMAND succeeds, FAIL with its output otherwise.
check() {
  local name="$1" out
  shift
  if out="$("$@" 2>&1)"; then pass "${name}"; else fail "${name}" "${out}"; fi
}

node_has() { # node, jsonpath, expected value
  local got
  got="$(kubectl get node "$1" -o jsonpath="$2")"
  [[ "${got}" == "$3" ]] || { echo "got '${got}', want '$3'"; return 1; }
}

for node in ${SYSTEM_NODES}; do
  check "${node} is in the system pool" \
    node_has "${node}" '{.metadata.labels.tenantry\.io/compute-pool}' system
  check "${node} keeps other Pods off" \
    node_has "${node}" '{.spec.taints[?(@.key=="tenantry.io/compute-pool")].effect}' NoSchedule
done
for node in ${SHARED_NODES}; do
  check "${node} is in the shared pool" \
    node_has "${node}" '{.metadata.labels.tenantry\.io/compute-pool}' shared-general
done

exit "${failed}"
