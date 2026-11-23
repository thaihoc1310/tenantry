# ADR-0003: Group đi vào OIDC claim, Kubernetes tự authorize request

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §4.3, §9.3, §10.1–10.4](../PRODUCT-PLAN.md), [03 mục 4, 8](../architecture/03-flows.md), [05 mục 8](../architecture/05-network-and-scheduling.md), [06 mục 1, 2, 4, 5](../architecture/06-security-and-identity.md), [ADR-0002](0002-keycloak-accounts-platform-db-organizations.md), [ADR-0005](0005-fixed-role-templates.md), [ADR-0013](0013-kubelogin-oidc-per-cluster-capsule-proxy.md)

## Bối cảnh

Portal biết ai thuộc group nào và group nào có role gì ở Workspace nào (PostgreSQL, [ADR-0002](0002-keycloak-accounts-platform-db-organizations.md)). User lại làm việc với Kubernetes bằng kubectl và Headlamp, đi qua Capsule Proxy tới kube-apiserver. Phải chọn ai quyết định một request Kubernetes được phép.

Ràng buộc từ docs:

- Portal không bao giờ cầm token gọi được Kubernetes ([06 mục 1](../architecture/06-security-and-identity.md)).
- Central plane không nằm trên đường dữ liệu: Phase 2 có nhiều Cluster, Cluster `Unavailable` không được kéo portal theo và ngược lại (PRODUCT-PLAN §2.2, §12.4).
- Cam kết thu hồi phải nói được bằng một con số, không hứa "revoke ngay" ([06 mục 5](../architecture/06-security-and-identity.md)).

## Quyết định

- **Hai lớp authorization**: lớp 1 (Platform API, dữ liệu DB) quyết định cấu hình; lớp 2 (kube-apiserver, RBAC, Capsule, admission) quyết định runtime. Không có đường nào để portal trực tiếp "cho phép" một request Kubernetes ([06 mục 4](../architecture/06-security-and-identity.md)).
- Group Sync Worker đưa membership sang Keycloak với tên machine immutable `org_<ULID>:group_<ULID>` (§4.3). Chỉ client `kubectl` và `headlamp-{clusterId}` có group mapper; client `portal` không có.
- kube-apiserver dùng structured authentication: username `oidc:<sub>`, group `oidc:<claim groups>`, audience `kubernetes` và `headlamp-cluster-a` ([05 mục 8](../architecture/05-network-and-scheduling.md)).
- Binding group ↔ Workspace được controller render thành Capsule owner (TenantOwner) và `additionalRoleBindings` (Developer, Viewer), xem [ADR-0005](0005-fixed-role-templates.md). Group `oidc:tenantry:tenant-users` là Capsule user group, do worker tính lại khi binding đổi (§9.3).
- Không introspection: kube-apiserver tự kiểm chữ ký và hạn của JWT. Access token sống 5 phút, đó là trần độ trễ thu hồi khi gỡ người khỏi group.
- Gỡ khỏi group không logout user: Keycloak chạy lại group mapper ở mỗi lần refresh, nên token kế tiếp đã mất group ([06 mục 5](../architecture/06-security-and-identity.md)).
- ValidatingAdmissionPolicy `tenantry-tenant-rolebinding-subjects` chỉ cho RoleBinding do tenant tạo trỏ tới group của chính Organization hoặc ServiceAccount của chính Tenant, để mọi quyền Kubernetes vẫn chỉ đến qua group (§10.4).

## Phương án đã cân nhắc

### Portal làm proxy authorization cho Kubernetes

Mọi request kubectl/Headlamp đi qua portal; portal tra DB rồi impersonate user tới kube-apiserver. Thu hồi tức thì, nhưng portal phải cầm quyền impersonate, tức gần bằng cluster-admin, trái với [06 mục 1](../architecture/06-security-and-identity.md). Central plane nằm trên đường dữ liệu của mọi Cluster: portal chết là mất kubectl ở mọi nơi. Không chọn.

### Webhook authorizer hoặc introspection gọi về portal/Keycloak ở mỗi request

