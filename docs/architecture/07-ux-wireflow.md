# 07 — Portal UX: information architecture và wireflow

> Góc nhìn, không định nghĩa gì mới: IA và yêu cầu ở `PRODUCT-PLAN.md` mục 19, 22, 28; route thật ở `IMPLEMENTATION-PLAN.md` mục 6. Đây là sơ đồ luồng màn hình để review trải nghiệm, không phải thiết kế giao diện.
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
  Transfer["/ownership-transfers/:token<br/>xác nhận nhận Owner"]:::pub

  Switch["Organization switcher<br/>/orgs/:orgSlug"]:::org
  Over["Overview<br/>quota theo ClusterGrant, Workspace count,<br/>pool/node remaining, network availability"]:::org
  WSL["Workspaces<br/>list + Create"]:::org
  Mem["Members & invitations"]:::org
  Grp["Groups"]:::org
  Set["Settings<br/>display name, transfer ownership,<br/>leave / delete Org"]:::org
  Cl["Granted Clusters<br/>quota, shared pools,<br/>dedicated nodes, network pools"]:::org
  Aud["Audit"]:::org

  WSD["Workspace detail<br/>/orgs/:org/workspaces/:ws"]:::ws
  W1["Overview: state, conditions,<br/>operations timeline"]:::ws
  W2["Resources / quota"]:::ws
  W3["Compute: pool hoặc node,<br/>reservation"]:::ws
  W4["Network / CIDR / IP usage"]:::ws
  W5["Members & role bindings"]:::ws
  W6["Namespaces: list/create/delete/usage"]:::ws
  W7["Access: kubeconfig,<br/>Open Web Console, hostname zone"]:::ws
  W8["Danger zone: cordon, delete"]:::ws

  Adm["/admin (chỉ realm role platform-admin)"]:::adm
  A1["Organizations: duyệt/từ chối yêu cầu,<br/>tạo thẳng Org + Owner + grant"]:::adm
  A2["Clusters: registry, health, endpoints"]:::adm
  A3["Onboarding policy"]:::adm
  A4["ClusterGrants per Org"]:::adm
  A5["Shared pools / Dedicated nodes"]:::adm
  A6["Network pools"]:::adm
  A7["Operations (retry / force)"]:::adm
  A8["Platform health"]:::adm

  Login --> Switch
  Login --> Empty --> Switch
  Invite --> Switch
  Transfer --> Switch
  Switch --> Over & WSL & Mem & Grp & Set & Cl & Aud
  WSL --> WSD
  WSD --> W1 & W2 & W3 & W4 & W5 & W6 & W7 & W8
  Switch -.->|"nếu có role"| Adm
  Adm --> A1 & A2 & A3 & A4 & A5 & A6 & A7 & A8
```

**Điểm cần review**

- URL dùng **slug** (`/orgs/acme/workspaces/solar`) vì slug immutable — đổi display name không làm hỏng bookmark.
- OrgMember chỉ có binding vào một Workspace vẫn thấy Overview của Org? Đề xuất: thấy Overview rút gọn (Workspace mình có quyền), không thấy quota/usage toàn Org, không thấy Members/Groups/Settings/Clusters.

## 2. Wireflow — user mới (Alice)

**Mục đích:** từ chưa có tài khoản tới Workspace Ready; Admin chỉ can thiệp đúng một lần, ở bước duyệt yêu cầu tạo Organization.

```mermaid
flowchart LR
  classDef s fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef d fill:#fff3cd,stroke:#b58900,color:#000
  classDef e fill:#e2f0d9,stroke:#2d6a4f,color:#000

  S1["Landing<br/>nút Sign up / Login"]:::s
  S2["Keycloak Register<br/>email verify"]:::s
  S3["Callback → tạo user record<br/>màn trống: xin tạo Organization"]:::s
  S3a["Gửi yêu cầu: slug 'acme'<br/>preview tên Kubernetes acme-*"]:::s
  S3b["Chờ duyệt<br/>(từ chối → hiện lý do, gửi lại)"]:::d
  S4["Admin duyệt → Overview Org acme<br/>banner: bạn có quota Shared<br/>2 CPU / 4 GiB / 2 Workspace"]:::e
  S5["Workspaces → Create"]:::s
  W["Wizard 12 bước<br/>xem mục 4"]:::d
  S6["Operation view<br/>Validating → Reserving →<br/>Provisioning → Ready"]:::s
  S7["Workspace Overview<br/>conditions xanh,<br/>nút Kubeconfig + Web Console bật"]:::e
  S8["Members → Invite Joe<br/>copy link"]:::s
  S9["Groups → developers<br/>add Joe"]:::s
  S10["Workspace → Members & bindings<br/>developers = Developer"]:::s

  S1 --> S2 --> S3 --> S3a --> S3b --> S4 --> S5 --> W --> S6 --> S7
  S4 --> S8 --> S9 --> S10
