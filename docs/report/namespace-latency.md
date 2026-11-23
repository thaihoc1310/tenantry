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
- Mẫu thứ hai cùng kiểu, từ e2e của A (2026-11-23 23:33 +07, mạng nhà qua `wg0`): Workspace `e2ewzthi6fc-solar`, Operation `namespace.create` chạy từ 16:33:11Z tới 16:34:18Z (67 s), dù Namespace đã có trên cụm. Capsule ghi race replication trên Namespace `e2ewzthi6fc-solar-api` lúc 16:33:13,5Z, tức khoảng 65 s trước khi Operation xong. GlobalTenantResource của nó đã bị purge xoá trước khi kịp đọc status. Cùng buổi còn ba Namespace khác bị race (`e2ewsthlrbw-solar-backend` 16:35:42Z, `e2ewsthlrbw-solar-scratch` 16:35:49Z, `e2enwthosgz-water-app` 16:38:06Z). Thời gian Operation của chúng đã mất: các test đó PASS nên Playwright không giữ trace, và purge lúc 23:49–23:54 đã xoá các dòng Operation. Vẫn là tương quan: trong lần đo lại của mình có 7 lần race không kéo theo 60 s.

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

## Lần đo thứ ba: tách từng phần (2026-11-23 23:56, mạng nhà qua `wg0`)

Cùng operator rev 14 (`src-856c90f9001c`), cùng cách đo, n = 20 cộng 4 Workspace. Test mới (f99566b) ghi thêm, theo giờ server, lúc từng phần của mỗi Namespace có mặt, tính từ lúc CR đổi (`NSPARTS`), và dòng thời gian condition của mỗi Workspace (`TIMELINE`).

| | p50 | p95 | max |
|---|---|---|---|
| t_user, giờ server (Operation) | 7,6 s | 15,0 s | 22,0 s |
| t_cluster, giờ server | 7 s | 11 s | 11 s |

`workspace.create`: 3,8 s; 3,3 s; 4,8 s; 4,2 s.

**Giả thuyết "status của GlobalResourceQuota làm t_cluster tăng": bác bỏ.**

Theo NSPARTS, tính từ lúc CR đổi:

| Phần | Trễ |
|---|---|
| Namespace | 0–4 s |
| Status quota (GRQ đếm Namespace) | 0–5 s |
| NetworkPolicy `tenant-default-deny` | 0–11 s |
| LimitRange `tenantry-defaults` | 0–11 s |

Hai phần đến muộn nhất là NetworkPolicy và LimitRange, do Capsule replicate từ GlobalTenantResource. Độ trễ của chúng tăng dần theo lần chạy: Namespace đầu 0–3 s, Namespace cuối 8–11 s. Vậy t_cluster tăng là do replication của Capsule chậm dần khi các Namespace của cùng Tenant đến dồn dập, không phải do quota.

Metric của Capsule trong 5 phút đo (16:56–17:00Z), giải thích vì sao replication chậm dần khi mỗi Tenant đạt khoảng 11 Namespace:

| | Giá trị |
|---|---|
| Request API của Capsule | 25 230 (khoảng 84 req/s), so với 559 trong 5 phút yên tĩnh một giờ trước, gấp khoảng 45 lần |
| Một lần reconcile, p95 | `replications/namespace-trigger` 13,9 s; `replications/globaltenantresource` 18 s; `tenants` 3,1 s |
| Retry của workqueue | `replications/globaltenantresource` 55; `tenants/namespace-cleanup` 60; `namespace-trigger` 7,5 |
| Hàng đợi lớn nhất | tối đa 2: việc không xếp hàng lâu, mà mỗi lần reconcile chạy lâu |

