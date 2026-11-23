# ADR-0020: Tenant khai báo Route thay vì tự viết Ingress, mọi Route dùng chung một wildcard cert

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §0 (v0.7 → v0.8 điểm 8, 9, 10; v0.5 → v0.6 điểm 4), §2.4, §7.6](../PRODUCT-PLAN.md), [05 mục 6, 7](../architecture/05-network-and-scheduling.md), [06 mục 1](../architecture/06-security-and-identity.md), [02 mục 4](../architecture/02-data-model.md)

## Bối cảnh

Tenant cần đưa Service ra internet qua HTTPS ([PRODUCT-PLAN §2.4](../PRODUCT-PLAN.md)). Mỗi Cluster có một Traefik dùng chung, domain thật `tenantry.site`, cert từ Let's Encrypt.

Thiết kế v0.6 cho tenant tự viết Ingress với hostname `<app>.<org>-<ws>.apps...`, mỗi Workspace một wildcard Certificate do CA nội bộ cấp, Capsule `allowedHostnames` khóa vùng tên. v0.8 bỏ thiết kế đó. Lý do ghi ở [§7.6](../PRODUCT-PLAN.md) và [05 mục 7](../architecture/05-network-and-scheduling.md):
- Ingress do tenant viết, kèm annotation tùy ý, là một đường tấn công ingress controller đã biết;
- ClusterRole `admin` mà Capsule cấp cho chủ Workspace có sẵn quyền ghi Ingress, và cert-manager gộp quyền ghi `Certificate`/`Issuer` vào `admin`. Không chặn thì tenant xin được cert cho hostname của người khác qua ClusterIssuer;
- user không nên phải viết YAML Ingress.

## Quyết định

**Route thay Ingress.** User tạo Route trên portal: Namespace, tên, Service, cổng. API ghép hostname `<tên>-<org-slug>-<ws-slug>.<apps domain>` (ví dụ `web-acme-solar.mvp.tenantry.site`), giữ `UNIQUE (hostname) WHERE state <> 'Deleted'`, tối đa 20 Route mỗi Workspace. Route xuống Cluster qua `PlatformWorkspace.spec.routes`; sub-reconciler `routes` dựng một Ingress cho mỗi Route trong đúng Namespace đó, IngressClass `platform-shared`, không có `tls.secretName` (`operator/internal/render/routes.go`), rồi báo `status.routes`.

**Tenant không ghi đường vào.** VAP `tenantry-tenant-ingress-lockdown` từ chối user tenant (`oidc:`) CREATE/UPDATE/DELETE Ingress, mọi tài nguyên `cert-manager.io`, `acme.cert-manager.io`, `gateway.networking.k8s.io`, `traefik.io` trong Namespace tenant, trả 403. Luật chỉ khớp người gọi là tenant, nên operator, cert-manager và garbage collector vẫn xóa được. Service `NodePort`/`LoadBalancer`/`ExternalName` bị Capsule cấm, nên Route là đường ra duy nhất. Traefik chỉ bật provider Kubernetes Ingress.

**Một wildcard cert.** cert-manager xin `*.mvp.tenantry.site` từ Let's Encrypt bằng DNS-01 qua Cloudflare (`deploy/cluster/manifests/wildcard-certificate.yaml`, Secret `mvp-wildcard-tls` trong `ingress-system`). Cert này là cert mặc định của Traefik. Hostname phẳng một nhãn dưới apps domain để một wildcard phủ được mọi Route, vì wildcard chỉ phủ đúng một nhãn. Hostname của platform (`portal`, `auth`, `kube-a`, `console-a`) nằm ngoài apps domain.

## Phương án đã cân nhắc

### Tenant tự viết Ingress, khóa hostname bằng Capsule

Thiết kế v0.6. Không chọn vì các lý do ở Bối cảnh: annotation tùy ý tới ingress controller, quyền ghi `Certificate`/`Issuer` tự gộp vào `admin`, và user phải viết YAML.

### Mỗi Workspace một wildcard cert

Cũng là v0.6 (`*.<org>-<ws>.apps...`). Không chọn: số cert tăng theo số Workspace, trong khi Let's Encrypt giới hạn số cert mỗi domain mỗi tuần; MVP với một wildcard chỉ cần năm cert ([05 mục 7](../architecture/05-network-and-scheduling.md)). Mỗi Workspace còn cần một lượt DNS-01 riêng.

