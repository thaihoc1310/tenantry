# 06 — Security & Identity: trust boundary, OIDC client, hai lớp authorization

> Nguồn sự thật: `docs/PRODUCT-PLAN.md` v0.6, mục 8, 9, 10, 11, 12, 20. Tài liệu này vẽ lại để review đường đi của token và quyền.
> Cách đọc: trust boundary → OIDC client/audience → hai lớp authorization → revocation → credential nội bộ → ma trận quyền.

## 1. Trust boundaries

**Mục đích:** thấy rõ cái gì tin cái gì, credential nào đi qua biên nào.

```mermaid
flowchart TB
  classDef untrusted fill:#fde2e4,stroke:#c9184a,color:#000
  classDef semi fill:#fff3cd,stroke:#b58900,color:#000
  classDef trusted fill:#e2f0d9,stroke:#2d6a4f,color:#000

  subgraph Z0["Zone 0 — không tin: máy user, tenant workload"]
    B["Browser"]:::untrusted
    K["kubectl + kubelogin"]:::untrusted
    Pod["Tenant Pod<br/>trong acme-solar-*"]:::untrusted
  end

  subgraph Z1["Zone 1 — public edge, xác thực token"]
    Portal["Portal Web + Platform API<br/>cookie session, CSRF"]:::semi
    KC["Keycloak<br/>issuer id.tenantry.example"]:::semi
    CP["Capsule Proxy<br/>verify JWT, filter"]:::semi
    HL["Headlamp<br/>token user, không SA quyền"]:::semi
    Ing["ingress-nginx"]:::semi
  end

  subgraph Z2["Zone 2 — central plane nội bộ"]
    PG[("PostgreSQL")]:::trusted
    PW["Provisioning Worker<br/>SA chỉ CRD tenantry.io"]:::trusted
    GS["Group Sync Worker<br/>Keycloak admin client<br/>scope tối thiểu"]:::trusted
  end

  subgraph Z3["Zone 3 — cluster control"]
    APIS["kube-apiserver<br/>OIDC authn, RBAC,<br/>Capsule webhook, PodNodeSelector"]:::trusted
    WC["Workspace Controller<br/>quyền tạo Tenant/IPPool/RBAC/Node label"]:::trusted
  end

  B -->|"HTTPS, cookie"| Portal
  B -->|"OIDC redirect"| KC
  K -->|"OIDC PKCE, localhost callback"| KC
  K -->|"bearer JWT aud kubernetes"| CP
  B -->|"SSO"| HL
  HL -->|"bearer JWT aud headlamp-cluster-a"| CP
  Portal --> PG
  Portal -->|"token introspect/JWKS"| KC
  PW --> PG
  PW -->|"apply CR"| APIS
  GS --> PG
  GS -->|"Admin API"| KC
  CP -->|"forward JWT"| APIS
  WC --> APIS
  Pod -.->|"bị chặn: metadata, cross-ws,<br/>kube-apiserver trực tiếp không có token"| APIS
  B -->|"HTTPS app"| Ing
  Ing --> Pod
```

**Điểm cần review**

- **Portal không bao giờ cầm token có thể gọi Kubernetes.** Token `portal-api` không nằm trong audience kube-apiserver chấp nhận. Nếu API bị chiếm, kẻ tấn công chỉ có thể tạo CR (qua worker) — và CR bị controller giới hạn trong khuôn Tenant.
- Capsule Proxy là **điểm vào duy nhất** cho tenant tới kube-apiserver; kube-apiserver không expose public. Tenant Pod trong cluster vẫn có thể resolve `kubernetes.default` — cần NetworkPolicy chặn egress tới API server từ tenant namespace hay chấp nhận (RBAC vẫn bảo vệ)? Đề xuất: **không chặn** (nhiều app dùng ServiceAccount hợp lệ) nhưng ServiceAccount tenant không có RoleBinding mặc định.
- Zone 2 và Zone 3 ở MVP nằm cùng cluster; ranh giới là RBAC của ServiceAccount, không phải network. Phase 2 tách thành mạng khác.

## 2. OIDC clients và audience

