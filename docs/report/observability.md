# Observability trên lab: Prometheus, Grafana và dashboard Tenantry

Cài bằng `deploy/cluster/90-observability.sh` (IMPLEMENTATION-PLAN M12 mục 1), Namespace `observability`, chạy trên node system (`fdp-k8s-wk1`). Kiểm lần đầu ngày 2026-11-23: 8/8 target `up`, Grafana nạp dashboard, cả 20 panel có truy vấn chạy được. Lúc đó lab rảnh, backend `41713e6`: DB pool api 0/20, worker-groupsync 1/6, worker-provisioning 3/6 connection, không có lần chờ nào. Cùng ngày, sau buổi demo, thêm kube-state-metrics (chart prometheus revision 2): 9/9 target `up`, Grafana nạp dashboard 22 panel. Truy vấn của panel 21 và 22 đã chạy trên Prometheus thật (kết quả ở mục dưới).

## Mở Grafana

Grafana không có Ingress. Mở bằng port-forward từ máy có kubeconfig của lab:

```sh
kubectl --context hocbt@kubernetes -n observability port-forward svc/grafana 3000:80
```

Vào <http://localhost:3000>. Tài khoản là `admin`. Mật khẩu do chart sinh, nằm trong Secret `grafana`, key `admin-password`, Namespace `observability`:

```sh
kubectl --context hocbt@kubernetes -n observability get secret grafana \
  -o jsonpath='{.data.admin-password}' | base64 -d; echo
```

Dashboard: Dashboards → folder **Tenantry** → **Tenantry** (uid `tenantry-operator`, đường dẫn `/d/tenantry-operator/tenantry`). Mặc định xem 6 giờ gần nhất, tự làm mới mỗi 30 giây.

Prometheus không có giao diện công khai. Muốn xem target: `kubectl -n observability port-forward svc/prometheus-server 9090:80`, rồi vào <http://localhost:9090/targets>.

## Prometheus scrape gì

Mỗi 60 giây, giữ 24 giờ, không có PVC (restart Pod thì mất dữ liệu cũ; known-limitations mục 20).

| Job | Target |
|---|---|
| `tenantry-operator` | `tenantry-operator-metrics.tenantry-operator.svc:8443` (HTTPS, token của SA `prometheus-server`, được bind ClusterRole `tenantry-operator-metrics-reader`) |
| `tenantry-backend` | `tenantry-api-metrics`, `tenantry-worker-provisioning-metrics`, `tenantry-worker-groupsync-metrics` (Namespace `tenantry-system`, port 9090) |
| `capsule`, `capsule-proxy` | metrics Service của hai thành phần trong `capsule-system` |
| `kube-apiserver` | `kubernetes.default.svc:443`, chỉ giữ metric request |
| `kube-state-metrics` | `prometheus-kube-state-metrics.observability.svc:8080`: subchart của chart prometheus (KSM 2.20.0), chỉ Pod, Node, Namespace và 6 metric |
| `prometheus` | chính nó |

## 22 panel và câu hỏi mỗi panel trả lời

Phần trên là Workspace Operator, giữa (sau hàng "Platform API and workers") là backend, cuối (21, 22) là sức chứa của shared pool, ghép từ kube-state-metrics, operator và backend.

