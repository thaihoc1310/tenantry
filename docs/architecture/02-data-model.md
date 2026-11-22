# 02 — Data model: bảng, constraint, invariant

> Nơi định nghĩa cho bảng, cột, constraint và thứ tự lock. DDL thật nằm ở `backend/migrations/`; bảng nào đã có migration thì file này phải khớp migration. Quy tắc nghiệp vụ đứng sau từng invariant ở `PRODUCT-PLAN.md` mục 4, 5.
> PostgreSQL là source of truth cho product state; Kubernetes chỉ giữ runtime state qua CR `PlatformWorkspace`.

Quy ước chung cho mọi bảng:

- **ID** là `text` chứa ULID có tiền tố (`org-`, `ws-`, `grp-`, `op-`, `netalloc-`, `nodegrant-`…); riêng `clusters.id` là tên do Admin đặt (`cluster-a`), `outbox_events.id` và `audit_events.id` là `bigint` tăng dần.
- **Thời gian** là `timestamptz`; bảng nào sửa được thì có `created_at`, `updated_at` (không vẽ lại trong ERD).
- **Lượng tài nguyên** lưu số nguyên theo đơn vị gốc: CPU là milliCPU, memory/storage là byte, cột `bigint`. API nhận và trả Kubernetes quantity (`"4"`, `8Gi`), đổi ở biên. Nhờ vậy `SUM` trong SQL là chính xác và không phải parse chuỗi dưới row lock.
- **Trạng thái** là `text` có `CHECK`; tập giá trị và cạnh chuyển định nghĩa ở `04-state-machines.md`.

## 1. ERD tổng thể

```mermaid
erDiagram
  users ||--o{ organization_members : "là member"
  users ||--o{ organizations : "owner_user_id"
  users ||--o{ sessions : ""
  users ||--o{ idempotency_keys : ""
  organizations ||--o{ organization_members : ""
  organizations ||--o{ invitations : ""
  organizations ||--o{ ownership_transfers : ""
  organizations ||--o{ groups : ""
  groups ||--o{ group_members : ""
  users ||--o{ group_members : ""
  organizations ||--|| organization_entitlements : ""
  organizations ||--o{ organization_cluster_grants : ""
  clusters ||--o{ organization_cluster_grants : ""
  clusters ||--o{ shared_compute_pools : ""
  organization_cluster_grants }o--o{ shared_compute_pools : "cluster_grant_shared_pools"
  clusters ||--o{ dedicated_node_grants : ""
  organizations ||--o{ dedicated_node_grants : ""
  clusters ||--o{ network_pools : ""
  organizations o|--o{ network_pools : "optional"
  organization_cluster_grants }o--o{ network_pools : "cluster_grant_network_pools"
  organizations ||--o{ workspaces : ""
  clusters ||--o{ workspaces : ""
  organization_cluster_grants ||--o{ workspaces : ""
  shared_compute_pools o|--o{ workspaces : "Shared"
  workspaces ||--o| workspace_node_reservations : "Dedicated"
  dedicated_node_grants ||--o{ workspace_node_reservations : ""
  workspaces ||--|| network_allocations : ""
  network_pools ||--o{ network_allocations : ""
  workspaces ||--o{ workspace_group_bindings : ""
  groups ||--o{ workspace_group_bindings : ""
  workspaces ||--o{ workspace_namespaces : ""
  workspaces ||--o{ workspace_routes : ""
  organizations ||--o{ operations : ""
  operations ||--o{ outbox_events : ""
  clusters ||--o{ outbox_events : "target"
  organizations ||--o{ audit_events : ""
```

**Điểm cần review**

- `workspaces → organization_cluster_grants` là quan hệ **bắt buộc**: Workspace luôn thuộc đúng một grant (org × cluster). Đây là chỗ enforce "Cluster phải được cấp cho Organization".
- Reservation quota của ClusterGrant **không có bảng riêng**: nó là `SUM` các cột tài nguyên của `workspaces` trong grant, tính dưới row lock của grant. NodeReservation có bảng riêng vì có state và lifecycle riêng.
- `cluster_agents` (cluster_id, identity fingerprint, version, status, last_seen) chỉ tạo ở Phase 2.

