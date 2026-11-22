# Tenantry — 03. Luồng nghiệp vụ và kỹ thuật

Tài liệu này vẽ 13 luồng chính của Tenantry dưới dạng `sequenceDiagram`. Đây là **góc nhìn**, không định nghĩa gì mới: quy tắc ở `PRODUCT-PLAN.md` (số trong ngoặc vuông như [5.3] trỏ về đó), trạng thái ở `04-state-machines.md`, schema ở `02-data-model.md`, phiên và thu hồi quyền ở `06-security-and-identity.md`.
Mỗi luồng có ba phần: **Mục đích** một dòng, **sơ đồ Mermaid** đánh số bước bằng `autonumber`, và **Điểm cần review** liệt kê quyết định/bất biến mà luồng phụ thuộc cùng nhánh lỗi — người review đi từng bước và tick từng bullet.
Nhánh lỗi, nhánh tùy chọn và xử lý đồng thời được vẽ bằng `alt` / `opt` / `loop` / `par`; số bước dùng để trích dẫn khi góp ý.
Ví dụ xuyên suốt: Organization `acme` (id `org-01jabc`), Workspace `solar` → tên Kubernetes `acme-solar`, Cluster `cluster-a`, node dedicated `worker-a-05` qua `nodegrant-a1`, các Workspace Dedicated `water` và `green`.
Sơ đồ vẽ trạng thái đích của MVP; bước chỉ có từ một milestone về sau thì ghi "(từ Mx)" (milestone ở `IMPLEMENTATION-PLAN.md`). Câu hỏi còn mở trỏ về `REVIEW-CHECKLIST.md` bằng mã của nó; tài liệu này không tự chọn phương án.
Audit action viết theo dạng `<resource>.<verb>` như hằng số trong `backend/internal/audit/audit.go`.

## Chú giải participant

Tên và alias được giữ nguyên trong mọi sơ đồ; sơ đồ nào không cần participant nào thì bỏ qua nhưng không đổi tên.

| Alias | Tên trong sơ đồ | Vai trò trong Tenantry |
|---|---|---|
| `U` | User / Browser | Người dùng cuối, OrgOwner, Platform Admin hoặc trình duyệt của họ |
| `SPA` | Portal SPA | Frontend Vue, gọi Platform API qua cookie phiên |
| `API` | Platform API | Backend Go kiêm BFF: authz, validation, reservation, operation, phiên |
| `DB` | PostgreSQL | Source of truth cho product state, outbox, phiên portal |
| `KC` | Keycloak | Account, OIDC, group claim, realm role `platform-admin` |
| `PW` | Outbox / Provisioning Worker | Đọc `outbox_events`, apply `PlatformWorkspace` CR bằng credential scoped, đồng bộ status về DB |
| `GS` | Group Sync Worker | Đồng bộ group/membership từ DB sang Keycloak, logout user khi thu hồi quyền |
| `K8S` | kube-apiserver | API server của Cluster chứa Workspace |
| `CTRL` | Workspace Controller | controller-runtime trong Cluster, reconcile CR `tenantry.io` |
| `CAP` | Capsule | Capsule Operator: Tenant (kèm quota chung), TenantResource, admission |
| `CAL` | Calico | IPPool, IPAM, NetworkPolicy enforcement |
| `PROXY` | Capsule Proxy | Endpoint `kube-a.tenantry.site`, filter cluster-scoped |
| `HL` | Headlamp | Web console per Cluster, client OIDC `headlamp-cluster-a` |
| `KL` | kubectl + kubelogin | CLI của user với exec plugin OIDC, client `kubectl` |
| `ING` | Traefik | Shared ingress controller, cert wildcard `*.mvp.tenantry.site` |

---

## 1. Đăng nhập lần đầu và xin tạo Organization

### 1a. First login / sign-up: chỉ tạo user record và phiên

**Mục đích:** Một account đăng ký qua Keycloak; callback chỉ upsert user và tạo phiên portal. Không Organization nào được tạo tự động [4.2][8.1][8.3].

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant KC as Keycloak
  participant DB as PostgreSQL

  U->>SPA: Mở portal, bấm Sign up hoặc Login
  SPA->>API: GET /auth/login
  API-->>U: Set-Cookie __Host-tenantry_login (state, nonce, PKCE), 302 Keycloak
  U->>KC: Đăng ký, verify email, đăng nhập
  KC-->>U: 302 callback kèm authorization code
  U->>API: GET /auth/callback?code=...&state=...
  API->>API: Mở cookie __Host-tenantry_login, so state
  API->>KC: Đổi code + PKCE verifier lấy token
  KC-->>API: ID token, access token aud portal-api, refresh token
  API->>API: Validate issuer, audience, nonce, expiry, email_verified
  alt email_verified = false hoặc token không hợp lệ
    API-->>U: Trang lỗi, không tạo phiên
  end
  API->>DB: BEGIN
  API->>DB: Upsert users theo issuer + sub, cập nhật email, display_name
  API->>DB: INSERT sessions (hash cookie, realm_roles, refresh token mã hóa)
  API->>DB: COMMIT
  API-->>U: Set-Cookie __Host-tenantry_session + __Host-tenantry_csrf, 302 về trang ban đầu
  SPA->>API: GET /api/v1/me, GET /api/v1/organizations
  alt User chưa thuộc Organization nào
    API-->>SPA: danh sách rỗng
    SPA-->>U: Màn trống: "Xin tạo Organization" hoặc chờ lời mời
  else Đã có Organization hoặc yêu cầu Pending/Rejected
    API-->>SPA: Organization + trạng thái yêu cầu
  end
```

**Điểm cần review**

- Bước 13: identity key duy nhất là `(issuer, sub)`; email/display name chỉ là snapshot [4.1]. Upsert nên login hai lần, hoặc hai callback song song, không tạo hai user.
- Bước 10–11: `email_verified=true` là điều kiện tạo phiên, để flow Invitation (mục 3) tin được email của phiên (Keycloak required action Verify Email). Thư xác minh ở cụm MVP đi vào Mailpit, xem `06-security-and-identity.md` mục 3.
- Bước 3, 7, 14, 16: cơ chế cookie, phiên, refresh và logout định nghĩa ở `06-security-and-identity.md` mục 3; sơ đồ này không lặp lại.
- User có `users.status = Disabled` không qua được request kế tiếp (bước 17): API trả 401 và xóa phiên.

### 1b. Xin tạo Organization → Platform Admin duyệt hoặc từ chối

**Mục đích:** User gửi yêu cầu (Organization `Pending`, giữ chỗ slug, người xin là OrgOwner); Admin duyệt kèm ClusterGrant điền sẵn từ onboarding policy, hoặc từ chối kèm lý do và nhả slug [4.2][5.2.1][16.5]. Trạng thái: `04-state-machines.md` mục 11.

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant GS as Group Sync Worker
  participant KC as Keycloak

  U->>SPA: Alice nhập slug acme, display name, mô tả
  SPA->>API: POST /api/v1/organizations
  API->>API: Validate slug theo [4.2] (chữ thường và số, không có -), từ cấm
  API->>DB: BEGIN
  API->>DB: INSERT organizations status=Pending, owner=alice
  API->>DB: INSERT organization_members role OrgOwner
  alt Slug đã bị giữ
    API->>DB: ROLLBACK
    API-->>SPA: 409 SLUG_TAKEN
  else Alice đang có một yêu cầu Pending khác
    API->>DB: ROLLBACK
    API-->>SPA: 409 ORG_REQUEST_PENDING
  else Hợp lệ
    API->>DB: INSERT audit organization.create, COMMIT
    API-->>SPA: 201 Organization Pending
    SPA-->>U: Màn "đang chờ duyệt"
  end
  Note over U,SPA: Platform Admin mở khu Admin, hàng đợi duyệt
  SPA->>API: GET /api/v1/admin/organizations?status=Pending
  API->>API: Check platform-admin trong phiên
  API-->>SPA: Yêu cầu của Alice + entitlement và grant điền sẵn từ onboarding policy
  alt Admin duyệt (có thể sửa trước)
    SPA->>API: POST /admin/organizations/org-01jabc:approve
    API->>DB: BEGIN, SELECT organizations FOR UPDATE
    alt Không còn Pending (Admin khác đã xử lý)
      API->>DB: ROLLBACK
      API-->>SPA: 409 CONFLICT
    end
    API->>DB: SELECT clusters cluster-a FOR SHARE
    alt Cluster không Ready
      API->>DB: ROLLBACK
      API-->>SPA: 409 CLUSTER_NOT_READY
    end
    API->>DB: Check shared pool và NetworkPool cùng cluster_id, có ít nhất một NetworkPool
    API->>DB: INSERT organization_cluster_grants is_default=true, cluster_grant_shared_pools, cluster_grant_network_pools
    API->>DB: UPDATE organizations status=Active
    API->>DB: INSERT organization_entitlements
    API->>DB: INSERT groups admins, developers, viewers (Pending)
    API->>DB: INSERT group_members alice vào admins (Pending)
    API->>DB: INSERT audit organization.approve, COMMIT
    API-->>SPA: 200 Organization Active + grant
    GS->>DB: Lấy group, group_member sync_status=Pending
    GS->>KC: Tạo group org_01jabc:group_..., add Alice vào admins
    GS->>DB: UPDATE sync_status=Synced
  else Admin từ chối
    SPA->>API: POST /admin/organizations/org-01jabc:reject kèm lý do
    API->>DB: BEGIN, SELECT organizations FOR UPDATE, còn Pending?
    API->>DB: UPDATE status=Rejected, review_note, INSERT audit organization.reject, COMMIT
    API-->>SPA: 200
    Note over U,SPA: Alice thấy lý do, slug acme được nhả, gửi được yêu cầu mới
  end
```

**Điểm cần review**

- Bước 5–10: hai nhánh lỗi đến từ hai partial unique index (slug, một `Pending` mỗi user), không phải từ `SELECT` trước khi insert — nên hai request đồng thời vẫn chỉ một thắng.
- Bước 18–30 là **một** transaction: Organization không bao giờ `Active` mà thiếu entitlement, default grant hay group mặc định. Lock đi theo thứ tự cố định ở `02-data-model.md` mục 4 (`organizations` → `clusters`).
- Bước 18–20 và 36: duyệt và từ chối đều khóa row Organization và chỉ chạy khi còn `Pending`, nên hai Admin xử lý cùng lúc thì người sau nhận `409`.
- Bước 21–23: Cluster phải `Ready`; onboarding policy trỏ default Cluster không tồn tại hoặc không `Ready` thì Admin phải chọn Cluster khác hoặc sửa policy trước.
- Bước 32–34 chạy bất đồng bộ; Alice vào wizard ngay được nhưng tạo Workspace sẽ bị `GROUP_SYNC_PENDING` tới khi group `admins` sync xong [16.5][17.1].
- Grant đã cấp là snapshot của policy tại lúc duyệt [5.2.1].

