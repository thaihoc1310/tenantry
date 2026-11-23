# ADR-0014: Organization phải được Platform Admin duyệt, không tự tạo kèm quota mặc định

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §0 (v0.5 → v0.7), §4.2, §5.2.1, §8.3, §9.2](../PRODUCT-PLAN.md), [02 mục 2, 3](../architecture/02-data-model.md), [03 mục 1b, 2a](../architecture/03-flows.md), [04 mục 11](../architecture/04-state-machines.md)

## Bối cảnh

Một Organization `Active` có entitlement và ít nhất một ClusterGrant: CPU/RAM thật trên shared pool, NetworkPool để cấp CIDR, StorageClass (§5.2). Lab chỉ có 4 node, shared pool `shared-general` gồm 2 worker 3 CPU / 12 GiB mỗi node ([benchmark](../report/benchmark.md), mục Setup). Mỗi Organization được cấp là một phần thật của dung lượng đó.

Account thì tạo tự do: ai có email đều đăng ký được qua Keycloak (§8.1). Mô hình đã đi qua ba bước:

- v0.5: first login tự tạo personal Organization kèm grant mặc định (Shared, quota nhỏ).
- v0.6: vẫn personal Organization, nhưng Organization bổ sung chỉ Admin tạo, để "đóng lỗ hổng nhân free tier". Lỗ vẫn còn ở chỗ mỗi account mới vẫn có một personal Organization có quota.
- v0.7: bỏ personal Organization; user xin, Admin duyệt (PRODUCT-PLAN §0, v0.6 → v0.7).

## Quyết định

- First login **chỉ tạo user record**. User có Organization bằng một trong ba cách: xin tạo, Admin tạo thẳng, hoặc được mời (§4.2).
- **Xin tạo** sinh Organization `Pending`: giữ chỗ slug, người xin là OrgOwner. Organization `Pending` chưa có entitlement, grant hay group; mọi endpoint cần Organization đã duyệt trả `409 ORG_NOT_ACTIVE` (§9.2).
- **Admin duyệt** trong một transaction: entitlement, ClusterGrant, ba group mặc định `admins/developers/viewers`, Owner vào `admins`, audit ([04 mục 11](../architecture/04-state-machines.md)). Grant chỉ cấp được trên Cluster `Ready` (`409 CLUSTER_NOT_READY`).
- **Onboarding policy** là mẫu entitlement và grant điền sẵn vào form duyệt, không phải luồng cấp tự động. Admin sửa được trước khi bấm; grant đã cấp là snapshot (§5.2.1).
- **Từ chối** bắt buộc có lý do, người xin thấy lý do, slug được nhả, người xin gửi yêu cầu mới được.
- Mỗi user tối đa một yêu cầu `Pending` (`UNIQUE (owner_user_id) WHERE status = 'Pending'`). Hai Admin duyệt cùng lúc được tuần tự hóa bằng row lock của Organization.

## Phương án đã cân nhắc

### Tự tạo kèm quota mặc định (auto-grant, mô hình v0.5)

Không chọn. Quota mặc định nhân theo số account, và account thì miễn phí. Kẻ muốn chiếm dung lượng chỉ cần đăng ký nhiều email. Trần duy nhất khi đó là trần của shared pool (`allocationOvercommitRatio × reservable`, [ADR-0017](0017-shared-soft-dedicated-hard.md)), nghĩa là người dùng thật bị từ chối vì người khác giữ chỗ.

### Tự tạo không quota, xin quota sau

Organization tạo ngay nhưng grant rỗng cho tới khi Admin cấp. Không chọn: kết quả cuối vẫn là một lần Admin duyệt, chỉ thêm một trạng thái nửa vời (Organization `Active` mà không làm được gì). Group mặc định và lời mời cũng sẽ sinh ra trước khi biết Organization có được cấp hay không. Luồng xin nâng quota cho Organization đã `Active` vẫn là stretch (§4.2).

### Chỉ Admin tạo (bỏ luồng xin)

Vẫn giữ làm đường thứ hai cho khách (§4.2 cách 2, [03 mục 2a](../architecture/03-flows.md)). Không dùng làm đường duy nhất vì người mới không có cách nào tự lên tiếng ngoài portal; màn trống của user chưa có Organization cần một lối ra (§8.3).

## Hệ quả

- Admin là bước bắt buộc trước khi người mới có tài nguyên. Không có SLA hay thông báo: người xin phải mở lại portal để thấy kết quả (e2e bấm "Check again"). Hàng đợi duyệt sắp cũ nhất trước (IMPLEMENTATION-PLAN §12 quyết định 5).
- Admin là người chọn Cluster, pool và NetworkPool cho từng Organization ngay từ đầu, nên grant luôn trỏ tới tài nguyên tồn tại và cùng Cluster (kiểm khi duyệt).
- Organization `Pending`/`Rejected` chỉ người xin thấy; người khác nhận `404` như không tồn tại.
- Thêm một vòng đời phải giữ đúng: slug của yêu cầu bị từ chối và của Organization đã xóa được nhả (migration `0015_org_slug_reuse.sql`).
- Ở cụm MVP, thư xác minh email đi vào Mailpit trong cụm, nên người ngoài chưa tự đăng ký được nếu không có người vận hành lấy link giúp ([06 mục 3.2](../architecture/06-security-and-identity.md)). Đây là giới hạn của bước đăng ký, không phải của bước duyệt.
- [known-limitations](../report/known-limitations.md) không có mục riêng cho quyết định này.

## Kiểm chứng

- `backend/internal/httpapi/organizations_test.go`:
  - `TestCreateOrganization`: yêu cầu mới là `Pending`, người xin là `OrgOwner`; yêu cầu thứ hai khi còn `Pending` bị `409 ORG_REQUEST_PENDING`; slug của yêu cầu bị từ chối xin lại được.
  - `TestCreateOrganizationRace`: 10 user cùng một slug thì đúng một `201` và chín `409`; một user gửi 10 slug cùng lúc cũng đúng một `201`.
- `backend/internal/httpapi/admin_test.go`:
  - `TestApproveOrganization`, `TestRejectOrganization` (lý do trống bị `400`).
  - `TestApproveRefused`: 18 trường hợp từ chối (Cluster không `Ready`, pool của Cluster khác, NetworkPool của Organization khác, `maxGroups` dưới 3…); sau đó không còn row nào trong grant, entitlement, group.
  - `TestApproveConcurrently`: hai Admin duyệt cùng lúc, một `200` một `409`, Organization có đúng một grant.
  - `TestAdminNeedsTheRole`, `TestAdminAuthorizationMatrix` (`authz_admin_test.go`).
- `web/e2e/onboarding.spec.ts` trên lab: "a newcomer gets an approved Organization" (Admin thấy form điền sẵn `CPU requests` = 2 từ policy, duyệt, người xin thấy quota) và "a rejected requester sees why and may ask again".
- [e2e-personas](../report/e2e-personas.md): bước đầu, Alice xin, Admin duyệt bằng grant của onboarding policy, ba group mặc định được sync sang Keycloak.
