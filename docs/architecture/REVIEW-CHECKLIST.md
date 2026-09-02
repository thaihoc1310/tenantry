# Checklist review — các điểm mở gom từ bộ sơ đồ

> Gom toàn bộ mục **Điểm cần review** và chỗ đánh dấu **(giả định)** trong `01`–`07` và `IMPLEMENTATION-PLAN.md` thành một danh sách để duyệt một lượt. Mỗi mục có phương án đề xuất; đánh ✅ giữ, ✏️ đổi, hoặc ghi chú. Sau khi duyệt, các quyết định sẽ được ghi vào `PRODUCT-PLAN.md` mục 31 (hoặc ADR) và sơ đồ được sửa theo.

## A. Nghiệp vụ / hành vi người dùng

| # | Câu hỏi | Đề xuất | Ở đâu |
|---|---|---|---|
| A1 | Slug personal Org sinh thế nào? | Từ `preferred_username` chuẩn hóa `[a-z0-9-]`, thêm hậu tố ngẫu nhiên nếu trùng; hiện cho user thấy ngay sau login vì immutable | 03 §1, 07 §2 |
| A2 | Onboarding grant lỗi (default Cluster không Ready) thì sao? | Vẫn tạo Org, bỏ qua grant, ghi audit, banner "chờ Admin cấp quota"; không chặn login | 03 §1 |
| A3 | Admin tạo Org với owner email chưa từng login? | Từ chối `OWNER_USER_NOT_FOUND`; Owner phải login một lần trước | 03 §2 |
| A4 | Invitation accept sai email: đốt token hay giữ? | Giữ token (không tiêu) để đúng người vẫn dùng được link; trả lỗi rõ | 03 §3, 04 §7 |
| A5 | Accept invite khi đã là member? | 409, không đổi role ngầm | 03 §3 |
| A6 | Transfer ownership: tối đa một Proposed/Org; người nhận mất role OrgAdmin trước khi accept? | Một Proposed; kiểm tra lại lúc accept và từ chối | 03 §11, 04 §8 |
| A7 | Thông báo transfer cho người nhận qua đâu? | In-app trong portal (đã là member), không cần link | 03 §11 |
| A8 | Confirm xóa Workspace gõ theo gì? | Gõ slug (`water`), UI liệt kê Namespace/PVC sẽ mất | 03 §12 |
| A9 | OrgMember chỉ có binding một Workspace thấy gì ở Overview Org? | Overview rút gọn: Workspace mình có quyền; ẩn quota Org, Members, Groups, Settings, Clusters | 07 §1 |
| A10 | Owner không nằm trong group nào bound vào Workspace: ẩn hay hiện nút kubeconfig? | Hiện kèm cảnh báo 403 và gợi ý tự thêm vào group Owner | 06 §6, 07 §3 |
| A11 | Developer có `exec`/log và đọc Secret không? | Mặc định: được exec/log, không đọc Secret; mở bằng policy profile sau | 06 §6 |
| A12 | Namespace `source=portal` bị xóa bằng kubectl: controller tạo lại hay portal đồng bộ theo thực tế? | Đồng bộ theo thực tế (bỏ khỏi `spec.namespaces`, cảnh báo); không tạo lại | 04 §9, 03 §7 |
| A13 | Xóa Namespace `source=kubectl` từ portal có được không? | Được, cùng rule confirm nếu có PVC | 04 §9 |
| A14 | Wizard MVP: gộp bước 2-3-5a (Cluster/quota/pool đều auto) thành một màn read-only? | Gộp, còn ~8 bước; data model không đổi | 07 §4 |
| A15 | `:validate` gọi khi nào? | Debounce khi rời bước 6/7/9 + lần cuối trước Submit | 07 §4 |
| A16 | Onboarding policy đổi → grant đã cấp có đổi theo không? | Không; grant là snapshot lúc tạo | 02 §3 |

## B. State machine và lifecycle

