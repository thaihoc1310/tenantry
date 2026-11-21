# Checklist review — các câu hỏi còn mở

> Chỉ chứa câu hỏi **chưa chốt**. Mỗi mục có phương án đề xuất; đánh ✅ giữ, ✏️ đổi, hoặc ghi chú. Chốt xong thì ghi quyết định vào nơi định nghĩa của nó (bản đồ ở `docs/README.md`) rồi **xóa dòng khỏi file này** — không giữ bản sao quyết định ở đây. Mã số giữ nguyên qua các lần sửa nên có khoảng trống.

## A. Nghiệp vụ / hành vi người dùng

| # | Câu hỏi | Đề xuất | Ở đâu |
|---|---|---|---|
| A3 | Admin tạo thẳng Org với owner email chưa từng login? | Từ chối `OWNER_USER_NOT_FOUND`; Owner phải login một lần trước | 03 §2 |
| A4 | Invitation accept sai email: đốt token hay giữ? | Giữ token (không tiêu) để đúng người vẫn dùng được link; trả lỗi rõ | 03 §3, 04 §7 |
| A5 | Accept invite khi đã là member? | 409, không đổi role ngầm | 03 §3 |
| A7 | Thông báo transfer cho người nhận qua đâu? | In-app trong portal (đã là member), không cần link | 03 §11 |
| A8 | Confirm xóa Workspace gõ theo gì? | Gõ slug (`water`), UI liệt kê Namespace/PVC sẽ mất | 03 §12 |
| A9 | OrgMember chỉ có binding một Workspace thấy gì ở Overview Org? | Overview rút gọn: Workspace mình có quyền; ẩn quota Org, Members, Groups, Settings, Clusters | 07 §1 |
| A10 | Owner không nằm trong group nào bound vào Workspace: ẩn hay hiện nút kubeconfig? | Hiện kèm cảnh báo 403 và gợi ý tự thêm vào group Owner | 06 §6, 07 §3 |
| A11 | Developer có `exec`/log và đọc Secret không? | Mặc định: được exec/log, không đọc Secret; mở bằng policy profile sau | 06 §6 |
| A12 | Namespace `source=portal` bị xóa bằng kubectl: controller tạo lại hay portal đồng bộ theo thực tế? | Đồng bộ theo thực tế (bỏ khỏi `spec.namespaces`, cảnh báo); không tạo lại | 04 §9, 03 §7 |
| A13 | Xóa Namespace `source=kubectl` từ portal có được không? | Được, cùng rule confirm nếu có PVC | 04 §9 |
| A14 | Wizard MVP: gộp bước 2-3-5a (Cluster/quota/pool đều auto) thành một màn read-only? | Gộp, còn ~8 bước; data model không đổi | 07 §4 |
| A15 | `:validate` gọi khi nào? | Debounce khi rời bước 6/7/9 + lần cuối trước Submit | 07 §4 |

## B. State machine và lifecycle

| # | Câu hỏi | Đề xuất | Ở đâu |
|---|---|---|---|
| B1 | `Draft` có persist không (lưu wizard dở)? | Không; `Draft` chỉ là trạng thái trong request | 04 §1 |
| B3 | Một Workspace tối đa một Operation đang chạy? | Có; request mới trả `WORKSPACE_NOT_READY` | 04 §2 |
| B4 | Node `NotReady` → Workspace trên node chuyển `Degraded`? | Có, `ComputeReady=False`; không tự đổi node | 04 §5 |
| B5 | Cạnh `Cordoned → NotReady` cho node và `Unavailable → Maintenance` cho Cluster | Thêm (để Admin "đóng băng" Cluster đang flap) | 04 §5, §6 |
| B6 | Ai chuyển Cluster `Registered → Ready`? | Worker check CRD/Capsule sẵn sàng, Admin bấm xác nhận | 04 §6 |
| B7 | Cluster `Maintenance`: delete/retry Workspace vẫn xử lý? | Có; chỉ chặn create/resize | 04 §6 |
| B8 | Cluster `Unavailable` giữa lúc delete: Operation `Running` hay `Blocked`? | `Blocked` với reason `ClusterUnavailable`, tự tiếp khi Cluster Ready | 04 §10 |
| B10 | Thứ tự delete: release NodeReservation (D6) trước xóa Tenant (D7) như plan, hay đảo? | Giữ như plan; reservation chỉ release sau khi Pod đã rời node | 04 §10 |
| B11 | Release ClusterGrant reservation cùng lúc quarantine CIDR hay sớm hơn? | Cùng lúc (đơn giản, một bước cuối) | 04 §10 |
| B12 | Capacity state của node lưu cột hay tính lúc đọc? | Lưu cột, tính lại trong mỗi transaction reservation | 04 §5 |
| B13 | Mapping `Cordoned` → `Tenant.spec.cordoned` của Capsule | Xác nhận field tồn tại ở Capsule 0.13.x | 04 §1 |