| # | Panel | Trả lời câu hỏi |
|---|---|---|
| 1 | Workspaces by phase | Cụm đang có bao nhiêu Workspace ở mỗi phase (Provisioning, Ready, Updating, Degraded, Failed, Deleting)? |
| 2 | Provision duration (create to first Ready) | Từ lúc PlatformWorkspace được tạo trên cụm (creationTimestamp) tới phase Ready lần đầu mất bao lâu (p50, p95, cửa sổ 1 giờ)? |
| 3 | Reconciles per second, by controller | Mỗi controller của operator đang làm việc nhiều tới đâu? |
| 4 | Reconcile errors per second, by controller | Có controller nào reconcile lỗi không? |
| 5 | Reconcile duration p95, by controller | Một lần reconcile chậm nhất (p95) mất bao lâu? |
| 6 | Work queue depth | Có object nào đang xếp hàng chờ reconcile không? Hàng đợi tăng mãi là controller bị tụt lại. |
| 7 | Workspace IP usage | Mỗi Workspace đã dùng bao nhiêu phần trăm CIDR của nó? Gần 100% thì Workspace đó không chạy thêm Pod được. |
| 8 | Shared pools: Ready nodes and allocatable CPU | Mỗi shared pool còn bao nhiêu node Ready và bao nhiêu CPU allocatable? |
| 9 | Routes not ready | Bao nhiêu Route đã có Ingress (ready), bao nhiêu chưa? |
| 10 | Capsule Proxy probes, last 15 minutes, by result | Capsule Proxy có đang lọc đúng không: probe thấy đúng Namespace của Workspace (Reconciled), hay ProxyUnreachable/ProxyNotFiltering? |
| 11 | Operator requests to the API server, by code | Operator gọi kube-apiserver bao nhiêu request mỗi giây, theo mã trả về? |
| 12 | Platform API and workers | (Tiêu đề hàng, không có số liệu.) |
| 13 | API requests per second, by status | Platform API đang nhận bao nhiêu request, bao nhiêu lỗi 4xx/5xx? |
| 14 | API latency p95 | Request chậm nhất (p95) của Platform API mất bao lâu? |
| 15 | workspace.create duration p50 / p95 | Operation `workspace.create` từ lúc tạo trên portal tới lúc kết thúc (Succeeded hoặc Failed) mất bao lâu (cửa sổ 1 giờ)? |
| 16 | Outbox: pending events and age of the oldest | Còn bao nhiêu event chưa giao tới cụm, event cũ nhất đã chờ bao lâu? Khác 0 lâu là provisioning worker chậm hoặc dừng. |
| 17 | Free address blocks per network pool and prefix length | Mỗi network pool còn cấp được bao nhiêu CIDR theo từng prefix? |
| 18 | Group sync: pending, oldest, failures | Còn bao nhiêu thay đổi group/membership chưa đẩy sang Keycloak, cái cũ nhất chờ bao lâu, có lỗi không? |
| 19 | DB pool: connections in use against the maximum | Từng process (api, worker-provisioning, worker-groupsync) đang dùng bao nhiêu connection so với tối đa (20, 6, 6)? Chạm trần là mọi request phải chờ. |
| 20 | DB pool: waits for a connection | Có lần nào phải chờ connection không, và chờ trung bình bao lâu? Bình thường là 0; dòng thời gian chờ trung bình để trống khi chưa có lần chờ nào. |
| 21 | Shared pools: CPU requested by running Pods, allocatable, allocated and ceiling | Với từng shared pool: các Pod đang chạy trên node của pool xin bao nhiêu CPU (kube-state-metrics), node giữ được bao nhiêu (operator), platform đã cấp bao nhiêu cho Workspace Shared và trần cấp phát `ratio × reservable` (backend). Request chạm allocatable là Pod mới không xếp được nữa, dù phần đã cấp còn dưới trần (ADR-0017). Pod có thể kẹt sớm hơn: chỗ trống tính trên từng node, nên tổng còn 1,7 CPU vẫn không xếp được Pod 1 CPU nào (mục "Pool đầy trên lab"). |
| 22 | Pods the scheduler cannot place, by Workspace | Workspace nào có Pod Pending vì scheduler không tìm được chỗ? Với Workspace Shared thường nghĩa là pool đầy, không phải quota của Workspace hết. |

## Đọc dashboard khi demo

