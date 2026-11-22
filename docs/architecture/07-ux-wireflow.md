# 07 — Portal UX: information architecture và wireflow

> Góc nhìn, không định nghĩa gì mới: IA và yêu cầu ở `PRODUCT-PLAN.md` mục 19, 22, 28; route thật ở `IMPLEMENTATION-PLAN.md` mục 6; màn hình mẫu ở `docs/design/mockups/`. Đây là sơ đồ luồng màn hình để review trải nghiệm, không phải thiết kế giao diện.
> Cách đọc: sitemap → wireflow từng persona → wizard tạo Workspace → màn hình trạng thái/lỗi bắt buộc.

## 1. Sitemap

**Mục đích:** toàn bộ route của portal và ai thấy gì.

```mermaid
flowchart TB
  classDef pub fill:#e9ecef,stroke:#6c757d,color:#000
  classDef org fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef ws fill:#e2f0d9,stroke:#2d6a4f,color:#000
  classDef adm fill:#fde2e4,stroke:#c9184a,color:#000

  Login["/login → redirect Keycloak"]:::pub
  Empty["Chưa thuộc Org nào:<br/>xin tạo Organization / chờ duyệt"]:::pub
  Invite["/invitations/:token<br/>accept sau khi login"]:::pub
  Transfer["Đề nghị nhận Owner đang chờ<br/>hiện ở Settings của Org, accept theo transferId"]:::pub

  Switch["Organization switcher<br/>/orgs/:orgSlug"]:::org
  Over["Overview<br/>OrgOwner/OrgAdmin: quota theo ClusterGrant, Workspace count,<br/>pool/node remaining, network availability<br/>OrgMember: chỉ Workspace mình có quyền"]:::org
  WSL["Workspaces<br/>list + Create"]:::org
  Mem["Members & invitations"]:::org
  Grp["Groups"]:::org
  Set["Settings<br/>display name, transfer ownership,<br/>leave / delete Org"]:::org
  Cl["Granted Clusters<br/>quota, shared pools,<br/>dedicated nodes, network pools"]:::org
  Aud["Audit"]:::org

  WSD["Workspace detail<br/>/orgs/:orgSlug/workspaces/:wsSlug"]:::ws
  W1["Overview: state, conditions,<br/>operations timeline"]:::ws
  W2["Resources / quota"]:::ws
  W3["Compute: pool hoặc node,<br/>reservation"]:::ws
  W4["Network / CIDR / IP usage"]:::ws
  W5["Members & role bindings"]:::ws
  W6["Namespaces: list/create/delete/usage"]:::ws
  W6R["Routes: list/create/delete,<br/>URL và trạng thái"]:::ws
  W7["Access: kubeconfig,<br/>Open Web Console"]:::ws
  W8["Danger zone: cordon/uncordon, delete"]:::ws

  Adm["/admin (chỉ realm role platform-admin)"]:::adm
  A1["Organizations: duyệt/từ chối yêu cầu,<br/>tạo thẳng Org + Owner + grant"]:::adm
  A2["Clusters: registry, status, node của Cluster,<br/>shared pool (node, ratio, systemReserve),<br/>dedicated node, network pool"]:::adm
  A3["Onboarding policy"]:::adm
  A4["Grants: danh sách /admin/grants,<br/>sửa một grant /admin/grants/:orgSlug/:clusterId"]:::adm
  A7["Operations (retry / force)"]:::adm
  A8["Platform health"]:::adm

  Login --> Switch
  Login --> Empty --> Switch
  Invite --> Switch
  Transfer --> Switch
  Switch --> Over & WSL & Mem & Grp & Set & Cl & Aud
  WSL --> WSD
  WSD --> W1 & W2 & W3 & W4 & W5 & W6 & W6R & W7 & W8
  Switch -.->|"nếu có role"| Adm
  Adm --> A1 & A2 & A3 & A4 & A7 & A8
```

**Ai thấy gì.** OrgOwner và OrgAdmin thấy mọi mục. OrgMember chỉ thấy Overview rút gọn (các Workspace mình có quyền) và Workspaces; không thấy quota/usage toàn Org, Members, Groups, Clusters, Audit; Settings của OrgMember chỉ có nút Leave. API từ chối tương ứng (`PRODUCT-PLAN.md` mục 9.2), portal không chỉ ẩn menu. Organization `Pending`/`Rejected` chỉ có màn trạng thái yêu cầu.

**Điểm cần review**

- URL dùng **slug** (`/orgs/acme/workspaces/solar`) vì slug immutable — đổi display name không làm hỏng bookmark.

## 2. Wireflow — user mới (Alice)

**Mục đích:** từ chưa có tài khoản tới Workspace Ready; Admin chỉ can thiệp đúng một lần, ở bước duyệt yêu cầu tạo Organization.