## C. Dữ liệu và transaction

| # | Câu hỏi | Đề xuất | Ở đâu |
|---|---|---|---|
| C6 | Đổi `allowed_storage_classes` của grant → fan-out re-render mọi CR trong grant | Thêm task ở M5/M7 | 02 §5 |
| C8 | Mã lỗi các luồng dùng nhưng chưa có trong enum `ErrorCode` của OpenAPI | Thêm khi làm tới endpoint tương ứng: `OWNER_USER_NOT_FOUND`, `CLUSTER_GRANT_BELOW_ALLOCATION`, `NETWORK_POOL_EXHAUSTED`, `NAMESPACE_QUOTA_EXCEEDED`, `DEDICATED_NODE_NOT_CLEAN`, `INVITATION_INVALID`, `INVITATION_EMAIL_MISMATCH`, `RATE_LIMITED`, `IDEMPOTENCY_KEY_REUSED` | 03, 06 |

## D. Kubernetes, network, scheduling

| # | Câu hỏi | Đề xuất | Ở đâu |
|---|---|---|---|
| D3 | System pool: label `tenantry.io/compute-pool=system` + taint NoSchedule | Giữ; lab hiện chưa có node system riêng → M1 phải tạo | 05 §1, 01 §3 |
| D4 | Enforce scheduling: `PodNodeSelector` + `PodTolerationRestriction` (admission plugin kubeadm) hay validating webhook của controller? | MVP dùng hai admission plugin có sẵn + test bắt buộc; webhook là hardening sau | 05 §2 |
| D5 | Shared ceiling tính trên `requests`; `limits` kiểm bằng ratio riêng | Giữ | 05 §4 |
| D6 | Chia supernet `10.240.0.0/16`: `/18` pool chung, `/18` pool riêng đầu tiên | Chốt bảng chia để pool chung/riêng không overlap | 05 §5 |
| D7 | Egress internet: replicate policy theo profile Workspace; deny metadata `169.254.169.254` | Mặc định cho phép internet egress (demo pull image), deny metadata luôn | 05 §6 |
| D8 | ValidatingAdmissionPolicy chặn tenant ghi Ingress/Certificate có chặn nhầm controller hoặc HTTP-01 solver không? | Test âm ở M11 | 05 §7 |
| D9 | Annotation IPPool cho Namespace tạo bằng kubectl: Capsule replication có đặt kịp không? | Controller bù annotation nếu thiếu; test | 03 §7 |
| D10 | Tenant Pod gọi `kubernetes.default` có chặn bằng NetworkPolicy không? | Không chặn; ServiceAccount tenant không có RoleBinding mặc định | 06 §1 |
| D11 | Controller cần `nodes patch`: thu hẹp bằng ValidatingAdmissionPolicy chỉ cho đổi label/taint prefix `tenantry.io/` | Thêm ở M8 hardening | 06 §5 |
| D14 | LimitRange thêm `max` per PVC? | Có, để một PVC không nuốt cả `requests.storage` | 05 §9 |

## E. Bảo mật / identity

| # | Câu hỏi | Đề xuất | Ở đâu |
|---|---|---|---|
| E3 | Client `tenantry-sync` với `manage-users` + `query-groups` có đủ để tạo group, thêm/gỡ member, logout user không? | Kiểm chứng trên Keycloak 26 của lab ở M4 | 06 §7 |
| E6 | Keycloak có chạy lại group mapper khi refresh token không? Nếu có, token refresh ngay sau khi gỡ group đã mất group claim | Kiểm chứng ở M4; nếu đúng thì bỏ bước logout user để người bị gỡ không bị đăng xuất mọi nơi | 06 §5 |
| E5 | Kích thước JWT header khi user thuộc ~20 group | Test qua Traefik, Capsule Proxy, Headlamp | 06 §2 |
| E7 | Ba con số phiên (access 5 phút, SSO idle 8 giờ, SSO max 24 giờ) có hợp với buổi demo không? | Giữ; chỉnh trong realm export nếu cần | 06 §3 |