```

**Điểm cần review**

- Callback (S3) chỉ upsert user và tạo phiên nên nhanh và idempotent; không có gì ở đây phụ thuộc Cluster.
- Slug do user tự chọn ở S3a và **không sửa được** sau đó; form phải hiện ngay tên Kubernetes sẽ sinh ra (`acme-<workspace>`) và báo `SLUG_TAKEN` tại chỗ.
- S3b phải nói rõ đang chờ ai và cho thấy lý do khi bị từ chối; một user chỉ có một yêu cầu chờ duyệt tại một thời điểm.

## 3. Wireflow — người được mời (Joe) và người xem (Bob)

```mermaid
flowchart LR
  classDef s fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef e fill:#e2f0d9,stroke:#2d6a4f,color:#000
  classDef x fill:#fde2e4,stroke:#c9184a,color:#000

  J1["Mở invite link"]:::s
  J2["Login/Register Keycloak"]:::s
  J3{"email verified<br/>khớp invite?"}:::s
  J4["Accept → membership OrgMember<br/>+ vào group đã chọn"]:::e
  J5["Lỗi: email không khớp,<br/>gợi ý login đúng tài khoản"]:::x
  J6["Org acme → Workspaces<br/>chỉ thấy Solar"]:::s
  J7["Solar → Access<br/>Download kubeconfig<br/>hướng dẫn cài kubelogin"]:::s
  J8["Terminal: kubectl get ns<br/>→ browser SSO → OK"]:::e
  J9["Open Web Console<br/>→ Headlamp SSO"]:::e
  J10["Namespaces tab: chỉ xem<br/>(Developer)"]:::s

  J1 --> J2 --> J3
  J3 -->|"có"| J4 --> J6 --> J7 --> J8
  J3 -->|"không"| J5
  J6 --> J9
  J6 --> J10
```

Bob (Viewer) đi cùng đường, khác ở chỗ Headlamp/kubectl chỉ đọc; thử `kubectl delete` → 403 kèm thông điệp của RBAC.

**Điểm cần review**

- Khi Joe chưa cài kubelogin, lỗi xảy ra ở terminal, không ở portal. Trang Access nên có block "Kiểm tra: `kubectl oidc-login --version`" và link cài.
- Joe thuộc nhiều Workspace cùng Cluster sẽ thấy tất cả Namespace được phép trong một `kubectl get ns` — plan nói đây không phải lỗi; UI Access nên ghi một dòng để tránh thắc mắc khi demo.

## 4. Wizard tạo Workspace

**Mục đích:** 12 bước của plan 19.1, chỗ nào gọi API validate, chỗ nào auto-select.

```mermaid
flowchart TB
  classDef st fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef auto fill:#e9ecef,stroke:#6c757d,color:#000
  classDef api fill:#fff3cd,stroke:#b58900,color:#000
  classDef err fill:#fde2e4,stroke:#c9184a,color:#000

  B1["1. Name + slug preview<br/>'acme-solar' sẽ là tên Tenant"]:::st
  B2["2. Cluster<br/>auto-select nếu 1 grant"]:::auto
  B3["3. Remaining quota của grant<br/>(read-only)"]:::auto
  B4["4. Compute mode<br/>Shared | Dedicated (disabled nếu grant không cho)"]:::st
  B5s["5a. Shared pool<br/>auto nếu 1 pool; hiện allocated/ratio×reservable"]:::auto
  B5d["5b. Dedicated node picker<br/>card: health, capacity state,<br/>remaining CPU/RAM/Pod, Workspaces đang dùng"]:::st
  B6["6. Resources<br/>Shared: requests + limits theo ratio<br/>Dedicated: requests = limits, khóa limits<br/>live check ≤ remaining"]:::st
  B7["7. Network<br/>mặc định Auto /24<br/>Advanced: pool, prefix, requested CIDR"]:::st
  B8["8. Namespace quota"]:::st
  B9["9. Owner groups (≥1)"]:::st
  B10["10. Developer / Viewer groups<br/>cảnh báo group chưa Synced"]:::st
  V["POST workspaces:validate<br/>trả lỗi theo mã, gắn vào bước"]:::api
  B11["11. Review: Cluster, compute, node,<br/>network, resources, access,<br/>hostname zone *.acme-solar.apps…"]:::st
  B12["12. Submit → 202 operation<br/>→ Operation view SSE"]:::api
  E["Lỗi: CLUSTER_GRANT_CPU_QUOTA_EXCEEDED,<br/>DEDICATED_NODE_INSUFFICIENT_CAPACITY,<br/>CIDR_OVERLAP, LAST_OWNER_BINDING…"]:::err

  B1 --> B2 --> B3 --> B4
  B4 -->|"Shared"| B5s --> B6
  B4 -->|"Dedicated"| B5d --> B6
  B6 --> B7 --> B8 --> B9 --> B10 --> V
  V -->|"ok"| B11 --> B12
  V -->|"lỗi"| E --> B6
