# ADR-0017: Shared là soft allocation có overcommit ratio, Dedicated là hard reservation

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §0 (v0.5 → v0.6 điểm 5), §5.3, §5.4, §6.2, §6.3](../PRODUCT-PLAN.md), [05 mục 3, 4](../architecture/05-network-and-scheduling.md), [02 mục 3, 4](../architecture/02-data-model.md), [04 mục 3, 5](../architecture/04-state-machines.md)

## Bối cảnh

Workspace có hai compute mode (§6). Shared chạy trên pool dùng chung giữa nhiều Organization; Dedicated chạy trên node Platform Admin cấp riêng cho một Organization. Cả hai đều có allocation CPU/RAM/Pod, nhưng hai mode hứa hai điều khác nhau:

- Shared hứa rẻ và có ngay. Workspace hiếm khi dùng hết quota cùng lúc; giữ chỗ cứng theo quota thì phần lớn pool nằm không.
- Dedicated hứa "Workspace được reserve 4 CPU / 8 GiB trên node này", và câu đó phải kiểm chứng được. Nếu tổng reservation vượt dung lượng node thì lời hứa không còn nghĩa.

Kubernetes không biết gì về reservation, nó chỉ thấy requests/limits khi Pod đã chạy (§5.3). Phép tính giữ chỗ phải nằm ở platform database.

## Quyết định

- **Shared: soft allocation.** Quota Workspace là trần, không phải chỗ giữ. Trần của pool là `Σ requests của mọi Workspace Shared trên pool (mọi Organization) ≤ allocationOvercommitRatio × reservable`, với `reservable = Σ node (allocatable − systemReserve)`. Admin đặt ratio cho từng pool (`numeric(4,2)`, `CHECK ≥ 1`; seed của lab là 3.0). `limits` chỉ bị trần bởi quota của Workspace và quota limits của grant, `limits ≥ requests` (§5.4).
- **Dedicated: hard reservation, ratio cố định 1.** `Σ NodeReservation Pending/Active/Releasing + reservation mới ≤ allocatable − systemReserve` của node (§6.3). Bắt buộc Guaranteed QoS: `requests = limits`, LimitRange `maxLimitRequestRatio: {cpu: 1, memory: 1}`. Quota của Workspace đặt đúng bằng reservation.
- Cả hai kiểm dưới row lock trong cùng transaction tạo/resize: `shared_compute_pools FOR UPDATE` (Workspace Shared luôn khóa pool) hoặc `dedicated_node_grants FOR UPDATE` ([02 mục 4](../architecture/02-data-model.md)). Vượt trả `COMPUTE_POOL_CAPACITY_EXCEEDED` hoặc `DEDICATED_NODE_INSUFFICIENT_CAPACITY` kèm `details.remaining`.
- Quota CPU/RAM/Pod của ClusterGrant chỉ tính Workspace Shared; Dedicated chỉ bị giới hạn bởi node của chính Organization (§5.2, [ADR-0007](0007-cluster-grant-per-organization.md)).
- Dedicated resize xuống giữ reservation cũ tới khi Operation `Succeeded`, để DB không bao giờ thấp hơn `hard` đang áp trên Cluster. Drift (allocatable giảm, systemReserve tăng) làm node `Overcommitted`: chặn reservation mới, Workspace liên quan `Degraded`, không tự evict ([04 mục 5](../architecture/04-state-machines.md)).
- Trên Cluster, allocation thành quota chung của Tenant (`Tenant.spec.rules[].quota[]`, Capsule chặn ở admission trên tổng mọi Namespace), cho cả hai mode (§5.4).

## Phương án đã cân nhắc

### Cả hai mode cùng hard reservation

Không chọn: shared tier sẽ đắt như dedicated. Với `shared-general` của lab (2 node, mỗi node 3 CPU) chỉ vài Workspace là hết chỗ dù Pod thật dùng rất ít. Overcommit có kiểm soát là đặc điểm của shared tier.

### Cả hai mode cùng soft (Dedicated cũng có ratio)

Không chọn: lời hứa "được reserve trên node" mất nghĩa; Pod của Workspace Dedicated có thể `Pending` vì Workspace khác cùng Organization. Không còn gì phân biệt Dedicated với một shared pool riêng.

### Không giữ chỗ ở platform, để scheduler tự lo

Chỉ dùng ResourceQuota, không cộng allocation ở DB. Không chọn: không trả lời được "còn tạo thêm được bao nhiêu" trước khi tạo, không chặn được hai request đồng thời overbook node, và Admin không có con số nào để quyết định ratio.

### Dedicated cho phép `limits > requests` (Burstable)

Không chọn: reservation dùng requests; limits cao hơn nghĩa là tổng limits trên node vượt dung lượng vật lý, đúng điều hard reservation muốn loại (§5.4).

## Hệ quả