---

## 2. Platform Admin tạo thẳng Organization cho khách và nâng entitlement

### 2a. Tạo thẳng Organization kèm Owner và ClusterGrant ban đầu

**Mục đích:** Platform Admin (realm role `platform-admin`) tạo Organization `Active` ngay cho khách, không qua bước xin–duyệt: gán đúng một OrgOwner và ClusterGrant ban đầu trong một thao tác [4.2][16.5].

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant GS as Group Sync Worker
  participant KC as Keycloak

  U->>SPA: Admin mở khu Admin, form New Organization
  SPA->>API: POST /api/v1/admin/organizations
  Note over SPA,API: body slug, ownerUserId, entitlement, ClusterGrant đầu, header Idempotency-Key
  API->>API: Check platform-admin trong phiên
  alt Phiên không có platform-admin
    API-->>SPA: 403 FORBIDDEN
  end
  API->>DB: BEGIN
  API->>DB: INSERT idempotency_keys (user, key) ON CONFLICT DO NOTHING
  alt Key đã có, cùng method, path và request hash
    API->>DB: ROLLBACK
    API-->>SPA: Trả lại response đã lưu, Idempotent-Replayed: true
  else Key đã có, khác request
    API->>DB: ROLLBACK
    API-->>SPA: 422 IDEMPOTENCY_KEY_REUSED
  end
  API->>DB: SELECT users WHERE id=ownerUserId
  alt Owner chưa tồn tại trong users
    API->>DB: ROLLBACK
    API-->>SPA: 422 OWNER_USER_NOT_FOUND
  end
  API->>DB: INSERT organizations status=Active, slug acme
  API->>DB: INSERT organization_members owner role OrgOwner
  API->>DB: SELECT clusters WHERE id=cluster-a FOR SHARE
  API->>DB: Check shared pool và NetworkPool cùng cluster_id, có ít nhất một NetworkPool
  alt Slug trùng, Cluster không Ready hoặc ref sai Cluster
    API->>DB: ROLLBACK
    API-->>SPA: 409 SLUG_TAKEN, 409 CLUSTER_NOT_READY hoặc 422 VALIDATION_FAILED
  end
  API->>DB: INSERT organization_cluster_grants is_default=true, cluster_grant_shared_pools, cluster_grant_network_pools
  API->>DB: INSERT organization_entitlements
  API->>DB: INSERT groups admins, developers, viewers (Pending)
  API->>DB: INSERT group_members owner vào admins (Pending)
  API->>DB: INSERT audit organization.create (actor là Admin), UPDATE idempotency_keys (response 201), COMMIT
  API-->>SPA: 201 Organization + grant
  GS->>DB: Poll group sync_status=Pending
  GS->>KC: Tạo group machine name, add owner vào admins
  GS->>DB: UPDATE sync_status
