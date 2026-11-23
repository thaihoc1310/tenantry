# ADR-0018: User không tự tạo Organization, và mỗi Organization có đúng một OrgOwner

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §0 (v0.5 → v0.6 điểm 2, 6; v0.6 → v0.7 điểm 1, 2, 4), §4.2, §9.2](../PRODUCT-PLAN.md), [02 mục 2](../architecture/02-data-model.md), [03 mục 11](../architecture/03-flows.md), [04 mục 8, 11](../architecture/04-state-machines.md)

## Bối cảnh

Organization là boundary quản lý và là chủ của mọi tài nguyên ([ADR-0007](0007-cluster-grant-per-organization.md)). Hai câu hỏi đi cùng nhau:

- **Ai được sinh ra Organization?** Mỗi Organization `Active` là một ClusterGrant trên cụm thật. Account Keycloak tạo tự do, nên nếu mỗi account tự sinh được Organization thì mỗi account là một phần quota. v0.6 gọi đây là lỗ "nhân free tier" (PRODUCT-PLAN §0).
- **Ai chịu trách nhiệm cuối cùng cho một Organization?** Có những hành động không nên chia đôi: xóa Organization, chuyển quyền sở hữu, đổi role của người đứng đầu. Platform Admin cũng cần một người cụ thể để liên hệ về Organization mình đã duyệt.

## Quyết định

- **Không có Organization nào sinh ra mà không qua Platform Admin.** First login không tạo Organization (bỏ personal Organization ở v0.7). User xin tạo, Admin duyệt hoặc từ chối ([ADR-0014](0014-organization-requires-admin-approval.md)); hoặc Admin tạo thẳng cho khách. Mỗi user tối đa một yêu cầu `Pending` cùng lúc. Không có trần số Organization một user được làm Owner: mỗi Organization thêm đều phải qua một lần duyệt.
- Một account là member của bao nhiêu Organization cũng được, với role khác nhau ở mỗi nơi (§8.3).
- **Đúng một OrgOwner mỗi Organization**, nhiều OrgAdmin. Database giữ cả hai chiều: `organizations.owner_user_id NOT NULL` (ít nhất một) và unique index `organization_members (org_id) WHERE role = 'OrgOwner'` (nhiều nhất một) ([02 mục 2](../architecture/02-data-model.md)).
- OrgAdmin quản lý member, group, Workspace như Owner; chỉ Owner transfer ownership, xóa Organization, đổi role của Owner (§9.2).
- **Đổi Owner chỉ qua transfer hai bước**, hoàn toàn trong portal: Owner chọn một OrgAdmin, người nhận xác nhận (hết hạn 7 ngày, tối đa một yêu cầu đang chờ mỗi Organization). Transaction xác nhận kiểm lại dưới lock, hạ Owner cũ thành OrgAdmin **trước** rồi nâng người nhận **sau**, vì index một-Owner không deferrable.
- Không mời ai làm OrgOwner, không `PATCH` role thành OrgOwner, Owner không tự rời và không bị remove khi chưa transfer.

## Phương án đã cân nhắc

### Personal Organization cho mỗi account (v0.5, v0.6)

Không chọn ở v0.7: dù chỉ một mỗi account, quota vẫn nhân theo số account; và personal Organization kéo theo luật riêng (không xóa, không transfer, không rời được) làm mô hình phức tạp thêm (PRODUCT-PLAN §0, v0.6 → v0.7 điểm 4).

### User tự tạo Organization rỗng, Admin cấp quota sau

Bàn ở [ADR-0014](0014-organization-requires-admin-approval.md): kết quả vẫn là một lần duyệt, chỉ thêm một trạng thái Organization `Active` mà không làm được gì.

### Nhiều Owner ngang quyền

Không chọn: hai Owner có thể cùng đề nghị transfer, một người xóa Organization khi người kia không muốn, hoặc gỡ lẫn nhau. Phải thêm luật "Owner cuối cùng không được rời". OrgAdmin đã phủ việc vận hành hằng ngày; chỉ ba hành động cuối cần một người.

### Transfer một bước hoặc qua link/token

Không chọn: chuyển quyền cho người không muốn nhận thì Organization có Owner không hoạt động. Link/token thêm một bí mật phải bảo vệ; người nhận vốn đã đăng nhập portal nên xác nhận tại chỗ là đủ (v0.8 điểm 23).

## Hệ quả

- Người mới phải chờ Admin trước khi có Organization của riêng mình; trong lúc đó vẫn được mời vào Organization khác.
- Một người duy nhất là điểm hỏng: Owner nghỉ việc hoặc mất account thì không ai transfer được, vì transfer cần Owner đề nghị. API hiện không có endpoint admin nào đổi Owner (đã soát các path `/admin/organizations*` trong `backend/openapi/tenantry.v1.yaml`). MVP chấp nhận giới hạn này, gán lại Owner bởi Platform Admin để Phase 2 ([known-limitations](../report/known-limitations.md) mục 5a).
- Transfer phải tính tới đua với các thay đổi membership khác; xác nhận transfer và rời Organization cùng khóa row Organization nên chạy lần lượt.
- Người nhận phải là OrgAdmin trước, nên chuyển cho member thường là hai bước: nâng lên OrgAdmin rồi transfer.

## Kiểm chứng

- `backend/migrations/migrations_test.go`, `TestConstraints`: "second owner in one org" bị unique index chặn; "invitation to become owner" bị `CHECK` chặn; "transfer to oneself", "second waiting transfer in one org" bị chặn.
- `backend/internal/httpapi/organizations_test.go`: `TestCreateOrganization` (yêu cầu thứ hai khi còn `Pending` bị `409 ORG_REQUEST_PENDING`), `TestCreateOrganizationRace` (một user gửi 10 slug đồng thời: một `201`, chín `409`).
- `backend/internal/httpapi/members_test.go`:
  - `TestPatchMember`: OrgAdmin đổi role Owner bị `403`; Owner tự hạ mình bị `403`; đặt role `OrgOwner` bị `400`.
  - `TestLeaveOrganization`: Owner rời bị `409 LEAVE_NOT_ALLOWED`.
  - `TestTransferOwnership`, `TestTransferRefusals` (chuyển cho OrgMember, người ngoài hoặc chính mình bị `422 RECIPIENT_NOT_ORG_ADMIN`), `TestCancelTransfer`.
  - `TestAcceptTransferWhileLeaving`: người nhận xác nhận đúng lúc rời, chạy 5 lần, lần nào Organization cũng còn đúng một Owner và Owner là member.
- `web/e2e/people.spec.ts`: "ownership changes hands when the recipient accepts".
