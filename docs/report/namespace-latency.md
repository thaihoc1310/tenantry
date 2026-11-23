# Thời gian tạo Namespace trên portal (NFR PRODUCT-PLAN §23, ADR-0019)

Đo ngày 2026-11-23, 13:42–14:04 (+07), bằng `go test -tags lab -run TestNamespaceLatency ./test/isolation` (`operator/test/isolation/measure_test.go`, commit 4a39b1c). Operator chạy trên lab là bản a8928a8.

- **Mạng:** laptop ở mạng công ty, đi thẳng tới lab qua Wi-Fi (không WireGuard).
- **Tổ chức thử:** một Organization optest được Platform Admin tạo, có 2 Workspace Shared (`nsa`, `nsb`) trên `shared-general`.
- **Cách thêm Namespace:** mỗi Workspace thêm 10 Namespace qua portal (`POST /workspaces/{id}/namespaces`), tuần tự. API chỉ cho mỗi Workspace một Operation chạy tại một lúc; hai Workspace chạy song song. Tổng cộng 20 Namespace.
- **Dọn dẹp:** Organization đã xoá sau quarantine, account đã disable. Kiểm lại thì không còn Namespace, PlatformWorkspace, Tenant hay IPPool optest nào.

## Đo gì

- **t_cluster, phía cụm:**
  - Bắt đầu: lúc CR `PlatformWorkspace` có Namespace mới trong `spec.namespaces`. Theo giờ server, đó là `managedFields` Apply của `tenantry-provisioning-worker`.
  - Kết thúc: lúc Namespace có đủ mọi thứ. Theo giờ server, đó là mốc muộn nhất trong: `creationTimestamp` của Namespace, của NetworkPolicy `tenant-default-deny`, của LimitRange `tenantry-defaults`, và lần ghi status của GlobalResourceQuota `<tenant>-workspace` khi nó bắt đầu đếm Namespace này.
  - Mốc của Kubernetes có độ phân giải 1 s.
- **t_user, người dùng thấy:** từ `createdAt` tới `finishedAt` của Operation `namespace.create`, theo giờ của Platform API.
- **Giờ poll:** test tự poll mỗi 250 ms để đối chiếu. Số này có lẫn độ trễ của đường mạng laptop, nên chỉ dùng phụ.

## Kết quả (n = 20)

| | p50 | p95 | max |
|---|---|---|---|
| t_cluster, giờ server | 1 s | 2 s | 5 s |
| t_cluster, giờ poll | 0,91 s | 2,42 s | 5,29 s |
| t_user, giờ server (Operation) | 31,8 s | 72,6 s | 73,1 s |
| t_user, giờ poll | 31,9 s | 73,0 s | 73,4 s |

t_user không trải đều mà rơi vào hai mức: 14 Namespace mất khoảng 31–34 s, 6 Namespace mất khoảng 71–73 s.

**Đánh giá theo NFR "Namespace create p95 < 15 s":**
- Phía cụm đạt: p95 là 2 s.
- Phía người dùng KHÔNG đạt với operator a8928a8: p95 là 72,6 s.

## Vì sao người dùng chờ 31 s hoặc 72 s

Operation chỉ Succeeded khi Workspace về `Ready`. `Ready` lại cần `ProxyAccessReady`: probe của operator phải thấy, qua Capsule Proxy, đúng các Namespace của Workspace.

Diễn biến khi một Namespace mới xuất hiện:
1. Capsule Proxy chưa liệt kê ngay Namespace đó. Probe đầu tiên trả `Progressing` ("waiting for Capsule Proxy to show …").
2. Code cũ coi mọi kết quả chưa đạt như một lỗi, nên chỉ probe lại sau `probeRetry` = 30 s (`operator/internal/subreconciler/proxyaccess.go`).
3. Vì vậy người dùng chờ khoảng 30 s, cộng thời gian của chính việc tạo, ra mức 31 s. Khi lần thử thứ hai vẫn chưa thấy thì thêm 30 s nữa, cộng các bước khác, ra mức 72 s.

Metric khớp với giải thích này: trong lúc đo, `tenantry_proxy_probes_total` tăng khoảng 23 ở `Progressing` và 23 ở `Reconciled`. Tức là khoảng một lần chờ cho mỗi Namespace mới (20) và mỗi Workspace mới (2).

## Sau khi sửa: đo lại

Bản sửa (commit e35daa1): khi probe chỉ đang chờ Proxy bắt kịp (`Progressing`), nó thử lại sau 2 s, trong tối đa 1 phút; quá 1 phút thì về nhịp 30 s. Lỗi thật (`ProxyUnreachable`, `ProxyNotFiltering`) vẫn thử lại sau 30 s. Envtest `TestWorkspaceProxyAccess` có ca này.

Đo lại ngày 2026-11-23 lúc 14:31–14:35 (+07):
- operator e35daa1 (release revision 14, image `src-856c90f9001c`), backend `src-582a475972a2` vừa deploy;
- cùng mạng công ty đi thẳng, cùng cách đo, n = 20 (commit 703a26c).

| | trước sửa: p50 / p95 / max | sau sửa: p50 / p95 / max |
|---|---|---|
| t_user, giờ server (Operation) | 31,8 / 72,6 / 73,1 s | **7,7 / 13,8 / 16,4 s** |
| t_user, giờ poll | 31,9 / 73,0 / 73,4 s | 7,9 / 13,8 / 16,5 s |
| t_cluster, giờ server | 1 / 2 / 5 s | 5 / 10 / 11 s |
| t_cluster, giờ poll | 0,91 / 2,42 / 5,29 s | 5,67 / 10,11 / 11,50 s |