## 2. Vùng Identity, phiên và Organization

```mermaid
erDiagram
  users {
    text id PK
    text issuer
    text subject
    text email
    text display_name
    text status "Active | Disabled"
  }
  sessions {
    bytea id_hash PK "sha256 của cookie"
    text user_id FK
    text[] realm_roles "snapshot ở lần refresh gần nhất"
    bytea refresh_token_enc "AES-256-GCM"
    timestamptz access_expires_at
    timestamptz expires_at "hạn của refresh token"
  }
  idempotency_keys {
    text user_id PK
    text key PK
    text method
    text path
    bytea request_hash "sha256 của body"
    int response_status "0 khi mới giành key, 2xx khi đã commit"
    text response_location "header Location của response, nullable"
    json response_body "json để giữ nguyên từng byte, nullable"
    timestamptz created_at
  }
  organizations {
    text id PK
    text slug "immutable"
    text display_name
    text description
    text owner_user_id FK
    text status "Pending | Active | Rejected | Deleted"
    text review_note
  }
  organization_members {
    text org_id PK
    text user_id PK
    text role "OrgOwner | OrgAdmin | OrgMember"
  }
  invitations {
    text id PK
    text org_id FK
    text email
    text token_hash
    text role
    text[] group_ids "optional"
    timestamptz expires_at
    text accepted_by FK
    text state
  }
  ownership_transfers {
    text id PK
    text org_id FK
    text from_user_id FK
    text to_user_id FK
    text token_hash
    timestamptz expires_at
    text state
  }
  groups {
    text id PK
    text org_id FK
    text display_name
    text machine_name "org_<id>:group_<id>"
    text keycloak_group_id
    timestamptz deleted_at "tombstone"
    text sync_status "Pending | Synced | Error"
    int sync_attempts
    timestamptz next_sync_at
    text last_error
  }
  group_members {
    text group_id PK
    text user_id PK
    timestamptz deleted_at "tombstone"
    text sync_status "Pending | Synced | Error"
    int sync_attempts
    timestamptz next_sync_at
    text last_error
  }
  users ||--o{ sessions : ""
  users ||--o{ idempotency_keys : ""
  users ||--o{ organization_members : ""
  organizations ||--o{ organization_members : ""
  organizations ||--o{ invitations : ""
  organizations ||--o{ ownership_transfers : ""
  organizations ||--o{ groups : ""
  groups ||--o{ group_members : ""
  users ||--o{ group_members : ""
```

| Invariant | Enforce bằng |
|---|---|
| Một account ↔ một identity Keycloak | `UNIQUE (issuer, subject)` |
| Slug Organization unique, yêu cầu bị từ chối nhả slug | `UNIQUE (slug) WHERE status <> 'Rejected'` |
| Mỗi user tối đa một yêu cầu `Pending` | `UNIQUE (owner_user_id) WHERE status = 'Pending'` |
| Đúng một OrgOwner mỗi Organization | `organizations.owner_user_id NOT NULL` (ít nhất một) + `UNIQUE (org_id) ON organization_members WHERE role = 'OrgOwner'` (nhiều nhất một) |
| Owner luôn là member | service: đổi `owner_user_id` và role hai bên trong một transaction |
| Group display name unique trong Organization (không tính group đã xóa), machine name unique toàn cục | `UNIQUE (org_id, display_name) WHERE deleted_at IS NULL`; `UNIQUE (machine_name)` |
| Không xóa group còn binding | service chặn trước khi đặt `deleted_at`; `FK ... ON DELETE RESTRICT` từ `workspace_group_bindings` là lưới an toàn |
| Member của group phải là member của Organization sở hữu group | service kiểm khi thêm; **không** FK sang `organization_members` (xem dưới) |
| Tối đa một transfer đang chờ mỗi Organization | `UNIQUE (org_id) ON ownership_transfers WHERE state = 'Proposed'` |
| Cùng `Idempotency-Key` không tạo hai lần | `PRIMARY KEY (user_id, key)` trên `idempotency_keys` |

**Xóa là xóa thật, trừ thứ còn phải đồng bộ ra ngoài:**