**Mục đích:** một realm, ba client, ba audience — token nào dùng được ở đâu.

```mermaid
flowchart LR
  classDef c fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef t fill:#fff3cd,stroke:#b58900,color:#000
  classDef r fill:#e2f0d9,stroke:#2d6a4f,color:#000

  subgraph Realm["Keycloak realm tenantry"]
    U["User alice<br/>sub 1f3a…<br/>realm roles: (platform-admin)<br/>groups: org_x:group_y…"]
    C1["client portal<br/>confidential, PKCE<br/>redirect portal.tenantry.example/callback"]:::c
    C2["client headlamp-cluster-a<br/>confidential<br/>redirect console-cluster-a…/oidc-callback"]:::c
    C3["client kubectl<br/>public, PKCE<br/>redirect http://localhost:8000"]:::c
  end

  T1["access token<br/>aud portal-api<br/>claims: sub, email, roles<br/>KHÔNG có groups"]:::t
  T2["id/access token<br/>aud headlamp-cluster-a<br/>claims: sub, groups"]:::t
  T3["id token<br/>aud kubernetes<br/>claims: sub, groups"]:::t

  R1["Platform API<br/>chấp nhận aud portal-api"]:::r
  R2["kube-apiserver cluster-a<br/>structured authn chấp nhận<br/>aud kubernetes VÀ headlamp-cluster-a<br/>username oidc:sub<br/>groups oidc:…"]:::r

  U --> C1 --> T1 --> R1
  U --> C2 --> T2 --> R2
  U --> C3 --> T3 --> R2
  T1 -.->|"bị từ chối"| R2
  T3 -.->|"bị từ chối"| R1
```

Bảng quyết định:

| Client | Kiểu | Group mapper | Token TTL đề xuất | Ghi chú |
|---|---|---|---|---|
| `portal` | confidential + PKCE, BFF | không | access 15 phút, session cookie 8 giờ, refresh trong BFF | groups không cần vì API tra DB |
| `headlamp-cluster-a` | confidential | có | 5 phút, refresh có | mỗi Cluster mới thêm một client theo mẫu |
| `kubectl` | public + PKCE | có | 5 phút, refresh có (kubelogin cache) | dùng chung cho mọi Cluster; Cluster phân biệt bằng endpoint Proxy |

**Điểm cần review**

- Group claim chỉ nằm trong token của `headlamp-*` và `kubectl`. User có nhiều group → header JWT lớn; cần test kích thước với ~20 group (ingress-nginx `large-client-header-buffers`).
- Có nên cho `headlamp-cluster-a` dùng luôn audience `kubernetes` để bớt một audience? Plan chọn tách để có thể thu hồi/rotate secret Headlamp độc lập với kubectl. Giữ.
- Platform Admin có role `platform-admin` trong **mọi** token — không sao, vì Kubernetes không map realm role sang group; Admin muốn kubectl vào Tenant vẫn phải qua group binding như người khác (hoặc dùng kubeconfig admin ngoài platform).

## 3. Hai lớp authorization

**Mục đích:** một hành động của user đi qua lớp nào, ai quyết định.

```mermaid
flowchart TB
  classDef p fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef k fill:#e2f0d9,stroke:#2d6a4f,color:#000
  classDef q fill:#fff3cd,stroke:#b58900,color:#000

  Req["Yêu cầu của user"]:::q
  Which{"Đích là gì?"}:::q

  subgraph L1["Lớp 1 — Platform authorization (Platform API, dữ liệu trong PostgreSQL)"]
    A1["Xác thực session cookie → user id"]:::p
    A2{"Endpoint admin?"}:::p
    A3["Check realm role platform-admin trong token"]:::p
    A4["Resolve orgId từ resource,<br/>KHÔNG tin orgId client gửi"]:::p
    A5["Check organization_members(role)"]:::p
    A6["Rule theo hành động:<br/>Owner/Admin: workspace, group, member<br/>Namespace: Owner/Admin hoặc TenantOwner-group<br/>Transfer/Delete org: chỉ Owner"]:::p
  end

  subgraph L2["Lớp 2 — Kubernetes authorization (kube-apiserver + Capsule)"]
    B1["JWT → username oidc:sub, groups oidc:org:group"]:::k
    B2["Capsule Proxy: filter list Namespace/Node/…<br/>theo Tenant user thuộc"]:::k
    B3["RBAC: Tenant owner group → Capsule owner<br/>Developer/Viewer group → RoleBinding ClusterRole tenantry:*"]:::k
    B4["Capsule webhook: quota, nodeSelector,<br/>registry, hostname, IPPool annotation, PSS"]:::k
    B5["PodNodeSelector + taint:<br/>đúng pool/node"]:::k
  end

  Req --> Which
  Which -->|"portal / API"| A1 --> A2
  A2 -->|"có"| A3 --> A6
  A2 -->|"không"| A4 --> A5 --> A6
  Which -->|"kubectl / Headlamp"| B1 --> B2 --> B3 --> B4 --> B5
```