```mermaid
flowchart LR
  classDef s fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef d fill:#fff3cd,stroke:#b58900,color:#000
  classDef e fill:#e2f0d9,stroke:#2d6a4f,color:#000

  S1["Landing<br/>nút Sign up / Login"]:::s
  S2["Keycloak Register (email, tên)<br/>bấm link xác minh rồi mới đặt mật khẩu"]:::s
  S3["Callback → tạo user record<br/>màn trống: xin tạo Organization"]:::s
  S3a["Gửi yêu cầu: slug 'acme'<br/>preview tên Kubernetes acme-*"]:::s
  S3b["Chờ duyệt<br/>(từ chối → hiện lý do, gửi lại)"]:::d
  S4["Admin duyệt → Overview Org acme<br/>banner: bạn có quota Shared<br/>2 CPU / 4 GiB / 2 Workspace"]:::e
  S5["Workspaces → Create"]:::s
  W["Wizard 7 màn, xem mục 4<br/>Solar, Shared, 2 CPU / 4 GiB,<br/>admins = Owner, developers = Developer,<br/>viewers = Viewer"]:::d
  S6["Operation view<br/>Operation Pending → Running → Succeeded,<br/>timeline theo Condition"]:::s
  S7["Workspace Overview<br/>conditions xanh,<br/>nút Kubeconfig + Web Console bật"]:::e
  S7a["Namespaces → tạo frontend<br/>(sau khi app chạy: Routes → tạo web)"]:::s
  S8["Members → Invite<br/>Joe (group developers), Bob (group viewers)"]:::s
  S9["Copy link, gửi tay"]:::s

  S1 --> S2 --> S3 --> S3a --> S3b --> S4 --> S5 --> W --> S6 --> S7 --> S7a
  S4 --> S8 --> S9
```

Ghi chú:

- Callback (S3) chỉ upsert user và tạo phiên nên nhanh và idempotent; không có gì ở đây phụ thuộc Cluster.
- Slug Organization do user tự chọn ở S3a, chỉ gồm chữ thường và số (không có `-`) và **không sửa được** sau đó [4.2]; form phải hiện ngay tên Kubernetes sẽ sinh ra (`acme-<workspace>`) và báo `SLUG_TAKEN` tại chỗ.
- S3b phải nói rõ đang chờ ai và cho thấy lý do khi bị từ chối; một user chỉ có một yêu cầu chờ duyệt tại một thời điểm.
- Ngay sau duyệt, group `admins` còn `Sync pending` vài giây; wizard tạo Workspace báo `GROUP_SYNC_PENDING` tới khi group sync xong.

## 3. Wireflow — Developer (Joe) và Viewer (Bob)

**Mục đích:** người được mời vào group `developers`/`viewers` dùng data plane; tạo Namespace và Route là việc của OrgOwner/OrgAdmin hoặc group TenantOwner, không phải của Developer.

```mermaid
flowchart LR
  classDef s fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef e fill:#e2f0d9,stroke:#2d6a4f,color:#000
  classDef x fill:#fde2e4,stroke:#c9184a,color:#000

  J1["Mở invite link"]:::s
  J2["Login/Register Keycloak"]:::s
  J3{"email verified<br/>khớp invite?"}:::s
  J4["Accept → membership OrgMember<br/>+ vào group developers"]:::e
  J5["Lỗi: email không khớp,<br/>gợi ý login đúng tài khoản"]:::x
  J6["Org acme → Overview rút gọn<br/>chỉ thấy Solar"]:::s
  J7["Solar → Access<br/>Download kubeconfig<br/>hướng dẫn cài kubelogin"]:::s
  J8["Terminal: kubectl get ns<br/>→ browser SSO → thấy acme-solar-frontend"]:::e
  J8a["kubectl apply app + PVC<br/>vào acme-solar-frontend"]:::e
  J9["Open Web Console<br/>→ Headlamp SSO"]:::e
  J10["Namespaces, Routes: chỉ xem<br/>(Developer không tạo được)"]:::s

  J1 --> J2 --> J3
  J3 -->|"có"| J4 --> J6 --> J7 --> J8 --> J8a
  J3 -->|"không"| J5
  J6 --> J9
  J6 --> J10
```

Bob (group `viewers`, Viewer) đi cùng đường, khác ở chỗ Headlamp/kubectl chỉ đọc; thử `kubectl delete` → 403 kèm thông điệp của RBAC. Route `web` cho app của Joe do Alice tạo trên portal.

**Điểm cần review**

- Khi Joe chưa cài kubelogin, lỗi xảy ra ở terminal, không ở portal. Trang Access nên có block "Kiểm tra: `kubectl oidc-login --version`" và link cài.
- Joe thuộc nhiều Workspace cùng Cluster sẽ thấy tất cả Namespace được phép trong một `kubectl get ns` — plan nói đây không phải lỗi; UI Access nên ghi một dòng để tránh thắc mắc khi demo.