- Remove member khỏi Organization xóa hẳn row `organization_members`; lịch sử nằm ở `audit_events`.
- `groups` và `group_members` dùng **tombstone**: thao tác xóa đặt `deleted_at = now()` và `sync_status = 'Pending'` trong cùng transaction; Group Sync Worker gỡ trên Keycloak xong mới xóa hẳn row. Row có `deleted_at` không còn hiệu lực với mọi câu hỏi phân quyền và danh sách — mọi query nghiệp vụ lọc `deleted_at IS NULL`. Thêm lại một member đang là tombstone chỉ cần đặt `deleted_at = NULL`, `sync_status = 'Pending'`.
- Vì tombstone phải sống lâu hơn membership, `group_members` **không** có FK sang `organization_members`: remove member khỏi Organization xóa row membership ngay, đồng thời đặt tombstone cho mọi `group_members` của user trong Organization đó để worker còn việc mà làm.

**Phiên và idempotency:**

- `sessions`: cơ chế ở `06-security-and-identity.md` mục 3. Index trên `user_id` (thu hồi mọi phiên của một user) và `expires_at` (dọn rác).
- `idempotency_keys`: cơ chế ở `IMPLEMENTATION-PLAN.md` mục 3.4. Row được giành ở đầu transaction của request và điền response trước khi commit, cùng transaction với thay đổi nghiệp vụ; giữ 24 giờ rồi vòng `housekeeping` xóa.

## 3. Vùng Cluster & Entitlement

```mermaid
erDiagram
  clusters {
    text id PK "cluster-a"
    text display_name
    text status "Registered|Ready|Maintenance|Unavailable"
    text proxy_endpoint
    text console_url
    text apps_domain "mvp.tenantry.site"
    jsonb capabilities
    timestamptz last_health_check
  }
  organization_entitlements {
    text org_id PK
    int max_members
    int max_groups
    int max_workspaces_total
  }
  onboarding_policies {
    text id PK
    text default_cluster_id FK
    jsonb entitlement_template
    jsonb grant_template
    bool active
  }
  organization_cluster_grants {
    text id PK
    text org_id FK
    text cluster_id FK
    bool is_default
    int max_workspaces
    int max_namespaces_total
    bigint requests_cpu_milli
    bigint requests_memory_bytes
    bigint limits_cpu_milli
    bigint limits_memory_bytes
    int pods
    bigint requests_storage_bytes
    text[] allowed_storage_classes
  }
  shared_compute_pools {
    text id PK
    text cluster_id FK
    text name
    numeric allocation_overcommit_ratio
    jsonb system_reserve
    jsonb capacity_snapshot
  }
  cluster_grant_shared_pools {
    text grant_id FK
    text pool_id FK
  }
  cluster_nodes {
    text cluster_id PK
    text node_name PK
    text kind "ControlPlane|System|Worker"
    bool ready
    bool schedulable
    bigint allocatable_cpu_milli
    bigint allocatable_memory_bytes
    int allocatable_pods
    text shared_pool_id FK "nullable: pool Admin đã chọn"
    timestamptz observed_at
  }
  dedicated_node_grants {
    text id PK
    text org_id FK
    text cluster_id FK
    text node_name
    bigint allocatable_cpu_milli
    bigint allocatable_memory_bytes
    int allocatable_pods
    bigint reserve_cpu_milli
    bigint reserve_memory_bytes
    int reserve_pods
    text node_state "Ready|NotReady|Cordoned"
    text capacity_state "Available|PartiallyAllocated|FullyAllocated|Overcommitted"
  }
  network_pools {
    text id PK
    text cluster_id FK
    text org_id FK "nullable = pool chung"
    cidr[] cidrs
    int default_prefix_length
    int[] allowed_prefix_lengths
  }
  cluster_grant_network_pools {
    text grant_id FK
    text pool_id FK
  }
  clusters ||--o{ organization_cluster_grants : ""
  clusters ||--o{ shared_compute_pools : ""
  clusters ||--o{ dedicated_node_grants : ""
  clusters ||--o{ network_pools : ""
  clusters ||--o{ cluster_nodes : ""
  shared_compute_pools ||--o{ cluster_nodes : "node của pool"
  organization_cluster_grants ||--o{ cluster_grant_shared_pools : ""
  shared_compute_pools ||--o{ cluster_grant_shared_pools : ""
  organization_cluster_grants ||--o{ cluster_grant_network_pools : ""
  network_pools ||--o{ cluster_grant_network_pools : ""
  onboarding_policies ||--o{ organization_cluster_grants : "điền sẵn khi duyệt"
```

