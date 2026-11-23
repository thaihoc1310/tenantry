# ADR-0013: Kubeconfig dùng kubelogin exec OIDC và trỏ vào Capsule Proxy của từng Cluster

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §11, §8.4, §20.3](../PRODUCT-PLAN.md), [03 mục 8](../architecture/03-flows.md), [05 mục 8](../architecture/05-network-and-scheduling.md), [06 mục 1, 2, 5](../architecture/06-security-and-identity.md), [ADR-0003](0003-groups-as-oidc-claims.md), [ADR-0006](0006-headlamp-separate-service-sso.md)

## Bối cảnh

User tải kubeconfig từ portal để dùng kubectl và helm trong Workspace. Hai câu hỏi:

1. **Credential**: file chứa gì để chứng minh user là ai?
2. **Endpoint**: kubectl nói chuyện với đâu?

Ràng buộc: thu hồi quyền phải theo được group ([ADR-0003](0003-groups-as-oidc-claims.md)); portal không phát token gọi được Kubernetes ([06 mục 1](../architecture/06-security-and-identity.md)); `kubectl get ns` của tenant chỉ được thấy Namespace của mình, việc RBAC không làm được cho `list` toàn cluster ([03 mục 8](../architecture/03-flows.md)); Phase 2 có nhiều Cluster.

## Quyết định

- **Kubeconfig không chứa credential** (§11.2). User dùng exec plugin `kubectl oidc-login get-token` (kubelogin có sẵn), `--oidc-issuer-url` của realm `tenantry`, `--oidc-client-id=kubectl`. Plugin lo browser flow, cache và refresh token; user cài một lần.
- Client `kubectl` là public + PKCE, redirect `http://localhost:8000` và `:18000`, audience `kubernetes`, có group mapper, dùng chung cho mọi Cluster ([06 mục 2](../architecture/06-security-and-identity.md)). Nếu user đã đăng nhập portal, SSO của Keycloak thường không hỏi lại password.
- **Server là Capsule Proxy của Cluster chứa Workspace** (`https://kube-a.tenantry.site`), lấy từ `workspace.cluster_id` → `clusters.proxy_endpoint`; user không nhập endpoint (§11.1). Mỗi Cluster một endpoint; nhiều Cluster thì nhiều context khác server, cùng một identity.
- Cách Proxy gọi kube-apiserver, đọc từ mã nguồn capsule-proxy 0.15.3:
  - Proxy xác định người gọi bằng `TokenReview` trên chính token của user (`internal/request/http.go:203-227`).
  - Request thường (Pod, Deployment, Secret…): Proxy thay header `Authorization` bằng token ServiceAccount của nó, rồi thêm `Impersonate-User` và `Impersonate-Group` bằng username và group vừa xác định (`internal/webserver/webserver.go:531-559`). kube-apiserver authorize theo user và group được impersonate, nên RBAC và admission vẫn là của user.
  - Request được Proxy lọc bằng label selector (list Namespace, và list tài nguyên cluster-scoped khi bật `ProxyClusterScoped`): Proxy bỏ mọi header impersonate, gắn `labelSelector` rồi gửi bằng token ServiceAccount của nó, không impersonate (`internal/webserver/webserver.go:500-529`). Kết quả chỉ đúng nhờ selector.
  - `SelfSubjectAccessReview` và `SelfSubjectRulesReview` (`internal/authorization/const.go:3-6`): Proxy gửi bằng chính token của user, rồi sửa kết quả theo Tenant (`internal/webserver/webserver.go:397-430`).
  - Vậy câu "Proxy forward token" chỉ đúng với hai loại review này; phần còn lại là impersonation hoặc ServiceAccount của Proxy.
