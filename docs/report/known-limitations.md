# Giới hạn đã biết (MVP, đo trên lab tới M12)

Mỗi mục ghi: hành vi, vì sao, ảnh hưởng tới người dùng, và cách xử lý hoặc hướng làm tiếp. Số liệu đo nằm trong các report cùng thư mục (`isolation-cluster.md`, `failure-cluster.md`, `benchmark.md`, `token-size.md`).

## Quyền truy cập

1. **Thu hồi quyền Kubernetes không tức thì khi gỡ người khỏi group.** Token đã cấp còn hiệu lực tới hết TTL (≤ 5 phút); token mới không còn group. Gỡ binding của group khỏi Workspace thì mất quyền ngay (RoleBinding bị xóa); portal mất quyền ngay trong mọi trường hợp (`architecture/06-security-and-identity.md` mục 5). UI ghi đúng hai mốc này.
2. **Capsule Proxy chặn mọi tài nguyên cluster-scoped.** Gate `ProxyClusterScoped` của capsule-proxy 0.15.3 (Alpha) đóng lỗ đọc Node của bản mặc định; hệ quả là `kubectl get nodes` và StorageClass qua Proxy trả rỗng hoặc 403 cho mọi tenant, kể cả Dedicated. Portal hiển thị node, capacity và allow-list StorageClass. Gate Alpha: Proxy CrashLoop khi một APIService không Available lúc khởi động; `cluster-verify` kiểm APIService, runbook ở `architecture/05-network-and-scheduling.md` mục 8.
3. **Kubeconfig cần kubelogin**, một context cho mỗi Workspace, không kèm CA (kube-a dùng cert Let's Encrypt được tin sẵn).
4. **Link sâu vào Headlamp** giả định `inClusterContextName` của Headlamp bằng cluster id (`cluster-a`).
5. **Owner của Workspace sửa được ProxySetting** trong Namespace của mình (quyền admin trong ns); vì Node đã bị chặn, việc này chỉ lộ tên Namespace của chính Tenant đó.

## Tài nguyên và quota

6. **Namespace quota trễ khoảng 1 giây**: hai Namespace tạo cách nhau dưới 1 giây có thể cùng lọt (lab: 4 trên quota 3).
7. **Dedicated là Guaranteed**: limits = requests, LimitRange `maxLimitRequestRatio: 1`. Resize xuống thì reservation cũ được giữ tới khi cluster áp xong.
8. **Mỗi Workspace một Calico IPPool cố định kích thước**: không đổi được prefix sau khi tạo. Xóa Workspace thì block vào quarantine 10 phút, giữ luôn slug/`k8s_name`; xóa Organization bị chặn tới khi quarantine cuối cùng hết (`ORGANIZATION_NOT_EMPTY` ghi giờ).
9. **`:validate` của wizard** chạy create trong transaction rồi rollback: giữ lock của org/grant/pool vài ms, và block "nếu tạo bây giờ" có thể khác lúc tạo thật.

## Capsule 0.15.2

10. **Race replication nội bộ Capsule**: khi Namespace mới xuất hiện (hoặc đang bị xóa), hai controller của Capsule cùng replicate `GlobalTenantResource` và một bên báo lỗi (`already exists`, `the object has been modified`, `being terminated`) cho tới lần reconcile kế tiếp. Operator coi ba dạng này là đang xử lý trong tối đa 2 phút (trước sửa: 45% mẫu Degraded nhầm; sau sửa: 0). Nháp issue upstream: `upstream-issue-capsule-replication-race.md` (chưa gửi).
11. **Operation chờ thay vì Failed khi CR Degraded tạm thời**; chỉ Failed khi CR báo `Failed` hoặc quá `TENANTRY_OPERATION_TIMEOUT` (10 phút), lúc đó người dùng bấm Retry.
12. **Capsule đặt nhịp tạo Workspace** (không phải operator): mặc định 1 worker; lab chạy 4 worker, QPS 40, burst 80 (Ready p50 khi tạo cùng lúc 20 Workspace: 2m36s → 1m17s). Thêm worker là ALPHA trong Capsule.
13. **Capsule `namespace-cleanup` gỡ finalizer của mọi object trừ Pod** trong Namespace tenant đang xóa: PVC finalizer (kể cả của tool backup) không giữ được việc xóa; chỉ Pod kẹt giữ được.
14. **`:force` khi có Pod kẹt** bỏ lại Tenant và Namespace Terminating (đã gỡ quyền, có Event `ForceDeleteLeftovers`); Capsule dọn nốt khi Pod hết kẹt. Bước IPPool vẫn chờ IPAM sạch; IP rò gỡ tay bằng `calicoctl` (runbook ở 05 mục 5, `release` chưa thử trên lab).
15. **PodTolerationRestriction từ chối toleration ngoài whitelist bằng HTTP 500** (cách plugin upstream trả lời), không phải 403.
16. **Pod mới trong Namespace mới bị từ chối vài giây** (VAP `tenantry-tenant-network-ready`, 422 message cố định của apiserver) tới khi NetworkPolicy baseline có mặt; Deployment tự lên sau khoảng 6 giây.

## Mạng và Route

17. **Route chỉ host → Service:port**, HTTP/HTTPS (gồm WebSocket, gRPC); chưa có path, timeout, custom domain. Route `Active` nghĩa là Ingress đã có, không bảo đảm Pod chạy (không có Pod thì 503).
18. **Internet egress mở cho mọi Workspace** (trừ dải private và metadata endpoint); chưa có profile egress chặt hơn.

## Vận hành

19. **Một provisioning worker cho mỗi cluster**, không leader election (replicas 1); housekeeping chạy chung Deployment và idempotent.
20. **Metrics scrape qua Service**: API nhiều replica thì mỗi lần scrape chỉ thấy một Pod; port metrics của backend không có auth, chỉ trong cụm. Prometheus trên lab không có PVC (retention 24h, mất khi restart), Grafana không có Ingress (port-forward).
21. **Portal tải font từ Google Fonts** (CSP cho phép), chưa tự host.
22. **Trang Audit lọc phía client** trên các trang đã tải; danh sách Workspace không phân trang (tối đa `maxWorkspacesTotal`).
23. **Postgres local cho dev dùng mật khẩu mặc định** `tenantry` (chỉ `docker-compose.dev.yml` và Makefile); chart lab sinh mật khẩu ngẫu nhiên.
24. **Client `tenantry-sync` có `manage-users`** (mạnh ngang Platform Admin); thu hẹp bằng fine-grained admin permissions v2 của Keycloak là việc còn lại của M12.

## Chưa kiểm hết

- Cookie session OIDC của Headlamp trong trình duyệt khi user thuộc khoảng 20 group (JWT ~3 KiB, giới hạn cookie ~4 KiB): kiểm bằng Playwright trong `web/e2e/access.spec.ts`.
- `calicoctl ipam release` cho IP rò: chưa có IP rò thật để thử.