| Invariant | Enforce bằng |
|---|---|
| Một org có tối đa một default grant | `UNIQUE (org_id) WHERE is_default` |
| Một org × cluster có tối đa một grant | `UNIQUE (org_id, cluster_id)` |
| Node chỉ dedicated cho một org | `UNIQUE (cluster_id, node_name)` |
| Pool/NodeGrant/NetworkPool trong grant phải cùng `cluster_id` với grant | service + integration test |
| Pool riêng chỉ vào grant của đúng org | service khi Admin gán |
| Không giảm grant dưới tổng allocation hiện tại | transaction: lock grant, `SUM` cột tài nguyên của `workspaces` so với giá trị mới; CPU/RAM/Pod chỉ cộng Workspace Shared (plan 5.5) |
| Node thuộc tối đa một pool | một cột `cluster_nodes.shared_pool_id` |
| Node không vừa thuộc pool vừa dedicated | service: lock row `cluster_nodes` của node trước khi thêm vào pool hoặc tạo `dedicated_node_grants`; operator từ chối lần nữa trên Cluster |
| Compute mode dùng được | suy ra: Shared khi grant có `cluster_grant_shared_pools`, Dedicated khi có `dedicated_node_grants` của org trên cluster; không có cột riêng |
| Không revoke node grant còn reservation chưa `Released` | `FK ON DELETE RESTRICT` từ `workspace_node_reservations` + check state |

**Điểm cần review**

- `capacity_snapshot` của shared pool và `allocatable_*` của dedicated node là **dữ liệu chép từ Kubernetes**. Controller báo qua status của CR phụ (`PlatformNodeGrant` cho dedicated node, `PlatformSharedPool` cho shared pool, `PlatformNodeInventory` cho danh sách node trong `cluster_nodes`), worker chép về DB — worker không đọc Node trực tiếp. Chênh lệch với reservation → `Overcommitted`.
- `onboarding_policies.entitlement_template` và `grant_template` là JSON điền sẵn vào form duyệt; entitlement và grant đã cấp là snapshot, đổi policy sau đó không ảnh hưởng [5.1][5.2.1].
- `id` của `clusters`, `shared_compute_pools` và `network_pools` là tên do Platform Admin đặt (`cluster-a`, `shared-general`, `onboarding-pod-network`), không phải ULID. Chúng thành giá trị label và tên object trên Kubernetes nên phải là DNS label (CHECK trong schema). Hệ quả: tên pool là duy nhất trên toàn platform, không phải trong từng Cluster; Cluster thứ hai phải đặt tên pool khác.
- Grant phải có ít nhất một row `cluster_grant_network_pools` (service kiểm khi duyệt và khi sửa grant); `cluster_grant_shared_pools` được để trống.
- `shared_compute_pools` chưa có cột trạng thái: chưa có luồng nào cần nó. Thêm khi M5 định nghĩa được giá trị.

## 4. Vùng Workspace & Allocation