Nguyên tắc: **lớp 1 quyết định cấu hình, lớp 2 quyết định runtime**. Portal đổi binding → lớp 1 ghi DB → outbox → controller đổi RoleBinding + Group Sync đổi Keycloak → lớp 2 áp dụng ở token mới. Không có đường nào để portal trực tiếp "cho phép" một request Kubernetes.

**Điểm cần review**

- Trường hợp hai lớp lệch: OrgOwner **không** nằm trong group nào bound vào Workspace → có toàn quyền cấu hình trên portal nhưng `kubectl` bị từ chối. Đây là hành vi mong muốn (plan 10.1) nhưng UI phải giải thích ("bạn chưa có quyền Kubernetes ở Workspace này, thêm mình vào group Owner").
- Namespace UI là chỗ duy nhất lớp 1 **đọc** binding Kubernetes (TenantOwner-group) để quyết định quyền portal. Cần test riêng ma trận này.

## 4. Revocation contract

**Mục đích:** minh bạch độ trễ mất quyền, để không hứa "revoke ngay".

```mermaid
sequenceDiagram
  autonumber
  participant O as OrgOwner
  participant API as Platform API
  participant DB as PostgreSQL
  participant GS as Group Sync Worker
  participant KC as Keycloak
  participant J as Joe kubectl
  participant CP as Capsule Proxy

  O->>API: remove Joe khỏi group backend
  API->>DB: delete group_members + outbox GroupSync
  API-->>O: 202 operation
  GS->>DB: lấy outbox
  GS->>KC: remove user from group
  GS->>KC: revoke sessions của Joe
  GS->>DB: sync_status Synced
  Note over J,CP: token cũ của Joe còn hạn tới TTL ~5 phút
  J->>CP: request với token cũ
  CP-->>J: 200 vẫn được
  Note over J: token hết hạn
  J->>KC: refresh
  KC-->>J: từ chối, session đã revoke
  J->>KC: login lại
  KC-->>J: token mới, groups KHÔNG còn backend
  J->>CP: request
  CP-->>J: 403 Forbidden
```

Cam kết ghi vào UI/audit: *"Quyền Kubernetes bị thu hồi chậm nhất sau `token TTL` (mặc định 5 phút). Phiên portal bị thu hồi ngay."*

**Điểm cần review**

- Có cần chặn sớm hơn ở Capsule Proxy (Proxy gọi Keycloak introspection mỗi request)? Trade-off: thêm độ trễ và phụ thuộc Keycloak trên đường dữ liệu. Plan quyết định **không**; giữ TTL ngắn.
- Portal session: khi user bị remove khỏi org, request tiếp theo đọc `organization_members` và mất quyền **ngay** — không phụ thuộc token. Ghi rõ điểm khác biệt này trong tài liệu người dùng.

## 5. Credential nội bộ và bí mật