### Mỗi Route một cert (HTTP-01)

Không chọn cho apps domain: số cert tăng theo số Route, cùng giới hạn của Let's Encrypt; Ingress phải mang `tls.secretName` nên private key nằm trong Namespace tenant. Cách này chỉ dành cho custom domain về sau, khi domain là của chính khách hàng ([§7.6](../PRODUCT-PLAN.md)).

### Gateway API hoặc `IngressRoute` của Traefik

Không bật, để chỉ một loại object mở được đường vào ([05 mục 7](../architecture/05-network-and-scheduling.md)).

## Hệ quả

Mặt tốt:
- Private key của apps domain không bao giờ nằm trong Namespace tenant.
- Hai Workspace không giữ được cùng hostname; Route không trùng được host của platform.
- User chỉ chọn Service và cổng; portal hiện URL và trạng thái từng Route.

Mặt xấu và giới hạn:
- Route chỉ là host → Service:port, HTTP/HTTPS (gồm WebSocket, gRPC); chưa có path, timeout, custom domain. Route `Active` nghĩa là Ingress đã có, không bảo đảm Pod chạy (không có Pod thì 503) ([known-limitations](../report/known-limitations.md) mục 17).
- Helm chart có sẵn Ingress phải tắt phần đó.
- Ghép chuỗi có thể trùng: (`a-b`, `c`) và (`a`, `b-c`) ra cùng hostname, cái đến sau nhận `ROUTE_HOSTNAME_TAKEN`; nhãn đầu tối đa 63 ký tự.
- Một cert cho mọi Route: lộ private key của wildcard là lộ cho mọi Route của Cluster. Key chỉ nằm trong `ingress-system`.
- Route cùng registrable domain `tenantry.site` với portal và Keycloak: trang tenant đặt được cookie `Domain=tenantry.site`, có thể làm login CSRF với cookie của Keycloak. Portal tự bảo vệ bằng cookie `__Host-` và CSRF header; Phase 2 chuyển apps domain sang registrable domain riêng ([06 mục 1](../architecture/06-security-and-identity.md)).
- NetworkPolicy `tenant-allow-ingress-controller` cho Traefik tới mọi Pod trong Namespace, không chỉ Pod được expose ([05 mục 6](../architecture/05-network-and-scheduling.md), điểm cần review).
- TLS dừng ở Traefik; phía sau là HTTP trong mạng Pod ([05 mục 7](../architecture/05-network-and-scheduling.md)).

## Kiểm chứng

- Web e2e trên lab `web/e2e/routes.spec.ts` ("a Route publishes a Service over HTTPS and goes with its Namespace"): Route trả lời qua HTTPS, Route tới Service không có thì báo lý do, hostname trùng bị từ chối, tenant không tự ghi được Ingress, xóa Namespace thì Route và Ingress đi theo.
- Backend `TestWorkspaceRoutes` (`backend/internal/httpapi/workspaces_test.go`) và `TestRoutesFollowTheStatus` (`backend/internal/provisioning/status_test.go`).
- Operator envtest `TestWorkspaceRoutes` (`operator/internal/controller/routes_test.go`): một Ingress mỗi Route trong đúng Namespace, không bao giờ ở Namespace của tenant khác, Route hỏng không làm Workspace Degraded. VAP Ingress và RoleBinding được kiểm qua `TestChartPolicies` (`operator/internal/controller/policies_test.go`, `access_policies_test.go`).
- Isolation trên lab, T18: Ingress cho hostname của B và Certificate cho `portal.tenantry.site` đều bị `tenantry-tenant-ingress-lockdown` từ chối (403) ([isolation-cluster.md](../report/isolation-cluster.md)).
- Persona: Route `web` → Service `web`:80 `Active`, `https://web-<org>-solar.mvp.tenantry.site` trả 200 ([e2e-personas.md](../report/e2e-personas.md)).
- `make cluster-verify`: "every Certificate is Ready", "https://demo.${APPS_DOMAIN} answers with a valid certificate", "http://demo.${APPS_DOMAIN} redirects to HTTPS" (`deploy/cluster/verify.sh`).
- Chưa kiểm: custom domain (chưa làm).