kube-apiserver hoặc Capsule Proxy hỏi portal (hoặc endpoint introspection của Keycloak) cho từng request. [06 mục 5](../architecture/06-security-and-identity.md) loại introspection ở Proxy: thêm độ trễ và đặt Keycloak lên đường dữ liệu. Webhook authorizer về portal có cùng nhược điểm, cộng với việc sửa cờ kube-apiserver cho mỗi Cluster. Immediate revocation thật sự không thuộc scope đồ án (§9.3).

### RoleBinding cho từng user thay vì cho group

Gỡ người là xóa RoleBinding, nên mất quyền ngay. Nhưng mỗi thay đổi membership thành một lần ghi Kubernetes ở mọi Workspace người đó có quyền, qua outbox và controller, và số RoleBinding tăng theo số user thay vì số group. Mô hình binding của PRODUCT-PLAN §10.4 là group ↔ Workspace. Không chọn; lý do rút từ mô hình binding, chưa đo.

### Đưa display name của group vào claim

Hai Organization có thể cùng có group `developers` (§4.3). Không chọn; claim mang machine name immutable.

## Hệ quả

- Portal hay PostgreSQL chết không ảnh hưởng request Kubernetes với token còn hạn; Keycloak chết thì không lấy được token mới.
- Gỡ khỏi group: portal mất quyền ngay, Kubernetes mất quyền chậm nhất khi token hiện tại hết hạn ([known-limitations](../report/known-limitations.md) mục 1). Gỡ binding group khỏi Workspace thì mất quyền ngay vì RoleBinding bị xóa.
- Token lớn dần theo số group, khoảng 45 byte mỗi group (known-limitations mục 3).
- Thu hồi phụ thuộc Group Sync: Keycloak lỗi lâu thì token mới vẫn mang group cũ ([03 mục 4](../architecture/03-flows.md)).
- Hai lớp lệch có chủ ý: OrgOwner không nằm trong group nào bound vào Workspace thì toàn quyền cấu hình trên portal nhưng kubectl bị 403; UI phải giải thích ([06 mục 4](../architecture/06-security-and-identity.md)).
- Cam kết "chậm nhất 5 phút" chỉ đúng khi VAP `tenantry-tenant-rolebinding-subjects` có mặt; gỡ VAP là gỡ cam kết.

## Kiểm chứng

- Thu hồi đo trên lab: `web/e2e/access.spec.ts` "someone taken out of a group loses the portal at once and Kubernetes within 5 minutes". Gỡ lúc 23:15:41.8Z, token cũ bị từ chối lúc 23:20:41.7Z, đúng lúc token hết hạn, 300 giây ([known-limitations](../report/known-limitations.md) mục 1).
- [e2e-personas](../report/e2e-personas.md): Alice gỡ Joe khỏi Organization, token mới không còn `developers` và bị 403 ở Solar; token cũ dùng được tới khi hết hạn.
- Kích thước token: [token-size](../report/token-size.md), 21 group, JWT 3,009 byte, cả ba đường (kube-apiserver, Traefik → Capsule Proxy, Traefik → Headlamp → Capsule Proxy) trả 201 với 22 tên `oidc:`, kể cả khi thêm header 64 KiB (`TestTokenSize`, `operator/test/isolation/jwt_test.go`).
- RoleBinding do owner tự tạo: [isolation-cluster](../report/isolation-cluster.md) T08, bốn lần bind ra ngoài Organization bị VAP từ chối (403), bind group của chính Organization được phép (`TestIsolation`, `operator/test/isolation/isolation_test.go`).
- M4 trên Keycloak 26.8: refresh token lấy trước khi đổi group vẫn ra token theo membership mới; sau khi gỡ, claim `groups` biến mất trong khoảng 60 giây ([06 mục 5](../architecture/06-security-and-identity.md)).
- Token có group mới: `web/e2e/people.spec.ts` "an invited newcomer joins, gets a group, and their token names it". `oidc:tenantry:tenant-users` theo binding: `TestTenantUsersFollowBindings` (`backend/internal/keycloak/tenantusers_test.go`).
