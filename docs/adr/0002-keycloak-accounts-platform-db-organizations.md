# ADR-0002: Account nằm ở Keycloak, Organization và Group nằm ở platform database

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §4.1–4.3, §8.1, §9.3, §14.1](../PRODUCT-PLAN.md), [02 mục 2](../architecture/02-data-model.md), [03 mục 4](../architecture/03-flows.md), [06 mục 2, 3.2, 7](../architecture/06-security-and-identity.md), [ADR-0003](0003-groups-as-oidc-claims.md)

## Bối cảnh

Tenantry có hai loại dữ liệu về con người:

- **Danh tính đăng nhập**: password, xác minh email, đặt lại mật khẩu, MFA, SSO session, signing key, và OIDC token cho ba nơi khác nhau (portal, Headlamp, kubectl). kube-apiserver chỉ tin token của một OIDC issuer, nên dù thế nào cũng phải có một IdP.
- **Mô hình sản phẩm**: Organization, membership kèm role, invitation, group, group member, binding group ↔ Workspace, entitlement. Phần này có invariant phải giữ trong transaction: đúng một OrgOwner mỗi Organization, mỗi user tối đa một yêu cầu `Pending`, `maxMembers` kiểm lại dưới lock khi accept lời mời, không xóa group còn binding, member của group phải là member của Organization ([02 mục 2](../architecture/02-data-model.md)). Binding và reservation còn phải commit cùng transaction với outbox và audit (PRODUCT-PLAN §14).

Kubernetes chỉ cần một phần rất nhỏ của mô hình sản phẩm: user thuộc group machine nào.

## Quyết định

- **Keycloak giữ account**: password, verify email, reset, MFA, session, signing key, OIDC token, group claim, realm role `platform-admin` (PRODUCT-PLAN §8.1). User đăng nhập và đăng ký trên trang của Keycloak (theme giống portal). Portal không lưu password.
- **PostgreSQL giữ bản chiếu của account**: `users` gồm internal id, `(issuer, subject)` với `UNIQUE (issuer, subject)`, email và display name chỉ là snapshot, `status` `Active`/`Disabled`. Email không phải identity vì đổi được (§4.1).
- **PostgreSQL là source of truth cho Organization, membership, invitation, group và group member** (§9.3, §14.1). Quyền trên portal luôn đọc từ DB; client `portal` không có group mapper ([06 mục 2](../architecture/06-security-and-identity.md)).
- **Đồng bộ một chiều DB → Keycloak** cho đúng phần Kubernetes cần: machine group `org_<ULID>:group_<ULID>`, membership, group `tenantry:tenant-users` và cờ `enabled` của account. Group Sync Worker làm việc này qua client `tenantry-sync`; xóa là tombstone, row chỉ biến mất khi Keycloak đã gỡ xong, lỗi thì retry có backoff ([03 mục 4](../architecture/03-flows.md)).
- User không tự sửa group Kubernetes qua Keycloak Account Console (§9.3). Rời Organization không bao giờ xóa account Keycloak (§9.4).

## Phương án đã cân nhắc

### Portal tự quản lý account và password

Portal phải tự làm hash password, reset, xác minh email, MFA, chống brute-force, và vẫn phải phát OIDC token mà kube-apiserver tin. Tức là tự viết một IdP. Không chọn: tăng rủi ro và scope, không liên quan bài toán tenancy.

### Form đăng nhập tự viết rồi gọi Keycloak

§8.1 loại cách này: portal sẽ phải cầm password của user và xử lý password flow. Client `kubectl` cũng không bật direct grant (IMPLEMENTATION-PLAN M1, bước realm).

### Keycloak là source of truth cho cả Organization và group

Dùng group của Keycloak (hoặc tính năng Organizations của Keycloak) để chứa Organization, role và membership; portal đọc ngược từ Keycloak. Không chọn, lý do rút ra từ yêu cầu ở trên, không phải từ thử nghiệm:

