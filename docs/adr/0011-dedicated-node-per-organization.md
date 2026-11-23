# ADR-0011: Dedicated node thuộc Organization, mỗi Workspace giữ một phần capacity

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §5.2, §5.3, §6.3, §6.4, §17.2](../PRODUCT-PLAN.md), [02 mục 3, 4](../architecture/02-data-model.md), [04 mục 3, 5](../architecture/04-state-machines.md), [05 mục 2, 3](../architecture/05-network-and-scheduling.md)

## Bối cảnh

Một số Organization cần compute không chung với tenant khác (Shared soft, Dedicated hard: [ADR-0017](0017-shared-soft-dedicated-hard.md)). Một Organization có nhiều Workspace, và một node thường lớn hơn nhu cầu của một Workspace: trên lab, `wk3` sau system reserve còn 2,5 CPU và chứa cả Water lẫn Green ([e2e-personas.md](../report/e2e-personas.md)).

Entitlement thuộc Organization chứ không thuộc account cá nhân ([§6.3](../PRODUCT-PLAN.md), [ADR-0007](0007-cluster-grant-per-organization.md)). Kubernetes không biết reservation: nó chỉ thấy requests/limits khi Pod đã chạy ([§5.3](../PRODUCT-PLAN.md)).

## Quyết định

**Node cấp cho Organization.** Platform Admin tạo `DedicatedNodeGrant` (Organization, Cluster, node); `UNIQUE (cluster_id, node_name) WHERE revoked_at IS NULL` nên một node chỉ thuộc một Organization. Node phải `Ready`, schedulable, không thuộc pool nào và không còn Pod tenant nào, kể cả Pod Shared của chính Organization. Operator kiểm lại qua `PlatformNodeGrant` rồi gắn label và taint `tenantry.io/dedicated-org=<org-id>`.

**Workspace giữ phần capacity.** Mỗi Workspace Dedicated chọn đúng một node đã cấp và tạo một `NodeReservation` (CPU, RAM, Pod; `Pending/Active/Releasing/Released`). Trong transaction tạo hoặc resize, API khóa row `dedicated_node_grants` `FOR UPDATE` và kiểm `Σ reservation chưa Released + mới ≤ allocatable − systemReserve` ở từng chiều; vượt thì `409 DEDICATED_NODE_INSUFFICIENT_CAPACITY` kèm phần còn lại ([02 mục 4](../architecture/02-data-model.md)).

Đi kèm:
- Guaranteed QoS: API ép `limits = requests`, LimitRange `maxLimitRequestRatio: 1`; quota chung của Workspace bằng đúng reservation ([05 mục 3](../architecture/05-network-and-scheduling.md));
- Workspace Dedicated không trừ vào CPU/RAM/Pod của ClusterGrant; số Workspace, số Namespace và storage vẫn trừ ([§5.2](../PRODUCT-PLAN.md));
- membership label `workspaces.tenantry.io/<ws-id>=true` trên node, Tenant chọn node theo label đó; `PodNodeSelector` ép selector, `PodTolerationRestriction` chỉ cho toleration của Organization; VAP `tenantry-node-labels` và `tenantry-tenant-node-name` ([05 mục 2](../architecture/05-network-and-scheduling.md));
- không thu hồi node khi còn reservation chưa `Released`; drift làm node `Overcommitted` thì Workspace liên quan `Degraded`, không evict, không chuyển node ([04 mục 5](../architecture/04-state-machines.md)).

## Phương án đã cân nhắc

### Node dành riêng cho một Workspace

Không chọn. Workspace nhỏ thì phần còn lại của node bỏ phí, và Organization phải xin thêm node cho mỗi Workspace. Kịch bản lab hai Workspace 1 CPU trên một node 2,5 CPU không làm được.

### Node gắn với account cá nhân

Không chọn: grant là entitlement của Organization; OrgOwner/OrgAdmin chỉ là người dùng entitlement đó ([§6.3](../PRODUCT-PLAN.md), [ADR-0007](0007-cluster-grant-per-organization.md)).

### Node thuộc Organization nhưng các Workspace dùng chung tự do, không reservation

