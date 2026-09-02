# 02 — Data model: ERD và invariant

> Nguồn sự thật: `docs/PRODUCT-PLAN.md` v0.6, mục 4, 5, 15. PostgreSQL là source of truth cho product state; Kubernetes chỉ giữ runtime state qua CR `PlatformWorkspace`.
> Cách đọc: ERD tổng → ERD theo từng vùng nghiệp vụ → bảng invariant và cách enforce (constraint vs transaction). Sau mỗi sơ đồ có **Điểm cần review**.

## 1. ERD tổng thể

**Mục đích:** nhìn một lần toàn bộ entity và quan hệ; chi tiết cột ở các mục sau.

```mermaid
erDiagram
  users ||--o{ organization_members : "là member"
  users ||--|| organizations : "sở hữu personal org"
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
  workspaces ||--o{ operations : ""
  operations ||--o{ outbox_events : ""
  clusters ||--o{ outbox_events : "target"
  organizations ||--o{ audit_events : ""
  onboarding_policies ||--o{ organization_cluster_grants : "template"
```

**Điểm cần review**

- `workspaces → organization_cluster_grants` là quan hệ **bắt buộc**: Workspace luôn thuộc đúng một grant (org × cluster). Đây là chỗ enforce "Cluster phải được cấp cho Organization".
- `network_pools.organization_id` nullable: pool chung cho onboarding. Grant tham chiếu pool qua bảng nối; pool riêng chỉ được tham chiếu bởi grant của đúng org (check ở service layer + trigger đơn giản).
- Reservation quota của ClusterGrant **không có bảng riêng**: nó được suy ra bằng `SUM(workspaces.resources)` trong grant, kiểm tra dưới row lock của grant. NodeReservation thì có bảng riêng vì cần state riêng và lifecycle riêng.

## 2. Vùng Identity & Organization

```mermaid
erDiagram
  users {
    ulid id PK
    text issuer
    text subject
    text email
    text display_name
    text status
    timestamptz created_at
  }
  organizations {
    ulid id PK
    text slug "immutable, global unique"
    text display_name
    bool is_personal
    ulid owner_user_id FK "đúng một Owner"
    text status
  }
  organization_members {
    ulid org_id FK
    ulid user_id FK
    text role "OrgOwner | OrgAdmin | OrgMember"
    text status
  }
  invitations {
    ulid id PK
    ulid org_id FK
    text email
    text token_hash
    text role
    ulid[] group_ids "optional"
    timestamptz expires_at
    ulid accepted_by FK
    text state
  }
  ownership_transfers {
    ulid id PK
    ulid org_id FK
    ulid from_user_id FK
    ulid to_user_id FK
    text token_hash
    timestamptz expires_at
    text state
  }
  groups {
    ulid id PK
    ulid org_id FK
    text display_name "unique trong org"
    text machine_name "org_<id>:group_<id>, global unique"
    text keycloak_group_id
    text sync_status
  }
  group_members {
    ulid group_id FK
    ulid user_id FK
    text sync_status
  }
  users ||--o{ organization_members : ""
  organizations ||--o{ organization_members : ""
  organizations ||--o{ invitations : ""
  organizations ||--o{ ownership_transfers : ""
  organizations ||--o{ groups : ""
  groups ||--o{ group_members : ""
  users ||--o{ group_members : ""
```

Constraint quan trọng:

| Invariant | Enforce bằng |
|---|---|
| Một account ↔ một identity Keycloak | `UNIQUE (issuer, subject)` |
| Một user có đúng một personal org | `UNIQUE (owner_user_id) WHERE is_personal` |
| Đúng một OrgOwner mỗi org | `organizations.owner_user_id NOT NULL` + `UNIQUE (org_id) ON organization_members WHERE role = 'OrgOwner'` |
| Owner luôn là member | trigger/service: khi set `owner_user_id` phải có row member role Owner |
| Group display name unique trong org, machine name unique toàn cục | hai `UNIQUE` |
| Không xóa group còn binding | `FK ... ON DELETE RESTRICT` từ `workspace_group_bindings` |
| Email invite phải khớp email verified khi accept | service layer (đọc claim `email_verified`) |

