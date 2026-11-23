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

**Chưa giải thích được:**
- **Workspace `wsx0` mất 65,3 s.** Log operator chỉ có một lần conflict khi ghi status (tự thử lại ngay). CR đã bị xoá khi dọn, nên không còn dòng thời gian của các condition để biết bước nào chậm. Ứng viên: cửa sổ 2 phút operator dành cho race replication của Capsule (known-limitations mục 10), hoặc Proxy chậm quá 1 phút nên probe rơi về nhịp 30 s. Muốn biết thì lần đo tới phải ghi lại `lastTransitionTime` của từng condition trước khi xoá.
- **t_cluster chậm hơn lần trước** (p50 1 s lên 5 s), dù phần cụm không đổi. Có 6 mẫu t_cluster còn lớn hơn t_user, nghĩa là Operation đã Succeeded trước khi GlobalResourceQuota ghi nhận Namespace. Nghi phạm là lần ghi status của GlobalResourceQuota, vốn nằm trong mốc kết thúc của t_cluster. Chưa tách được thành phần nào trễ.

**Dọn dẹp sau lần đo lại:**
- Quarantine hết lúc 14:44:33, nhưng ngay sau đó phiên của account owner bị 401, nên test không tự xoá được Organization.
- Platform Admin đã xoá nó bằng `DELETE /admin/organizations/{id}` (204).
- Test đã được sửa: lần sau, khi phiên owner hết thì tự dùng đường admin (commit cb9a886).
- Không còn Namespace, PlatformWorkspace hay account optest nào chưa disable.