Không chọn. Một Workspace có thể chiếm hết node của Workspace khác cùng Organization, và Dedicated mất tính "hard reservation" vốn là điểm khác bản chất với Shared ([§5.3](../PRODUCT-PLAN.md)).

### Một Workspace trải nhiều node

Không làm trong MVP: cần reservation theo từng node và scheduling policy phức tạp hơn ([§6.3](../PRODUCT-PLAN.md)). Organization vẫn có nhiều node, mỗi Workspace chọn một.

## Hệ quả

Mặt tốt:
- Không overbook node kể cả khi hai request tranh phần cuối cùng.
- Xóa một Workspace chỉ nhả reservation và membership label của nó; Workspace khác và label/taint của Organization giữ nguyên.

Mặt xấu và giới hạn:
- Workload Dedicated phải Guaranteed; resize xuống thì reservation cũ được giữ tới khi Cluster áp xong ([known-limitations](../report/known-limitations.md) mục 7).
- `systemReserve` do Admin nhập tay, chưa gợi ý từ request của DaemonSet ([05 mục 3](../architecture/05-network-and-scheduling.md), điểm cần review).
- Cấp node đòi node sạch: Admin phải bớt node khỏi pool, drain, uncordon rồi mới cấp. Một lần chạy persona trước đã bị từ chối đúng `NodeNotClean` vì thiếu bước drain ([e2e-personas.md](../report/e2e-personas.md)).
- Node NotReady không tự thay bằng node khác; `Overcommitted` cần Admin xử lý.
- Reservation của Workspace `Failed` không có TTL ([04 mục 3.2](../architecture/04-state-machines.md)).
- Enforcement dựa vào hai admission plugin của kube-apiserver; tắt plugin thì placement hỏng im lặng (risk 4, [IMPLEMENTATION-PLAN §11](../IMPLEMENTATION-PLAN.md)). `PodTolerationRestriction` từ chối bằng HTTP 500 (mục 15).
- Tenant không thấy Node qua Proxy, kể cả node của chính mình; portal hiển thị node và capacity (mục 2).

## Kiểm chứng

- Isolation Dedicated trên lab: 4/4 subtest pass (`operator/test/isolation/dedicated_test.go`, `TestIsolationDedicated`; [isolation-cluster.md](../report/isolation-cluster.md), mục Dedicated). D1 Pod chạy đúng `wk3`, QoS Guaranteed; D2 requests 100m limits 200m bị LimitRange từ chối; D3 Pod Workspace khác tolerate taint của Organization bị từ chối (500); D4 xin 2 CPU khi node 2,5 CPU đã giữ 1 bị `409 DEDICATED_NODE_INSUFFICIENT_CAPACITY`.
- Kịch bản persona: Water và Green 1 CPU mỗi cái Succeeded; Workspace thứ ba 1 CPU bị từ chối với message "node fdp-k8s-wk3 can hold 2500m requestsCpu; its Workspaces hold 2 and this asks 1 more"; xóa Water thì Green giữ Ready, `wk3` còn 1500m trên 2500m ([e2e-personas.md](../report/e2e-personas.md)).
- Backend: `TestDedicatedWorkspaces` (`backend/internal/httpapi/workspaces_test.go`, hai create cùng lúc cho 0,5 CPU cuối, đúng một 202 và một 409), `TestDedicatedNodes` (`admin_pools_test.go`), `TestDedicatedReservationLifecycle` (`backend/internal/provisioning/status_test.go`), `TestReservableAndRemaining` và `TestCapacityState` (`backend/internal/reservation/capacity_test.go`).
- Operator envtest: `TestNodeGrant`, `TestWorkspaceDedicated` (`operator/internal/controller/compute_test.go`).
- Web e2e: `web/e2e/dedicated.spec.ts` ("Dedicated Workspaces share a node without overbooking it"). `make cluster-verify` kiểm `PodNodeSelector` và `PodTolerationRestriction` đang bật.
- Chưa kiểm trên lab: nhánh `Overcommitted` do drift (chỉ có unit test `TestCapacityState`), node NotReady làm Workspace `Degraded`. Cả hai cần dừng kubelet hoặc đổi allocatable trên node, tức phải ssh và sudo vào node.
