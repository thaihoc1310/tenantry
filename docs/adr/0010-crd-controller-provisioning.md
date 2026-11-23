# ADR-0010: Provisioning qua outbox, CRD và controller; Cluster Agent mTLS để Phase 2

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §0 (v0.4 → v0.5 điểm 3), §13, §14.1–14.4, §20.2](../PRODUCT-PLAN.md), [IMPLEMENTATION-PLAN §4, §5.1, §5.3, §7 Phase 2](../IMPLEMENTATION-PLAN.md), [01 mục 1, 2, 5](../architecture/01-system-context.md), [02 mục 5](../architecture/02-data-model.md), [06 mục 1, 7](../architecture/06-security-and-identity.md)

## Bối cảnh

PostgreSQL là source of truth của product state ([PRODUCT-PLAN §14.1](../PRODUCT-PLAN.md)). Desired state đó phải xuống Cluster thành nhiều object: Capsule Tenant, `GlobalTenantResource`, Calico IPPool, RoleBinding, Ingress, `ProxySetting`, label và taint trên Node.

Các ràng buộc:
- DB commit và apply vào Kubernetes không nằm trong một transaction; apply mất sau commit phải được thử lại ([§14.3](../PRODUCT-PLAN.md));
- xóa phải theo thứ tự: gỡ quyền, xóa Namespace, xóa Tenant, chờ Calico nhả địa chỉ rồi mới xóa IPPool ([IMPLEMENTATION-PLAN §4.3](../IMPLEMENTATION-PLAN.md));
- sửa tay object managed phải được khôi phục ([§14.4](../PRODUCT-PLAN.md));
- central plane không được giữ credential cluster-admin ([§20.2](../PRODUCT-PLAN.md));
- Phase 2 central plane sẽ nằm ngoài Cluster.

## Quyết định

Luồng một chiều DB → CR → controller:
1. API ghi Workspace, reservation, CIDR, Operation và outbox event trong một transaction ([§14.3](../PRODUCT-PLAN.md)).
2. Provisioning worker đọc outbox (`FOR UPDATE SKIP LOCKED`, thứ tự theo `ordering_key`, backoff 1s → 5 phút) và server-side apply CR `PlatformWorkspace` (`tenantry.io/v1alpha1`, cluster-scoped) vào Cluster của Workspace ([IMPLEMENTATION-PLAN §5.1](../IMPLEMENTATION-PLAN.md)).
3. Credential của worker chỉ có quyền trên bốn CRD `tenantry.io`, không ghi `*/status` (ClusterRole `tenantry-worker-provisioning` trong `deploy/charts/tenantry-backend/templates/worker.yaml`).
4. Operator viết bằng controller-runtime chạy trong Cluster: `Render(spec)` là pure function (`operator/internal/render`), các sub-reconciler compute, network, tenant, namespaces, policy, rbac, routes, proxyaccess (`operator/internal/subreconciler`) báo kết quả qua sáu Condition (`ComputeReady`, `NetworkReady`, `CapsuleTenantReady`, `PolicyReady`, `IdentityBindingsReady`, `ProxyAccessReady`) và `status.routes`, finalizer `tenantry.io/workspace` làm thứ tự xóa, SSA với `ForceOwnership` khôi phục drift ([IMPLEMENTATION-PLAN §4.1–4.3](../IMPLEMENTATION-PLAN.md)).
5. CR chỉ chứa giá trị đã resolve (tên node, CIDR, group subject, hostname), controller không bao giờ hỏi DB ([02 mục 5](../architecture/02-data-model.md)).
6. Việc liên quan Node đi qua ba CRD phụ `PlatformNodeGrant`, `PlatformSharedPool`, `PlatformNodeInventory`; central plane không đọc Node ([IMPLEMENTATION-PLAN §4.5](../IMPLEMENTATION-PLAN.md)).
7. Worker đọc status qua informer, chỉ tin status khi `observedGeneration` bằng `generation`, rồi cập nhật Workspace và Operation.

Cluster Agent outbound mTLS (M14) thay bước 2 và 7: agent trong Cluster kéo assignment và báo status, central không giữ credential nào. Outbox và contract CR giữ nguyên ([§13](../PRODUCT-PLAN.md)). MVP chưa làm vì central plane chạy ngay trong Cluster, nên ServiceAccount in-cluster với quyền hẹp là đủ.

## Phương án đã cân nhắc

### Backend gọi thẳng kube-apiserver tạo Tenant, IPPool, RBAC

Không chọn. Backend sẽ cần quyền rộng trên Tenant, IPPool, RoleBinding, Node; mất cơ chế khôi phục drift; thứ tự xóa và việc chờ Calico phải viết trong backend; apply dở dang giữa chừng khó thử lại. Nếu API bị chiếm, kẻ tấn công có thẳng quyền đó.