**NFR "p95 < 15 s" phía người dùng:** đạt sau khi sửa (13,8 s), nhưng sát ngưỡng. Mẫu chậm nhất mất 16,4 s, và chỉ có 20 mẫu.

**Probe trong lúc đo lại:** `tenantry_proxy_probes_total` tăng khoảng 29 ở `Progressing` và 24 ở `Reconciled`. Tức là mỗi Namespace mới vẫn phải chờ Proxy 1–2 lần, nhưng giờ mỗi lần chờ chỉ khoảng 2 s.

**Thời gian tạo Workspace** (`workspace.create`, giờ server, cùng lần chạy, Workspace Shared nhỏ):

| | trước sửa | sau sửa |
|---|---|---|
| Từng Workspace | 2 mẫu, chỉ biết theo bucket của histogram (`tenantry_operation_duration_seconds`, `tenantry_workspace_provision_seconds`): một cái 30–60 s, một cái 60–120 s | 5,1 s; 4,7 s; 6,1 s; 65,3 s |

Ba trong bốn Workspace xong trong khoảng 5 s.

**Chưa giải thích được** (đọc thêm từ log Capsule và Prometheus, ngày 2026-11-23 tối, chỉ đọc):

*Workspace `wsx0` mất 65,3 s.*
- Controller `capsule/replications/namespace-trigger` báo race replication (`applying of N resources failed`) trên Namespace `base` của **cả bốn** Workspace: `nsa` 07:30:51Z, `nsb` 07:30:57Z, `wsx0` 07:31:03Z, `wsx1` 07:32:12Z. Ba Workspace kia vẫn xong trong 4,7–6,1 s, nên race một mình không giải thích được 65 s.
- Controller GlobalTenantResource của Capsule không có lần reconcile nào lỗi trong lần chạy. Mọi lần chạy của nó kết thúc bằng `requeue_after` (khoảng 46 lần trong 12 phút), tức tự hẹn lần sau theo `resyncPeriod`. CRD mặc định `resyncPeriod` là 60 s, và operator không đặt giá trị này.
- Operator chỉ coi `PolicyReady` đạt khi điều kiện Ready của GlobalTenantResource là True. Khi Ready=False vì race, nó chờ (tối đa 2 phút, `replicationRaceGrace`).
- Giả thuyết dẫn đầu: với `wsx0`, status của GlobalTenantResource giữ kết quả lỗi cho tới lần reconcile định kỳ kế tiếp, khoảng 60 s sau. Khoảng 5 s bình thường cộng 60 s ra đúng 65 s. Với ba Workspace kia, status được ghi lại sau khi lần thử lại đã thành công. Giả thuyết này **chưa chứng minh**: CR và GlobalTenantResource đã bị xoá khi dọn.
- Đã sửa test (f99566b): lần đo tới, ngay khi `workspace.create` xong, test ghi `lastTransitionTime` của từng condition trên Workspace, Ready và thời điểm ghi status của GlobalTenantResource.

*t_cluster chậm hơn lần trước* (p50 1 s lên 5 s).
- Tải không khác: trong 16 phút đo, operator reconcile PlatformWorkspace 406 lần trước sửa và 415 lần sau sửa. Capsule có nhiều hơn một chút (Tenant 102 lên 164, GlobalResourceQuota 116 lên 139, GlobalTenantResource 45 lên 58). Hàng đợi của Capsule tối đa 1.
- Race replication không phải nguyên nhân. Bốn Namespace bị race (`nsa-n00`, `nsb-n00`, `nsa-n02`, `nsb-n02`) có t_cluster chỉ 2–4 s, còn các mẫu cao nhất (9–11 s: `nsa-n06`, `nsb-n07`, `nsa-n09`, `nsb-n06`, `nsb-n08`) không bị race.
- t_cluster tăng dần trong lần chạy: các Namespace đầu 2–4 s, các Namespace cuối 8–11 s. Khác biệt chính so với lần trước là nhịp: sau sửa, mỗi Workspace thêm một Namespace khoảng mỗi 8 s (trước là 31 s), hai Workspace song song. Mỗi Tenant cũng có dần tới 11 Namespace.
- Có 6 mẫu t_cluster lớn hơn t_user, tức Operation đã Succeeded trước khi phần cuối cùng tới. Operator không chờ GlobalResourceQuota đếm Namespace, nên nghi phạm là lần ghi status của GlobalResourceQuota: càng nhiều Namespace và càng nhiều thay đổi dồn dập thì càng trễ. **Chưa tách được.**
- Đã sửa test (f99566b): lần đo tới ghi riêng mốc của Namespace, NetworkPolicy, LimitRange, status quota và lúc Operation xong, tính từ lúc CR đổi.
- Hệ quả với người dùng: không có. t_user không chờ phần này, và p95 t_user vẫn 13,8 s.

**Dọn dẹp sau lần đo lại:**
- Quarantine hết lúc 14:44:33, nhưng ngay sau đó phiên của account owner bị 401, nên test không tự xoá được Organization.
- Platform Admin đã xoá nó bằng `DELETE /admin/organizations/{id}` (204).
- Test đã được sửa: lần sau, khi phiên owner hết thì tự dùng đường admin (commit cb9a886).
- Không còn Namespace, PlatformWorkspace hay account optest nào chưa disable.