## 4. Wizard tạo Workspace

**Mục đích:** 7 màn gom 12 mục của plan 19.1, chỗ nào gọi API validate, chỗ nào auto-select. Khớp mockup `pages-workspace.js`.

| Màn | Mục của plan 19.1 |
|---|---|
| 1 Name | 1 |
| 2 Cluster and compute | 2, 3, 4, 5 |
| 3 Resources | 6 |
| 4 Network | 7 |
| 5 Namespaces | 8, cộng Namespace tạo luôn (tùy chọn, trường `namespaces` của body tạo, plan 16.4) |
| 6 Access | 9, 10 |
| 7 Review | 11, 12 |

```mermaid
flowchart TB
  classDef st fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef auto fill:#e9ecef,stroke:#6c757d,color:#000
  classDef api fill:#fff3cd,stroke:#b58900,color:#000
  classDef err fill:#fde2e4,stroke:#c9184a,color:#000

  B1["1. Name<br/>tên, slug; preview Tenant acme-solar,<br/>Namespace acme-solar-*, Route *-acme-solar.mvp.tenantry.site"]:::st
  B2["2. Cluster and compute<br/>Cluster auto nếu 1 grant, quota còn lại (read-only);<br/>mode Shared | Dedicated, mode không dùng được thì disable kèm lý do;<br/>Shared: pool auto nếu 1, hiện allocated/ratio×reservable;<br/>Dedicated: node card (health, capacity state, remaining, Workspace đang dùng)"]:::st
  B3["3. Resources<br/>Shared: requests + limits (limits ≥ requests, trong quota còn lại của grant)<br/>Dedicated: requests = limits, khóa limits, ≤ remaining của node<br/>Pods, storage"]:::st
  B4["4. Network<br/>mặc định Auto /24<br/>Advanced: pool, prefix, requested CIDR"]:::st
  B5["5. Namespaces<br/>namespaceQuota + Namespace tạo luôn (tùy chọn)"]:::st
  B6["6. Access<br/>Owner groups (≥1), Developer / Viewer groups<br/>group chưa Synced bị disable"]:::st
  V["POST workspaces:validate<br/>trả lỗi theo mã, gắn vào màn"]:::api
  B7["7. Review → Create<br/>202 operation → Operation view SSE"]:::api
  E["Lỗi gắn vào màn:<br/>SLUG_TAKEN → 1<br/>CLUSTER_GRANT_QUOTA_EXCEEDED, COMPUTE_POOL_CAPACITY_EXCEEDED,<br/>DEDICATED_NODE_INSUFFICIENT_CAPACITY → 3<br/>CIDR_OVERLAP, NETWORK_POOL_EXHAUSTED → 4<br/>LAST_OWNER_BINDING, GROUP_SYNC_PENDING → 6"]:::err

  B1 --> B2 --> B3 --> B4 --> B5 --> B6 --> V
  V -->|"ok"| B7
  V -->|"lỗi"| E
```

Nội dung giải thích bắt buộc trong wizard (plan 19.1): Cluster không đổi sau khi tạo; Shared vs Dedicated khác ở placement; node thuộc Org nhưng chia capacity; Dedicated dùng Guaranteed QoS; Workspace Shared trừ CPU/RAM/Pod vào quota của grant, Workspace Dedicated chỉ trừ vào dung lượng còn lại của node đã chọn, số Workspace, Namespace và storage trừ vào grant với cả hai mode; CIDR không đổi tùy tiện; Owner tạo được Namespace, Developer không; không hỗ trợ cluster-scoped resource.

Hết `maxWorkspacesTotal` của entitlement (`ENTITLEMENT_EXCEEDED`) hoặc `maxWorkspaces` của grant thì nút Create ở danh sách Workspace đã disable kèm lý do (mục 5), không vào được wizard.

Rời mỗi màn, wizard gọi `:validate` với payload hiện tại và gọi lần cuối trước Submit (IMPLEMENTATION-PLAN mục 6.4); sơ đồ chỉ vẽ lần cuối. Submit vẫn tự validate lại trong transaction.

## 5. Màn hình trạng thái, rỗng và lỗi bắt buộc

**Mục đích:** plan 19.2 yêu cầu không chỉ làm happy path; mỗi trạng thái dưới đây phải có màn hình/thông điệp.

