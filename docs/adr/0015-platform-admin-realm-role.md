# ADR-0015: Platform Admin là account cùng realm, phân biệt bằng realm role `platform-admin`

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §4.1, §10.1, §20.1](../PRODUCT-PLAN.md), [06 mục 2, 3.2, 5, 7, 8](../architecture/06-security-and-identity.md), [07 mục 1, 6](../architecture/07-ux-wireflow.md), [ADR-0002](0002-keycloak-accounts-platform-db-organizations.md), [ADR-0022](0022-portal-session-follows-keycloak-sso.md)

## Bối cảnh

Platform Admin đăng ký Cluster, duyệt hoặc tạo Organization, cấp entitlement, ClusterGrant, node, pool, disable user và force operation ([06 mục 8](../architecture/06-security-and-identity.md)). Phải chọn admin đăng nhập bằng gì và backend nhận ra admin thế nào.

Docs đã có sẵn một realm `tenantry` với ba loại client cho người dùng ([06 mục 2](../architecture/06-security-and-identity.md)), portal là BFF với phiên trong PostgreSQL ([ADR-0022](0022-portal-session-follows-keycloak-sso.md)). MVP có một người vận hành và một cụm.

## Quyết định

- **Platform Admin là account Keycloak bình thường trong realm `tenantry`**, được gán realm role `platform-admin`. Không có hệ identity riêng cho admin ở MVP (PRODUCT-PLAN §4.1; changelog v0.4 → v0.5 mục 11).
- API chép `realm_access.roles` của access token vào `sessions.realm_roles` lúc đăng nhập và ở mỗi lần refresh. Middleware `requirePlatformAdmin` kiểm role cho mọi path `/api/v1/admin/`; `:force`, `GET /operations/{id}` và `:retry` của Organization mà admin không là member tự kiểm role trong service `operation` ([IMPLEMENTATION-PLAN mục 3.5](../IMPLEMENTATION-PLAN.md)).
- Frontend chỉ dùng `GET /api/v1/me` (`isPlatformAdmin`) để hiện khu `/admin`; backend luôn kiểm lại (§10.1, §20.1).
- Role không cho quyền thành viên: Platform Admin không phải member thì nhận 404 ở `/organizations/{orgId}`, `/workspaces/*`, `/groups/*` như mọi người ngoài; làm việc với Organization qua `/api/v1/admin/*` ([06 mục 8](../architecture/06-security-and-identity.md)).
- Role không cho quyền Kubernetes: kube-apiserver không map realm role sang group. Admin muốn kubectl vào Tenant phải qua group binding như người khác, hoặc dùng kubeconfig admin ngoài platform ([06 mục 2](../architecture/06-security-and-identity.md)).
- Portal không có API gán hay gỡ `platform-admin`; việc đó làm trong Keycloak admin console, chỉ vào được qua `port-forward` ([06 mục 1](../architecture/06-security-and-identity.md)).

## Phương án đã cân nhắc

### Realm riêng cho admin (hoặc dùng realm `master`)

Tách hẳn danh sách account admin khỏi account khách hàng. Không chọn ở MVP: portal phải nói chuyện với hai issuer, hai bộ client, hai luồng đăng nhập và hai loại phiên, trong khi chỉ có một người vận hành. Realm `master` còn bị khóa khỏi internet có chủ ý, vì `admin-cli` của nó nhận mật khẩu trực tiếp ([06 mục 1](../architecture/06-security-and-identity.md)).

### Identity plane riêng (VPN, bastion, client certificate cho khu admin)

Mạnh hơn về cô lập, nhưng thêm hạ tầng ngoài phạm vi đồ án; PRODUCT-PLAN §2.3 loại compliance enterprise. Không chọn ở MVP.

### Cờ admin trong platform database (`users.is_admin`)

Không phụ thuộc Keycloak. Không chọn: tách nơi giữ quyền mạnh nhất khỏi nơi giữ credential, và ai ghi được DB (API và hai worker dùng chung role PostgreSQL, [06 mục 1](../architecture/06-security-and-identity.md)) là tự phong admin được. Với realm role, Keycloak giữ cả credential lẫn role, và gỡ role có hiệu lực theo cùng cơ chế refresh của phiên.

## Hệ quả

- Một lần đăng nhập, một loại phiên, một bộ thời hạn; admin dùng mọi tính năng account của Keycloak (reset, MFA khi bật) như người khác.
- Gỡ `platform-admin` có hiệu lực trên portal chậm nhất 5 phút, ở lần refresh kế tiếp; trong lúc Keycloak không trả lời, request nhận 500 chứ không dùng tiếp role cũ ([06 mục 3.2, 5](../architecture/06-security-and-identity.md)).
- Role có trong mọi token của admin, kể cả token cho Kubernetes; vô hại vì Kubernetes không map realm role ([06 mục 2](../architecture/06-security-and-identity.md)).
- Account admin dùng chung trang đăng nhập public với khách hàng. Realm export bật chống brute-force nhưng không bắt OTP cho admin; MFA enrollment UI nằm ở stretch (PRODUCT-PLAN §25). MVP và demo không bật OTP. Khuyến nghị cho production: bắt OTP cho account giữ `platform-admin` bằng conditional OTP theo role của Keycloak (cấu hình realm, không đổi code).
- Ai gán được realm role là tự phong admin được. Bản đầu, client `tenantry-sync` có `manage-users` nên làm được việc đó, và secret của nó mạnh ngang Platform Admin ([06 mục 7](../architecture/06-security-and-identity.md), [known-limitations](../report/known-limitations.md) mục 24). `deploy/cluster/keycloak-fgap.py` thu hẹp nó bằng fine-grained admin permissions v2: không `map-roles`, không reset password, và mọi user giữ `platform-admin` được đưa vào group `tenantry:platform-admins` mà `tenantry-sync` bị cấm chạm. Lần áp đầu ở M12 thất bại và đã rollback; bản sửa sau đó xanh trên lab, `tenantry-sync` chỉ còn `query-users` và `query-groups` cùng các permission của FGAP.
- Muốn tách admin ra realm riêng về sau thì phải thêm issuer thứ hai cho API; dữ liệu Organization không đổi vì không gắn với role.

## Kiểm chứng

- `TestAdminAuthorizationMatrix` (`backend/internal/httpapi/authz_admin_test.go`) và `TestAdminNeedsTheRole` (`backend/internal/httpapi/admin_test.go`): endpoint admin cần role.
- `TestRefresh` (`backend/internal/httpapi/refresh_test.go`): role gỡ ở Keycloak vẫn còn tới khi access token hết hạn, sau lần refresh thì mất; `TestRefreshWhileKeycloakDown`: Keycloak không trả lời thì 500, không dùng role cũ.
- `web/e2e/onboarding.spec.ts` "a person without the role cannot open the admin area": người không có role bị đưa về `/`, `GET /api/v1/admin/organizations` trả 403.
- Admin làm việc qua portal trên lab: `web/e2e/admin.spec.ts`, `web/e2e/health.spec.ts`; [e2e-personas](../report/e2e-personas.md) dùng account `admin` duyệt Organization và cấp node.
- Test cho bản thu hẹp: `TestLiveKeycloakNarrowed` (`backend/internal/keycloak/client_live_test.go`) kiểm 403 cho cấp `platform-admin`, reset password, sửa, disable hay đưa Platform Admin vào group; chỉ chạy khi đặt `KEYCLOAK_TEST_FGAP`. FGAP đã bật trên lab ở M12; kết quả chạy test này trên lab chưa được ghi vào report nào. `make cluster-verify` (nhánh FGAP bật) chạy `keycloak-fgap.py check`.
