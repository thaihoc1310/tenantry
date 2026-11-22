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

# The shared pool's label (tenantry.io/compute-pool=<pool id>) is not set
# here: the Workspace Operator puts it on the nodes PlatformSharedPool lists,
# and takes it off the others (IMPLEMENTATION-PLAN §4.5). The provisioning
# worker writes those CRs from the database (cluster_nodes.shared_pool_id,
# set by the seed and by a Platform Admin on the portal).