- Các invariant (một OrgOwner, `maxMembers` dưới lock, không xóa group còn binding) cần constraint và lock trong cùng database với Workspace binding, reservation và outbox. Admin API của Keycloak không tham gia được transaction đó.
- Mỗi request portal sẽ phải hỏi Keycloak để authorize, đặt Keycloak lên đường request của portal.
- Group của Keycloak không mang được trạng thái `Pending`/`Rejected`/`Deleted`, lời mời theo email đã xác minh, hay transfer ownership hai bước mà không thêm một kho riêng.

### Ghi đồng thời vào DB và Keycloak ngay trong request

Keycloak lỗi thì request lỗi, hoặc hai bên lệch mà không ai biết phải sửa. Tombstone và hàng đợi `sync_status` cho cùng kết quả cuối mà không mất thay đổi nào.

## Hệ quả

- Quyền portal đổi ngay ở request kế tiếp khi remove member hay đổi role, vì mỗi request đọc `organization_members` ([06 mục 5](../architecture/06-security-and-identity.md)).
- Thay đổi nghiệp vụ và việc cần đồng bộ commit cùng transaction; Keycloak tạm lỗi chỉ làm chậm, không làm mất thay đổi.
- Có hai bản group: DB và Keycloak lệch nhau trong lúc sync. Ngay sau khi duyệt Organization, group `admins` còn `Sync pending` vài giây và wizard trả `GROUP_SYNC_PENDING` ([07 mục 2](../architecture/07-ux-wireflow.md)). Keycloak lỗi lâu thì token mới vẫn mang group cũ; cần alert sync lag ([03 mục 4](../architecture/03-flows.md)).
- Email và display name trong DB là snapshot, có thể cũ so với Keycloak.
- Client `tenantry-sync` cần `manage-users`, mạnh ngang Platform Admin ([06 mục 7](../architecture/06-security-and-identity.md), [known-limitations](../report/known-limitations.md) mục 24). Hướng thu hẹp là fine-grained admin permissions v2 (`deploy/cluster/keycloak-fgap.py`). Lần áp trên lab ở M12 thất bại ở ca dương (việc `tenantry-sync` vẫn phải làm được) và đã rollback, nên lab vẫn ở `manage-users` + `query-groups`; `make cluster-verify` kiểm đúng trạng thái này và tự đổi sang kiểm bộ quyền hẹp khi FGAP được bật.
- API và hai worker dùng chung một role PostgreSQL: chiếm được API là ghi được `group_members`, và Group Sync sẽ đưa bất kỳ ai vào group Keycloak của bất kỳ Organization nào ([06 mục 1](../architecture/06-security-and-identity.md)).
- Tự đăng ký cần xác minh email. MVP gửi thư vào Mailpit trong cụm, nên người ngoài chưa tự đăng ký được nếu không có người vận hành lấy link giúp ([06 mục 3.2](../architecture/06-security-and-identity.md)).

## Kiểm chứng

- Quyền portal từ DB, thứ tự 404 → 403 → 409: `TestAuthorizationMatrix`, `TestAuthorizationOfAnOrganizationNotActive` (`backend/internal/httpapi/authz_test.go`).
- Group Sync: `TestSync`, `TestSyncRetriesWithBackoff`, `TestSyncAfterCrash`, `TestSyncReAddWhileRemoving`, `TestSyncUserEnabled` (`backend/internal/keycloak/sync_test.go`); `TestTenantUsersFollowBindings` (`backend/internal/keycloak/tenantusers_test.go`); `TestLiveKeycloak` chạy với Keycloak thật khi đặt `KEYCLOAK_TEST_URL` (`backend/internal/keycloak/client_live_test.go`).
- Disable user xóa mọi phiên: `TestDisableEnableUser` (`backend/internal/httpapi/admin_orgs_test.go`).
- Trên lab: `web/e2e/people.spec.ts` "an invited newcomer joins, gets a group, and their token names it"; [e2e-personas](../report/e2e-personas.md): ba persona đăng ký qua trang Keycloak, xác minh email từ Mailpit, group `admins`/`developers`/`viewers` được sync sang Keycloak.
- M4 trên Keycloak 26.8: `manage-users` + `query-groups` đủ để tạo/xóa group và thêm/gỡ member ([06 mục 7](../architecture/06-security-and-identity.md)).

