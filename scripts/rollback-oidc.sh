#!/usr/bin/env bash
set -euo pipefail

CONTROL_PLANE="${CONTROL_PLANE:-fdp-k8s-cp1}"
FLAG='--authentication-config=/etc/kubernetes/pki/capsule-lab-authentication-config.yaml'

kubectl debug "node/${CONTROL_PLANE}" \
  --image=python:3.13-alpine \
  --profile=sysadmin \
  --attach \
  -- \
  python -c "from pathlib import Path; p=Path('/host/etc/kubernetes/manifests/kube-apiserver.yaml'); s=p.read_text(); line=next((x for x in s.splitlines(True) if x.strip() == '- ${FLAG}'), None); assert line, 'OIDC flag is not present'; p.write_text(s.replace(line, '', 1)); print('OIDC flag removed; kube-apiserver will restart')"

for attempt in $(seq 1 30); do
  if kubectl get --raw=/readyz >/dev/null 2>&1; then
    echo "kube-apiserver is ready without Capsule lab OIDC."
    exit 0
  fi
  sleep 2
done
exit 1

