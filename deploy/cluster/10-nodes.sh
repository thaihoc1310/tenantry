#!/usr/bin/env bash
# Compute pools (architecture/05-network-and-scheduling.md §1). Runs before
# anything that targets the system pool, or those Pods would stay Pending.
#
# The taint keeps every Pod off the system pool unless it tolerates
# tenantry.io/compute-pool=system; the label is what cluster components select
# with nodeSelector. Tenant Pods get neither, so they land on the shared pool.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

for node in ${SYSTEM_NODES}; do
  kubectl label node "${node}" tenantry.io/compute-pool=system --overwrite
  kubectl taint node "${node}" tenantry.io/compute-pool=system:NoSchedule --overwrite
done

# A node granted to one Organization as a Dedicated node (M8) must leave
# SHARED_NODES, or the next run puts it back into the shared pool.
for node in ${SHARED_NODES}; do
  kubectl label node "${node}" tenantry.io/compute-pool=shared-general --overwrite
done