Mỗi Namespace mới kích một lượt replication. Mỗi lượt đọc và ghi lại mọi bản sao trong mọi Namespace của Tenant, nên công việc tăng theo số Namespace. Client của Capsule trên lab đặt QPS 40, burst 80 (`deploy/cluster/values/capsule.yaml`). Ở 84 req/s trung bình, nhiều khả năng client bị chính bộ giới hạn tốc độ của nó giữ lại, nên một lần reconcile kéo dài tới 14–18 s. **Chưa chứng minh** được phần này: Capsule 0.15.2 không xuất metric thời gian chờ của bộ giới hạn (`rest_client_rate_limiter_duration_seconds` không có series nào).

**Đã sửa (35e9510, chưa deploy):** `PolicyReady` giờ chỉ đạt khi mọi Namespace đang sống đã có đủ bản sao LimitRange và NetworkPolicy, đọc thẳng từ apiserver. Trong lúc chờ, operator thử lại sau 2 s.
- Chọn requeue ngắn thay cho watch. Một watch trên NetworkPolicy và LimitRange buộc operator cache mọi object loại đó trong cụm, và cần quyền list/watch toàn cụm, chỉ để đỡ tối đa 2 s; requeue chỉ chạy khi có Workspace đang chờ.
- Nếu sau này cần watch, có thể lọc được. Đọc trên lab (Namespace `acme-solar-frontend`, 2026-11-24): mỗi bản sao Capsule tạo mang các nhãn `projectcapsule.dev/tenant: <tenant>`, `projectcapsule.dev/created-by: replications`, `capsule.clastix.io/managed-by: <tenant>`, và ownerReference trỏ về GlobalTenantResource cùng tên Workspace. Không có nhãn `tenantry.io/managed` của operator. Cách làm khi đó: cache chỉ các object có `projectcapsule.dev/created-by=replications`, rồi map qua nhãn `projectcapsule.dev/tenant` về PlatformWorkspace cùng tên. Vẫn cần quyền list/watch trên hai loại này.
- Quyền thêm vào operator chỉ là `get` trên `limitranges` và `networkpolicies`.
- Namespace nào đã đủ bản sao thì được nhớ và không đọc lại; Capsule tự đặt lại bản sao bị xoá.
- Envtest `TestWorkspaceWaitsForReplicas` kiểm đúng ca này.
- Kỳ vọng sau khi deploy: t_user sẽ tiến sát t_cluster (p50 khoảng 7 s, p95 khoảng 11 s ở lần đo này). Con số đó trung thực hơn, vì người dùng chỉ được báo xong khi Pod thật sự tạo được trong Namespace.

**Phát hiện thêm: Operation có thể Succeeded trước khi NetworkPolicy có mặt.** Ví dụ `nsb-n04` (Operation xong lúc 4 s, NetworkPolicy lúc 7 s), `nsb-n05` (6 s và 11 s), `nsb-n08` (8 s và 11 s). `PolicyReady` chỉ đọc Ready của GlobalTenantResource, không kiểm từng Namespace mới đã có bản sao chưa. Hệ quả cho người dùng: ngay sau khi portal báo xong, Pod tạo trong Namespace đó có thể bị VAP `tenantry-tenant-network-ready` từ chối vài giây (đúng với known-limitations mục 16). Một Deployment sẽ tự tạo lại Pod; `kubectl run` thì nhận lỗi.

**Giả thuyết "GlobalTenantResource chờ resync 60 s" cho ca 65 s: lần này không thử được.**
- Không Workspace nào chậm: TIMELINE cho thấy Ready của GlobalTenantResource ở 1–3 s, `PolicyReady` ở 1–4 s.
- Capsule vẫn ghi 6 race replication, trong đó có Namespace `base` của `nsa`, `wsx0`, `wsx1`. Vậy một race trên Namespace không làm GlobalTenantResource mất Ready.
- Tính cả ba lần chạy và e2e của A: khoảng 18 race, 2 lần chậm khoảng 65 s.
- Cơ chế của ca hiếm vẫn chưa chứng minh. Ứng viên còn lại: chính controller GlobalTenantResource gặp race lúc nó tự reconcile, nên status ghi lỗi và giữ tới lần resync kế tiếp. Lần chậm tới, dòng TIMELINE sẽ cho câu trả lời.
- Chưa đủ cơ sở để đặt `resyncPeriod` ngắn hơn.

