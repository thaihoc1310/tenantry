#!/usr/bin/env bash
# Undo 70-apiserver.sh: put back the kube-apiserver manifest kubeadm wrote
# (saved on the node before the first change), and wait until it is ready.
# Keycloak tokens stop working and the two admission plugins are off again.
# Not numbered: up.sh never runs it.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

ssh "${CONTROL_PLANE_SSH}" sudo python3 - rollback <"${CLUSTER_DIR}/apiserver-patch.py"