```mermaid
erDiagram
  workspaces {
    text id PK
    text org_id FK
    text cluster_id FK
    text cluster_grant_id FK
    text slug "immutable, unique trong org"
    text k8s_name "org_slug-ws_slug, global unique"
    text display_name
    text state
    bigint requests_cpu_milli
    bigint requests_memory_bytes
    bigint limits_cpu_milli
    bigint limits_memory_bytes
    int pods
    bigint requests_storage_bytes
    int namespace_quota
    text compute_mode "Shared|Dedicated"
    text shared_pool_id FK "nullable"
    bigint desired_generation
    bigint observed_generation
    jsonb conditions
    timestamptz deleted_at
  }
  workspace_node_reservations {
    text id PK
    text workspace_id FK
    text node_grant_id FK
    bigint reserved_cpu_milli
    bigint reserved_memory_bytes
    int reserved_pods
    text state "Pending|Active|Releasing|Released"
  }
  network_allocations {
    text id PK
    text workspace_id FK
    text cluster_id FK
    text pool_id FK
    cidr cidr
    text state "Allocated|Releasing|Quarantined|Released"
    timestamptz quarantine_until
  }
  workspace_group_bindings {
    text workspace_id PK
    text group_id PK
    text role "TenantOwner|Developer|Viewer"
  }
  workspace_namespaces {
    text workspace_id FK
    text name "prefix k8s_name-"
    text source "portal|kubectl"
    text state
    jsonb usage_snapshot
  }
  workspace_routes {
    text id PK "rte-…"
    text workspace_id FK
    text namespace "tên Namespace đầy đủ"
    text name "nhãn user đặt"
    text hostname "UNIQUE khi state <> Deleted"
    text service_name
    int service_port
    text state "Desired|Creating|Active|Deleting|Deleted"
    text created_by FK
  }
  operations {
    text id PK
    text org_id FK
    text resource_type
    text resource_id
    text type
    text state "Pending|Running|Succeeded|Failed|Blocked"
    smallint progress "0–100"
    text error_code
    text error_message
    text correlation_id
    text created_by FK "NULL khi platform tự khởi tạo"
    timestamptz finished_at
  }
  outbox_events {
    bigint id PK "thứ tự giao"
    text operation_id FK
    text ordering_key "resource_type/resource_id"
    text target_cluster_id FK
    text type
    jsonb payload
    bigint version
    int attempts
    timestamptz next_attempt_at
    text last_error
    timestamptz processed_at
  }
  audit_events {
    bigint id PK
    text actor_user_id
    text org_id
    text action
    text target_type
    text target_id
    text result
    jsonb detail
    timestamptz at
  }
  workspaces ||--o| workspace_node_reservations : ""
  workspaces ||--|| network_allocations : ""
  workspaces ||--o{ workspace_group_bindings : ""
  workspaces ||--o{ workspace_namespaces : ""
  workspaces ||--o{ workspace_routes : ""
  operations ||--o{ outbox_events : ""
```

| Invariant | Enforce bằng |
|---|---|
| `k8s_name` global unique, không tái dùng trong thời gian quarantine | `UNIQUE (k8s_name) WHERE state <> 'Deleted'`; cửa sổ quarantine do service kiểm lúc tạo: từ chối nếu còn Workspace `Deleted` cùng tên mà `network_allocations` chưa `Released` |
| Namespace name unique toàn cluster và có tiền tố `k8s_name-` | `UNIQUE (cluster_id, name)`; tiền tố do service ghép, không nhận từ client |
| Một group tối đa một role mỗi Workspace | `PRIMARY KEY (workspace_id, group_id)` |
| Workspace luôn còn ≥1 TenantOwner binding | service trong transaction + test; không có constraint SQL thuận tiện |
| Dedicated Workspace có đúng một reservation đang giữ chỗ | `UNIQUE (workspace_id) WHERE state IN ('Pending','Active')` |
| Σ reservations trên node ≤ reservable | **transaction**: `SELECT ... FOR UPDATE` row `dedicated_node_grants`, `SUM` reservation chưa `Released`, so sánh, rồi insert |
| Σ allocation Shared trên pool ≤ ratio × reservable | **transaction**: lock row `shared_compute_pools`, `SUM` cột tài nguyên của `workspaces` trên pool |
| Σ allocation trong grant ≤ grant | **transaction**: lock row `organization_cluster_grants` |
| CIDR không overlap trong cluster | `EXCLUDE USING gist (cluster_id WITH =, cidr inet_ops WITH &&) WHERE (state <> 'Released')` (extension `btree_gist`) |
| Hostname của Route duy nhất toàn nền tảng | `UNIQUE (hostname) WHERE state <> 'Deleted'` trên `workspace_routes`; vi phạm trả `ROUTE_HOSTNAME_TAKEN` |
| Tối đa 20 Route mỗi Workspace | **transaction**: lock row `workspaces`, `COUNT` Route chưa `Deleted` |
| Event cùng resource giao đúng thứ tự | worker chỉ lấy event khi không còn event cũ hơn chưa giao cùng `ordering_key` |

