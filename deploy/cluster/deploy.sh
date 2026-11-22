#!/usr/bin/env bash
# Install or upgrade Tenantry on this cluster: the backend (API, worker,
# migrations, seed) and the portal, with the values in values/. Needs the
# foundation (make cluster-up) first. Not numbered: up.sh builds the
# foundation only. Safe to run again: unchanged images keep their tag, so
# nothing restarts.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"
ROOT="${CLUSTER_DIR}/../.."

# ---- images ---------------------------------------------------------------
# No registry yet (pushing to Docker Hub would publish them), so each image
# is copied into the containerd of the nodes that run the central plane.
# The tag is a hash of what goes into the image (the Dockerfile and every
# file of the build context that git does not ignore), not the image id,
# which changes on every build (BuildKit stamps a time into it). Same
# sources, same tag: nothing is rebuilt, loaded or restarted.
image() { # name dockerfile context: build if needed, load, print the tag
  local name="$1" dockerfile="$2" context="$3" tag node missing=""
  tag="src-$({
    cat "${dockerfile}"
    (cd "${context}" && git ls-files -co --exclude-standard -z | sort -z | xargs -0 sha256sum)
  } | sha256sum | cut -c1-12)"
  for node in ${SYSTEM_NODES}; do
    ssh -o BatchMode=yes "${node}" sudo -n ctr -n k8s.io images ls -q "name==docker.io/${name}:${tag}" | grep -q . ||
      missing="${missing} ${node}"
  done
  if [[ -n "${missing}" ]]; then
    docker build -q -f "${dockerfile}" -t "${name}:${tag}" -t "${name}:dev" "${context}" >/dev/null
    for node in ${missing}; do
      echo "loading ${name}:${tag} into ${node}" >&2
      docker save "${name}:${tag}" | ssh -o BatchMode=yes "${node}" sudo -n ctr -n k8s.io images import - >/dev/null
    done
  fi
  echo "${tag}"
}
backend_tag="$(image tenantry/backend "${ROOT}/deploy/Dockerfile.backend" "${ROOT}/backend")"
web_tag="$(image tenantry/web "${ROOT}/deploy/Dockerfile.web" "${ROOT}/web")"
echo "images: tenantry/backend:${backend_tag} tenantry/web:${web_tag}"

# ---- secrets --------------------------------------------------------------
# The portal client's secret (from the realm's Secret) and the session key
# (32 random bytes, base64), created once: a new session key would log
# everyone out. Values go through a pipe, never a command line.
if ! kubectl -n tenantry-system get secret tenantry-portal >/dev/null 2>&1; then
  {
    printf 'client-secret=%s\n' "$(kubectl -n keycloak get secret keycloak-realm -o jsonpath='{.data.PORTAL_CLIENT_SECRET}' | base64 -d)"
    printf 'session-key=%s\n' "$(openssl rand -base64 32)"
  } | kubectl -n tenantry-system create secret generic tenantry-portal --from-env-file=/dev/stdin >/dev/null
  echo "secret/tenantry-portal generated in tenantry-system"
fi

# tenantry-sync's client secret, mirrored from the realm's Secret into one
# of its own that holds nothing else: only the group sync Deployment mounts
# it, the API and the provisioning worker never see it (architecture/06 §7).
if [[ -z "$(kubectl -n keycloak get secret keycloak-realm -o jsonpath='{.data.TENANTRY_SYNC_CLIENT_SECRET}')" ]]; then
  echo "keycloak-realm has no TENANTRY_SYNC_CLIENT_SECRET: run deploy/cluster/60-keycloak.sh (make cluster-up) first" >&2
  exit 1
fi
copy_secret_key keycloak keycloak-realm TENANTRY_SYNC_CLIENT_SECRET tenantry-system tenantry-keycloak-sync client-secret

# ---- charts ---------------------------------------------------------------
# The backend chart migrates before the new Pods start (pre-upgrade hook)
# and seeds after (post-upgrade hook).
helm upgrade --install tenantry "${CLUSTER_DIR}/../charts/tenantry-backend" \
  --namespace tenantry-system \
  --values "${CLUSTER_DIR}/values/tenantry-backend.yaml" \
  --set image.tag="${backend_tag}" \
  --set api.auth.issuer="https://${AUTH_HOST}/realms/tenantry" \
  --set api.auth.publicURL="https://${PORTAL_HOST}" \
  --set seed.proxyEndpoint="https://${KUBE_HOST}" \
  --set seed.consoleURL="https://${CONSOLE_HOST}" \
  --set seed.appsDomain="${APPS_DOMAIN}" \
  --set worker.groupsync.keycloak.url="http://keycloak-http.keycloak.svc" \
  --wait --timeout 5m

helm upgrade --install tenantry-web "${CLUSTER_DIR}/../charts/tenantry-web" \
  --namespace tenantry-system \
  --values "${CLUSTER_DIR}/values/tenantry-web.yaml" \
  --set image.tag="${web_tag}" \
  --set ingress.host="${PORTAL_HOST}" \
  --set ingress.clusterIssuer="${CLUSTER_ISSUER}" \
  --wait --timeout 5m

kubectl -n tenantry-system wait certificate/tenantry-portal-tls --for=condition=Ready --timeout=10m
echo "portal: https://${PORTAL_HOST}"