### Apply ngay trong request HTTP, không outbox

Không chọn: DB đã commit mà apply lỗi hoặc tiến trình chết thì thay đổi mất, không có gì để thử lại ([§14.3](../PRODUCT-PLAN.md)).

### GitOps (ghi manifest vào Git, công cụ như Argo CD đồng bộ)

Docs không nêu phương án này. Không chọn vì thêm một hệ thống và một kho lưu nữa giữa DB và Cluster, trong khi worker vẫn phải đọc Condition của từng thành phần về DB để cập nhật Operation, và logic xóa (chờ Namespace, chờ IPAM) vẫn cần một controller.

### Làm Cluster Agent mTLS ngay trong MVP

Không chọn. Agent cần giao thức assignment/status, heartbeat, bảng `cluster_agents`, quản lý cert mTLS và tách central plane ra ngoài Cluster; M14 được ước lượng XL ([IMPLEMENTATION-PLAN §7](../IMPLEMENTATION-PLAN.md)). Với một Cluster và central plane nằm trong chính nó, agent không đem lại thêm cô lập đáng kể so với một ServiceAccount chỉ có quyền trên CRD `tenantry.io`.

## Hệ quả

Mặt tốt:
- Operator restart giữa lúc provisioning không tạo lại gì: Ready 1m8s sau khi Pod bị kill, Tenant giữ nguyên UID ([failure-cluster.md](../report/failure-cluster.md)).
- Operator nhỏ: đỉnh 64m CPU và 34Mi ở N=50, khoảng 180–225 request mỗi Workspace ([benchmark.md](../report/benchmark.md)).
- Lỗi chỉ đúng chỗ: mỗi sub-reconciler một Condition, portal hiện message của Condition đang False.
- API bị chiếm vẫn không tạo được cluster-admin binding qua worker ([01 mục 2](../architecture/01-system-context.md)).

Mặt xấu và giới hạn:
- Bất đồng bộ: người dùng chờ Operation. CR Degraded tạm thời thì Operation chờ, chỉ `Failed` khi CR báo `Failed` hoặc quá 10 phút ([known-limitations](../report/known-limitations.md) mục 11).
- Một provisioning worker mỗi Cluster, không leader election (mục 19).
- API và hai worker dùng chung một role PostgreSQL, nên ai điều khiển được API là điều khiển được cả worker qua DB ([06 mục 1](../architecture/06-security-and-identity.md)).
- MVP central plane vẫn giữ một credential vào Cluster (SA in-cluster) tới khi có Cluster Agent.
- Ba contract (OpenAPI, DB, CR) phải đổi cùng lúc; lệch là bug tích hợp (risk 12, [IMPLEMENTATION-PLAN §11](../IMPLEMENTATION-PLAN.md)).
- Thời gian tới Ready do Capsule quyết định chứ không phải operator (mục 12).

## Kiểm chứng

- Outbox (`backend/internal/outbox/outbox_test.go`): `TestDeliversInOrderAndMarksOperationRunning`, `TestFailingEventHoldsBackOnlyItsOwnResource`, `TestFailuresBackOffThenFailTheOperation`, `TestConcurrentWorkersDeliverEachEventOnce`, `TestPermanentFailure`, `TestClusterScopeAndUnavailable`.
- Worker và status (`backend/internal/provisioning/status_test.go`): `TestRedeliveryCreatesNoDuplicate` (worker dừng sau apply, giao lại vẫn một CR, cùng generation), `TestClusterDownAfterCommit` (apiserver chết ngay sau commit, giao lại khi Cluster trả lời, không mất gì), `TestTimeoutThenRetry`, `TestProvisioningToReady`.
- Operator envtest (`operator/internal/controller/platformworkspace_controller_test.go`): `TestWorkspaceProvisioning`, `TestWorkspaceFinalizer`, `TestWorkspaceForceDelete`; RBAC của operator `TestOperatorRole` (`rbac_test.go`); golden `TestGolden` (`operator/internal/render/render_test.go`).
- Lab: failure suite 4/4 (`operator/test/isolation/failure_test.go`; [failure-cluster.md](../report/failure-cluster.md)); benchmark N=10/20/50, mọi Workspace Ready và xóa sạch ([benchmark.md](../report/benchmark.md)); kịch bản persona ([e2e-personas.md](../report/e2e-personas.md)).
- Chưa kiểm: quyền của SA `tenantry-worker-provisioning` chưa có check riêng trong `make cluster-verify` (chỉ có trong chart). Cluster Agent chưa có.
