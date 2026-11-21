#!/usr/bin/env bash
# StorageClass standard on NFS through csi-driver-nfs (05 §9). The default
# class, so a PVC that names no class lands here.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

helm upgrade --install csi-driver-nfs csi-driver-nfs \
  --repo https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/master/charts \
  --version "${CSI_DRIVER_NFS_VERSION}" \
  --namespace kube-system \
  --values "${CLUSTER_DIR}/values/csi-driver-nfs.yaml" \
  --set storageClass.parameters.server="${NFS_SERVER}" \
  --set storageClass.parameters.share="${NFS_SHARE}" \
  --wait --timeout 5m
