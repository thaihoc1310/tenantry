# ADR-0005: Workspace dùng ba role template cố định, không có RBAC tùy ý

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §2.3, §2.4, §10.3–10.5](../PRODUCT-PLAN.md), [06 mục 4, 8](../architecture/06-security-and-identity.md), [ADR-0003](0003-groups-as-oidc-claims.md)

## Bối cảnh

Quyền Kubernetes của user trong một Workspace đến từ binding group ↔ Workspace kèm một role ([ADR-0003](0003-groups-as-oidc-claims.md)). Phải chọn role đó là gì.

Ràng buộc từ docs:

- Customer không tự tạo ClusterRole, CRD, webhook và không nhập policy/YAML tùy ý vào control plane (PRODUCT-PLAN §2.3).
- Ma trận quyền phải test được bằng `kubectl auth can-i` và negative test phía server (§10.5).
- Built-in `edit` mà lab cũ dùng rộng hơn nhu cầu: có quyền với Secret và exec gián tiếp (§10.5).
- Cam kết thu hồi quyền chỉ đúng khi mọi quyền đến qua group của Organization (§10.4).

## Quyết định

Ba role template, enum `TenantOwner | Developer | Viewer` trong `backend/openapi/tenantry.v1.yaml`:

| Template | Ánh xạ | Ý chính |
|---|---|---|
| `TenantOwner` | Capsule owner group của Tenant (Capsule cấp ClusterRole `admin` trong các Namespace) | tạo Namespace, quản trị workload và RBAC trong Tenant |
| `Developer` | `additionalRoleBindings` tới `tenantry:tenant-developer` | CRUD workload, log, exec, port-forward; ghi Secret nhưng không `get`/`list`; Ingress, NetworkPolicy chỉ đọc; không RoleBinding, không quota |
| `Viewer` | `additionalRoleBindings` tới `tenantry:tenant-viewer` | chỉ đọc, có log; không Secret, không exec |

- Hai ClusterRole nằm trong chart operator, `deploy/charts/tenantry-operator/files/tenant-roles.yaml`; operator render binding ở `operator/internal/render/render.go`.
- Mỗi Workspace có ít nhất một group `TenantOwner`; API từ chối thao tác làm mất owner cuối cùng (`LAST_OWNER_BINDING`, §10.4).
- Một group có thể giữ role khác nhau ở mỗi Workspace.
- Trong Namespace của mình, TenantOwner vẫn tự tạo được Role/RoleBinding bằng kubectl (capability contract §2.4), nhưng subject chỉ được là group của chính Organization hoặc ServiceAccount của chính Tenant (VAP `tenantry-tenant-rolebinding-subjects`, [06 mục 4](../architecture/06-security-and-identity.md)).
- Role của Organization (OrgOwner, OrgAdmin, OrgMember) là platform role, không tự sinh quyền Kubernetes (§9.2).

## Phương án đã cân nhắc

### RBAC tùy ý, hoặc custom role builder trên portal

User tự chọn verb và resource, portal sinh ClusterRole/Role. Không chọn ở MVP (§10.3): ma trận không còn hữu hạn để test; role sinh ra có thể chạm tới tài nguyên cluster-scoped hay quyền nằm ngoài khuôn Tenant, trái §2.3; UI và validation phức tạp. IMPLEMENTATION-PLAN liệt "custom role builder" vào danh sách cắt trước khi trễ; "fine-grained role template ngoài ba role chuẩn" nằm ở stretch (PRODUCT-PLAN §25).

### Dùng nguyên built-in `edit` và `view`

Ít việc nhất, nhưng `edit` cho đọc Secret và các quyền gián tiếp rộng hơn nhu cầu (§10.5); nội dung của nó còn đổi theo phiên bản Kubernetes và theo aggregation label của CRD cài thêm. Không chọn.

### Bind role theo từng user

Xem [ADR-0003](0003-groups-as-oidc-claims.md), phương án "RoleBinding cho từng user".

## Hệ quả

- Ma trận quyền hữu hạn, test được ở ba tầng (envtest, cluster-verify, e2e trên lab).
- Developer coi như đọc được Secret: ai tạo được Pod thì mount được mọi Secret của Namespace. Chỉ Viewer không chạm được Secret ([06 mục 8](../architecture/06-security-and-identity.md), điểm cần review).
- Developer có `exec` và xem log; MVP không có policy profile để tắt bớt (§10.3).
- Nhu cầu khác ba role (CI robot identity, role hẹp hơn) chưa có; nằm ở stretch.
- Hai ClusterRole dùng chung cho mọi Workspace: sửa file là đổi quyền của mọi Workspace cùng lúc sau khi nâng chart.
- TenantOwner có ClusterRole `admin` trong Namespace nên sửa được ProxySetting của Tenant mình; vì Node đã bị chặn, việc này chỉ lộ tên Namespace của chính Tenant đó ([known-limitations](../report/known-limitations.md) mục 5).
- OrgOwner không nằm trong group nào bound vào Workspace thì kubectl bị 403 dù toàn quyền trên portal ([06 mục 4](../architecture/06-security-and-identity.md)).

## Kiểm chứng

- Envtest bằng SubjectAccessReview trên đúng file ClusterRole của chart: `TestTenantRoles` (`operator/internal/controller/rbac_test.go`).
- `make cluster-verify` trên lab: hàm `tenant_roles` trong `deploy/cluster/verify.sh` kiểm `can-i` của Developer (tạo Deployment, `pods/exec` có; `get secrets`, tạo RoleBinding, tạo Ingress không) và Viewer (list Pod, `pods/log` có; `get secrets`, `pods/exec`, xóa Pod không).
- Token OIDC thật qua `kube-a`: `web/e2e/access.spec.ts` "a Developer and a Viewer reach their own Workspace only, through the proxy and the console". Developer thấy Namespace của Workspace mình, tạo Pod được, không tạo được Namespace hay Ingress, không vào Workspace khác; Viewer chỉ đọc.
- [e2e-personas](../report/e2e-personas.md): Bob (Viewer) list được Pod, bị từ chối xóa Deployment và đọc Secret (403), bị từ chối tạo Namespace trên portal (403).
- [isolation-cluster](../report/isolation-cluster.md) T08: owner không bind được group của Organization khác, `oidc:tenantry:tenant-users`, User lẻ hay ServiceAccount của Tenant khác (VAP, 403).
- Owner cuối cùng: case "no TenantOwner" trả 409 `LAST_OWNER_BINDING` trong `TestCreateWorkspaceRefusals`, và `TestWorkspaceBindings` (`backend/internal/httpapi/workspaces_test.go`).
