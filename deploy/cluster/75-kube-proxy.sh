#!/usr/bin/env bash
# kube-proxy tells local Pods by their interface, not their address
# (IMPLEMENTATION-PLAN M9 step 0, architecture/05 §5). Tenant Pods get
# addresses from their Workspace's IP pool, outside clusterCIDR; detected by
# CIDR, kube-proxy would SNAT their calls to Services and the NetworkPolicy
# that only lets a Workspace talk to itself would refuse them. Calico names
# every Pod interface cali*. kubeadm keeps this ConfigMap across upgrades.
#
#   75-kube-proxy.sh            print the diff, save the current config.conf,
#                               patch it and restart kube-proxy
#   75-kube-proxy.sh --dry-run  print the diff only
#   75-kube-proxy.sh --restore FILE
#                               put a saved config.conf back and restart
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

BACKUP_DIR="${BACKUP_DIR:-${HOME}/.local/state/tenantry}"

# write CONFIG: a merge patch of config.conf alone (kubeconfig.conf stays),
# then a rolling restart, since kube-proxy reads its config only at start.
write() {
  printf '%s' "$1" | python3 -c 'import json, sys; print(json.dumps({"data": {"config.conf": sys.stdin.read()}}))' |
    kubectl -n kube-system patch configmap kube-proxy --type=merge --patch-file=/dev/stdin >/dev/null
  kubectl -n kube-system rollout restart daemonset/kube-proxy
  kubectl -n kube-system rollout status daemonset/kube-proxy --timeout=5m
}

if [[ "${1:-}" == --restore ]]; then
  [[ -s "${2:-}" ]] || { echo "usage: $0 --restore FILE" >&2; exit 1; }
  write "$(<"$2")"
  exit 0
fi

current="$(kubectl -n kube-system get configmap kube-proxy -o jsonpath='{.data.config\.conf}')"
wanted="$(sed -e 's/^detectLocalMode: .*/detectLocalMode: InterfaceNamePrefix/' \
  -e '/^detectLocal:/,/^[^ ]/ s/^  interfaceNamePrefix: .*/  interfaceNamePrefix: cali/' <<<"${current}")"
if ! grep -q '^detectLocalMode: InterfaceNamePrefix$' <<<"${wanted}" || ! grep -q '^  interfaceNamePrefix: cali$' <<<"${wanted}"; then
  echo "kube-proxy config.conf does not have the expected detectLocal fields" >&2; exit 1;
fi

if [[ "${current}" == "${wanted}" ]]; then
  echo "kube-proxy already detects local Pods by the cali interface prefix"
  exit 0
fi
diff -u --label 'config.conf (live)' --label 'config.conf (wanted)' <(printf '%s\n' "${current}") <(printf '%s\n' "${wanted}") || true
[[ "${1:-}" == --dry-run ]] && exit 0

mkdir -p "${BACKUP_DIR}"
backup="${BACKUP_DIR}/kube-proxy-config.conf.$(date +%Y%m%dT%H%M%S)"
printf '%s' "${current}" >"${backup}"
echo "saved the live config.conf to ${backup} (undo: $0 --restore ${backup})"
write "${wanted}"