- Shared có thể có Pod `Pending` khi nhiều Workspace cùng dùng hết quota; đây là hành vi chấp nhận của shared tier, portal hiện `allocated / (ratio × reservable)` để cảnh báo (§6.2).
- Admin phải tự chọn ratio. Tài liệu muốn hiện mức dùng thực (Prometheus) cạnh ceiling để Admin chỉnh ([05 mục 4](../architecture/05-network-and-scheduling.md)); dashboard Grafana hiện có `tenantry_shared_pool_allocatable` và số node Ready, chưa có mức dùng thực so với allocation (chưa kiểm).
- Bớt node, hạ ratio hoặc tăng systemReserve mà làm trần xuống dưới tổng allocation đều bị từ chối.
- Dedicated là Guaranteed: workload cần burst phải xin requests cao hơn ([known-limitations](../report/known-limitations.md) mục 7). Resize xuống chỉ nhả reservation khi cụm đã áp xong (cùng mục 7).
- Hard reservation chỉ đứng được nếu quota trên Cluster không bị vượt khi tạo Pod đồng thời. Đo trên Capsule 0.15.2 (§5.4): `rules[].quota` cho đúng 3 Pod ở cả 3 lần chạy (12 Pod 300m bắn đồng thời, `requests.cpu: 1`); cách cũ `spec.resourceQuotas` lọt 5–6 Pod, vượt trần 50–80%. Vì vậy chọn `rules[].quota`.
- Namespace quota của Capsule trễ khoảng 1 giây (lab: 4 trên quota 3, [known-limitations](../report/known-limitations.md) mục 6). Không ảnh hưởng CPU/RAM vì quota chung tính trên mọi Namespace.
- `:validate` của wizard chạy create trong transaction rồi rollback, giữ lock của pool hoặc node vài ms ([known-limitations](../report/known-limitations.md) mục 9).

## Kiểm chứng

- `backend/internal/reservation/capacity_test.go`:
  - `TestPoolCeiling`: 2 node 12 CPU, systemReserve 1 CPU mỗi node, reservable 22 CPU; ratio 3.0 cho trần 66 CPU; ratio 2.5 trên 1 CPU cho 2500m.
  - `TestReservableAndRemaining`, `TestCapacityState` (`Available`, `PartiallyAllocated`, `FullyAllocated`, `Overcommitted`).
- `backend/internal/httpapi/workspaces_test.go`:
  - `TestSharedPoolCeiling`: pool ratio 1, một node 2.5 CPU; Workspace 2 CPU được, Workspace thêm 1 CPU bị `COMPUTE_POOL_CAPACITY_EXCEEDED` (`limit=2500m`, `allocated=2`); làm rỗng pool đang dùng bị từ chối.
  - `TestDedicatedWorkspaces`: node 3 CPU, reserve 500m; hai Workspace 1 CPU được, Workspace thứ ba 1 CPU bị `DEDICATED_NODE_INSUFFICIENT_CAPACITY` (`reservable=2500m`, `reserved=2`); `limitsCpu` khác requests bị `400`; hai create đồng thời tranh 500m cuối, đúng một `202` một `409`; node về `FullyAllocated`; resize xuống giữ reservation cũ.
- `backend/internal/provisioning/status_test.go`, `TestDedicatedReservationLifecycle`.
- Lab, [isolation-cluster](../report/isolation-cluster.md) mục Dedicated, 4/4 pass: D1 Pod chạy trên `wk3`, QoS Guaranteed; D2 container requests 100m, limits 200m bị LimitRange từ chối; D4 Workspace thứ hai xin 2 CPU trên 2.5 reservable khi đã giữ 1 bị `409`.
- Lab, [e2e-personas](../report/e2e-personas.md): Water và Green 1 CPU mỗi cái trên `wk3`, Workspace thứ ba 1 CPU bị từ chối với message "node fdp-k8s-wk3 can hold 2500m requestsCpu; its Workspaces hold 2 and this asks 1 more"; xóa Water thì `wk3` còn 1500m trên 2500m.
- `web/e2e/dedicated.spec.ts`: "Dedicated Workspaces share a node without overbooking it".
- Lab, `TestSharedPending` ngày 2026-11-23 19:19Z ([observability](../report/observability.md) mục "Pool đầy trên lab"): hai Workspace Shared, cộng lại 8 CPU, cấp được vì dưới trần 15 CPU của `shared-general`: ratio 3,0 × reservable 5 CPU (2 node × (3 CPU − 500m system reserve)); allocatable là 6 CPU. Kết quả 4 Pod Running, 4 Pod `Pending` với `0/4 nodes are available: 2 Insufficient cpu, …`. Panel 21 cho request 4,265 trên allocatable 6, allocated 8, ceiling 15; panel 22 cho 4 Pod không xếp được ở Workspace `over`. Pod kẹt trước khi request chạm allocatable vì chỗ trống chia trên từng node. Lúc đó (operator revision 16) portal vẫn hiện Workspace `Ready` và quota CPU 4/4, không có dấu hiệu Pod Pending.
- Lab, `TestSchedulingStatus` ngày 2026-11-24 với operator revision 18 ([observability](../report/observability.md) mục "Portal cho thấy Pod không xếp được"): Pod xin 4 CPU, lớn hơn mọi node 3 CPU của pool, có trong `status.scheduling` của Workspace trong vòng dưới 2 s, có trên portal sau 2 s, với message `0/4 nodes are available: 2 Insufficient cpu, …`; xoá Pod thì cả hai hết trong 2 s. Workspace vẫn `Ready` suốt lần chạy.