**Điểm cần review**

- `group_members.user_id` chỉ được trỏ tới user **là member của org sở hữu group** — không diễn tả được bằng FK đơn; cần check ở service + test. Có chấp nhận không, hay dùng FK composite `(org_id, user_id)` sang `organization_members`? Đề xuất: FK composite, an toàn hơn và không tốn gì.
- Khi remove member: cascade xóa `group_members` (đồng thời ghi outbox sync Keycloak). Cần chốt là cascade DB hay service — đề xuất service, vì phải sinh outbox.

## 3. Vùng Cluster & Entitlement

```mermaid
erDiagram
  clusters {
    text id PK "cluster-a"
    text display_name
    text status "Registered|Ready|Maintenance|Unavailable"
    text proxy_endpoint
    text console_url
    text apps_wildcard_domain
    jsonb capabilities
    timestamptz last_health_check
  }
  organization_entitlements {
    ulid org_id PK
    int max_members
    int max_groups
    int max_workspaces_total
  }
  onboarding_policies {
    ulid id PK
    text default_cluster_id FK
    jsonb grant_template
    bool active
  }
  organization_cluster_grants {
    ulid id PK
    ulid org_id FK
    text cluster_id FK
    bool is_default
    int max_workspaces
    int max_namespaces_total
    jsonb resources "requests/limits cpu-mem, pods, storage"
    text[] allowed_compute_modes
    text[] allowed_storage_classes
    text[] allowed_ingress_classes
  }
  shared_compute_pools {
    ulid id PK
    text cluster_id FK
    text name
    jsonb node_selector
    numeric allocation_overcommit_ratio
    jsonb system_reserve
    jsonb capacity_snapshot
    text status
  }
  cluster_grant_shared_pools {
    ulid grant_id FK
    ulid pool_id FK
  }
  dedicated_node_grants {
    ulid id PK
    ulid org_id FK
    text cluster_id FK
    text node_name "unique per cluster"
    jsonb allocatable
    jsonb system_reserve
    text node_state "Ready|NotReady|Cordoned"
    text capacity_state "Available|PartiallyAllocated|FullyAllocated|Overcommitted"
  }
  network_pools {
    ulid id PK
    text cluster_id FK
    ulid org_id FK "nullable = pool chung"
    cidr[] cidrs
    int default_prefix_length
    int[] allowed_prefix_lengths
    text[] allowed_compute_modes
  }
  cluster_grant_network_pools {
    ulid grant_id FK
    ulid pool_id FK
  }
  clusters ||--o{ organization_cluster_grants : ""
  clusters ||--o{ shared_compute_pools : ""
  clusters ||--o{ dedicated_node_grants : ""
  clusters ||--o{ network_pools : ""
  organization_cluster_grants ||--o{ cluster_grant_shared_pools : ""
  shared_compute_pools ||--o{ cluster_grant_shared_pools : ""
  organization_cluster_grants ||--o{ cluster_grant_network_pools : ""
  network_pools ||--o{ cluster_grant_network_pools : ""
  onboarding_policies ||--o{ organization_cluster_grants : "sinh ra"
```

| Invariant | Enforce bằng |
|---|---|
| Một org có tối đa một default grant | `UNIQUE (org_id) WHERE is_default` |
| Một org × cluster có tối đa một grant | `UNIQUE (org_id, cluster_id)` |
| Node chỉ dedicated cho một org | `UNIQUE (cluster_id, node_name)` |
| Pool/NodeGrant/NetworkPool trong grant phải cùng `cluster_id` với grant | trigger hoặc check ở service + integration test |
| Pool riêng chỉ vào grant của đúng org | check ở service khi Admin gán |
| Không giảm grant dưới tổng allocation hiện tại | transaction: lock grant, `SUM(workspaces.resources)` so với giá trị mới |
| Không revoke node grant còn reservation Active | `FK ON DELETE RESTRICT` từ `workspace_node_reservations` + check state |

**Điểm cần review**

