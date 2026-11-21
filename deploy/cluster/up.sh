#!/usr/bin/env bash
# Build the cluster foundation: every numbered script, in order. Each one is
# idempotent, so after a failure fix the cause and run this again from the top.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

for script in "${CLUSTER_DIR}"/[0-9][0-9]-*.sh; do
  echo
  echo "==> $(basename "${script}")"
  "${script}"
done