- Proxy chạy `ProxyClusterScoped=true` và `--enable-reflector=false`: Namespace chỉ liệt kê cái của Tenant user sở hữu hoặc được ProxySetting cấp; Node, StorageClass, PV và các tài nguyên cluster-scoped khác trả rỗng hoặc 403 ([05 mục 8](../architecture/05-network-and-scheduling.md)).
- Một context mỗi Workspace, tên là `k8s_name` của Workspace, namespace mặc định là Namespace đầu tiên của Workspace nếu đã có. Context chỉ là UX, không phải ranh giới bảo mật (§11.3).
- File không kèm CA: `kube-a` dùng cert Let's Encrypt được tin sẵn (`backend/internal/httpapi/connect.go`).
- Cổng 6443 của kube-apiserver chỉ cho quản trị, phải giới hạn IP nguồn ở firewall. Tenant gọi thẳng 6443 vẫn bị RBAC và admission chặn như thường, chỉ mất phần lọc của Proxy ([06 mục 1](../architecture/06-security-and-identity.md)).

## Phương án đã cân nhắc

### Token tĩnh trong kubeconfig (ServiceAccount token hoặc client certificate)

Đơn giản cho user, không cần plugin. Không chọn: file thành credential phải bảo vệ và thu hồi; client certificate không thu hồi được trước hạn trong Kubernetes; token ServiceAccount gắn với một ServiceAccount thay vì group của user, nên gỡ người khỏi group không gỡ được quyền của file đã tải (§11.4). Portal cũng phải cầm quyền tạo token, trái [06 mục 1](../architecture/06-security-and-identity.md).

### CLI riêng của platform

Bọc login và kubeconfig trong một binary của Tenantry. Không chọn ở MVP: kubelogin đã làm đủ browser flow, PKCE, cache và refresh; CLI riêng nằm ở stretch (§11.2, §25).

### Trỏ kubeconfig thẳng vào kube-apiserver

RBAC vẫn đúng, nhưng `kubectl get ns`, `get nodes` của tenant bị 403 thay vì thấy đúng phần của mình, và mọi tenant chạm thẳng vào kube-apiserver. Không chọn; Proxy là điểm vào duy nhất của tenant.

### Một proxy trung tâm cho mọi Cluster

Một endpoint, kubeconfig gọn hơn khi có nhiều Cluster. Không chọn: central plane nằm trên đường dữ liệu của mọi Cluster, trái hướng Phase 2 "Cluster A unavailable, portal và Cluster B vẫn hoạt động" (§2.2). Mỗi Cluster đã có Capsule riêng, nên Proxy đi cùng Capsule của Cluster đó.

## Hệ quả

