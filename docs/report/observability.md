# Observability trên lab: Prometheus, Grafana và dashboard Tenantry

Cài bằng `deploy/cluster/90-observability.sh` (IMPLEMENTATION-PLAN M12 mục 1), Namespace `observability`, chạy trên node system (`fdp-k8s-wk1`). Kiểm lần cuối ngày 2026-11-23: 8/8 target `up`, Grafana nạp dashboard, cả 20 panel có truy vấn chạy được. Lúc đó lab rảnh, backend `41713e6`: DB pool api 0/20, worker-groupsync 1/6, worker-provisioning 3/6 connection, không có lần chờ nào.

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
| `prometheus` | chính nó |

## 20 panel và câu hỏi mỗi panel trả lời

Phần trên là Workspace Operator, phần dưới (sau hàng "Platform API and workers") là backend.

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

## Đọc dashboard khi demo

Sau mỗi bước trên portal (tạo Workspace, thêm Namespace, tạo Route), nhìn panel 16 (outbox lên rồi về 0), panel 1 (Workspace qua Provisioning rồi Ready), panel 9 (Route ready) và panel 2/15 (thời gian tạo). Số liệu trễ tối đa khoảng 1 phút vì Prometheus scrape mỗi 60 giây. Panel 4, 10, 19 và 20 là phần "sức khoẻ": lỗi reconcile bằng 0, probe toàn Reconciled, DB pool dưới trần và không có lần chờ, tức là platform đang khoẻ.

## Lưu ý trước demo

- Lab không có Workspace nào (sau demo-reset): panel 1 hiện số 0 ở cả 6 phase (operator từ `7f2809c`, chạy trên lab từ revision 13), còn panel 7 "No data" cho tới khi có Workspace đầu tiên có CIDR.
- Panel 2 và 15 chỉ có số khi đã có Workspace được tạo trong 1 giờ gần nhất, kể từ lần khởi động gần nhất của operator (panel 2) hoặc backend (panel 15). Muốn có số khi trình bày thì tạo một Workspace trước demo ít nhất vài phút.
- Panel 18, dòng failures/s, để trống khi chưa có lần sync nào lỗi kể từ lúc worker khởi động. Đó là trạng thái tốt.
