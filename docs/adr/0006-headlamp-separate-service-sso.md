# ADR-0006: Headlamp là service riêng của mỗi Cluster, vào bằng SSO của portal

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §12](../PRODUCT-PLAN.md), [03 mục 9](../architecture/03-flows.md), [05 mục 8](../architecture/05-network-and-scheduling.md), [06 mục 1, 2](../architecture/06-security-and-identity.md), [ADR-0003](0003-groups-as-oidc-claims.md), [ADR-0013](0013-kubelogin-oidc-per-cluster-capsule-proxy.md), [ADR-0022](0022-portal-session-follows-keycloak-sso.md)

## Bối cảnh

Portal chỉ quản lý Organization, Workspace, quyền, tài nguyên, Namespace và Route. User vẫn cần xem và sửa object Kubernetes (Pod, log, Deployment) trên web, không chỉ bằng kubectl (FR-08). Viết một Kubernetes UI trong portal là một dự án riêng, không liên quan tenancy.

Yêu cầu bảo mật cho web console: không có ServiceAccount quyền rộng, mọi quyền là của token user, đi qua Capsule Proxy để lọc tài nguyên cluster-scoped, không truyền token qua URL (§12.4, §12.5).

## Quyết định

**Tích hợp ở trải nghiệm, tách ở kỹ thuật** (§12.1):

- Mỗi Cluster chạy một Headlamp (chart upstream, `deploy/cluster/values/headlamp.yaml`) trên system pool, subdomain riêng (`console-a.tenantry.site`).
- Portal có nút Open Web Console; `GET /workspaces/{id}/console-link` lấy URL từ Cluster registry kèm deep link vào Namespace. Cluster `Unavailable` thì trả `409 CLUSTER_NOT_READY`; `Maintenance` vẫn mở được.
- Mỗi Cluster một OIDC client confidential `headlamp-{clusterId}`, callback exact, audience riêng, có group mapper. kube-apiserver của Cluster chấp nhận audience đó cùng với `kubernetes` ([06 mục 2](../architecture/06-security-and-identity.md)).
- Headlamp gửi token của user tới Capsule Proxy (`kube-a`). Chart tắt `clusterRoleBinding` mặc định (cluster-admin); token ServiceAccount mount vào Pod mang audience không ai nhận (`headlamp-has-no-kubernetes-rights`).
- Mở ở tab mới, không iframe. SSO đi qua redirect và cookie của Keycloak; vì portal đã có SSO session, user thường vào thẳng không nhập lại password.

## Phương án đã cân nhắc

### Nhúng Headlamp bằng iframe trong portal

§12.2 liệt kê vấn đề: CSP và `frame-ancestors`, third-party cookie và `SameSite`, callback OIDC trong frame, routing và nút back, cấu hình sai có thể cho portal và Headlamp chạm tài nguyên của nhau, khó debug WebSocket, nâng Headlamp có thể phá layout portal. Không chọn cho kiến trúc chính; để ở stretch như một thử nghiệm.

### Fork Headlamp, hoặc tự viết workload UI trong portal

§12.3: đồ án thành dự án bảo trì Kubernetes UI, phải theo API và plugin của Headlamp, khó nhận bản vá bảo mật. Không chọn.

### Headlamp dùng ServiceAccount của nó và tự lọc theo user

Cấu hình mặc định của chart bind ServiceAccount vào cluster-admin. Lúc đó Headlamp thành một điểm có toàn quyền cụm đứng ở public edge, và cách ly phụ thuộc vào Headlamp thay vì RBAC và Capsule. Không chọn: §12.4 và PRODUCT-PLAN §20.2 yêu cầu ServiceAccount của Headlamp không có quyền workload.

### Một Headlamp trung tâm cho mọi Cluster

Đặt central plane lên đường dữ liệu của mọi Cluster và dùng chung một client secret. Không chọn: mỗi Cluster một instance và một client để Cluster lỗi không kéo console của Cluster khác, và rotate secret độc lập ([06 mục 2](../architecture/06-security-and-identity.md)).

## Hệ quả