- Kubeconfig không cần thu hồi: user bị gỡ khỏi group vẫn giữ file, nhưng token mới không còn quyền (§11.4). Token đã phát còn dùng được tới khi hết hạn, chậm nhất 5 phút ([known-limitations](../report/known-limitations.md) mục 1).
- User phải cài kubelogin; một context mỗi Workspace (known-limitations mục 3a).
- `kubectl get nodes` và StorageClass qua Proxy rỗng hoặc 403 với mọi tenant, kể cả Dedicated; portal hiển thị node, capacity và allow-list StorageClass (known-limitations mục 2).
- Gate `ProxyClusterScoped` còn Alpha: Proxy CrashLoop nếu một APIService không Available lúc khởi động; `cluster-verify` kiểm APIService, runbook ở [05 mục 8](../architecture/05-network-and-scheduling.md).
- User có nhiều Workspace trong một Cluster thấy tổng Namespace được cấp qua cùng endpoint (§11.3).
- kubelogin cache token: test thu hồi phải xóa cache hoặc ép refresh (IMPLEMENTATION-PLAN M11, rủi ro).
- ServiceAccount của Proxy là credential rất mạnh đứng ở public edge. ClusterRole `capsule-proxy:capsule-proxy` trên lab cho `get/list/watch` mọi tài nguyên (kể cả Secret) và `impersonate` mọi `users`, `groups`, `serviceaccounts` không giới hạn tên, tức impersonate được cả `system:masters`. Lộ token đó, hoặc một lỗi lọc của Proxy, là ngang cluster-admin. Đây là giá của mô hình impersonation, không phải cấu hình riêng của Tenantry.
- Audit log của kube-apiserver ghi request thường với `user` là ServiceAccount của Proxy và `impersonatedUser` là `oidc:<sub>`. Request lọc bằng selector chỉ ghi ServiceAccount của Proxy; người gọi thật chỉ có trong log của Proxy ở mức verbose (`internal/webserver/webserver.go:501-504`).
- Lab bật gate `ProxyClusterScoped`; module node cũ của bản mặc định lọc Node theo `nodeSelector` của Tenant mà không kiểm `proxySettings`, và đọc bằng ServiceAccount của Proxy, nên tenant đọc được Node của pool (issue công khai projectcapsule/capsule-proxy#1158, 2026-11-23) ([upstream-issue-capsule-proxy-nodes](../report/upstream-issue-capsule-proxy-nodes.md)).

## Kiểm chứng

- Nội dung file: `TestKubeconfigAndConsole` (`backend/internal/httpapi/workspaces_test.go`) kiểm server là proxy endpoint và không có trường nào khác trong `cluster`, context đúng Workspace và Namespace, exec `kubectl oidc-login get-token` với client `kubectl`, không có `token:`, `client-secret`, `password`, `client-certificate`, `client-key`; có audit `workspace.kubeconfig_download`; người không thấy Workspace nhận 404.
- Qua `kube-a` với token thật của client `kubectl`: `web/e2e/access.spec.ts` (Developer chỉ thấy Namespace Workspace mình, Node rỗng hoặc cấm, không vào Workspace khác; kubeconfig từ portal chứa `server: https://kube-a.tenantry.site` và `oidc-login`). [e2e-personas](../report/e2e-personas.md): Joe dùng kubeconfig từ portal, qua proxy chỉ thấy `<org>-solar-frontend`, `helm install` chạy được.
- Lọc của Proxy: [isolation-cluster](../report/isolation-cluster.md) T13/T14, Namespace chỉ của Workspace mình; Node, một Node, status/proxy, watch, metrics trả rỗng hoặc 403 (check `proxy_view` của `cluster-verify`).
- Token đi qua Proxy: [token-size](../report/token-size.md), 21 group, Traefik → Capsule Proxy trả 201.
- Cơ chế impersonation: đọc từ mã nguồn (dòng dẫn ở mục Quyết định) và ClusterRole của Proxy trên lab. Audit log của kube-apiserver: chưa kiểm, vì audit tắt trên lab (không có `--audit-policy-file`) và bật nó cần quyền vào control-plane node; log của Proxy ở mức 2 không ghi dòng impersonate (mức 4). Đã thấy trên lab (2026-11-23 16:51Z): bật tạm `--zap-log-level=4` cho Capsule Proxy bằng `--set` rồi trả về mức 2, gửi `GET /api/v1/namespaces/default/pods` qua `kube-a`. Proxy ghi `"msg":"impersonating for the current request","username":"system:serviceaccount:tenantry-operator:tenantry-proxy-probe","groups":["system:serviceaccounts","system:serviceaccounts:tenantry-operator","system:authenticated"]` với token ServiceAccount, và `"username":"oidc:ac2b398e-c7b1-410b-863c-1c514545a26c","groups":["system:authenticated"]` với ID token của client `kubectl` cho account demo `alice` (lúc đó alice không ở group nào nên token không có claim `groups`). Cả hai được kube-apiserver trả 403, tức request đi tiếp với danh tính được impersonate, không phải danh tính của Proxy (`operator/test/isolation/proxyv4_test.go`).
- Binary kubelogin: test tự động không chạy nó. Cả Playwright lẫn persona test lấy token bằng authorization code + PKCE của client `kubectl`, như kubelogin làm (`kubectlTokens` trong `web/e2e/support.ts`). Luồng kubelogin thật: chưa kiểm trong test tự động.