```mermaid
flowchart LR
  classDef ok fill:#e2f0d9,stroke:#2d6a4f,color:#000
  classDef warn fill:#fff3cd,stroke:#b58900,color:#000
  classDef bad fill:#fde2e4,stroke:#c9184a,color:#000

  subgraph Org["Cấp Organization"]
    O1["Hết quota onboarding<br/>→ banner 'liên hệ Admin', nút Create disabled kèm lý do"]:::warn
    O2["Default Cluster Maintenance/Unavailable<br/>→ Create disabled, Workspace hiện có vẫn mở"]:::warn
    O3["Không có compute option hợp lệ<br/>(grant không có shared pool và Organization<br/>chưa có dedicated node Ready trên Cluster)<br/>→ giải thích, gợi ý liên hệ Admin"]:::warn
  end
  subgraph WS["Cấp Workspace"]
    W1["Provisioning lâu > 2 phút<br/>→ hiện condition đang chờ"]:::warn
    W2["Failed<br/>→ condition False + error code + nút Retry"]:::bad
    W3["Degraded (node Overcommitted)<br/>→ badge + 'Admin đang xử lý', workload vẫn chạy"]:::warn
    W4["Cordoned<br/>→ chỉ đọc, nút Uncordon"]:::warn
    W5["Delete Blocked bởi PVC/finalizer<br/>→ liệt kê object, nút Retry,<br/>'chờ, retry hoặc liên hệ Admin'"]:::bad
    W6["Cluster Unavailable<br/>→ Workspace hiện 'unknown', Web Console disable"]:::warn
  end
  subgraph Acc["Cấp Access"]
    A1["Group chưa Synced<br/>→ badge trên binding, cảnh báo token chưa có group"]:::warn
    A2["Vừa bị gỡ khỏi group<br/>→ 'quyền Kubernetes hết hạn sau ≤5 phút,<br/>người bị gỡ phải đăng nhập lại'"]:::warn
    A3["Owner không thuộc group nào<br/>→ kubeconfig kèm cảnh báo 403"]:::warn
  end
  subgraph Net["Cấp Network/Node"]
    N1["NetworkPool hết CIDR (NETWORK_POOL_EXHAUSTED)<br/>→ lỗi màn 4, gợi ý chọn pool khác trong grant<br/>hoặc liên hệ Admin"]:::bad
    N2["Không có node Ready còn capacity<br/>→ picker rỗng kèm lý do từng node"]:::warn
  end
```

**Điểm cần review**

- Operation view cần **timeline theo Condition**, không phải progress bar chung: người bảo vệ sẽ hỏi "đang kẹt ở đâu", câu trả lời phải là "NetworkReady=False, reason IPPoolNotReady".
- Thông điệp lỗi hiển thị mã ổn định (ví dụ `DEDICATED_NODE_INSUFFICIENT_CAPACITY`) cộng câu tiếng Việt/Anh; mã này cũng xuất hiện trong test E2E nên UI và test cùng một nguồn (OpenAPI enum).

## 6. Khu Admin — luồng cấp phát cho một khách hàng

```mermaid
flowchart LR
  classDef a fill:#fde2e4,stroke:#c9184a,color:#000
  classDef e fill:#e2f0d9,stroke:#2d6a4f,color:#000

  A1["Organizations → duyệt yêu cầu của alice<br/>(grant điền sẵn từ onboarding policy)<br/>hoặc Create thẳng: slug, owner, grant"]:::a
  A2["Clusters → cluster-a<br/>shared pool shared-general (ratio 3.0, node của pool),<br/>node của Cluster, network pool"]:::a
  A3["Clusters → Give a node: worker-a-05 cho acme<br/>chỉ liệt kê node tự do (không thuộc pool, chưa dedicated);<br/>precondition: không còn Pod tenant → Operation node.grant"]:::a
  A4["Grants → acme/cluster-a<br/>maxWorkspaces 4, maxNamespacesTotal 10, storage 50Gi<br/>(CPU/RAM/Pod giữ nguyên: Dedicated không tính phần này), thêm network pool riêng nếu cần"]:::a
  A5["Org acme thấy node trong wizard<br/>khi node.grant Succeeded"]:::e
  A6["Operations → lọc Blocked<br/>→ Force (audit)"]:::a

  A1 --> A2 --> A3 --> A4 --> A5
  A4 --> A6
```

Ghi chú:

- Mode Dedicated không phải một ô trong grant: Organization dùng được Dedicated ngay khi có dedicated node trên Cluster (plan 5.2).
- Bước A3: muốn lấy một node của shared pool, Admin bớt node khỏi pool trên portal → drain → uncordon → cấp (`05-network-and-scheduling.md` mục 1). Nếu node chưa sạch, UI liệt kê Pod lạ (`DEDICATED_NODE_NOT_CLEAN`); drain làm bằng kubectl ngoài portal, vì platform không quản trị Node (non-goal).
- Bước A4: giảm grant dưới allocation hiện tại bị từ chối `CLUSTER_GRANT_BELOW_ALLOCATION`; UI hiện số tối thiểu ngay dưới từng ô (plan 5.5).
