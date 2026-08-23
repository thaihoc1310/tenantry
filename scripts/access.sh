#!/usr/bin/env bash
set -euo pipefail

cleanup() {
  jobs -p | xargs -r kill
}
trap cleanup EXIT INT TERM

kubectl port-forward -n identity-system service/keycloak 8443:8443 &
kubectl port-forward -n capsule-system service/headlamp 8444:80 &
kubectl port-forward -n monitoring-system service/kube-prometheus-stack-grafana 3000:80 &

echo "Keycloak: https://keycloak.127.0.0.1.nip.io:8443"
echo "Headlamp: http://headlamp.127.0.0.1.nip.io:8444"
echo "Grafana:  http://grafana.127.0.0.1.nip.io:3000"
echo "Keycloak SSO logout: https://keycloak.127.0.0.1.nip.io:8443/realms/capsule/protocol/openid-connect/logout"
wait