| # | Câu hỏi | Đề xuất | Ở đâu |
|---|---|---|---|
| B1 | `Draft` có persist không (lưu wizard dở)? | Không; `Draft` chỉ là trạng thái trong request | 04 §1 |
| B2 | `Failed` sau khi DB đã commit: cạnh `Failed → Updating` (retry) và `Failed → Deleting` | Thêm hai cạnh này vào plan §17 | 04 §1.3 |
| B3 | Một Workspace tối đa một Operation đang chạy? | Có; request mới trả `WORKSPACE_NOT_READY` | 04 §2 |
| B4 | Node `NotReady` → Workspace trên node chuyển `Degraded`? | Có, `ComputeReady=False`; không tự đổi node | 04 §5 |
| B5 | Cạnh `Cordoned → NotReady` cho node và `Unavailable → Maintenance` cho Cluster | Thêm (để Admin "đóng băng" Cluster đang flap) | 04 §5, §6 |
| B6 | Ai chuyển Cluster `Registered → Ready`? | Worker check CRD/Capsule sẵn sàng, Admin bấm xác nhận | 04 §6 |
| B7 | Cluster `Maintenance`: delete/retry Workspace vẫn xử lý? | Có; chỉ chặn create/resize | 04 §6 |
| B8 | Cluster `Unavailable` giữa lúc delete: Operation `Running` hay `Blocked`? | `Blocked` với reason `ClusterUnavailable`, tự tiếp khi Cluster Ready | 04 §10 |
| B9 | Quarantine CIDR bao lâu; giữ row lịch sử? | 10–30 phút cấu hình được; giữ row `Released` làm lịch sử | 04 §4 |
| B10 | Thứ tự delete: release NodeReservation (D6) trước xóa Tenant (D7) như plan, hay đảo? | Giữ như plan; reservation chỉ release sau khi Pod đã rời node | 04 §10 |
| B11 | Release ClusterGrant reservation cùng lúc quarantine CIDR hay sớm hơn? | Cùng lúc (đơn giản, một bước cuối) | 04 §10 |
| B12 | Capacity state của node lưu cột hay tính lúc đọc? | Lưu cột, tính lại trong mỗi transaction reservation | 04 §5 |
| B13 | Mapping `Cordoned` → `Tenant.spec.cordoned` của Capsule | Xác nhận field tồn tại ở Capsule 0.13.x | 04 §1 |

## C. Dữ liệu và transaction

| # | Câu hỏi | Đề xuất | Ở đâu |
|---|---|---|---|
| C1 | `workspaces.resources` JSONB hay tách 6 cột numeric? | Tách cột để `SUM` và index dễ | 02 §4 |
| C2 | `group_members` dùng FK composite `(org_id, user_id)` → `organization_members`? | Có | 02 §2 |
| C3 | Remove member: cascade DB hay service xóa `group_members`? | Service (phải sinh outbox sync Keycloak) | 02 §2 |
| C4 | `Idempotency-Key` scope | Unique theo `(org_id, key)`, trả lại operation cũ | 03 §5 |
| C5 | Ai cập nhật `capacity_snapshot` / `allocatable` từ Kubernetes, tần suất? | Controller báo qua `PlatformNodeGrant.status` (Dedicated); worker poll Node mỗi 5 phút cho shared pool | 02 §3, 03 §2 |
| C6 | Đổi `allowed_storage_classes` của grant → fan-out re-render mọi CR trong grant | Thêm task ở M5/M7 | 02 §5 |
| C7 | Outbox: `SKIP LOCKED` + lease/timeout cho row claim chưa processed | Lease 60s, worker khác lấy lại khi quá hạn | 03 §13 |
| C8 | Mã lỗi còn thiếu trong plan §16.5 | Thêm: `OWNER_USER_NOT_FOUND`, `CLUSTER_GRANT_BELOW_ALLOCATION`, `NETWORK_POOL_EXHAUSTED`, `NAMESPACE_QUOTA_EXCEEDED`, `PERSONAL_ORG_IMMUTABLE`, `TRANSFER_PENDING_EXISTS` | 03, 04 |

## D. Kubernetes, network, scheduling