Thứ tự lock cố định để tránh deadlock: `organization_cluster_grants` → `shared_compute_pools` hoặc `dedicated_node_grants` → `network_pools`. Mọi transaction create/resize/delete lock theo đúng thứ tự này.

**Ghi chú**

- "CIDR còn trống" không phải một state: đó là mọi khoảng trong pool không bị row nào khác `Released` phủ lên. Row `Released` được giữ làm lịch sử.
- "Reservation vẫn giữ khi provisioning `Failed`" thể hiện bằng việc row `workspaces` còn tồn tại ở state `Failed` và vẫn nằm trong `SUM`. Chỉ row `Deleted` mới bị loại khỏi `SUM`; mọi query tính tổng phải viết rõ điều kiện này.
- Exclusion constraint là **lớp bảo vệ cuối**; IPAM vẫn tự tính subnet trống dưới lock của `network_pools`. Khi constraint bắn (SQLSTATE `23P01`), API trả `CIDR_OVERLAP`, không phải 500.
- `outbox_events` chỉ mang thay đổi đẩy xuống Cluster, nên `target_cluster_id` là bắt buộc. Đồng bộ sang Keycloak **không** đi qua bảng này: Group Sync Worker đọc thẳng `sync_status` của `groups`/`group_members`.

## 5. Ánh xạ DB ↔ CR ↔ Kubernetes

**Mục đích:** biết trường nào ở DB sinh ra trường nào trong CR và object nào trong cluster; dùng khi review controller.

```mermaid
flowchart LR
  classDef db fill:#e2f0d9,stroke:#2d6a4f,color:#000
  classDef cr fill:#fff3cd,stroke:#b58900,color:#000
  classDef k8s fill:#d1e7ff,stroke:#0b5ed7,color:#000

  W["workspaces<br/>k8s_name, tài nguyên,<br/>namespace_quota, compute_mode"]:::db
  NR["workspace_node_reservations<br/>node_grant → node_name"]:::db
  NA["network_allocations.cidr"]:::db
  GB["workspace_group_bindings<br/>+ groups.machine_name"]:::db
  WN["workspace_namespaces<br/>source = portal"]:::db
  G["organization_cluster_grants<br/>allowed_storage_classes"]:::db
  WR["workspace_routes<br/>hostname đã ghép"]:::db
  C["clusters.apps_domain"]:::db

  CR["PlatformWorkspace<br/>metadata.name = k8s_name<br/>spec.resources<br/>spec.compute<br/>spec.network.cidr<br/>spec.accessBindings<br/>spec.namespaces<br/>spec.routes<br/>spec.storageClasses"]:::cr

  T["Capsule Tenant k8s_name<br/>owners, nodeSelector,<br/>namespaceOptions.quota,<br/>storageClasses"]:::k8s
  RP["ResourcePool + LimitRange"]:::k8s
  IP["Calico IPPool + NetworkPolicy"]:::k8s
  RB["RoleBinding tenantry:tenant-developer / viewer"]:::k8s
  NS["Namespace k8s_name-*"]:::k8s
  ROUTE["Ingress của Route<br/>trong Namespace tenant"]:::k8s
  NODE["Node label workspaces.tenantry.io/ws-id"]:::k8s

  W --> CR
  NR --> CR
  NA --> CR
  GB --> CR
  WN --> CR
  G --> CR
  C -->|"API ghép hostname"| WR
  WR --> CR
  CR --> T
  CR --> RP
  CR --> IP
  CR --> RB
  CR --> NS
  CR --> ROUTE
  CR --> NODE
```

**Điểm cần review**

- CR chứa **giá trị đã resolve** (node name, CIDR, group subject, hostname của Route), không chứa ID tham chiếu về DB mà controller phải tra. Điều này giữ controller không phụ thuộc DB và là nền cho Cluster Agent Phase 2.
- Nếu Admin đổi `allowed_storage_classes` của grant, mọi Workspace trong grant cần re-render CR → một "fan-out" trong service entitlement sinh nhiều outbox event. Task của M5/M7.
