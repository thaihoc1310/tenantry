#!/usr/bin/env bash
# kube-apiserver: accept Keycloak tokens of the tenantry realm (structured
# authentication config) and enable the PodNodeSelector and
# PodTolerationRestriction admission plugins (architecture/05 §2, 06 §2).
#
# This is the one step that restarts kube-apiserver, and only the first time:
# the change is made on the control-plane node over SSH, the previous manifest
# is kept, and if the new kube-apiserver is not ready within 3 minutes the
# previous manifest is put back automatically. To undo it on purpose:
# ./rollback-apiserver.sh. To see the authentication config without changing
# anything: ./70-apiserver.sh --print.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

# Staging certificates chain up to a root no system trusts, kube-apiserver's
# included (it uses the node's trust store): name the staging roots while on
# staging. Production certificates need nothing.
ca_block=""
if [[ "${CLUSTER_ISSUER}" == letsencrypt-staging ]]; then
  ca_block="      certificateAuthority: |
$(for root in letsencrypt-stg-root-x1 letsencrypt-stg-root-x2; do
    curl -fsS "https://letsencrypt.org/certs/staging/${root}.pem"
  done | sed 's/^/        /')"
fi

# Usernames become oidc:<sub>, groups oidc:<group> (06 §2): nothing a token
# says can collide with a Kubernetes user or group such as system:masters.
config="$(cat <<YAML
apiVersion: apiserver.config.k8s.io/v1
kind: AuthenticationConfiguration
jwt:
  - issuer:
      url: https://${AUTH_HOST}/realms/tenantry
      audiences: [kubernetes, headlamp-cluster-a]
      audienceMatchPolicy: MatchAny
${ca_block}
    claimMappings:
      username:
        claim: sub
        prefix: "oidc:"
      groups:
        claim: groups
        prefix: "oidc:"
    userValidationRules:
      - expression: "!user.username.startsWith('system:')"
        message: username must not use the reserved system prefix
YAML
)"

if [[ "${1:-}" == --print ]]; then
  echo "${config}" # preview only: nothing is changed
  exit 0
fi

# The script travels on stdin; the config (no secret in it) as an argument.
ssh "${CONTROL_PLANE_SSH}" sudo python3 - apply "$(base64 -w0 <<<"${config}")" <"${CLUSTER_DIR}/apiserver-patch.py"