**Thời gian người dùng: nhích qua ngưỡng ở lần này.**
- p95 là 15,0 s, max 22,0 s (lần trước 13,8 s và 16,4 s). Với n = 20, p95 chính là mẫu chậm thứ hai, nên con số dao động theo vài mẫu.
- Ở các mẫu chậm, mọi phần đã có từ trước khi Operation xong nhiều giây. Ví dụ `nsb-n06`: các phần có ở 1–5 s, Operation xong ở 21 s; `nsa-n01`: 7 s và 14 s.
- Outbox không tồn đọng, DB pool không có lần chờ nào. Probe trả khoảng 33 lần `Progressing` trên 29 lần `Reconciled`.
- Nhiều khả năng phần chênh là probe chờ Capsule Proxy liệt kê Namespace mới (mỗi lần thử cách 2 s). Chưa tách được.
- Kết luận cho NFR: phía người dùng ở quanh ngưỡng 15 s (13,8 s và 15,0 s qua hai lần đo), chưa đạt chắc chắn.

## Lần đo thứ tư: sau bản sửa bản sao (2026-11-24 01:28, mạng nhà qua `wg0`)

Operator rev 15 (`src-a7894de1906c`, commit d96f980): `PolicyReady` chờ bản sao LimitRange và NetworkPolicy trong mọi Namespace (35e9510, 74086c7). Cùng cách đo, n = 20 cộng 4 Workspace. **Đây là số liệu cuối cho luận văn.**

| | p50 | p95 | max |
|---|---|---|---|
| t_user, giờ server (Operation) | 11,9 s | 16,1 s | 18,1 s |
| t_cluster, giờ server | 6 s | 9 s | 11 s |
| `workspace.create` (4 Workspace) | 3,3 s; 3,6 s; 65,4 s; 68,8 s | | |

**Bản sửa đúng như mong đợi.** Ở cả 20 Namespace, Operation chỉ xong sau khi đã có đủ Namespace, NetworkPolicy, LimitRange và status quota (theo NSPARTS, `op_done` luôn sau mốc muộn nhất). Lần đo thứ ba còn có Operation xong lúc 4 s trong khi NetworkPolicy tới lúc 7 s. Đổi lại, t_user giờ là con số trung thực: t_cluster cộng 2–6 s (worker ghi status, probe Capsule Proxy).

**NFR "Namespace create p95 < 15 s" phía người dùng: không đạt** ở cấu hình này (16,1 s). Phía cụm đạt (9 s).

**Ca 65 s đã có bằng chứng.**
- Hai Workspace chậm (`nsb` 68,8 s, `wsx1` 65,4 s) đều bị Capsule race replication trên Namespace `base` (18:28:01Z và 18:29:16Z). Hai Workspace nhanh không bị race.
- TIMELINE: `nsb` có `PolicyReady`, Ready của GlobalTenantResource và lần ghi status của nó cùng lúc 1m6s; `wsx1` cùng lúc 1m4s.
- Vậy GlobalTenantResource giữ Ready=False tới lần ghi status kế tiếp, khoảng 60 s sau. Khớp với việc controller GlobalTenantResource không trả lỗi mà luôn `requeue_after` theo `resyncPeriod` 60 s.
- Namespace thêm vào một Workspace đã Ready thì race không làm GlobalTenantResource mất Ready: 9 race trong lần này, không cái nào kéo Operation lên 60 s. Chỉ lần reconcile đầu của một GlobalTenantResource mới bị giữ.
- Chưa giải thích được vì sao lần thứ ba có race trên `base` mà Workspace vẫn nhanh; có thể tuỳ race xảy ra trước hay sau lần reconcile đầu của GlobalTenantResource.