```

**Điểm cần review**

- Bước 3–4: authz admin ở backend theo realm role, không tin flag từ frontend [4.1][10.1].
- Bước 6–10 và 24: `Idempotency-Key` bắt buộc với mọi `POST /admin/*` tạo resource; giành ngay sau `BEGIN`, response ghi vào row trước `COMMIT`, như mục 5 (`IMPLEMENTATION-PLAN.md` mục 3.4).
- Bước 11–13: Owner phải là user đã login ít nhất một lần, nếu không trả `OWNER_USER_NOT_FOUND` (`04-state-machines.md` mục 11). Tạo invitation role OrgOwner thay thế sẽ vi phạm "đúng một OrgOwner" trong lúc chờ.
- Bước 16–19: mọi ref trong grant (shared pool, NetworkPool, StorageClass) phải cùng `cluster_id` với grant, grant có ít nhất một NetworkPool, NetworkPool riêng chỉ vào grant của đúng Org (`02-data-model.md` mục 3) [5.2][7.1]. Dedicated node không nằm trong thao tác này: cấp sau qua mục 2b.
- Bước 5–24 là một transaction, giống cạnh approve ở mục 1b, để Org không bao giờ `Active` mà thiếu Owner, default grant hay group mặc định.

### 2b. Nâng quota ClusterGrant và cấp dedicated node

**Mục đích:** Admin nâng quota nhưng không được hạ dưới tổng đã giữ chỗ; cấp node là một Operation: controller kiểm node không còn Pod tenant nào rồi mới gắn label/taint Organization [6.3][6.4]. API không có credential nào vào Cluster — mọi thứ đi qua CR `PlatformNodeGrant`.

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant PW as Outbox / Provisioning Worker
  participant K8S as kube-apiserver
  participant CTRL as Workspace Controller

  U->>SPA: Admin sửa quota ClusterGrant của acme
  SPA->>API: PUT /admin/organizations/acme/cluster-grants/cluster-a
  API->>DB: BEGIN, SELECT organization_cluster_grants FOR UPDATE
  API->>DB: SUM các Workspace chưa Deleted: CPU/RAM/Pod chỉ Workspace Shared, số Workspace, Σ namespace_quota, storage mọi Workspace
  alt Một con số mới thấp hơn tổng đã giữ chỗ
    API->>DB: ROLLBACK
    API-->>SPA: 409 CLUSTER_GRANT_BELOW_ALLOCATION kèm số tối thiểu
  else Hợp lệ
    API->>DB: UPDATE grant, INSERT audit cluster_grant.update (trước/sau), COMMIT
    API-->>SPA: 200 grant mới
  end
  Note over U,SPA: Trước đó Admin đã bớt worker-a-05 khỏi shared pool, drain rồi uncordon node
  U->>SPA: Admin chọn node worker-a-05 cấp cho acme
  SPA->>API: PUT .../cluster-grants/cluster-a/dedicated-nodes
  API->>DB: BEGIN, SELECT cluster_nodes (cluster-a, worker-a-05) FOR UPDATE
  alt Node không phải Worker, còn shared_pool_id hoặc đang có node grant chưa revoke
    API->>DB: ROLLBACK
    API-->>SPA: 409 CONFLICT
  end
  API->>DB: INSERT dedicated_node_grants node_state=NotReady
  API->>DB: INSERT operations node.grant, outbox node.grant, audit node_grant.create
  API->>DB: COMMIT
  API-->>SPA: 202 operationId
  PW->>DB: Lấy outbox node.grant
  PW->>K8S: Apply PlatformNodeGrant nodegrant-a1 (node, org id)
  K8S->>CTRL: Watch event
  CTRL->>K8S: GET node, LIST pod trên node
  alt Node NotReady, cordoned, control-plane/system hoặc còn label tenantry.io/compute-pool
    CTRL->>K8S: status Ready=False reason NodeNotReady hoặc NodeNotEligible
    PW->>DB: operations Failed DEDICATED_NODE_NOT_READY
  else Còn Pod tenant bất kỳ, kể cả Pod Shared của chính acme
    CTRL->>K8S: status Ready=False reason NodeNotClean + danh sách pod
    PW->>DB: operations Failed DEDICATED_NODE_NOT_CLEAN
  else Node sạch
    CTRL->>K8S: Patch label + taint tenantry.io/dedicated-org=org-01jabc
    CTRL->>K8S: status Ready=True, allocatable của node
    K8S->>PW: Watch status
    PW->>DB: UPDATE node grant allocatable, node_state=Ready
    PW->>DB: operations Succeeded
  end
```

**Điểm cần review**

- Bước 3–8: đối xứng với rule Workspace không giảm quota dưới usage; con số nào tính trên Workspace nào theo [5.5]. Phép `SUM` có từ M7, cùng transaction giữ chỗ (`IMPLEMENTATION-PLAN.md` M5, M7).
- Bước 11–13: API khóa row `cluster_nodes` trước khi tạo grant (thứ tự lock ở `02-data-model.md` mục 4), nên một node không thể vừa vào shared pool vừa được cấp dedicated; unique `(cluster_id, node_name)` của grant chưa revoke là lưới an toàn. Operator kiểm lại trên Cluster ở bước 21–25.
- Bước 14 và 29: grant mới có `node_state=NotReady` cho tới khi controller xác nhận, nên wizard chưa cho chọn node và reservation bị `DEDICATED_NODE_NOT_READY` — không cần thêm state nào cho "đang cấp".
- Bước 21–25: node phải `Ready`, schedulable, không phải control-plane/system, không mang label `tenantry.io/compute-pool` và không còn Pod tenant nào (Pod trong Namespace có label `capsule.clastix.io/tenant`), kể cả Pod Shared của chính acme; Pod hệ thống (CNI, CSI, monitoring, DaemonSet) không tính [6.3]. Quy trình của Admin: bớt node khỏi pool trên portal → drain → uncordon → cấp; node không còn label pool nên Pod Shared không quay lại. Operation `Failed` thì Admin dọn node rồi retry.
- Controller tiếp tục cập nhật `status.allocatable` và Ready của `PlatformNodeGrant`; worker chép về DB, đó là nguồn của `capacity_state` và drift `Overcommitted` (`04-state-machines.md` mục 5).
- Revoke node grant đặt `revoked_at` và bị chặn khi còn reservation chưa `Released` [6.3] (`02-data-model.md` mục 3).
- Sơ đồ là trạng thái từ M8, khi operator kiểm node và gắn label/taint. M5 chỉ ghi DB, chưa có Operation/outbox; từ M6 worker dựng `PlatformNodeGrant` từ DB lúc khởi động và mỗi lần resync (`IMPLEMENTATION-PLAN.md` M5, M6).

---

## 3. Invitation: tạo link → invitee login → accept theo email đã verify

**Mục đích:** Owner tạo invite link one-time có expiry; membership chỉ được tạo khi email đã verify của account đang login khớp email trong invitation [9.1].

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant KC as Keycloak
  participant GS as Group Sync Worker

  U->>SPA: OrgOwner nhập email joe@example.com, role, group
  SPA->>API: POST /api/v1/organizations/acme/invitations
  Note over SPA,API: header Idempotency-Key
  API->>DB: BEGIN, INSERT idempotency_keys (user, key) ON CONFLICT DO NOTHING
  alt Key đã có, cùng method, path và request hash
    API->>DB: ROLLBACK
    API-->>SPA: Invitation đã tạo, không kèm link, Idempotent-Replayed: true
  else Key đã có, khác request
    API->>DB: ROLLBACK
    API-->>SPA: 422 IDEMPOTENCY_KEY_REUSED
  end
  API->>DB: Authz: member (404) → OrgOwner/OrgAdmin (403) → acme Active (409 ORG_NOT_ACTIVE), lỗi thì ROLLBACK
  API->>DB: SELECT organization_entitlements FOR UPDATE, COUNT organization_members + 1 ≤ maxMembers?
  alt Đã đủ maxMembers
    API->>DB: ROLLBACK
    API-->>SPA: 409 ENTITLEMENT_EXCEEDED (maxMembers)
  end
  API->>DB: INSERT invitations token_hash, role, group_ids, expires_at, state=Pending
  API->>DB: INSERT audit invitation.create
  API->>DB: UPDATE idempotency_keys (response 201 đã bỏ link), COMMIT
  API-->>SPA: 201 kèm invite link chứa token thô một lần
  SPA-->>U: Hiển thị link, owner copy gửi tay
  Note over U,KC: Joe nhận link ngoài băng, email thật là stretch
  U->>SPA: Joe mở link /invitations/TOKEN
  SPA->>API: Chưa có phiên thì GET /auth/login, giữ returnTo
  API-->>U: 302 Keycloak, Joe login hoặc register + verify
  Note over API,KC: Flow 1a chạy, Joe có user record và phiên
  SPA->>API: GET /api/v1/invitations/TOKEN (preview, không accept)
  API->>DB: SELECT theo hash token: state=Pending, chưa quá expires_at
  alt Token sai, hết hạn, đã dùng hoặc bị thu hồi
    API-->>SPA: 404 INVITATION_INVALID
  end
  API-->>SPA: Org name, role, email được mời đã che một phần
  U->>SPA: Bấm Accept
  SPA->>API: POST /api/v1/invitations/TOKEN:accept
  API->>DB: BEGIN, tìm invitation theo hash, khóa organization_entitlements của Org rồi invitation FOR UPDATE
  API->>API: Kiểm state, expiry, so email đã verify của user với invitation.email
  alt Không còn Pending hoặc đã hết hạn
    API->>DB: ROLLBACK
    API-->>SPA: 404 INVITATION_INVALID
  else Email không khớp hoặc email_verified=false
    API->>DB: ROLLBACK, rồi INSERT audit invitation.accept result Refused trong transaction riêng
    API-->>SPA: 403 INVITATION_EMAIL_MISMATCH
    SPA-->>U: Báo đăng nhập đúng account được mời
  else Email khớp
    API->>DB: COUNT organization_members + 1 ≤ maxMembers?
    alt Đã đủ maxMembers
      API->>DB: ROLLBACK
      API-->>SPA: 409 ENTITLEMENT_EXCEEDED (maxMembers)
    end
    API->>DB: INSERT organization_members role từ invitation
    opt Invitation có group đi kèm
      API->>DB: Upsert group_members deleted_at=NULL, sync_status=Pending
    end
    API->>DB: UPDATE invitations state=Accepted, accepted_by
    API->>DB: INSERT audit invitation.accept, COMMIT
    API-->>SPA: 200 membership
    SPA-->>U: Organization switcher có thêm acme
    GS->>DB: Poll group_members Pending
    GS->>KC: Add Joe vào group Keycloak org_01jabc:group_...
    GS->>DB: UPDATE sync_status=Synced
  end
```

**Điểm cần review**

- Bước 3–7 và 14: `Idempotency-Key` bắt buộc với `POST …/invitations`, giành ngay sau `BEGIN` như mục 5. Response lưu trong `idempotency_keys` **không** chứa link/token, nên replay chỉ trả invitation và UI báo "link đã tạo trước đó, tạo link mới (và thu hồi link cũ)" (`IMPLEMENTATION-PLAN.md` mục 3.4).
- Bước 12 và 15: DB chỉ lưu hash; token thô chỉ xuất hiện đúng một lần trong response 201 và không được log [20.1].
- Bước 8: thứ tự kiểm membership (404) → role (403) → trạng thái (409) [9.2]; Organization chưa `Active` không mời được.
- Bước 9–11 và 33–35: `maxMembers` kiểm lúc tạo invite và kiểm lại lúc accept, dưới lock `organization_entitlements` (`02-data-model.md` mục 4), vì số member có thể đổi trong lúc chờ và hai accept đồng thời không được cùng lọt.
- Bước 17–23: preview chỉ dành cho người đã login và không accept.
- Bước 26 và 38: khóa row invitation rồi chuyển `state` `Pending → Accepted` bảo đảm one-time khi hai tab cùng bấm Accept (`04-state-machines.md` mục 7).
- Bước 27–32: so khớp với email đã verify trong `users` (chép từ ID token lúc login), so sánh case-insensitive; không có nhánh "owner xác nhận identity" [9.1]. Mismatch không đốt invitation để Joe login lại đúng account. Audit của lần bị từ chối ghi sau `ROLLBACK` trong transaction riêng, nếu không sẽ mất theo rollback.
- Bước 37: Joe có thể còn row tombstone trong `group_members` từ lần rời trước, nên dùng upsert (`02-data-model.md` mục 2).
- Nếu Joe đã là member acme, accept trả 409, không tạo membership trùng và không đổi role ngầm.

---

## 4. Gỡ user khỏi group → Keycloak sync → logout → contract thu hồi

**Mục đích:** Gỡ user khỏi group là một tombstone trong DB; Group Sync Worker gỡ trên Keycloak, bảo trì group `tenantry:tenant-users` và logout user; token cũ vẫn hợp lệ tới TTL 5 phút [9.3][11.4]. Contract và hệ quả: `06-security-and-identity.md` mục 5.

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant GS as Group Sync Worker
  participant KC as Keycloak
  participant KL as kubectl + kubelogin
  participant PROXY as Capsule Proxy
  participant K8S as kube-apiserver

  U->>SPA: Alice remove Joe khỏi group developers
  SPA->>API: DELETE /api/v1/groups/grp-dev/members/joe
  API->>API: Authz OrgOwner/OrgAdmin của acme
  API->>DB: BEGIN
  API->>DB: UPDATE group_members deleted_at=now, sync_status=Pending
  API->>DB: INSERT audit group_member.remove, COMMIT
  API-->>SPA: 200, Joe không còn trong danh sách member
  GS->>DB: Lấy group_members Pending tới hạn (SKIP LOCKED)
  GS->>KC: Admin API remove Joe khỏi org_01jabc:group_01jdev
  GS->>DB: Joe còn binding Workspace nào qua group khác?
  alt Joe không còn binding Workspace nào
    GS->>KC: Remove Joe khỏi group tenantry:tenant-users
  end
  opt Group developers đang có binding Workspace
    GS->>KC: Logout Joe: kết thúc mọi SSO session
  end
  alt Keycloak lỗi tạm thời
    GS->>DB: sync_status=Error, sync_attempts+1, next_sync_at theo backoff
  else Thành công
    GS->>DB: DELETE row group_members
  end
  Note over KL,K8S: Token cũ còn hiệu lực tối đa 5 phút
  KL->>PROXY: kubectl với token cũ còn group claim
  PROXY->>K8S: Forward token của user, JWT còn đúng signature và expiry
  K8S-->>KL: 200 vẫn truy cập được cho tới expiry
  KL->>KC: Refresh token khi access token hết hạn
  KC-->>KL: 400 session đã kết thúc, cần login lại
  KL->>KC: Browser login lại
  KC-->>KL: Token mới không còn group developers
  KL->>PROXY: kubectl get pods -n acme-solar-frontend
  PROXY->>K8S: Forward với identity mới
  K8S-->>KL: 403 Forbidden
```

**Điểm cần review**

- Bước 5–7: sau commit, Joe mất group ngay với portal (mọi query lọc `deleted_at IS NULL`); row tombstone chỉ còn là việc chờ worker. Nếu Keycloak lỗi lâu thì Kubernetes vẫn thấy group cũ khi Joe login lại — cần alert `sync lag` [21.2].
- Bước 10–11: `tenantry:tenant-users` là điều kiện Capsule coi subject là Capsule user; chỉ gỡ khi user không còn **bất kỳ** binding Workspace nào trên platform, không tính theo Cluster [9.3].
- Bước 12: logout chặn refresh/new token, không làm JWT đã phát hành hết hạn sớm. Nó kết thúc **mọi** phiên của Joe: portal, Headlamp và kubectl của các Organization khác cũng phải login lại. Bước 18–21 là hành vi đúng thiết kế, phải ghi trên UI [19.2].
- Bước 13–14: retry với backoff; mọi lời gọi Keycloak ở bước 9, 11, 12 idempotent nên chạy lại từ đầu là an toàn.
- Bước 15–24: contract "mất quyền Kubernetes chậm nhất sau TTL" chỉ đúng vì TenantOwner không tự bind được user hay group ngoài Organization bằng RoleBinding của riêng họ (luật admission ở `06-security-and-identity.md` mục 4).
- Remove member khỏi Organization và member tự rời (mục 11b) đi cùng đường này cho mọi group của Organization mà user thuộc.
- Đo "thời gian từ group change tới token mới có hiệu lực" là một chỉ số đánh giá [24.6].

---

## 5. Tạo Workspace Shared: transaction reservation → outbox → CR → controller → Ready

**Mục đích:** Một request tạo Workspace Shared đi qua validate, một transaction khóa entitlement + ClusterGrant + soft-check pool + auto-allocate CIDR + ghi outbox, rồi worker apply CR và controller reconcile toàn bộ tài nguyên [5.3][7.2][14.3][17.1].

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant PW as Outbox / Provisioning Worker
  participant K8S as kube-apiserver
  participant CTRL as Workspace Controller
  participant CAP as Capsule
  participant CAL as Calico

  U->>SPA: Wizard Solar, Shared, 2 CPU / 4Gi (limits 4 CPU / 8Gi), namespace frontend, auto network
  SPA->>API: POST /organizations/acme/workspaces:validate
  API-->>SPA: 200 hợp lệ, remaining quota, pool usage
  U->>SPA: Bấm Create
  SPA->>API: POST /organizations/acme/workspaces
  Note over SPA,API: Header Idempotency-Key = UUID sinh ở client
  API->>DB: BEGIN
  API->>DB: INSERT idempotency_keys (user, key) ON CONFLICT DO NOTHING
  alt Key đã có, cùng method, path và request hash
    API->>DB: ROLLBACK
    API-->>SPA: Trả lại đúng response đã lưu, Idempotent-Replayed: true
  else Key đã có, khác request
    API->>DB: ROLLBACK
    API-->>SPA: 422 IDEMPOTENCY_KEY_REUSED
  end
  API->>DB: Authz: member (404) → OrgOwner/OrgAdmin (403) → acme Active (409 ORG_NOT_ACTIVE), lỗi thì ROLLBACK
  API->>DB: SELECT organization_entitlements FOR UPDATE, COUNT Workspace chưa Deleted của acme + 1 ≤ maxWorkspacesTotal?
  API->>DB: Slug solar không lồng tiền tố với Workspace chưa Deleted nào của acme (solar / solar-prod)?
  API->>DB: SELECT clusters cluster-a FOR SHARE, Ready?
  alt Vượt maxWorkspacesTotal, slug lồng tiền tố hoặc Cluster không Ready
    API->>DB: ROLLBACK
    API-->>SPA: 409 ENTITLEMENT_EXCEEDED, SLUG_TAKEN hoặc CLUSTER_NOT_READY
  end
  API->>DB: SELECT organization_cluster_grants FOR UPDATE, grant có shared pool shared-general?
  API->>DB: SUM Workspace chưa Deleted trong grant + request: CPU/RAM/Pod chỉ Workspace Shared, số Workspace, Σ namespace_quota, storage mọi Workspace
  alt Grant không có pool đó hoặc vượt grant
    API->>DB: ROLLBACK
    API-->>SPA: 422 VALIDATION_FAILED hoặc 409 CLUSTER_GRANT_QUOTA_EXCEEDED
  end
  API->>DB: SELECT shared_compute_pools shared-general FOR UPDATE
  API->>DB: SUM tài nguyên mọi Workspace chưa Deleted trên pool
  Note over API,DB: Soft check Σ ≤ ratio × (allocatable − systemReserve)
  alt Vượt ratio × reservable
    API->>DB: ROLLBACK
    API-->>SPA: 409 COMPUTE_POOL_CAPACITY_EXCEEDED
  end
  API->>DB: Group TenantOwner thuộc acme, sync_status=Synced? (đọc, chưa khóa)
  alt Thiếu Owner group hoặc group chưa sync
    API->>DB: ROLLBACK
    API-->>SPA: 409 LAST_OWNER_BINDING hoặc GROUP_SYNC_PENDING
  end
  Note over API,DB: Các bước mạng dưới đây có từ M9
  API->>DB: SELECT network_pools onboarding-pod-network FOR UPDATE
  API->>DB: Chọn /24 trống đầu tiên không overlap trong cluster-a
  alt Pool hết CIDR
    API->>DB: ROLLBACK
    API-->>SPA: 409 NETWORK_POOL_EXHAUSTED
  end
  API->>DB: INSERT network_allocations state=Allocated
  API->>DB: INSERT workspaces k8s_name=acme-solar state=Provisioning, workspace_namespaces acme-solar-frontend Desired
  API->>DB: SELECT groups FOR SHARE (kiểm lại deleted_at IS NULL, Synced), INSERT workspace_group_bindings, group_members của các group đó sync_status=Pending
  API->>DB: INSERT operations workspace.create state=Pending
  API->>DB: INSERT outbox workspace.apply generation=1
  API->>DB: INSERT audit workspace.create, UPDATE idempotency_keys (response 202), COMMIT
  API-->>SPA: 202 workspaceId + operationId
  SPA->>API: GET /operations/op-1/events (SSE) hoặc poll GET /operations/op-1
  PW->>DB: BEGIN, lấy outbox tới hạn FOR UPDATE SKIP LOCKED
  PW->>K8S: Server-side apply PlatformWorkspace acme-solar
  Note over PW,K8S: Credential chỉ có quyền CRD tenantry.io
  alt kube-apiserver lỗi hoặc worker chết giữa apply
    PW->>DB: attempts+1, next_attempt_at theo backoff, COMMIT
    Note over PW,DB: DB đã commit, reservation giữ nguyên, apply lại
  else Apply thành công
    PW->>DB: outbox processed, operations Running, COMMIT
  end
  K8S->>CTRL: Watch event PlatformWorkspace generation=1
  CTRL->>CAP: Apply Tenant acme-solar: owners (group Owner + SA operator), additionalRoleBindings Developer/Viewer, nodeSelector pool (từ M8)
  CTRL->>CAP: Tenant resourceQuotas scope Tenant 2 CPU / 4Gi, limits 4 CPU / 8Gi, 20 pods
  CTRL->>CAL: Apply IPPool acme-solar cidr 10.240.12.0/24 (từ M9)
  CTRL->>CAP: Apply TenantResource NetworkPolicy baseline (từ M9)
  CTRL->>K8S: Create Namespace acme-solar-frontend theo spec.namespaces
  CTRL->>K8S: Update status phase=Ready, conditions True
  K8S->>PW: Watch status PlatformWorkspace
  PW->>DB: UPDATE workspaces Ready, workspace_namespaces Active, operations Succeeded
  API-->>SPA: SSE event operation Succeeded
  SPA-->>U: Workspace Ready, bật kubeconfig và Web Console
```

**Điểm cần review**

- Bước 6–11 và 38: `Idempotency-Key` được giành theo `(user, key)` ngay đầu transaction tạo Workspace và response được ghi vào row trước khi commit, nên retry của client nhận lại đúng response cũ và không tạo Workspace thứ hai [14.4]. Cơ chế: `IMPLEMENTATION-PLAN.md` mục 3.4.
- Bước 6–38 là **một** transaction, lock theo thứ tự cố định ở `02-data-model.md` mục 4: `organization_entitlements` (13) → `clusters` FOR SHARE (15) → grant (18) → shared pool (22) → NetworkPool (29) → `groups` FOR SHARE (35). Pool cũng phải khóa ghi, nếu không hai Organization cùng tạo trên một pool sẽ cùng đọc một tổng và cùng vượt trần; exclusion constraint trên `network_allocations` là lớp chặn overlap thứ hai [5.3][7.2].
- Bước 12: thứ tự kiểm membership (404) → role (403) → trạng thái (409) [9.2].
- Bước 13–17: `maxWorkspacesTotal` kiểm dưới lock `organization_entitlements`; lock này cũng xếp hàng mọi lần tạo Workspace của acme, nên kiểm slug lồng tiền tố ở bước 14 không bị race. Slug lồng tiền tố tại dấu `-` (`solar` và `solar-prod`) bị từ chối vì `forceTenantPrefix` của Capsule sẽ cho Workspace này tạo Namespace trong vùng tên của Workspace kia [4.4]; `UNIQUE (k8s_name)` là lưới an toàn.
- Bước 18–21: Shared dùng được khi grant có shared pool [5.2]. Workspace Shared trừ CPU/RAM/Pod vào quota của grant; số Workspace, Σ `namespace_quota` và storage trừ vào grant với cả hai mode. Vượt grant trả `CLUSTER_GRANT_QUOTA_EXCEEDED` với `details.resource`, `limit`, `allocated`, `requested`.
- Bước 22–25: shared là soft allocation — vượt `ratio × reservable` mới từ chối; portal cảnh báo khi pool gần trần nhưng Pod `Pending` khi mọi Workspace dùng hết quota là hành vi chấp nhận [6.2].
- Bước 29–33 và 48–49 có từ M9: Workspace tạo ở M6–M8 không có `network_allocations`, IPPool hay NetworkPolicy baseline, nên chưa được cô lập mạng (`IMPLEMENTATION-PLAN.md` M9).
- Bước 34: body có thể kèm `namespaces` (hậu tố, ví dụ `frontend`); số lượng không vượt `namespaceQuota`, được tạo cùng Workspace qua `spec.namespaces`.
- Bước 35: tập binding thay đổi nên mọi `group_members` còn sống của các group được bind chuyển `Pending`, để Group Sync tính lại `tenantry:tenant-users` ([9.3], `IMPLEMENTATION-PLAN.md` mục 5.2).
- Bước 41–44: "DB đã commit nhưng apply thất bại" được cứu bằng outbox retry; reservation **không** bị release vì controller lỗi tạm thời [17]. Server-side apply với field manager riêng bảo đảm apply lại không tạo trùng.
- Bước 45–51: controller không đọc DB; mọi thứ cần thiết nằm trong `spec` của CR. ServiceAccount của operator là owner của mọi Tenant nên tạo được Namespace qua Capsule. Tenant `nodeSelector` có từ M8.
- `Validating` và `Reserving` chỉ tồn tại trong request; row `workspaces` đầu tiên được ghi đã ở `Provisioning` (`04-state-machines.md` mục 1).

---

## 6. Tạo Workspace Dedicated: row lock chống overbook và membership label

**Mục đích:** Hai request đồng thời cùng reserve capacity trên `worker-a-05`; transaction khóa theo thứ tự cố định nên request thứ hai chờ, đọc lại tổng reservation dưới lock và bị từ chối `DEDICATED_NODE_INSUFFICIENT_CAPACITY`; controller gắn membership label lên node [5.3][6.3][6.4]. Sơ đồ là trạng thái từ M8.

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant API as Platform API
  participant DB as PostgreSQL
  participant PW as Outbox / Provisioning Worker
  participant K8S as kube-apiserver
  participant CTRL as Workspace Controller
  participant CAP as Capsule
  participant CAL as Calico

  Note over U,DB: worker-a-05 reservable 8 CPU, Water đã reserve 4 CPU
  par Request A tạo Green reserve 2 CPU
    U->>API: POST workspaces Dedicated nodegrant-a1, 2 CPU
    API->>API: Dedicated ép limits = requests, Guaranteed QoS
    API->>DB: BEGIN, giành Idempotency-Key, SELECT organization_entitlements FOR UPDATE
  and Request B tạo Blue reserve 3 CPU
    U->>API: POST workspaces Dedicated nodegrant-a1, 3 CPU
    API->>API: Dedicated ép limits = requests
    API->>DB: BEGIN, giành Idempotency-Key, SELECT organization_entitlements FOR UPDATE
    Note over API,DB: B chờ ngay tại đây vì A đang giữ row entitlement của acme
  end
  API->>DB: A: clusters FOR SHARE, ClusterGrant FOR UPDATE, còn chỗ cho số Workspace, Σ namespace_quota, storage
  API->>DB: A: SELECT dedicated_node_grants nodegrant-a1 FOR UPDATE, thuộc acme + cluster-a, node_state=Ready?
  API->>DB: A: SUM reservations chưa Released (Pending/Active/Releasing) = 4, remaining 4
  API->>DB: A: 4 + 2 ≤ 8 → INSERT workspace_node_reservations Pending, capacity_state=PartiallyAllocated
  API->>DB: A: allocate CIDR (từ M9), INSERT workspaces green, op, outbox, audit
  API->>DB: A: COMMIT, giải phóng lock
  API-->>U: A: 202 operationId
  API->>DB: B: nhận lock, đi lại cùng chuỗi lock, đọc lại SUM reservations = 6, remaining 2
  API->>DB: B: 6 + 3 = 9 > 8 → ROLLBACK
  API-->>U: B: 409 DEDICATED_NODE_INSUFFICIENT_CAPACITY
  PW->>DB: Lấy outbox workspace.apply acme-green
  PW->>K8S: Apply PlatformWorkspace compute.mode=Dedicated
  Note over PW,K8S: spec.compute nodeName=worker-a-05, nodegrant-a1
  K8S->>CTRL: Watch event
  CTRL->>K8S: GET node worker-a-05, đọc label dedicated-org
  alt Node thiếu org label/taint hoặc thuộc Org khác
    CTRL->>K8S: status ComputeReady=False reason NodeNotGranted
    K8S->>PW: Watch status
    PW->>DB: Quá ngưỡng thì workspace và operation Failed, reservation giữ nguyên
  else Node đúng Organization
    CTRL->>K8S: Patch label workspaces.tenantry.io/ws-01jgrn=true
    CTRL->>CAP: Tenant nodeSelector theo membership label
    CTRL->>CAP: quota chung = reservation 2 CPU, LimitRange maxLimitRequestRatio 1 (Guaranteed)
    CTRL->>CAL: IPPool nodeSelector theo membership label (từ M9)
    CTRL->>K8S: status phase=Ready, DedicatedNodeCapacityReserved, ComputeReady
    K8S->>PW: Watch status
    PW->>DB: UPDATE reservation Pending → Active, workspace Ready
  end
```

**Điểm cần review**

- Bước 3 và 6: transaction tạo Workspace khóa theo thứ tự cố định ở `02-data-model.md` mục 4 (`organization_entitlements` → `clusters` → ClusterGrant → `dedicated_node_grants` → NetworkPool), nên hai request của cùng Organization xếp hàng ngay ở row entitlement. Row `dedicated_node_grants` vẫn phải khóa vì resize, revoke và worker (chép allocatable) cũng ghi vào nó. Invariant `Σ reservation ≤ allocatable − systemReserve` được enforce bằng lock + đọc lại sau khi nhận lock (bước 14), không bằng `CHECK`.
- Bước 7: Workspace Dedicated không trừ CPU/RAM/Pod vào grant; chỉ số Workspace, Σ `namespace_quota` và storage tính chung với Shared [5.2].
- Bước 9: tổng gồm mọi reservation chưa `Released`, kể cả `Releasing` của Workspace đang xóa — capacity chỉ hoàn lại khi Workspace đó đã dọn xong (`04-state-machines.md` mục 3).
- Bước 10: reservation `Pending` được tính vào tổng ngay khi commit, không chờ Workspace Ready, và `capacity_state` tính lại trong cùng transaction; đây là lý do "reservation giữ nguyên khi provisioning lỗi" [17].
- Bước 2 và 5: Dedicated là hard reservation, ratio cố định 1, `requests = limits`; client gửi limits khác bị từ chối [5.4][16.4]. LimitRange với `maxLimitRequestRatio` 1 giữ Guaranteed QoS cho từng container.
- Bước 24–28: controller chỉ **thêm** label membership của Workspace, không đụng label Organization và label của Water [6.4]. `PodNodeSelector` và `PodTolerationRestriction` (annotation trên Namespace Dedicated) inject selector và toleration của Organization; kubelet kiểm lại nodeSelector nên `nodeName` không đưa Pod ra khỏi node được (`05-network-and-scheduling.md` mục 2).
- Bước 20–23: nếu node bị Admin đổi Org hoặc mất label (drift), Workspace chuyển `Degraded`/`Failed` với condition rõ, không tự chọn node khác [6.3][17].
- Bước 30: reservation `Pending → Active` khi Workspace `Ready` lần đầu (`04-state-machines.md` mục 3).
- Response B kèm `details.remaining` (CPU/RAM/Pod còn lại của node) để portal gợi ý allocation vừa đủ.

---

## 7. Namespace: tạo từ portal qua CR và Namespace tạo bằng kubectl hiện lên portal

### 7a. Tạo Namespace từ portal

**Mục đích:** Portal không gọi Kubernetes bằng token user; authz ở API (OrgOwner/OrgAdmin hoặc member group TenantOwner), patch `spec.namespaces` qua outbox, controller tạo Namespace dưới Tenant, `status.namespaces` sync về DB [4.5][FR-06].

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant PW as Outbox / Provisioning Worker
  participant K8S as kube-apiserver
  participant CTRL as Workspace Controller
  participant CAP as Capsule

  U->>SPA: Tab Namespaces của Solar, nhập frontend
  SPA->>API: POST /workspaces/ws-01jsol/namespaces name=frontend
  API->>DB: Membership và role của user trong acme
  API->>DB: Group của user có binding TenantOwner tại Solar?
  alt Không phải OrgOwner/OrgAdmin, không thuộc group TenantOwner
    API-->>SPA: 403 FORBIDDEN, Developer/Viewer chỉ xem
  end
  API->>DB: BEGIN, SELECT workspaces FOR UPDATE
  alt Workspace không Ready
    API->>DB: ROLLBACK
    API-->>SPA: 409 WORKSPACE_NOT_READY
  end
  API->>DB: COUNT workspace_namespaces chưa Deleted + 1 ≤ namespace_quota của Solar?
  alt Vượt namespace quota của Workspace
    API->>DB: ROLLBACK
    API-->>SPA: 409 NAMESPACE_QUOTA_EXCEEDED
  end
  API->>DB: INSERT workspace_namespaces acme-solar-frontend source=portal state=Desired
  API->>DB: INSERT operations namespace.create, outbox workspace.apply, audit namespace.create
  API->>DB: COMMIT
  API-->>SPA: 202 operationId
  PW->>DB: Poll outbox, đọc desired namespaces của Solar
  PW->>K8S: Server-side apply spec.namespaces += frontend, gen 2
  K8S->>CTRL: Watch event generation=2
  CTRL->>K8S: Create Namespace acme-solar-frontend (SA operator là owner của Tenant)
  Note over CTRL,CAP: Namespace mang label tenant acme-solar + ownerRef
  K8S->>CAP: Admission webhook Capsule
  CAP-->>K8S: Chấp nhận, prefix và quota Tenant hợp lệ
  CAP->>K8S: Gắn metadata của Tenant (annotation IPPool từ M9), replicate RoleBinding, LimitRange, NetworkPolicy (từ M9)
  CTRL->>K8S: status.namespaces += acme-solar-frontend source=portal
  K8S->>PW: Watch status
  PW->>DB: UPDATE workspace_namespaces Active, operation Succeeded
  API-->>SPA: SSE operation Succeeded
  SPA-->>U: Namespace hiện trong danh sách kèm usage
```

**Điểm cần review**

- Bước 3–5: authz hai nguồn — role Organization **hoặc** binding TenantOwner của group user thuộc; Developer/Viewer chỉ đọc. Đây là logic DB, không hỏi Kubernetes.
- Bước 6–8: Workspace đang có Operation chạy cũng trả `WORKSPACE_NOT_READY` (`04-state-machines.md` mục 2).
- Bước 9–11: quota Namespace check ở DB theo `namespace_quota` của Workspace (đếm cả Namespace `source=kubectl`); trần của grant là Σ `namespace_quota`, đã kiểm lúc tạo/resize Workspace [5.3]. Capsule kiểm lại ở bước 20–21; nếu hai lớp lệch nhau (ví dụ kubectl vừa tạo thêm), controller báo condition thay vì tạo lén.
- Bước 12 và 17: `workspace_namespaces.name` giữ tên đầy đủ, `spec.namespaces` giữ hậu tố (`02-data-model.md`).
- Bước 19: ServiceAccount của operator là Capsule user và owner của mọi Tenant, nên Namespace đi đúng đường owner của Capsule (prefix, quota Tenant); cấu hình Capsule `users` và kiểm lại trên Capsule 0.15.x ở M6 (`IMPLEMENTATION-PLAN.md` M6).
- Bước 22: annotation IPPool do Capsule gắn từ `Tenant.namespaceOptions.additionalMetadata` mà controller render, không phải controller patch Namespace; tenant bị Capsule cấm sửa annotation này [7.3]. Namespace mới dùng chung quota của Workspace, không nhận phần riêng ([5.4]).
- NFR: Namespace create p95 dưới 15 giây [23]; mã `NAMESPACE_QUOTA_EXCEEDED` thêm vào enum `ErrorCode` khi làm endpoint.

### 7b. Namespace tạo bằng kubectl hiện lên portal qua status sync

**Mục đích:** Owner tạo Namespace bằng kubectl/Headlamp vẫn đi qua Capsule; controller quan sát và ghi `status.namespaces` với `source=kubectl`, worker sync về DB, portal hiển thị [4.5][14.2].

```mermaid
sequenceDiagram
  autonumber
  participant KL as kubectl + kubelogin
  participant PROXY as Capsule Proxy
  participant K8S as kube-apiserver
  participant CAP as Capsule
  participant CTRL as Workspace Controller
  participant PW as Outbox / Provisioning Worker
  participant DB as PostgreSQL
  participant API as Platform API
  participant SPA as Portal SPA

  KL->>PROXY: kubectl create ns acme-solar-scratch
  PROXY->>K8S: Forward token của user (identity oidc:sub + groups)
  K8S->>CAP: Admission webhook Namespace
  alt Thiếu tiền tố acme-solar- hoặc vượt quota Tenant
    CAP-->>K8S: Deny kèm lý do
    K8S-->>KL: 403 admission denied
  else Owner group hợp lệ, còn quota
    CAP-->>K8S: Allow, gắn Namespace vào Tenant acme-solar
    CAP->>K8S: Gắn metadata của Tenant, replicate RoleBinding, LimitRange, NetworkPolicy
    K8S-->>KL: namespace/acme-solar-scratch created
  end
  K8S->>CTRL: Watch Namespace có label tenant acme-solar
  CTRL->>K8S: Annotation IPPool nếu thiếu
  CTRL->>K8S: status.namespaces += acme-solar-scratch source=kubectl
  Note over CTRL,K8S: Không thêm vào spec.namespaces, chỉ observed
  K8S->>PW: Watch status thay đổi
  PW->>DB: UPSERT workspace_namespaces source=kubectl
  SPA->>API: GET /workspaces/ws-01jsol/namespaces
  API->>DB: SELECT workspace_namespaces
  API-->>SPA: Danh sách gồm scratch với badge kubectl
```

**Điểm cần review**

- Bước 9–11: controller watch Namespace theo label tenant của Capsule, không theo tên; Namespace `source=kubectl` không được đưa vào `spec.namespaces` để tránh controller "nhận nuôi" rồi xóa khi user bỏ khỏi portal.
- Bước 7: không có Secret TLS nào được replicate vào Namespace của tenant; cert wildcard chỉ nằm ở namespace của platform (`05-network-and-scheduling.md` mục 7).
- Xóa Namespace `source=kubectl` từ portal: API thêm tên đầy đủ vào `spec.namespacesToDelete`, controller xóa rồi dọn; Namespace có PVC yêu cầu gõ tên [4.5].
- Bước 10: Capsule gắn annotation IPPool từ metadata của Tenant (từ M9); controller tự gắn nếu thấy Namespace của Tenant thiếu annotation (Namespace tạo bằng kubectl khi Capsule chưa kịp), test ở M9.
- Nếu Namespace bị xóa bằng kubectl, controller gỡ khỏi `status.namespaces`; với `source=portal`, API bỏ tên khỏi `spec.namespaces` và cảnh báo, controller **không** tạo lại (`04-state-machines.md` mục 9.2).

---

## 8. kubectl access: kubeconfig → kubelogin → Capsule Proxy → RBAC/Capsule

**Mục đích:** Kubeconfig không chứa static token; kubelogin lấy token client `kubectl` qua SSO Keycloak, request đi Capsule Proxy được filter cluster-scoped và RBAC/Capsule quyết định quyền trong Workspace [11][8.4].

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant KL as kubectl + kubelogin
  participant KC as Keycloak
  participant PROXY as Capsule Proxy
  participant K8S as kube-apiserver
  participant CAP as Capsule

  U->>SPA: Tab Access của Solar, Download kubeconfig
  SPA->>API: GET /workspaces/ws-01jsol/kubeconfig
  API->>DB: Authz user có binding tại Solar, lấy proxy endpoint
  API->>DB: INSERT audit kubeconfig.download, không lưu token
  API-->>SPA: kubeconfig server kube-a..., exec kubelogin
  Note over SPA,KL: Không có static token, chỉ exec plugin + CA data
  U->>KL: kubectl get pods -n acme-solar-frontend
  KL->>KL: Chưa có token cache → oidc-login get-token
  KL->>U: Mở browser tới Keycloak, client kubectl, PKCE
  U->>KC: Authorize, mang SSO cookie từ phiên portal
  alt Còn session Keycloak từ portal
    KC-->>U: Không hỏi password, redirect localhost callback
  else Chưa có session
    KC-->>U: Trang login
    U->>KC: Nhập credential
  end
  KC-->>KL: Token aud kubernetes, groups org_01jabc:group_01jdev
  KL->>KL: Cache token + refresh token
  KL->>PROXY: GET /api/v1/namespaces/acme-solar-frontend/pods
  PROXY->>PROXY: Xác thực token, lấy user + groups để lọc
  PROXY->>K8S: Forward token của user
  K8S->>K8S: Xác thực lại JWT (issuer, audience kubernetes), RBAC RoleBinding Developer
  K8S-->>KL: 200 pod list
  U->>KL: kubectl get ns
  KL->>PROXY: GET /api/v1/namespaces
  PROXY->>CAP: Đọc Tenant có owner/binding khớp groups của user
  CAP-->>PROXY: Tenant acme-solar
  PROXY->>K8S: List namespaces
  PROXY-->>KL: Chỉ Namespace của Tenant user có quyền
  U->>KL: kubectl get ns acme-water-frontend
  KL->>PROXY: GET namespace của Workspace khác
  PROXY->>K8S: Forward
  K8S-->>KL: 403 Forbidden
```

**Điểm cần review**

- Bước 3–5: endpoint proxy lấy từ `workspace.cluster_id` → `clusters.proxy_endpoint`; user không nhập endpoint; kubeconfig chứa Workspace ID chỉ để đặt context [11.1][11.2].
- Bước 8–13: SSO hoạt động vì cùng realm và cùng browser; client `kubectl` là public + PKCE, audience `kubernetes`; token `portal-api` không bao giờ được dùng cho Kubernetes [8.4].
- Bước 16–18: Capsule Proxy xác thực token để biết user thuộc Tenant nào rồi forward chính token đó, không impersonation (`05-network-and-scheduling.md` mục 8); kube-apiserver xác thực lại theo structured authentication, nên direct API và Proxy cho cùng kết quả ngoài phần filter [8.4][20.3]. Proxy forward token hay impersonation chưa được kiểm trên lab; kiểm bằng audit log của kube-apiserver trong e2e của M11.
- Bước 20–25: filter list cluster-scoped (Namespace, Node, PV, StorageClass) là việc của Proxy; RBAC không thể làm điều này cho `list` toàn cluster. Node `list` mặc định không được cấp trong Shared mode [6.2].
- Bước 26–29: từ chối đến từ RBAC của kube-apiserver, không phải Proxy; test `kubectl auth can-i` cho matrix TenantOwner/Developer/Viewer [10.5].

---

## 9. Headlamp web console qua SSO

**Mục đích:** Portal mở đúng Headlamp của Cluster chứa Workspace; Headlamp dùng OIDC client `headlamp-cluster-a` với SSO Keycloak, gửi token user tới Capsule Proxy cùng Cluster; không truyền token qua URL [12].

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant HL as Headlamp
  participant KC as Keycloak
  participant PROXY as Capsule Proxy
  participant K8S as kube-apiserver

  U->>SPA: Bấm Open Web Console tại Solar
  SPA->>API: GET /workspaces/ws-01jsol/console-link
  API->>DB: cluster_id → clusters.console_url, status
  alt Cluster Unavailable
    API-->>SPA: 409 CLUSTER_NOT_READY, disable nút kèm lý do
  else Cluster Ready hoặc Maintenance
    API-->>SPA: URL console-a.tenantry.site + deep link
    SPA-->>U: Mở tab mới, không truyền token trong URL
  end
  U->>HL: GET /c/cluster-a/namespaces/acme-solar-frontend
  HL-->>U: 302 Keycloak authorize, client headlamp-cluster-a
  U->>KC: Authorize với SSO cookie đã có từ portal
  alt SSO còn hiệu lực
    KC-->>U: Redirect callback exact của Headlamp, không hỏi lại
  else Hết session
    KC-->>U: Trang login, nhập credential
  end
  U->>HL: Callback kèm code
  HL->>KC: Đổi code bằng client secret headlamp-cluster-a
  KC-->>HL: ID token aud headlamp-cluster-a, groups
  HL-->>U: Set cookie/session Headlamp, render UI
  U->>HL: Xem Pods trong acme-solar-frontend
  HL->>PROXY: Forward request kèm Bearer token của user
  PROXY->>PROXY: Xác thực token aud headlamp-cluster-a, lấy user + groups
  PROXY->>K8S: Forward token của user, filter cluster-scoped theo Tenant
  K8S-->>HL: Pod list theo RBAC của user
  HL-->>U: Chỉ Namespace/Node/PV được phép
```

**Điểm cần review**

- Bước 3–6: URL console lấy từ Cluster registry; Cluster `Maintenance` vẫn cho mở console, chỉ `Unavailable` mới disable [6.1][12.4].
- Bước 7–15: mỗi Cluster một client `headlamp-{clusterId}` confidential, callback exact, audience riêng; structured authentication của Cluster phải chấp nhận audience này [8.4]. Phase 2 thêm Cluster là thêm client.
- Bước 16–21: Headlamp là identity-aware proxy — ServiceAccount của Headlamp không có quyền workload; mọi quyền là của token user [12.4].
- Deep link theo Namespace là tiện ích; user có nhiều Workspace trong Cluster sẽ thấy tất cả resource được phép — không phải lỗi isolation [12.5].
- Không iframe, không fork; mở tab mới để tránh CSP/cookie/callback trong frame [12.2][12.3].

---

## 10. Route: đưa app của tenant ra HTTPS

### 10a. Tạo Route trên portal → Ingress → request tới Pod

**Mục đích:** User khai báo Route trên portal; API ghép hostname và ghi `spec.routes` qua outbox; controller dựng Ingress; request đi qua Traefik với cert wildcard của platform [7.6].

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant PW as Outbox / Provisioning Worker
  participant K8S as kube-apiserver
  participant CTRL as Workspace Controller
  participant ING as Traefik

  U->>SPA: Tab Routes của Solar, namespace frontend, tên web, Service web:80
  SPA->>API: POST /workspaces/ws-01jsol/routes
  API->>DB: Quyền như Namespace (OrgOwner/OrgAdmin/TenantOwner-group)
  API->>DB: BEGIN, SELECT workspaces FOR UPDATE
  alt Workspace không Ready hoặc Namespace không Active
    API->>DB: ROLLBACK
    API-->>SPA: 409 WORKSPACE_NOT_READY
  end
  alt Đã có 20 Route chưa Deleted
    API->>DB: ROLLBACK
    API-->>SPA: 409 ROUTE_QUOTA_EXCEEDED
  end
  API->>API: hostname = web-acme-solar.mvp.tenantry.site
  API->>DB: INSERT workspace_routes state=Desired
  alt Vi phạm UNIQUE hostname
    API->>DB: ROLLBACK
    API-->>SPA: 409 ROUTE_HOSTNAME_TAKEN
  end
  API->>DB: INSERT operations route.create, outbox workspace.apply, audit route.create
  API->>DB: COMMIT
  API-->>SPA: 202 operationId
  PW->>K8S: Server-side apply spec.routes += web
  K8S->>CTRL: Watch event generation mới
  CTRL->>K8S: Apply Ingress web trong acme-solar-frontend (SA operator)
  Note over CTRL,K8S: class platform-shared, không tls.secretName, dùng cert wildcard mặc định của Traefik
  CTRL->>K8S: status.routes += web ready
  K8S->>PW: Watch status
  PW->>DB: UPDATE workspace_routes Active, operation Succeeded
  API-->>SPA: SSE operation Succeeded
  SPA-->>U: Hiện URL https://web-acme-solar.mvp.tenantry.site
  U->>ING: https://web-acme-solar.mvp.tenantry.site
  Note over U,ING: DNS *.mvp.tenantry.site → IP ingress
  ING->>ING: TLS bằng cert wildcard mặc định, khớp host với Ingress
  ING-->>U: 200 response từ Pod của Service web
```

**Điểm cần review**

- Bước 3–8: cùng luật authz và cùng row lock `workspaces` với tạo Namespace (mục 7a); đếm Route dưới lock đó.
- Bước 9–12: user không nhập hostname; trùng chỉ xảy ra khi hai cặp (tên, Workspace) ghép ra cùng một chuỗi, và constraint ở DB quyết định.
- Bước 18: Ingress do operator tạo. Luật `tenantry-tenant-ingress-lockdown` chỉ áp cho người gọi có username `oidc:`, nên operator và các controller của Kubernetes (xóa Ingress khi xóa Namespace) không bị chặn (`05-network-and-scheduling.md` mục 7). Test M11: Capsule không từ chối Ingress do operator tạo; xóa Namespace/Workspace có Route vẫn hoàn tất.
- Bước 24–26: TLS dừng ở Traefik bằng cert wildcard mặc định; không có Secret TLS hay private key nào trong Namespace của tenant [7.6]. NetworkPolicy baseline (từ M9) phải cho phép traffic từ namespace `ingress-system` [7.4]; nếu Service không có Pod, Traefik trả 503 và Route vẫn `Active`.
- Mã `ROUTE_QUOTA_EXCEEDED`, `ROUTE_HOSTNAME_TAKEN` thêm vào OpenAPI khi làm M11.

### 10b. Tenant tự tạo Ingress hoặc Certificate bị admission từ chối

**Mục đích:** Route là đường duy nhất mở HTTP ra ngoài; tenant không tự chiếm hostname hay xin cert được [7.6][20.3].

```mermaid
sequenceDiagram
  autonumber
  participant KL as kubectl + kubelogin
  participant PROXY as Capsule Proxy
  participant K8S as kube-apiserver

  KL->>PROXY: kubectl apply Ingress host web-acme-water.mvp.tenantry.site
  PROXY->>K8S: Forward
  K8S->>K8S: RBAC cho phép (ClusterRole admin có quyền ghi Ingress)
  K8S->>K8S: ValidatingAdmissionPolicy tenantry-tenant-ingress-lockdown
  Note over K8S: Namespace có label capsule.clastix.io/tenant và username người gọi bắt đầu bằng oidc:
  K8S-->>KL: Admission từ chối, dùng Route trên portal
  KL->>PROXY: kubectl apply Certificate dnsNames portal.tenantry.site
  PROXY->>K8S: Forward
  K8S->>K8S: Cùng policy, nhóm cert-manager.io
  K8S-->>KL: Admission từ chối
```

**Điểm cần review**

- Bước 3–4: chặn ở admission chứ không ở RBAC, vì RBAC chỉ cộng quyền và `admin` là role gộp sẵn; định nghĩa policy ở `05-network-and-scheduling.md` mục 7. Mã HTTP trả về do `validations[].reason` của policy quyết định: mặc định `Invalid` (422), đặt `reason: Forbidden` thì 403.
- Bước 6–9: không chặn `Certificate` thì tenant dùng được ClusterIssuer của platform để xin cert cho hostname bất kỳ dưới `tenantry.site`.
- Test isolation "tenant không tạo được Ingress/Certificate" [24.4].
- Custom domain là luồng riêng sau MVP, không có nhánh ở đây [7.6].

---

## 11. Transfer ownership hai bước và member tự rời Organization

### 11a. Transfer ownership: Owner đề nghị, OrgAdmin nhận xác nhận

**Mục đích:** Đúng một OrgOwner mỗi Organization; Owner đề nghị cho một OrgAdmin, người nhận xác nhận, đổi role hai bên trong một transaction [9.2][16.1]. Trạng thái: `04-state-machines.md` mục 8.

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL

  U->>SPA: Alice mở Settings, Transfer ownership tới Bob
  SPA->>API: POST /organizations/acme:transfer-ownership toUserId=bob
  API->>DB: BEGIN, SELECT organizations acme FOR UPDATE, Alice là OrgOwner?
  alt Caller không phải Owner
    API->>DB: ROLLBACK
    API-->>SPA: 403 TRANSFER_NOT_ALLOWED
  end
  API->>DB: Bob là OrgAdmin của acme?
  alt Bob không phải OrgAdmin
    API->>DB: ROLLBACK
    API-->>SPA: 422 RECIPIENT_NOT_ORG_ADMIN
  end
  API->>DB: UPDATE transfer Proposed cũ (nếu có) → Cancelled
  API->>DB: INSERT ownership_transfers from=alice, to=bob, expires_at, state=Proposed
  API->>DB: INSERT audit ownership_transfer.propose, COMMIT
  API-->>SPA: 201 transfer đang chờ Bob xác nhận
  Note over U,SPA: Bob login bằng account của mình
  SPA->>API: GET /organizations/acme/ownership-transfer
  API-->>SPA: Transfer Proposed, chỉ Owner và người nhận thấy
  U->>SPA: Bob bấm Accept trong portal
  SPA->>API: POST /ownership-transfers/{transferId}:accept
  API->>DB: BEGIN, SELECT organizations acme FOR UPDATE, ownership_transfers FOR UPDATE
  alt Không còn Proposed, hết hạn, caller khác to_user_id, Bob không còn OrgAdmin hoặc Alice không còn OrgOwner
    API->>DB: ROLLBACK
    API-->>SPA: 403 TRANSFER_INVALID
  else Hợp lệ
    API->>DB: UPDATE organization_members alice role=OrgAdmin (hạ Owner cũ trước)
    API->>DB: UPDATE organization_members bob role=OrgOwner
    API->>DB: UPDATE organizations owner_user_id=bob
    API->>DB: UPDATE ownership_transfers state=Accepted
    API->>DB: INSERT audit ownership_transfer.accept, COMMIT
    API-->>SPA: 200, Bob là Owner, Alice là OrgAdmin
  end
```

**Điểm cần review**

- Bước 3–8: chỉ Owner gọi được `:transfer-ownership`; OrgAdmin tự gọi là threat case phải test [20.3]. Người nhận bắt buộc là OrgAdmin hiện tại [9.2].
- Bước 9–10: tối đa một transfer `Proposed` mỗi Org (partial unique index), đề nghị mới thay đề nghị cũ. Owner hủy hoặc người nhận từ chối bằng `DELETE /ownership-transfers/{transferId}` → `Cancelled`.
- Bước 13–16: transfer là thông báo trong portal, không có token hay link: người nhận thấy nó qua `GET …/ownership-transfer` và accept bằng `transferId`; API chỉ nhận khi caller là `to_user_id` (`04-state-machines.md` mục 8).
- Bước 17–19: dưới lock, accept kiểm lại mọi guard: còn `Proposed`, chưa hết hạn, Bob vẫn là OrgAdmin, Alice vẫn là OrgOwner. Lock row Organization trước cũng xếp hàng accept với mục 11b, nên người nhận không thể vừa thành Owner vừa rời Org.
- Bước 20–24: một transaction nên không lúc nào Org có 0 hoặc 2 Owner. Thứ tự bắt buộc: hạ Owner cũ trước rồi mới nâng người nhận, vì partial unique index "một OrgOwner mỗi Org" trên `organization_members` không deferrable (`02-data-model.md` mục 2); làm ngược thì câu `UPDATE` đầu đã vi phạm index.
- Owner cũ thành OrgAdmin, không bị mất membership; group membership và binding không đổi [9.2].
- Mã `TRANSFER_NOT_ALLOWED`, `RECIPIENT_NOT_ORG_ADMIN`, `TRANSFER_INVALID` thêm vào enum `ErrorCode` khi làm endpoint.

### 11b. Member tự rời Organization

**Mục đích:** OrgMember/OrgAdmin tự rời Organization; Owner phải transfer trước; rời kéo theo gỡ mọi group của Organization đó [9.4].

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant GS as Group Sync Worker
  participant KC as Keycloak

  U->>SPA: Joe bấm Leave organization tại acme
  SPA->>API: POST /organizations/acme/members/me:leave
  API->>DB: BEGIN, SELECT organizations acme FOR UPDATE, membership của Joe
  alt Joe là OrgOwner
    API->>DB: ROLLBACK
    API-->>SPA: 409 LEAVE_NOT_ALLOWED, cần transfer trước
  else Được phép
    API->>DB: DELETE organization_members của Joe (trước, lấy row lock)
    API->>DB: Tombstone mọi group_members sống của Joe trong acme (câu lệnh sau)
    API->>DB: Transfer Proposed có to_user_id=Joe → Cancelled
    API->>DB: INSERT audit organization_member.leave, COMMIT
    API-->>SPA: 200, switcher bỏ acme
    GS->>DB: Poll group_members Pending
    GS->>KC: Remove Joe khỏi mọi group acme
    opt Joe không còn binding Workspace nào
      GS->>KC: Remove khỏi tenantry:tenant-users
    end
    opt Có group đang giữ binding Workspace
      GS->>KC: Logout Joe
    end
    GS->>DB: DELETE các row group_members tombstone
  end
```

**Điểm cần review**

- Bước 3–5: chỉ một điều kiện chặn — Joe là Owner thì phải transfer trước [9.4]. Role được đọc dưới lock row Organization, cùng lock với accept transfer ở mục 11a.
- Bước 6–10: xóa membership **trước** rồi mới tombstone group member, đúng thứ tự ở `02-data-model.md` mục 2 (chặn race với thêm vào group). Row membership bị xóa hẳn nên Joe mất quyền portal ở acme ngay; lịch sử nằm ở audit. Rời Org không xóa account Keycloak; Joe vẫn thuộc các Org khác [9.4].
- Bước 8: transfer đang chờ Joe nhận bị hủy (`04-state-machines.md` mục 8).
- Bước 11–15: cùng đường sync/logout như mục 4; contract token cũ tới TTL áp dụng y hệt.
- Owner remove member (`DELETE /members/{userId}`) dùng cùng nhánh `else`, thêm rule "không remove được OrgOwner" [9.4].

---

## 12. Delete Workspace: confirm → gỡ quyền → xóa → chờ finalizer → release có thứ tự

**Mục đích:** Xóa ngay và mất dữ liệu PVC sau confirm gõ tên; finalizer gỡ quyền tenant rồi dọn theo thứ tự an toàn, CIDR chỉ release sau khi Calico sạch và đi qua `Quarantined`; finalizer kẹt → Operation `Blocked`, OrgOwner/OrgAdmin retry, chỉ Platform Admin `:force` [7.5][17.4].

```mermaid
sequenceDiagram
  autonumber
  participant U as User / Browser
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant PW as Outbox / Provisioning Worker
  participant K8S as kube-apiserver
  participant CTRL as Workspace Controller
  participant CAP as Capsule
  participant CAL as Calico

  U->>SPA: Danger zone của Water, bấm Delete
  SPA->>API: GET /workspaces/ws-01jxyz/namespaces kèm PVC count
  API-->>SPA: 3 Namespace, 2 PVC sẽ mất vĩnh viễn
  U->>SPA: Gõ lại tên water, xác nhận
  SPA->>API: DELETE /workspaces/ws-01jxyz confirmName=water
  API->>DB: BEGIN, SELECT workspaces FOR UPDATE, authz OrgOwner/OrgAdmin, confirmName khớp slug
  alt Workspace đang Provisioning, Updating hoặc Deleting (còn Operation chưa xong)
    API->>DB: ROLLBACK
    API-->>SPA: 409 WORKSPACE_NOT_READY
  end
  API->>DB: UPDATE workspaces state=Deleting
  API->>DB: workspace_node_reservations Releasing (Dedicated), network_allocations Releasing (từ M9)
  API->>DB: group_members của các group được bind sync_status=Pending
  API->>DB: INSERT operations workspace.delete, outbox workspace.delete, audit workspace.delete, COMMIT
  API-->>SPA: 202 operationId
  PW->>DB: Poll outbox
  PW->>K8S: DELETE PlatformWorkspace acme-water, propagation Background
  Note over PW,K8S: CR có finalizer tenantry.io/workspace của controller
  K8S->>CTRL: Watch deletionTimestamp
  CTRL->>CAP: Gỡ owners và additionalRoleBindings khỏi Tenant, tenant mất quyền
  CTRL->>CAP: Bỏ cordoned của Tenant nếu đang bật
  CTRL->>K8S: DELETE mọi Namespace acme-water-*
  loop Đợi Namespace biến mất
    CTRL->>K8S: GET Namespace, kiểm tra finalizer, PVC, PV
    alt Kẹt quá ngưỡng, ví dụ PV finalizer treo
      CTRL->>K8S: condition Deleting=False reason BlockedByFinalizer kèm object kẹt
      K8S->>PW: Watch status
      PW->>DB: operations state=Blocked, workspaces.conditions kèm object kẹt
      API-->>SPA: SSE Blocked, OrgOwner/OrgAdmin chờ hoặc retry
      U->>SPA: Platform Admin bấm Force
      SPA->>API: POST /operations/op-del:force
      API->>DB: Check platform-admin, operation Running, outbox workspace.force-delete, audit operation.force
      PW->>K8S: Patch CR annotation tenantry.io/force-delete=true
      CTRL->>K8S: Gỡ finalizer của object kẹt
    end
  end
  CTRL->>CAP: DELETE TenantResource, Tenant acme-water, đợi biến mất
  CTRL->>CAL: Đợi không còn WorkloadEndpoint và IP trong CIDR, DELETE IPPool, đợi IPAMBlock hết (từ M9)
  CTRL->>K8S: Gỡ label workspaces.tenantry.io/ws-01jxyz trên worker-a-05
  Note over CTRL,K8S: Giữ dedicated-org label/taint và label của Green
  CTRL->>K8S: Gỡ finalizer CR, CR biến mất
  K8S->>PW: Watch CR deleted
  PW->>DB: BEGIN, khóa workspaces rồi dedicated_node_grants, reservation Released, tính lại capacity_state
  PW->>DB: UPDATE network_allocations Quarantined, quarantine_until (từ M9)
  PW->>DB: UPDATE workspaces Deleted, operation Succeeded, COMMIT
  API-->>SPA: SSE Deleted, worker-a-05 còn trống 6 CPU
  Note over PW,DB: Housekeeping: CIDR Quarantined → Released rồi xóa hẳn row workspaces (trước M9: xóa ngay khi Deleted)
```

**Điểm cần review**

- Bước 4–6: confirm gõ lại slug Workspace (`water`). UI phải liệt kê số Namespace/PVC trước khi cho bấm [17.4].
- Bước 6–8: DELETE khi Workspace còn Operation chưa xong trả `WORKSPACE_NOT_READY`, không có cancel; muốn bỏ một lần tạo dở thì chờ `Failed` rồi DELETE (`04-state-machines.md` mục 1). Không cordon trước khi xóa: finalizer gỡ quyền tenant ngay ở bước 17.
- Bước 10 và 35–36: reservation và CIDR sang `Releasing` ngay khi nhận lệnh và vẫn tính vào capacity; chỉ `Released`/`Quarantined` trong transaction của worker khi CR đã biến mất (`04-state-machines.md` mục 3, 4). ClusterGrant không có row reservation: Workspace `Deleted` ra khỏi `SUM` của grant ở bước 37 (`02-data-model.md` mục 4). Green vẫn chạy, org label/taint không đổi [6.3][6.4].
- Bước 11: tập binding thay đổi nên Group Sync tính lại `tenantry:tenant-users` cho member của các group được bind.
- Bước 20–29: `Blocked` là condition `Deleting=False` reason `BlockedByFinalizer` trên CR, worker map thành Operation `Blocked`. OrgOwner/OrgAdmin retry (`Blocked → Running`, controller báo lại nếu vẫn kẹt); `:force` chỉ dành cho Platform Admin và được audit [16.5]. Force đi bằng annotation, không bằng `spec`, để không sinh generation mới.
- Bước 17–33: thứ tự bắt buộc (`IMPLEMENTATION-PLAN.md` mục 4.3): gỡ quyền tenant → bỏ `cordoned` (cordon của Capsule chặn cả lệnh ghi của operator; kiểm lại trên 0.15.x ở M6) → xóa Namespace và đợi → xóa Tenant và đợi → Calico sạch rồi xóa IPPool → gỡ membership label → gỡ finalizer. Không release CIDR khi còn WorkloadEndpoint [7.4][7.5]. CR là cluster-scoped; ownerReference chỉ là lưới an toàn.
- Bước 36: CIDR qua `Quarantined` rồi mới `Released`; trong lúc đó `k8s_name` cũng chưa dùng lại được [17.4]. Độ dài quarantine cấu hình được (`04-state-machines.md` mục 4). Workspace tạo trước M9 không có CIDR nên không qua quarantine, `k8s_name` dùng lại ngay khi `Deleted`. Row `workspaces` bị xóa hẳn khi CIDR `Released`; lịch sử nằm ở audit (`02-data-model.md`).
- Worker chết giữa bước 35–37: transaction DB atomic; khi restart, worker thấy CR đã biến mất và Workspace còn `Deleting` → chạy lại bước 35–37 (xem mục 13b).

---

## 13. Lan truyền trạng thái Operation và phục hồi sau restart

### 13a. Worker ↔ CR status ↔ DB ↔ portal

**Mục đích:** Một Operation phản chiếu status của CR theo generation; condition cụ thể chỉ ra component lỗi; retry đi lại qua outbox mà không tạo resource trùng [14.3][14.4][17].

```mermaid
sequenceDiagram
  autonumber
  participant SPA as Portal SPA
  participant API as Platform API
  participant DB as PostgreSQL
  participant PW as Outbox / Provisioning Worker
  participant K8S as kube-apiserver
  participant CTRL as Workspace Controller

  SPA->>API: GET /operations/op-1/events (SSE) hoặc poll GET /operations/op-1 mỗi 2s
  API->>DB: SELECT operations, workspace state, conditions
  API-->>SPA: state=Pending, progress 0
  PW->>DB: Lấy outbox tới hạn FOR UPDATE SKIP LOCKED
  PW->>K8S: Apply CR generation N, field manager tenantry-provisioning-worker
  PW->>DB: operations Running, workspace Provisioning
  API-->>SPA: state=Running, step Apply CR
  K8S->>CTRL: Reconcile generation N
  loop Mỗi component
    CTRL->>K8S: Apply resource, update condition tương ứng
    Note over CTRL,K8S: CapsuleTenantReady, ComputeReady (từ M8), NetworkReady (từ M9)...
  end
  alt Một condition False quá ngưỡng, ví dụ NetworkReady
    CTRL->>K8S: status phase=Failed, condition reason IPPoolConflict, observedGeneration=N
    K8S->>PW: Watch status
    PW->>DB: operations Failed, workspace Failed, error_code, condition snapshot
    API-->>SPA: Failed kèm component/condition cụ thể, nút Retry
    SPA->>API: POST /operations/op-1:retry (OrgOwner/OrgAdmin hoặc Platform Admin)
    API->>DB: Operation Pending, workspace Provisioning, outbox workspace.apply với spec.retryNonce mới, audit operation.retry
    PW->>K8S: Apply CR generation N+1, chỉ retryNonce đổi
    K8S->>CTRL: Reconcile generation N+1
  else Tất cả True và observedGeneration = N
    CTRL->>K8S: status phase=Ready, observedGeneration=N
    K8S->>PW: Watch status
    PW->>DB: workspace Ready, operations Succeeded
    API-->>SPA: Succeeded
  end
  Note over PW,DB: Worker chỉ tin status có observedGeneration = generation
```

**Điểm cần review**

- Bước 4: `SKIP LOCKED` cho phép chạy nhiều worker replica mà không xử lý trùng một event. Không cần lease: row lock được giữ suốt transaction giao event, worker chết thì transaction rollback và lock tự nhả.
- Bước 9: condition nào chưa có sub-reconciler ở milestone hiện tại thì không được set và không bắt buộc để vào `Ready` (`04-state-machines.md` mục 1.2).
- Bước 10–13: không dùng một cờ `error` chung; mỗi condition có `reason` để portal chỉ ra component lỗi [14.4][23]. Operation `Failed` không tự chuyển `Succeeded`; Workspace giữ `Failed` tới khi có retry.
- Bước 14–17: retry một lỗi do controller báo ghi `spec.retryNonce` mới vào payload, nên generation tăng lên N+1 và controller reconcile lại; status cũ (`observedGeneration = N`, `Failed`) không còn khớp generation nên worker bỏ qua. Reservation và CIDR không đổi khi retry [17]. `:retry` nhận cả Operation `Blocked` (mục 12); `:force` chỉ dành cho Platform Admin (`04-state-machines.md` mục 2).
- Bước 18–20 và Note cuối: worker bỏ qua status có `observedGeneration < generation` để không đánh dấu `Ready` từ thế hệ cũ trong lúc update.
- SSE hay poll là lựa chọn triển khai; `GET /operations/{id}` và `GET /operations/{id}/events` trả cùng payload [14.3][16.5].

### 13b. Worker và controller restart giữa chừng

**Mục đích:** Sau restart, worker apply lại outbox chưa processed và resync mọi Workspace đang chuyển trạng thái; controller reconcile lại từ spec, so `observedGeneration`, không dựa vào in-memory state [14.4][23][24.5].

```mermaid
sequenceDiagram
  autonumber
  participant DB as PostgreSQL
  participant PW as Outbox / Provisioning Worker
  participant K8S as kube-apiserver
  participant CTRL as Workspace Controller

  Note over PW: Worker crash ngay sau apply, trước khi ghi DB
  PW->>PW: Restart
  PW->>DB: SELECT outbox processed_at IS NULL, tới hạn
  PW->>K8S: Server-side apply lại cùng payload, cùng field manager
  Note over PW,K8S: Idempotent, không tạo IPPool/RoleBinding trùng
  PW->>DB: Mark processed
  PW->>DB: SELECT workspaces state IN Provisioning, Updating, Deleting
  loop Resync mỗi Workspace đang chuyển trạng thái
    PW->>K8S: GET PlatformWorkspace status
    alt Workspace Deleting và CR không tồn tại
      PW->>DB: Chạy transaction release của mục 12 (bước 35–37)
    else Provisioning/Updating và CR không tồn tại dù outbox đã processed
      PW->>DB: Đưa outbox event về chưa processed để apply lại
    else observedGeneration nhỏ hơn generation
      PW->>PW: Chờ controller, không đổi DB
    else Đã Ready hoặc Failed
      PW->>DB: Đồng bộ state và operation
    end
  end
  Note over CTRL: Controller restart giữa reconcile
  CTRL->>K8S: Informer list toàn bộ PlatformWorkspace
  loop Mỗi CR
    CTRL->>K8S: Reconcile từ spec, so observedGeneration
    CTRL->>K8S: Restore resource managed bị sửa tay
  end
  Note over CTRL,K8S: Không dựa vào in-memory state, mọi bước idempotent
```

**Điểm cần review**

- Bước 2–4: "processed" chỉ được ghi **sau** khi apply thành công; nếu crash giữa hai bước thì apply lại là an toàn nhờ server-side apply cùng field manager [14.4].
- Bước 6–8: với Workspace `Deleting`, CR không tồn tại nghĩa là xóa xong → chạy phần release DB của mục 12 (bước 35–37), không apply lại; với `Provisioning`/`Updating` thì apply lại.
- Bước 11–13: controller-runtime requeue toàn bộ sau restart; drift do sửa tay resource managed được restore [14.4]; reconcile phải idempotent với cùng generation.
- Kịch bản test bắt buộc: worker chết giữa apply và retry không duplicate; controller restart giữa provisioning; kube-apiserver tạm lỗi sau DB commit [24.5].
- Reconcile latency theo số Workspace 10/20/50 là chỉ số đánh giá [24.6].