| Credential | Chủ | Quyền | Lưu ở | Rotate |
|---|---|---|---|---|
| SA `tenantry-provisioner` | Provisioning Worker | `platformworkspaces.tenantry.io` get/list/watch/create/patch + status | in-cluster SA (MVP); Phase 2: thay bằng Cluster Agent | tự động theo SA token projection |
| SA `tenantry-controller` | Workspace Controller | Tenant, ResourcePool, IPPool, NetworkPolicy, RoleBinding, Namespace, Certificate, Node patch labels/taints (chỉ label prefix `tenantry.io/` — enforce bằng test, K8s RBAC không lọc theo label) | in-cluster SA | theo SA |
| Keycloak client `tenantry-admin` | Group Sync Worker | service account với role `manage-users`, `query-groups`, `manage-realm` cho group thôi nếu tách được | Kubernetes Secret | thủ công, ghi runbook |
| Client secret `portal`, `headlamp-cluster-a` | API, Headlamp | OIDC | Kubernetes Secret | thủ công |
| PostgreSQL user `tenantry` | API + workers | DB `tenantry` | Secret | thủ công |
| CA lab `capsule-lab-ca` | cert-manager ClusterIssuer | ký mọi cert nội bộ + wildcard tenant | Secret trong `cert-manager` | 10 năm (đã có) |

Không có credential nào của user được lưu: kubeconfig không chứa token; portal không lưu password; audit không lưu token.

**Điểm cần review**

- Controller cần quyền patch Node — đây là quyền rộng nhất trong hệ thống. Có thể thu hẹp bằng ValidatingAdmissionPolicy chỉ cho SA controller đổi label/taint prefix `tenantry.io/`. Đề xuất thêm vào M8 như hardening.
- Keycloak Admin scope: kiểm tra thật xem role tối thiểu để tạo group + add member + revoke session là gì; nếu buộc phải `manage-users` thì ghi rõ trong ADR.

## 6. Ma trận quyền (rút gọn để review)

| Hành động | PlatformAdmin | OrgOwner | OrgAdmin | OrgMember + TenantOwner group | OrgMember + Developer | OrgMember + Viewer |
|---|---|---|---|---|---|---|
| Đăng ký Cluster, tạo Org, ClusterGrant, node, pool | ✅ | — | — | — | — | — |
| Đổi display name Org | ✅ | ✅ | ✅ | — | — | — |
| Transfer ownership, xóa Org (không personal) | — | ✅ | — | — | — | — |
| Mời, remove member, đổi role (trừ Owner) | — | ✅ | ✅ | — | — | — |
| Tạo/xóa group, thêm member | — | ✅ | ✅ | — | — | — |
| Tạo/resize/cordon/delete Workspace | — | ✅ | ✅ | — | — | — |
| Đổi group binding của Workspace | — | ✅ | ✅ | — | — | — |
| Namespace create/delete trên portal | — | ✅ | ✅ | ✅ | xem | xem |
| Xem Workspace trên portal | ✅ (admin view) | ✅ | ✅ | ✅ | ✅ | ✅ |
| Tải kubeconfig / mở Headlamp | — | ✅* | ✅* | ✅ | ✅ | ✅ |
| kubectl: tạo Namespace trong Tenant | — | * | * | ✅ | ❌ | ❌ |
| kubectl: CRUD workload | — | * | * | ✅ | ✅ | ❌ |
| kubectl: đọc Secret, exec | — | * | * | ✅ | theo policy | ❌ |
| kubectl: sửa label/annotation isolation, nodeSelector | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Retry operation | ✅ mọi org | ✅ | ✅ | — | — | — |
| Force finalizer khi delete kẹt | ✅ | ❌ | ❌ | — | — | — |

`*` = chỉ khi bản thân nằm trong một group được bind vào Workspace; platform role không tự sinh quyền Kubernetes.

**Điểm cần review**

- Hàng "Tải kubeconfig": Owner không có group binding tải được kubeconfig nhưng dùng sẽ bị 403 — nên ẩn nút hay hiện kèm cảnh báo? Đề xuất: hiện kèm cảnh báo, vì kubeconfig là per-user và họ có thể tự thêm mình vào group ngay sau đó.
- Developer có `exec`/đọc Secret hay không là quyết định policy chưa chốt (plan 10.3 "tùy policy"). Đề xuất mặc định: Developer **được** exec/log, **không** đọc Secret; có thể mở bằng policy profile sau.