```

Nội dung giải thích bắt buộc trong wizard (plan 19.1): Cluster không đổi sau khi tạo; Shared vs Dedicated khác ở placement; node thuộc Org nhưng chia capacity; Dedicated dùng Guaranteed QoS; allocation trừ vào grant; CIDR không đổi tùy tiện; Owner tạo được Namespace, Developer không; không hỗ trợ cluster-scoped resource.

**Điểm cần review**

- Với MVP một Cluster, một pool: bước 2, 3, 5a gần như rỗng → gộp thành một màn "Cluster & capacity" read-only để wizard thực tế còn ~8 bước. Data model không đổi, chỉ UI.
- `:validate` gọi khi nào? Đề xuất: gọi **mỗi khi rời bước 6/7/9** (debounce) để lỗi hiện đúng chỗ, và gọi lần cuối trước Submit. Submit vẫn tự validate lại trong transaction.
- Bước 1 hiện slug preview là quan trọng vì slug immutable; đồng thời hiện luôn hostname zone để user hiểu tên đó xuất hiện ở đâu.

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
    O3["Không có compute option hợp lệ<br/>→ giải thích grant chỉ cho mode X"]:::warn
  end
  subgraph WS["Cấp Workspace"]
    W1["Provisioning lâu > 2 phút<br/>→ hiện condition đang chờ"]:::warn
    W2["Failed<br/>→ condition False + error code + nút Retry"]:::bad
    W3["Degraded (node Overcommitted)<br/>→ badge + 'Admin đang xử lý', workload vẫn chạy"]:::warn
    W4["Cordoned<br/>→ chỉ đọc, nút Uncordon"]:::warn
    W5["Delete Blocked bởi PVC/finalizer<br/>→ liệt kê object, 'chờ hoặc liên hệ Admin'"]:::bad
  end
  subgraph Acc["Cấp Access"]
    A1["Group chưa Synced<br/>→ badge trên binding, cảnh báo token chưa có group"]:::warn
    A2["Vừa bị gỡ khỏi group<br/>→ 'quyền Kubernetes hết hạn sau ≤5 phút,<br/>người bị gỡ phải đăng nhập lại'"]:::warn
    A3["Owner không thuộc group nào<br/>→ kubeconfig kèm cảnh báo 403"]:::warn
  end
  subgraph Net["Cấp Network/Node"]
    N1["NetworkPool hết CIDR<br/>→ lỗi bước 7, gợi ý prefix nhỏ hơn"]:::bad
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
  A2["Clusters → cluster-a<br/>Shared pools: shared-general ratio 3.0<br/>Dedicated nodes: worker-a-05"]:::a
  A3["Dedicated nodes → Grant to Org acme<br/>precondition: không còn Pod tenant lạ"]:::a
  A4["ClusterGrants → acme/cluster-a<br/>tăng CPU/RAM, thêm mode Dedicated,<br/>gắn nodegrant, network pool riêng"]:::a
  A5["Org acme thấy ngay quota mới<br/>và node trong wizard"]:::e
  A6["Operations → lọc Blocked<br/>→ Force (audit)"]:::a

  A1 --> A2 --> A3 --> A4 --> A5
  A4 --> A6
```

**Điểm cần review**

- Bước A3: nếu node chưa sạch, UI phải liệt kê Pod lạ và hướng dẫn cordon/drain — Admin làm bằng kubectl ngoài portal. Chấp nhận, vì platform không quản trị Node (non-goal).
- Giảm grant dưới allocation hiện tại bị từ chối: UI hiển thị allocation hiện tại ngay cạnh ô nhập để Admin không phải thử-sai.
