# ADR-0016: Tên Kubernetes-visible là `<org-slug>-<ws-slug>`, hai slug immutable

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §0 (v0.5 → v0.6 điểm 3, v0.7 → v0.8 điểm 19), §4.2, §4.4, §4.5, §14.2](../PRODUCT-PLAN.md), [02 mục 2, 4](../architecture/02-data-model.md), [IMPLEMENTATION-PLAN §12 quyết định 3, 4](../IMPLEMENTATION-PLAN.md)

## Bối cảnh

Mỗi Workspace sinh ra nhiều tên mà người dùng nhìn thấy hằng ngày: tên Capsule Tenant, tên CR `PlatformWorkspace`, tiền tố mọi Namespace (`kubectl get ns`), hậu tố hostname của Route, tên context trong kubeconfig. Các tên này:

- phải duy nhất trên toàn Cluster (Tenant và CR là cluster-scoped, Namespace cũng vậy);
- phải là DNS label, tối đa 63 ký tự;
- không đổi được sau khi tạo: Kubernetes không rename Namespace hay Tenant, đổi tên nghĩa là tạo lại và mất dữ liệu PVC.

Capsule `forceTenantPrefix` chỉ cho owner tạo Namespace có tiền tố `<tên Tenant>-`. Nếu tên của hai Tenant lồng tiền tố vào nhau (`acme-solar` và `acme-solar-prod`) thì owner của Tenant ngắn tạo được Namespace nằm trong vùng tên của Tenant dài.

## Quyết định

- Tên Kubernetes-visible là `k8s_name = <org-slug>-<ws-slug>`, ví dụ `acme-solar`; Namespace là `<k8s_name>-<hậu tố>`, ví dụ `acme-solar-frontend` (§4.4, §4.5).
- **Slug Organization không chứa `-`**: `^[a-z][a-z0-9]*$`, 3–20 ký tự, `CHECK` trong DB (migration `0006`). Nhờ vậy `<org>-<ws>` duy nhất theo cấu tạo, và tiền tố `<org>-` của hai Organization không bao giờ lồng nhau.
- Slug Workspace `^[a-z][a-z0-9]*(-[a-z0-9]+)*$`, 2–20 ký tự, được chứa `-`, duy nhất trong Organization. Trong một Organization, slug lồng tiền tố với Workspace chưa `Deleted` tại ranh giới `-` (`solar` và `solar-prod`) bị `409 SLUG_TAKEN`, kiểm dưới lock entitlement của Organization ([02 mục 4](../architecture/02-data-model.md)).
- Hậu tố Namespace 1–20 ký tự, nên `<org>-<ws>-<ns>` ≤ 62 (IMPLEMENTATION-PLAN §12 quyết định 4). Một số từ bị cấm làm slug (`kube`, `default`, `system`, `capsule`, `tenantry`, `admin`).
- **Cả hai slug immutable**; display name đổi tự do.
- **Định danh máy dùng ID, không dùng slug**: label `tenantry.io/organization-id`, `tenantry.io/workspace-id` trên CR (§14.2), label/taint `tenantry.io/dedicated-org=<org id>` trên node (§6.4), machine name group `org_<ULID>:group_<ULID>` (§4.3). Controller và RBAC không phụ thuộc slug.
- `UNIQUE (k8s_name)` là lưới an toàn, đồng thời chặn tái dùng tên khi Workspace đã xóa còn giữ row trong thời gian quarantine CIDR.

## Phương án đã cân nhắc

### Tên bằng immutable ID (ví dụ `ws-01jsol...`)

Đây là thiết kế trước v0.6. Duy nhất và không bao giờ trùng. Không chọn: `kubectl get ns` toàn chuỗi ULID, người dùng không biết Namespace nào của Workspace nào, hostname và context kubeconfig cũng vậy. Phần cần ổn định cho máy đã có label mang ID, nên tên có thể dành cho người đọc.

### Slug Workspace duy nhất toàn platform, không có tiền tố Organization

Không chọn: các Organization tranh nhau những tên phổ biến (`prod`, `web`), người tới sau phải đặt tên lạ. Tên cũng không cho biết Workspace thuộc Organization nào, trong khi Organization là boundary quản lý (§4.2).

### Cho slug đổi được

Không chọn: đổi slug kéo theo tạo lại Tenant, mọi Namespace (mất PVC), IPPool, Route và kubeconfig. Đó là một lần xóa và tạo mới, không phải một lần sửa; người dùng muốn vậy thì tạo Workspace mới.

### Cho slug Organization chứa `-` rồi kiểm va chạm bằng DB

Cách v0.6. Không chọn: `acme` + `dev-api` và `acme-dev` + `api` cùng ra `acme-dev-api`, và tiền tố của hai Organization lồng nhau. Phải kiểm chéo mọi Organization dưới một lock toàn cục. Cấm `-` ở slug Organization làm bài toán chỉ còn trong phạm vi một Organization.

## Hệ quả

- Tên đọc được và tự giải thích trong `kubectl`, Headlamp và hostname (`web-<org>-solar.mvp.tenantry.site` trong [e2e-personas](../report/e2e-personas.md)).
- Slug chọn sai thì phải tạo Workspace mới; với Organization thì phải xóa Organization (khi đã hết Workspace và dedicated node) rồi xin lại.
- Xóa Workspace thì block CIDR vào quarantine 10 phút và giữ luôn slug/`k8s_name` trong thời gian đó; xóa Organization bị chặn tới khi quarantine cuối cùng hết ([known-limitations](../report/known-limitations.md) mục 8). Sau đó slug Organization được nhả (migration `0015`): cùng slug là Organization mới, ID mới.
- Ràng buộc `-` thêm ở migration `0006` với `NOT VALID`: row cũ không bị quét, Organization tạo trên lab trước đó có thể còn `-` trong slug.
- Hostname của Route `<tên>-<org>-<ws>` không duy nhất theo cấu tạo (tên Route và slug Workspace đều được chứa `-`), nên cần thêm `UNIQUE (hostname)` và mã `ROUTE_HOSTNAME_TAKEN` ([02 mục 4](../architecture/02-data-model.md); chi tiết ở [ADR-0020](0020-routes-and-shared-wildcard-cert.md)).
- Slug Organization tối đa 20 ký tự và không có `-` có thể chật cho tên công ty dài; display name không bị giới hạn này.

## Kiểm chứng

- `backend/migrations/migrations_test.go`:
  - `TestConstraints`: "slug with a hyphen", "slug ends with dash", "slug longer than 20", chữ hoa, gạch dưới đều bị `CHECK` chặn.
  - `TestSlugRuleKeepsOlderOrganizations`: migration `0006` chạy được trên DB có slug cũ chứa `-`, và từ chối slug mới chứa `-`.
- `backend/internal/httpapi/organizations_test.go`, `TestCreateOrganization`: slug `alice-team` và slug cấm `admin` bị `400`.
- `backend/internal/org/org_test.go`, `TestCheckSlug`.
- `backend/internal/httpapi/workspaces_test.go`, `TestCreateWorkspaceRefusals`: `solar-prod` khi đã có `solar` bị `409 SLUG_TAKEN`, trùng slug cũng vậy; `sol` thì được (lồng chỉ tính tại `-`).
- Lab, [isolation-cluster](../report/isolation-cluster.md) (T19): owner tạo Namespace mang tiền tố của Workspace khác bị Capsule `forceTenantPrefix` từ chối (403).
- `web/e2e/workspace.spec.ts`: Tenant và Namespace `<k8sName>-frontend`, `<k8sName>-backend` có mặt trên cụm đúng tên.