| # | Câu hỏi | Đề xuất | Ở đâu |
|---|---|---|---|
| D1 | Kênh gắn label/taint org lên node khi grant (trước khi có Workspace) | CRD phụ `PlatformNodeGrant` do worker apply, controller reconcile; worker vẫn chỉ đụng CRD `tenantry.io` | 01 §5, 03 §2, PLAN §12.9 |
| D2 | API cần credential read-only để kiểm tra Pod lạ trên node khi grant | Đưa check này vào controller của `PlatformNodeGrant` (báo về status) thay vì cho API quyền đọc Pod | 03 §2 |
| D3 | System pool: label `tenantry.io/compute-pool=system` + taint NoSchedule | Giữ; lab hiện chưa có node system riêng → M1 phải tạo | 05 §1, 01 §3 |
| D4 | Enforce scheduling: `PodNodeSelector` + `PodTolerationRestriction` (admission plugin kubeadm) hay validating webhook của controller? | MVP dùng hai admission plugin có sẵn + test bắt buộc; webhook là hardening sau | 05 §2 |
| D5 | Shared ceiling tính trên `requests`; `limits` kiểm bằng ratio riêng | Giữ | 05 §4 |
| D6 | Chia supernet `10.240.0.0/16`: `/18` pool chung, `/18` pool riêng đầu tiên | Chốt bảng chia để pool chung/riêng không overlap | 05 §5 |
| D7 | Egress internet: replicate policy theo profile Workspace; deny metadata `169.254.169.254` | Mặc định cho phép internet egress (demo pull image), deny metadata luôn | 05 §6 |
| D8 | Certificate wildcard đặt ở `tenantry-system` rồi replicate Secret bằng **GlobalTenantResource** (plan ghi TenantResource) | Dùng GlobalTenantResource; sửa plan §7.6 | 03 §10, 05 §7 |
| D9 | Annotation IPPool cho Namespace tạo bằng kubectl: Capsule replication có đặt kịp không? | Controller bù annotation nếu thiếu; test | 03 §7 |
| D10 | Tenant Pod gọi `kubernetes.default` có chặn bằng NetworkPolicy không? | Không chặn; ServiceAccount tenant không có RoleBinding mặc định | 06 §1 |
| D11 | Controller cần `nodes patch`: thu hẹp bằng ValidatingAdmissionPolicy chỉ cho đổi label/taint prefix `tenantry.io/` | Thêm ở M8 hardening | 06 §5 |
| D12 | NFS `archiveOnDelete` tắt hay bật? | Tắt, khớp cảnh báo "xóa là mất dữ liệu" | 05 §9 |
| D13 | Central plane (Keycloak/Prometheus/PostgreSQL) dùng chung StorageClass `standard` với tenant? | Class riêng `system-nfs` hoặc export NFS riêng, tránh tenant làm đầy ổ system | 05 §9 |
| D14 | LimitRange thêm `max` per PVC? | Có, để một PVC không nuốt cả `requests.storage` | 05 §9 |
| D15 | Keycloak dùng PostgreSQL riêng hay chung instance? | Chung instance, database riêng | 01 §3 |
| D16 | DNS wildcard cho máy demo | dnsmasq/CoreDNS nội bộ; `sslip.io` là phương án dự phòng | 01 §3, 05 §7 |

## E. Bảo mật / identity

| # | Câu hỏi | Đề xuất | Ở đâu |
|---|---|---|---|
| E1 | Token TTL | `kubectl`/`headlamp-*`: 5 phút + refresh; `portal`: access 15 phút, session 8 giờ | 06 §2 |
| E2 | Yêu cầu `email_verified=true` trước khi tạo session portal | Có (Keycloak required action Verify Email) | 03 §1 |
| E3 | Keycloak admin client scope tối thiểu để tạo group, add member, revoke session | Kiểm chứng thật; ghi ADR nếu buộc phải `manage-users` | 06 §5 |
| E4 | Có introspection tại Capsule Proxy để revoke sớm hơn TTL? | Không; giữ contract TTL | 06 §4 |
| E5 | Kích thước JWT header khi user thuộc ~20 group | Test với ingress-nginx `large-client-header-buffers` | 06 §2 |

## F. Kỹ thuật (từ IMPLEMENTATION-PLAN §12, cần chốt trước khi code)

Xem bảng 10 mục ở cuối `IMPLEMENTATION-PLAN.md`: ID kiểu ULID, quy tắc slug, tỷ lệ requests/limits Shared, pagination, OpenAPI codegen, migration tool, component library, outbox tuning, `PlatformNodeGrant`, artefact deploy. Mỗi mục đã có mặc định đề xuất.