- Với code và deploy, portal và từng Headlamp độc lập; nâng Headlamp không đụng portal.
- Thêm Cluster (Phase 2) là thêm một Headlamp và một client `headlamp-{clusterId}` (IMPLEMENTATION-PLAN M13).
- Headlamp không gọi thẳng Service của Proxy được (client-go in-cluster luôn nói HTTPS, Proxy chạy HTTP sau Traefik), nên Pod Headlamp có `hostAliases` trỏ `kube-a` về IP nội bộ của node Traefik. Headlamp phụ thuộc Traefik; đổi node ingress phải cài lại Headlamp ([05 mục 8](../architecture/05-network-and-scheduling.md)).
- Trang Nodes của Headlamp rỗng hoặc báo thiếu quyền, kể cả với Dedicated, vì Proxy chặn mọi tài nguyên cluster-scoped ([known-limitations](../report/known-limitations.md) mục 2). Portal hiển thị node và capacity thay.
- Deep link giả định `inClusterContextName` bằng cluster id `cluster-a` (known-limitations mục 4).
- Headlamp giữ token trong cookie; với nhiều group, cookie gần giới hạn 4 KiB của trình duyệt (known-limitations mục 3).
- User có nhiều Workspace trong cùng Cluster thấy mọi thứ họ được phép trên Headlamp của Cluster đó; không phải lỗi isolation (§12.5).
- Headlamp hỏi "Sign in" một lần rồi Keycloak trả về ngay: đúng một cú bấm, không có auto-login bằng token trong URL.
- Logout portal kết thúc SSO session, nên Headlamp cũng phải đăng nhập lại ([ADR-0022](0022-portal-session-follows-keycloak-sso.md)).

## Kiểm chứng

- `web/e2e/access.spec.ts` "a Developer and a Viewer reach their own Workspace only, through the proxy and the console": Joe mở console từ link của portal, vào bằng session Keycloak của portal không nhập password, thấy Namespace của mình; trang `/c/cluster-a/nodes` không hiện node nào.
- `web/e2e/access.spec.ts` "the console signs in someone in 20 groups": user 22 group (JWT 3033 byte) đăng nhập Headlamp được, cookie 3067/3071 byte (IMPLEMENTATION-PLAN, kết quả M11 track A; known-limitations mục 3).
- [token-size](../report/token-size.md): đường Traefik → Headlamp → Capsule Proxy trả 201 với token 21 group, kể cả thêm header 64 KiB.
- `TestKubeconfigAndConsole` (`backend/internal/httpapi/workspaces_test.go`): deep link vào Namespace, 404 cho người không thấy Workspace, 409 `CLUSTER_NOT_READY` khi Cluster `Unavailable`.
- Tính năng Helm của Headlamp đang tắt (§12.4 "disable Helm/cluster features"): Headlamp v0.46.0 trên lab chạy không có cờ `-enable-helm` (mặc định tắt) và thư mục `/headlamp/plugins` rỗng, nên không có Helm operations hay app catalog. Kiểm bằng args của Deployment và `ls` trong Pod ngày 2026-11-23, chưa có test tự động. Dù có bật, Helm của Headlamp cũng chỉ chạy bằng token của user qua Capsule Proxy.
- Logout rồi đổi user trên Headlamp (§12.4 "test logout và đổi user"): `web/e2e/headlamp.spec.ts` PASS trên lab ngày 2026-11-24 (2,6 phút, [e2e-portal](../report/e2e-portal.md) mục "Headlamp and logout"). Joe bấm Log out trong Headlamp, lần Sign in sau phải hiện form đăng nhập Keycloak. Bea (Viewer của Workspace khác) đăng nhập trên cùng trình duyệt chỉ thấy Namespace của mình, menu không mang tên Joe, không xem được log Pod của Joe.
  - Lần chạy đầu tìm ra một lỗi thật: Headlamp v0.46.0 tự kết thúc phiên ở Keycloak và xin quay về `https://${CONSOLE_HOST}/encoded-clusters/<cluster>/login`, realm chỉ cho `https://${CONSOLE_HOST}/` nên Keycloak báo "Invalid redirect uri" và giữ nguyên SSO session; Sign in kế tiếp vào thẳng bằng người vừa đăng xuất. Sửa ở `bc28255`: `post.logout.redirect.uris` của `headlamp-cluster-a` thêm đúng `https://${CONSOLE_HOST}/encoded-clusters/*`, kiểm bằng `deploy/keycloak/test_realm.py`.
- WebSocket qua Headlamp (exec, log stream): cùng spec trên, qua Capsule Proxy bằng token của người dùng. Log stream chạy tiếp ít nhất 3 dòng khi đang mở; Terminal trả lời lệnh sau 4,2–4,3 s.
