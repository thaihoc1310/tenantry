# ADR-0007: ClusterGrant và dedicated node gắn với Organization, không gắn với account

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §5, §5.2, §5.3, §5.5, §6.3](../PRODUCT-PLAN.md), [02 mục 3, 4](../architecture/02-data-model.md), [06 mục 4](../architecture/06-security-and-identity.md), [IMPLEMENTATION-PLAN §3.2](../IMPLEMENTATION-PLAN.md)

## Bối cảnh

Không có billing, nhưng Platform Admin vẫn phải cấp rõ ai được dùng Cluster nào, bao nhiêu CPU/RAM/Pod/storage, shared pool nào, NetworkPool nào và node dedicated nào (PRODUCT-PLAN §5). Câu "user A được cấp node" có thể hiểu theo hai cách: cấp cho account của A, hoặc cấp cho tập thể mà A đang thao tác thay mặt.

Những gì tiêu tài nguyên đều thuộc Organization: Workspace thuộc một Organization (§4.4), group thuộc một Organization (§4.3), quyền Kubernetes đi qua group chứ không qua user (§10). Người trong Organization thay đổi liên tục: mời thêm, rời đi, transfer ownership (§9). Nếu tài nguyên gắn với người thì mỗi lần người đổi, tài nguyên phải đổi theo.

## Quyết định

- **Grant gắn với Organization.** Mỗi `organization_cluster_grants` nối một Organization với một Cluster, `UNIQUE (org_id, cluster_id)`, đúng một grant default mỗi Organization (§5.2, [02 mục 3](../architecture/02-data-model.md)). Global entitlement (`maxMembers`, `maxGroups`, `maxWorkspacesTotal`) cũng là row của Organization (§5.1).
- **Dedicated node cũng thuộc Organization**, không thuộc account hay Workspace: `dedicated_node_grants.org_id`, node mang label/taint `tenantry.io/dedicated-org=<org id>` (§6.3, §6.4). Nhiều Workspace của cùng Organization chia node qua reservation ([ADR-0011](0011-dedicated-node-per-organization.md)).
- **User chỉ là actor.** OrgOwner/OrgAdmin dùng entitlement của Organization theo platform role (§9.2); OrgMember không đọc được quota (`403`).
- **Database ép quan hệ này**: FK ghép `(cluster_grant_id, org_id, cluster_id)` từ `workspaces` sang grant, và `(node_grant_id, org_id, cluster_id)` từ `workspace_node_reservations` sang node grant ([02 mục 4](../architecture/02-data-model.md)). Một Workspace không thể dùng grant hay node của Organization khác, kể cả khi service có lỗi.
- Phần Workspace lấy từ grant không có row riêng: là `SUM` cột tài nguyên của các Workspace chưa `Deleted` trong grant, tính dưới row lock của grant (§5.3, [IMPLEMENTATION-PLAN §3.2](../IMPLEMENTATION-PLAN.md)).

## Phương án đã cân nhắc

### Grant theo user account

Mỗi account có quota riêng, Workspace trừ vào quota của người tạo. Không chọn:

- Người tạo rời Organization hoặc bị disable thì Workspace của Organization mất chỗ dựa quota, phải chuyển sang người khác. Theo quyết định hiện tại, gỡ member không đụng tới Workspace nào.
- Quota của một nhóm thành tổng quota của từng người, Admin không đặt được một trần cho cả nhóm.
- Account tạo tự do qua Keycloak, nên quota theo account là đúng lỗ "nhân free tier" mà v0.6 đã đóng (PRODUCT-PLAN §0, v0.5 → v0.6 điểm 2; xem [ADR-0014](0014-organization-requires-admin-approval.md), [ADR-0018](0018-gated-organizations-single-owner.md)).
- Kubernetes không biết user nào sở hữu Tenant: owner của Tenant là group (§10.4). Gắn quota với user tạo thêm một trục không khớp với trục quyền.

### Grant theo từng Workspace

Admin duyệt quota cho từng Workspace. Không chọn: Admin thành nút cổ chai cho mọi lần tạo hay resize, trái với mục tiêu self-service. Grant là trần cho Organization tự chia (§5.5), Admin chỉ can thiệp khi nâng trần.

### Grant theo group

Group là đơn vị quyền, không phải đơn vị tài nguyên. Một Workspace có nhiều group với role khác nhau (§4.4), nên không có group nào "sở hữu" Workspace để trừ quota.

## Hệ quả

- Membership thay đổi không làm đổi tài nguyên. e2e persona: Alice gỡ Joe khỏi Organization, Workspace Solar vẫn chạy, chỉ token mới của Joe mất quyền ([e2e-personas](../report/e2e-personas.md)).
- Hai lớp quyền có thể lệch có chủ ý: OrgOwner dùng được quota trên portal nhưng không có quyền `kubectl` nếu không thuộc group nào được bind vào Workspace ([06 mục 4](../architecture/06-security-and-identity.md)). UI phải giải thích.
- Không có số liệu tiêu thụ theo từng người. Audit ghi actor của từng hành động, nhưng quota và usage chỉ có theo Organization và theo grant (§5.6).
- Sửa grant không bao giờ đụng Workspace đang chạy, và không hạ được dưới tổng đã giữ chỗ (`CLUSTER_GRANT_BELOW_ALLOCATION`, §5.5). Muốn thu hồi nhiều hơn, Organization phải tự thu gọn Workspace.
- Luồng xin nâng quota cho Organization đã `Active` vẫn là stretch (§4.2): hiện chỉ Admin chủ động sửa grant.
- [known-limitations](../report/known-limitations.md) không có mục riêng cho quyết định này.

## Kiểm chứng

- `backend/internal/httpapi/workspaces_test.go`:
  - `TestCreateWorkspaceReservesTheGrant`: hai lần tạo đồng thời, mỗi lần 1500m CPU trên grant 2 CPU, đúng một `202` và một `409`; lần tạo kế tiếp nhận `CLUSTER_GRANT_QUOTA_EXCEEDED` với `details.resource=requestsCpu`, `limit=2`, `allocated=1500m`.
  - `TestGrantGuardsWorkspaces`: Admin hạ grant dưới mức Workspace đang giữ bị `CLUSTER_GRANT_BELOW_ALLOCATION` (`minimum=1`); không gỡ được shared pool Workspace đang chạy; không xóa được grant còn Workspace.
  - `TestUsageCountsWorkspaces`: usage theo grant chỉ cộng Workspace chưa `Deleted`.
  - `TestDedicatedWorkspaces`: chọn node grant không thuộc Organization nhận `409 DEDICATED_NODE_NOT_GRANTED`; Workspace Dedicated không trừ CPU của grant; Admin không thu hồi được node còn reservation.
- `backend/internal/httpapi/admin_test.go`, `TestApproveRefused`: grant tham chiếu NetworkPool riêng của Organization khác bị từ chối (`networkPoolIds`).
- `backend/migrations/migrations_test.go`, `TestConstraints`: "node dedicated twice" bị unique index chặn.
- Lab: [e2e-personas](../report/e2e-personas.md) chạy trọn chuỗi Admin nâng grant, cấp `wk3` cho Organization, hai Workspace Dedicated cùng node và Workspace thứ ba bị từ chối.
