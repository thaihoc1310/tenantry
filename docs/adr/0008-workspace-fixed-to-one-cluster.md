# ADR-0008: Workspace nằm cố định trong đúng một Cluster

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §1, §2.3, §6.1, §17.2, §17.4](../PRODUCT-PLAN.md), [02 mục 4](../architecture/02-data-model.md), [04 mục 4, 6](../architecture/04-state-machines.md), [05 mục 5, 7](../architecture/05-network-and-scheduling.md)

## Bối cảnh

Platform quản lý nhiều Cluster có sẵn (MVP chỉ một, [ADR-0009](0009-single-cluster-mvp-multi-cluster-model.md)). Gần như mọi thứ của một Workspace gắn với một Cluster cụ thể:
- Capsule Tenant, Namespace, PVC trên NFS của Cluster đó;
- Calico IPPool với CIDR lấy từ NetworkPool của Cluster; mỗi Cluster nên có supernet riêng (`10.240.0.0/16` cho A, `10.241.0.0/16` cho B ở Phase 2) ([PRODUCT-PLAN §7.1](../PRODUCT-PLAN.md));
- NodeReservation trên một node của Cluster (Dedicated) hoặc shared pool của Cluster (Shared);
- hostname Route dưới apps domain của Cluster (`*.mvp.tenantry.site`);
- kubeconfig trỏ Capsule Proxy của Cluster (`kube-a`), web console là Headlamp của Cluster (`console-a`).

Phạm vi đồ án loại trừ migrate/failover Workspace giữa Cluster và mạng Pod/Service xuyên Cluster ([§2.3](../PRODUCT-PLAN.md)).

## Quyết định

`workspaces.cluster_id` được chọn lúc tạo (wizard; MVP tự preselect grant mặc định) và không bao giờ đổi ([§17.2](../PRODUCT-PLAN.md)).

Giữ bằng:
- API: body `PATCH /workspaces/{id}` (`WorkspacePatch` trong `backend/openapi/tenantry.v1.yaml`) chỉ nhận `displayName` và `resources`, `additionalProperties: false`;
- DB: FK ghép `(cluster_grant_id, org_id, cluster_id) → organization_cluster_grants`; mọi row con mang `cluster_id` (`workspace_namespaces`, `network_allocations`, `workspace_node_reservations`); outbox event mang `target_cluster_id` ([02 mục 4](../architecture/02-data-model.md));
- worker provisioning chỉ giao event của Cluster của nó (`TENANTRY_CLUSTER_ID`).

Muốn chuyển sang Cluster khác thì tạo Workspace mới ở Cluster đó, triển khai lại workload và dữ liệu, rồi xóa Workspace cũ.

## Phương án đã cân nhắc

### Cho đổi Cluster qua một migration flow

Không chọn. Đổi Cluster nghĩa là chuyển dữ liệu PVC, cấp CIDR mới trong supernet khác, đổi hostname sang apps domain khác, phát kubeconfig mới, và giữ hai bản trong lúc chuyển. Đó là một tính năng riêng, ngoài phạm vi ([§2.3](../PRODUCT-PLAN.md)).

### Một Workspace trải trên nhiều Cluster

Không chọn. Namespace cùng Workspace phải gọi được nhau (`tenant-allow-same-workspace`), nên cần mạng Pod/Service xuyên Cluster, thứ đã bị loại trừ. Quota chung, CIDR và Tenant cũng phải chia theo Cluster.

### Tự failover khi Cluster Unavailable

Không chọn. [§6.1](../PRODUCT-PLAN.md) ghi rõ không tự chuyển Workspace sang Cluster khác; dữ liệu PVC không đi theo và CIDR, hostname đều phụ thuộc Cluster.

## Hệ quả

Mặt tốt:
- Mọi invariant chỉ cần kiểm trong một Cluster: `EXCLUDE (cluster_id WITH =, cidr inet_ops WITH &&)` cho CIDR, `UNIQUE (cluster_id, name)` cho Namespace, capacity node dưới lock của grant node ([02 mục 4](../architecture/02-data-model.md)).
- Một Cluster hỏng không kéo theo Workspace ở Cluster khác (là kịch bản demo của Phase 2, chưa làm).

Mặt xấu và giới hạn:
- Cluster `Unavailable` thì Workspace ở đó hiện "unknown", Operation giữ `Pending`, không có đường dự phòng ([04 mục 6](../architecture/04-state-machines.md)).
- Chuyển Cluster phải xóa Workspace cũ, và xóa là mất hết kể cả PVC ([§17.4](../PRODUCT-PLAN.md)).
- `k8s_name` là `UNIQUE` toàn platform, không theo Cluster. Workspace vừa xóa giữ slug trong 10 phút quarantine, nên tạo lại cùng slug ở Cluster khác cũng phải chờ ([known-limitations](../report/known-limitations.md) mục 8).
- Bất biến được giữ ở cả ba tầng: API, DB, và CRD. `spec.clusterId`, `spec.organizationId`, `spec.workspaceId` của `PlatformWorkspace` có luật CEL `self == oldSelf` (`operator/api/v1alpha1/platformworkspace_types.go`), như `nodeName` và `organizationId` của `PlatformNodeGrant`. Ai ghi được CR, kể cả worker có lỗi, cũng không chuyển được Workspace sang Organization hay Cluster khác.
- Cùng lý do, Shared ↔ Dedicated, đổi node và đổi shared pool của Workspace cũng không đổi trực tiếp được ([§17.2](../PRODUCT-PLAN.md)).

## Kiểm chứng

- `TestCreateWorkspaceRefusals` (`backend/internal/httpapi/workspaces_test.go`): tạo Workspace trên Cluster chưa có grant hoặc không tồn tại trả `409 CLUSTER_NOT_GRANTED`.
- `TestPatchWorkspace` (cùng file): đổi tên và resize; resize khi Cluster `Maintenance` trả `409 CLUSTER_NOT_READY`.
- `TestClusterScopeAndUnavailable` (`backend/internal/outbox/outbox_test.go`): worker của `cluster-b` không lấy event của `cluster-a`; event của Cluster `Unavailable` không được giao, `attempts` giữ 0, Operation giữ `Pending`, giao lại khi Cluster `Ready`.
- Envtest `TestWorkspaceIdsImmutable` (`operator/internal/controller/platformworkspace_controller_test.go`): kube-apiserver từ chối đổi `organizationId`, `workspaceId`, `clusterId` với message `... is immutable`; đổi `displayName` vẫn được.
- Chưa kiểm: không có test gửi `clusterId` vào `PATCH` (schema đã chặn, nhưng chưa có ca test riêng). Chưa kiểm với Cluster thứ hai thật.