- `capacity_snapshot` của shared pool và `allocatable` của dedicated node là **dữ liệu chép từ Kubernetes**; ai cập nhật? Đề xuất: Provisioning Worker poll Node mỗi N phút và ghi lại; chênh lệch với reservation → chuyển `Overcommitted`. Cần chốt tần suất và ai chịu trách nhiệm (worker hay controller báo qua CR status).
- `onboarding_policies.grant_template` là JSON; khi Admin đổi policy, grant đã cấp **không** đổi theo (snapshot tại thời điểm tạo). Đúng ý định chứ?

## 4. Vùng Workspace & Allocation

```mermaid
erDiagram
  workspaces {
    ulid id PK
    ulid org_id FK
    text cluster_id FK
    ulid cluster_grant_id FK
    text slug "immutable, unique trong org"
    text k8s_name "org_slug-ws_slug, global unique"
    text display_name
    text state
    jsonb resources
    int namespace_quota
    text compute_mode "Shared|Dedicated"
    ulid shared_pool_id FK "nullable"
    int desired_generation
    int observed_generation
    jsonb conditions
    timestamptz deleted_at
  }
  workspace_node_reservations {
    ulid id PK
    ulid workspace_id FK
    ulid node_grant_id FK
    numeric reserved_cpu
    bigint reserved_memory
    int reserved_pods
    text state "Pending|Active|Releasing|Released"
  }
  network_allocations {
    ulid id PK
    ulid workspace_id FK
    text cluster_id FK
    ulid pool_id FK
    cidr cidr
    text state "Allocated|Releasing|Quarantined|Released"
    timestamptz quarantine_until
  }
  workspace_group_bindings {
    ulid workspace_id FK
    ulid group_id FK
    text role "TenantOwner|Developer|Viewer"
  }
  workspace_namespaces {
    ulid workspace_id FK
    text name "prefix k8s_name-"
    text source "portal|kubectl"
    text state
    jsonb usage_snapshot
  }
  operations {
    ulid id PK
    text resource_type
    ulid resource_id
    text type
    text state "Pending|Running|Succeeded|Failed|Blocked"
    jsonb progress
    text error_code
    text idempotency_key
    text correlation_id
  }
  outbox_events {
    bigserial id PK
    text target_cluster_id FK
    ulid operation_id FK
    text type
    jsonb payload
    int version
    int attempts
    timestamptz next_attempt_at
    timestamptz processed_at
  }
  audit_events {
    bigserial id PK
    ulid actor_user_id
    ulid org_id
    text action
    text target_type
    ulid target_id
    text result
    jsonb detail
    timestamptz at
  }
  workspaces ||--o| workspace_node_reservations : ""
  workspaces ||--|| network_allocations : ""
  workspaces ||--o{ workspace_group_bindings : ""
  workspaces ||--o{ workspace_namespaces : ""
  workspaces ||--o{ operations : ""
  operations ||--o{ outbox_events : ""
```

| Invariant | Enforce bằng |
|---|---|
| `k8s_name` global unique, không tái dùng trong quarantine | `UNIQUE (k8s_name)` trên cả row `deleted_at IS NOT NULL` cho tới khi hết quarantine (soft row) |
| Namespace name unique toàn cluster và có tiền tố `k8s_name-` | `UNIQUE (cluster_id, name)` + `CHECK (name LIKE k8s_name || '-%')` qua trigger |
| Một group tối đa một role mỗi Workspace | `PRIMARY KEY (workspace_id, group_id)` |
| Workspace luôn còn ≥1 TenantOwner binding | service check trong transaction + test; không có constraint SQL thuận tiện |
| Dedicated Workspace có đúng một reservation Active | `UNIQUE (workspace_id) WHERE state IN ('Pending','Active')` |
| Σ reservations trên node ≤ reservable | **transaction**: `SELECT ... FOR UPDATE` row `dedicated_node_grants`, tính `SUM` các reservation Active/Pending, so sánh, rồi insert |
| Σ allocation Shared trên pool ≤ ratio × reservable | **transaction**: lock row `shared_compute_pools`, `SUM(workspaces.resources)` trên pool |
| Σ allocation trong grant ≤ grant.resources | **transaction**: lock row `organization_cluster_grants` |
| CIDR không overlap trong cluster | `EXCLUDE USING gist (cluster_id WITH =, cidr inet_ops WITH &&) WHERE state <> 'Released'` (cần extension `btree_gist`) |
| Idempotency | `UNIQUE (idempotency_key)` trên `operations` |