Sau mỗi bước trên portal (tạo Workspace, thêm Namespace, tạo Route), nhìn panel 16 (outbox lên rồi về 0), panel 1 (Workspace qua Provisioning rồi Ready), panel 9 (Route ready) và panel 2/15 (thời gian tạo). Số liệu trễ tối đa khoảng 1 phút vì Prometheus scrape mỗi 60 giây. Panel 4, 10, 19 và 20 là phần "sức khoẻ": lỗi reconcile bằng 0, probe toàn Reconciled, DB pool dưới trần và không có lần chờ, tức là platform đang khoẻ. Panel 21 và 22 trả lời "pool còn chỗ không": khoảng cách giữa CPU đã request và allocatable là chỗ còn xếp được Pod, còn panel 22 trống là không ai đang chờ chỗ.

## Lưu ý trước demo

- Lab không có Workspace nào (sau demo-reset): panel 1 hiện số 0 ở cả 6 phase (operator từ `7f2809c`, chạy trên lab từ revision 13), còn panel 7 "No data" cho tới khi có Workspace đầu tiên có CIDR.
- Panel 2 và 15 chỉ có số khi đã có Workspace được tạo trong 1 giờ gần nhất, kể từ lần khởi động gần nhất của operator (panel 2) hoặc backend (panel 15). Muốn có số khi trình bày thì tạo một Workspace trước demo ít nhất vài phút.
- Panel 18, dòng failures/s, để trống khi chưa có lần sync nào lỗi kể từ lúc worker khởi động. Đó là trạng thái tốt.
- Panel 21: hai dòng "allocated to Workspaces" và "allocation ceiling" lấy từ gauge `tenantry_shared_pool_allocated` và `tenantry_shared_pool_allocation_ceiling` của worker provisioning (đã có trên lab, xem mục "Pool đầy trên lab").

## kube-state-metrics trên lab

- Cài bằng `90-observability.sh` (values ở `deploy/cluster/values/prometheus.yaml`). Pod `prometheus-kube-state-metrics` chạy trên node system `fdp-k8s-wk1`, request 10m / 32Mi, limit 128Mi.
- Đo ngày 2026-11-23 sau khi cài (`kubectl top`): 1m CPU, 11Mi RAM. Cả node wk1 lúc đó dùng 168m CPU và 2661Mi RAM.
- Khoảng 390 series: `kube_pod_status_phase` 260, `kube_pod_container_resource_requests` 85, `kube_node_status_allocatable` 24, `kube_namespace_labels` 19, `kube_node_labels` 4. `kube_pod_status_unschedulable` chỉ có series khi có Pod không xếp được.
- Chỉ có quyền list/watch Pod, Node và Namespace (ClusterRole của chart sinh theo danh sách collector). Nhãn được giữ: nhãn pool `tenantry.io/compute-pool` của Node và nhãn Tenant `capsule.clastix.io/tenant` của Namespace.
- Lần chạy đầu: panel 21 cho `shared-general` 0,285 CPU được request trên 3 CPU allocatable (lúc đó pool chỉ có wk2, vì wk3 vừa được cấp cho demo). Panel 22 trống vì không có Pod nào chờ chỗ.

## Pool đầy trên lab

`TestSharedPending` (`operator/test/isolation/measure_test.go`), chạy ngày 2026-11-23 19:19–19:33Z, operator revision 16, độc quyền lab:

