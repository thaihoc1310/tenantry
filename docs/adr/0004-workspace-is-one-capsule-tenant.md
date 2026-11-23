# ADR-0004: Mỗi Workspace là đúng một Capsule Tenant

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §4.4, §4.5, §5.4, §18](../PRODUCT-PLAN.md), [IMPLEMENTATION-PLAN §4.1, §4.3](../IMPLEMENTATION-PLAN.md), [01 mục 5](../architecture/01-system-context.md), [05 mục 2, 6](../architecture/05-network-and-scheduling.md)

## Bối cảnh

Workspace là đơn vị người dùng thấy trên portal. Mỗi Workspace có quota CPU/RAM/Pod/storage riêng, số Namespace tối đa, compute mode Shared hoặc Dedicated, một CIDR, danh sách StorageClass và các group giữ role Owner/Developer/Viewer ([PRODUCT-PLAN §4.4](../PRODUCT-PLAN.md)). Một Organization có nhiều Workspace, và các Workspace đó khác nhau: trong kịch bản persona, Solar là Shared trên `shared-general` còn Water và Green là Dedicated trên `wk3` ([e2e-personas.md](../report/e2e-personas.md)).

Capsule Tenant là đơn vị mà Capsule áp owners, quota chung, namespace quota, tiền tố tên, node selector, metadata managed và replication ([ADR-0001](0001-capsule-namespace-tenancy.md)). Cần chọn ranh giới nào của sản phẩm trùng với ranh giới Tenant.

## Quyết định

Một `PlatformWorkspace` sinh đúng một Capsule `Tenant`, cùng tên `<org-slug>-<ws-slug>` (quy tắc tên ở [ADR-0016](0016-kubernetes-names-from-slugs.md)). Tên này cũng là tiền tố Namespace, hậu tố hostname của Route và tên CR.

Sub-reconciler `tenant` (`operator/internal/subreconciler/tenant.go`) render Tenant từ spec:
- owners là group giữ role `TenantOwner` cộng ServiceAccount của operator; Developer/Viewer đi qua `additionalRoleBindings` (sub-reconciler `rbac`);
- quota chung của Workspace qua `spec.rules[].quota`, không chia trước theo Namespace ([§5.4](../PRODUCT-PLAN.md));
- `namespaceOptions.quota` và `forceTenantPrefix`;
- node selector theo mode, annotation IPPool là metadata managed;
- label `tenantry.io/org=<ULID>` gắn lên Namespace qua `additionalMetadata`;
- StorageClass allow-list.

Mỗi Workspace có một `GlobalTenantResource` riêng để phát NetworkPolicy baseline và LimitRange. Label `capsule.clastix.io/tenant` do Capsule gắn là căn cứ duy nhất cho "cùng Workspace" trong NetworkPolicy và trong các VAP ([05 mục 6](../architecture/05-network-and-scheduling.md)).

## Phương án đã cân nhắc

### Một Organization là một Tenant

Không chọn. Node selector, quota chung và annotation IPPool là thuộc tính của Tenant, nên mọi Workspace của Organization sẽ phải chung một compute mode, một node, một CIDR và một quota. Organization trong kịch bản persona không thể vừa có Solar Shared vừa có Water Dedicated. Owners cũng là của Tenant, nên không gán được group khác nhau cho từng Workspace.

### Không gom theo Tenant, mỗi Namespace tự đứng

Mỗi Namespace một ResourceQuota, controller tự gắn policy. Không chọn vì mất quota chung trải nhiều Namespace, mất việc tenant tự tạo Namespace bằng `kubectl` trong vùng tên của mình, và Capsule Proxy không còn đơn vị để lọc danh sách Namespace.

### Một Workspace gồm nhiều Tenant

Ví dụ tách Tenant theo Namespace hoặc theo compute mode. Không chọn vì quota của Workspace phải chia ra nhiều Tenant và giữ tổng; NetworkPolicy `tenant-allow-same-workspace` chọn theo một giá trị label tenant, nhiều Tenant thì luật "cùng Workspace" không còn viết bằng một selector được.

## Hệ quả

Mặt tốt:
- Mỗi thuộc tính cấp Workspace ánh xạ vào đúng một field của Tenant. Controller chỉ render, không cần biết grant hay capacity ([01 mục 5](../architecture/01-system-context.md)).
- "Cùng Workspace" dựa trên label tenant do Capsule gắn, tenant không sửa được.

Mặt xấu và giới hạn:
- Số Tenant bằng số Workspace. Capsule mặc định reconcile từng Tenant một, nên Capsule đặt nhịp tạo Workspace ([known-limitations](../report/known-limitations.md) mục 12).
- Quota chung, không có quota riêng từng Namespace. Namespace quota trễ khoảng 1 giây: lab đo được 4 Namespace trên quota 3 (mục 6).
- `forceTenantPrefix` cho Tenant tạo mọi Namespace bắt đầu bằng tên của nó, nên slug `solar` và `solar-prod` trong cùng Organization bị từ chối `409 SLUG_TAKEN` ([§4.4](../PRODUCT-PLAN.md)).
- Nhiều Workspace Shared cùng pool không được tách bằng node; cô lập bằng Namespace, NetworkPolicy và quota, đúng ý định ([05 mục 2](../architecture/05-network-and-scheduling.md)).
- Xóa Workspace phụ thuộc finalizer của Tenant: finalizer `controller.projectcapsule.dev/finalize` chờ mọi Namespace mất hẳn. Có Pod kẹt thì Tenant kẹt theo; `:force` bỏ lại Tenant Terminating, và Workspace mới cùng tên báo `TenantTerminating` tới khi Tenant cũ mất ([failure-cluster.md](../report/failure-cluster.md), known-limitations mục 14).
- Race replication của Capsule làm Workspace Degraded nhầm; operator coi ba dạng lỗi đó là đang xử lý trong tối đa 2 phút (mục 10).

## Kiểm chứng

- Envtest `TestWorkspaceProvisioning` (`operator/internal/controller/platformworkspace_controller_test.go`): tạo CR ra Tenant đúng owners, Workspace Ready, lần reconcile thứ hai không đổi gì. Golden test của `Render` (`operator/internal/render/render_test.go`, `TestGolden`, `TestDeterministic`).
- Isolation suite trên lab (`operator/test/isolation/isolation_test.go`; [isolation-cluster.md](../report/isolation-cluster.md)), T19: Pod 2 CPU vượt quota chung 1 CPU bị `GlobalResourceQuota` từ chối; PVC 5Gi vượt 2Gi bị từ chối; Namespace thứ tư trên quota 3 bị từ chối; Namespace mang tiền tố của B bị `forceTenantPrefix` từ chối. T15: hai Namespace cùng Workspace gọi được nhau. T13/T14: qua Proxy chỉ thấy Namespace của Workspace mình.
- Failure suite: restart operator giữa lúc provisioning, Tenant giữ nguyên UID, đúng 2 owner, đúng 2 Namespace (`operator/test/isolation/failure_test.go`; [failure-cluster.md](../report/failure-cluster.md)).
- Race replication trước và sau sửa: 45% mẫu Degraded xuống 0 ([benchmark.md](../report/benchmark.md); `operator/internal/controller/replication_race_test.go`).