Thứ tự lock cố định để tránh deadlock: `organization_cluster_grants` → `shared_compute_pools` hoặc `dedicated_node_grants` → `network_pools`. Mọi transaction create/resize/delete phải lock theo đúng thứ tự này.

**Điểm cần review**

- `workspaces.resources` là JSONB (cpu/mem/pods/storage). Tính `SUM` trên JSONB chậm hơn cột riêng; với vài chục Workspace không thành vấn đề, nhưng nếu muốn index thì nên tách 6 cột numeric. Đề xuất: **tách cột** ngay từ đầu, JSONB chỉ dùng cho `conditions`/`progress`.
- CIDR exclusion constraint là **lớp bảo vệ cuối**; IPAM service vẫn tự tính subnet trống trước. Cần test rằng khi hai request đồng thời chọn cùng subnet, một request nhận `CIDR_OVERLAP` sạch sẽ (map lỗi 23P01) thay vì 500.
- Reservation grant không có bảng riêng nên "reservation vẫn giữ khi provisioning Failed" được thể hiện bằng việc **row `workspaces` vẫn tồn tại với state Failed** và được tính vào SUM. Chỉ khi `Deleted` mới loại khỏi SUM. Đơn giản nhưng phải viết rõ trong query.

## 5. Ánh xạ DB ↔ CR ↔ Kubernetes

**Mục đích:** biết trường nào ở DB sinh ra trường nào trong CR và object nào trong cluster; dùng khi review controller.

```mermaid
flowchart LR
  classDef db fill:#e2f0d9,stroke:#2d6a4f,color:#000
  classDef cr fill:#fff3cd,stroke:#b58900,color:#000
  classDef k8s fill:#d1e7ff,stroke:#0b5ed7,color:#000

  W["workspaces<br/>k8s_name, resources,<br/>namespace_quota, compute_mode"]:::db
  NR["workspace_node_reservations<br/>node_grant → node_name"]:::db
  NA["network_allocations.cidr"]:::db
  GB["workspace_group_bindings<br/>+ groups.machine_name"]:::db
  WN["workspace_namespaces<br/>source = portal"]:::db
  G["organization_cluster_grants<br/>allowed_storage_classes,<br/>allowed_ingress_classes"]:::db
  C["clusters.apps_wildcard_domain"]:::db

  CR["PlatformWorkspace<br/>metadata.name = k8s_name<br/>spec.resources<br/>spec.compute<br/>spec.network.cidr<br/>spec.accessBindings<br/>spec.namespaces<br/>spec.ingress.hostnameZone<br/>spec.storageClasses"]:::cr

  T["Capsule Tenant k8s_name<br/>owners, nodeSelector,<br/>namespaceOptions.quota,<br/>storageClasses, ingressOptions"]:::k8s
  RP["ResourcePool + LimitRange"]:::k8s
  IP["Calico IPPool + NetworkPolicy"]:::k8s
  RB["RoleBinding tenantry:tenant-developer / viewer"]:::k8s
  NS["Namespace k8s_name-*"]:::k8s
  CERT["Certificate *.k8s_name.apps... + Secret"]:::k8s
  NODE["Node label workspaces.tenantry.io/ws-id"]:::k8s

  W --> CR
  NR --> CR
  NA --> CR
  GB --> CR
  WN --> CR
  G --> CR
  C --> CR
  CR --> T
  CR --> RP
  CR --> IP
  CR --> RB
  CR --> NS
  CR --> CERT
  CR --> NODE
```

**Điểm cần review**

- CR chứa **giá trị đã resolve** (node name, CIDR, group subject, hostname zone), không chứa ID tham chiếu về DB mà controller phải tra. Điều này giữ controller stateless với DB và là nền cho Cluster Agent Phase 2.
- Nếu Admin đổi `allowed_storage_classes` của grant, mọi Workspace trong grant cần re-render CR → cần một "fan-out" trong service entitlement sinh nhiều outbox event. Cần ghi nhận thành task ở M5/M7.