- Dựng: một Organization optest, grant 8 CPU trên `shared-general`; hai Workspace Shared `fill` và `over`, mỗi cái quota 4 CPU và một Deployment 4 Pod pause, mỗi Pod 1 CPU (requests bằng limits). Pool có wk2 và wk3, allocatable 6 CPU; trước khi chạy, wk2 đã có 235m được request (Pod `demo` và DaemonSet), wk3 có 30m. Tổng 8 CPU vẫn dưới trần cấp phát 15 CPU, nên API nhận cả hai Workspace. Trần tính trên phần reservable chứ không trên allocatable (ADR-0017): mỗi node 3 CPU trừ system reserve 500m, nên reservable 2 × 2,5 = 5 CPU, khác allocatable 6 CPU; với ratio 3,0 của pool trên lab (đọc trong DB, `shared_compute_pools`), trần là 3,0 × 5 = 15 CPU.
- Kết quả: 4 Pod Running, tất cả của `fill`; 4 Pod Pending, tất cả của `over`. Dự đoán là 5 Running, nhưng thực tế mỗi node chỉ xếp được 2 Pod 1 CPU: wk2 còn 0,765 CPU, wk3 còn 0,97 CPU, không node nào còn đủ 1 CPU.
- Message trên Pod (`PodScheduled=False`, reason `Unschedulable`), giống trong event `FailedScheduling`: `0/4 nodes are available: 2 Insufficient cpu, 2 node(s) had untolerated taint(s). no new claims to deallocate, preemption: 0/4 nodes are available: 2 No preemption victims found for incoming pod, 2 Preemption is not helpful for scheduling.` Hai node "taint" là cp1 và wk1 (system pool).
- Dashboard trong lúc giữ pool đầy (150 s, hai lần scrape):
  - panel 21: CPU request của Pod đang chạy là 4,265, allocatable 6; allocated to Workspaces 8 (19:21–19:23Z, về 0 khi test xoá Workspace), allocation ceiling 15;
  - panel 22: `optestzxzmgg-over` có 4 Pod không xếp được.
- Portal: cả hai Workspace `state: Ready`, mọi condition `True`; usage của mỗi cái là `requestsCpu` 4/4, `pods` 4/10. ResourceQuota tính cả Pod Pending, nên `over` trông như đang dùng hết quota, và portal không có dấu hiệu nào cho thấy Pod của nó không chạy. Người dùng chỉ thấy Pending qua kubectl hoặc Headlamp; admin thấy qua panel 21 và 22. Đó là lúc chạy (operator revision 16); từ revision 18 portal có hiện, xem mục dưới.
- Dọn: scale về 0, xoá Workspace, xoá Organization sau quarantine (`deletableAfter` 19:32:36Z). Sau đó không còn Pod optest trên wk2, wk3; `demo-preflight` 15/15 PASS.

### Portal cho thấy Pod không xếp được (operator revision 18)

Từ revision 18 (3617cdf), operator ghi `status.scheduling` khi có Pod của Workspace mà scheduler báo `PodScheduled=False`, reason `Unschedulable`. Trường này gồm số Pod, message và `since` của Pod kẹt lâu nhất, kèm số Pod như vậy của từng Namespace. Workspace vẫn `Ready`. Operator chỉ cache Pod `Pending` (field selector `status.phase=Pending`), không cache mọi Pod. Backend chép trường này sang API (backend src-6103476ea3f6, web src-3411de693f79).

`TestSchedulingStatus` (`operator/test/isolation/scheduling_test.go`), chạy ngày 2026-11-24 04:43Z, độc quyền lab:

- Dựng: một Workspace Shared quota 4 CPU, một Pod xin 4 CPU. Mỗi node của `shared-general` chỉ có 3 CPU, nên Pod không bao giờ xếp được.
- Operator: `status.scheduling` có 1 Pod ngay ở lần đọc đầu sau khi tạo Pod (test đọc 2 s một lần, nên dưới 2 s). Message: `0/4 nodes are available: 2 Insufficient cpu, 2 node(s) had untolerated taint(s). no new claims to deallocate, preemption: 0/4 nodes are available: 4 Preemption is not helpful for scheduling.` Namespace đếm 1, phase `Ready`.
- Portal: `GET /workspaces/{id}` có `scheduling` 2 s sau khi tạo Pod, `state` vẫn `Ready`.
- Xoá Pod: operator bỏ `status.scheduling` ở lần đọc đầu (dưới 2 s), Namespace về 0; portal hết `scheduling` 2 s sau lần xoá. Workspace vẫn `Ready`.
- Dọn: xoá Workspace, xoá Organization sau quarantine, không còn Pod optest trên wk2, wk3; `demo-preflight` 15/15 PASS. Phía đầu cuối (UI), `web/e2e/scheduling.spec.ts` của track A PASS trên cùng bản deploy.

