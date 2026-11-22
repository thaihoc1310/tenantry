# 01 — Sơ đồ hệ thống: Context, Container, Deployment

> Góc nhìn, không định nghĩa gì mới: quy tắc ở `PRODUCT-PLAN.md`, phần còn lại theo bản đồ ở `docs/README.md`.
> Cách đọc: đi từ trên xuống — ai dùng hệ thống (Context) → hệ thống gồm những khối gì (Container) → chạy ở đâu (Deployment) → bên trong controller có gì (Component). Sau mỗi sơ đồ có mục **Điểm cần review**.

## 1. System Context — ai tương tác với Tenantry

**Mục đích:** xác định biên giới hệ thống, ba persona và các hệ ngoài mà Tenantry phụ thuộc.

```mermaid
flowchart LR
  classDef actor fill:#fff3cd,stroke:#b58900,color:#000
  classDef sys fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef ext fill:#e9ecef,stroke:#6c757d,color:#000

  Admin["Platform Admin<br/>realm role platform-admin"]:::actor
  Owner["OrgOwner / OrgAdmin<br/>tự phục vụ Organization"]:::actor
  Member["OrgMember<br/>Developer / Viewer"]:::actor

  T["Tenantry<br/>Portal + Platform API + Workers + Controller"]:::sys

  KC["Keycloak<br/>account, OIDC, group claim"]:::ext
  K8s["Kubernetes Cluster A<br/>kube-apiserver, Capsule, Calico"]:::ext
  Proxy["Capsule Proxy<br/>endpoint kubectl/Headlamp"]:::ext
  HL["Headlamp<br/>web console per Cluster"]:::ext
  Ing["Traefik<br/>*.mvp.tenantry.site"]:::ext
  Mon["Prometheus / Grafana"]:::ext

  Admin -->|"đăng ký Cluster, tạo Org,<br/>cấp ClusterGrant/node/network"| T
  Owner -->|"mời member, tạo group,<br/>tạo Workspace, Namespace"| T
  Member -->|"tải kubeconfig,<br/>mở web console"| T

  T -->|"OIDC login, Admin API<br/>sync group"| KC
  T -->|"apply CR tenantry.io<br/>credential scoped"| K8s
  T -->|"đọc metrics usage"| Mon

  Member -->|"kubectl + kubelogin<br/>token audience kubernetes"| Proxy
  Member -->|"SSO, token audience<br/>headlamp-cluster-a"| HL
  HL --> Proxy
  Proxy --> K8s
  Member -->|"HTTPS tới app của tenant"| Ing
  Ing --> K8s
  KC -.->|"JWT được kube-apiserver<br/>verify offline"| K8s
```

**Điểm cần review**

- Tenantry **không** nằm trên đường dữ liệu kubectl/Headlamp: user đi thẳng vào Capsule Proxy bằng token Keycloak. Portal chỉ phát kubeconfig và link. Nếu Portal chết, người đã có kubeconfig vẫn làm việc được.
- Ba persona dùng **một** realm Keycloak; Admin chỉ khác ở realm role. Không có identity plane riêng.
- Mọi thứ Tenantry ghi vào Cluster đi qua đúng một cửa: các CRD `tenantry.io` — `PlatformWorkspace` và ba CRD phụ `PlatformNodeGrant`, `PlatformSharedPool`, `PlatformNodeInventory` (IMPLEMENTATION-PLAN mục 4.5). Đây là contract cho Phase 2 (Cluster Agent) nên cần được xem là API công khai nội bộ.

## 2. Container view — các khối chạy được

**Mục đích:** liệt kê process/deployment, ai gọi ai, và dữ liệu nằm ở đâu.

```mermaid
flowchart TB
  classDef ui fill:#fde2e4,stroke:#c9184a,color:#000
  classDef svc fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef data fill:#e2f0d9,stroke:#2d6a4f,color:#000
  classDef k8s fill:#fff3cd,stroke:#b58900,color:#000
  classDef ext fill:#e9ecef,stroke:#6c757d,color:#000

  subgraph Client["Máy người dùng"]
    Browser["Browser"]:::ui
    Kubectl["kubectl + kubelogin"]:::ui
  end

  subgraph Central["Central plane (MVP: system pool của Cluster A)"]
    SPA["Portal Web<br/>Vue + TS, SPA tĩnh"]:::ui
    API["Platform API<br/>Go modular monolith<br/>REST /api/v1, BFF session"]:::svc
    PW["Provisioning Worker<br/>đọc outbox → apply CR<br/>watch CR status → DB"]:::svc
    GS["Group Sync Worker<br/>DB group → Keycloak"]:::svc
    PG[("PostgreSQL<br/>source of truth, outbox,<br/>phiên portal")]:::data
    KC["Keycloak<br/>realm tenantry"]:::ext
  end

  subgraph ClusterA["Cluster A — data plane"]
    APIS["kube-apiserver<br/>structured authn OIDC"]:::k8s
    CRD[("CR tenantry.io/v1alpha1<br/>PlatformWorkspace + 3 CRD phụ")]:::data
    WC["Workspace Controller<br/>controller-runtime"]:::svc
    Capsule["Capsule Operator<br/>Tenant, quota, policy"]:::k8s
    Calico["Calico<br/>IPPool per Workspace"]:::k8s
    CM["cert-manager<br/>ClusterIssuer Let's Encrypt"]:::k8s
    CP["Capsule Proxy"]:::k8s
    HL["Headlamp"]:::k8s
    Ing["Traefik"]:::k8s
    NFS["csi-driver-nfs<br/>StorageClass standard"]:::k8s
    Mon["Prometheus / Grafana"]:::ext
    WL["Tenant Namespaces<br/>acme-solar-*, acme-water-*"]:::k8s
  end

  Browser --> SPA
  SPA -->|"same-origin /api, cookie HttpOnly"| API
  Browser -->|"OIDC redirect"| KC
  API -->|"validate token, exchange code"| KC
  API --> PG
  API -->|"ghi outbox cùng transaction"| PG
  PW --> PG
  PW -->|"server-side apply CR<br/>SA chỉ có quyền tenantry.io"| APIS
  APIS --> CRD
  WC -->|"watch"| CRD
  WC -->|"tạo Tenant, RBAC, NetworkPolicy,<br/>Ingress, IPPool, node label"| APIS
  APIS --> Capsule
  APIS --> Calico
  APIS --> CM
  Capsule --> WL
  GS --> PG
  GS -->|"Admin API: group, member,<br/>enable/disable user"| KC
  API -->|"PromQL usage"| Mon

  Kubectl -->|"exec plugin → browser"| KC
  Kubectl -->|"bearer token"| CP
  Browser -->|"console-a"| HL
  HL --> CP
  CP -->|"impersonation-free forward"| APIS
  Browser -->|"web-acme-solar.mvp.tenantry.site"| Ing
  Ing --> WL
```

**Điểm cần review**

- **Hai worker là process riêng** (Provisioning, Group Sync) nhưng cùng codebase với API; scale/restart độc lập, không cần message broker: outbox trong PostgreSQL là hàng đợi.
- Provisioning Worker là **thành phần duy nhất** có credential vào Cluster, và credential đó chỉ có quyền trên các CRD `tenantry.io`. Controller (chạy trong cluster) mới là thứ có quyền tạo Tenant/IPPool/RBAC. Nếu API bị chiếm, kẻ tấn công không tạo được cluster-admin binding, nhưng điều khiển được cả hai worker qua PostgreSQL (`06-security-and-identity.md` mục 1).
- Portal Web là SPA tĩnh, gọi API same-origin qua cookie → không có CORS, không giữ token trong JS.
- Headlamp nói với Capsule Proxy bằng **token của user**; ServiceAccount của Headlamp không có quyền Kubernetes nào (plan 12.4, `make cluster-verify` kiểm).

## 3. Deployment view — MVP một Cluster

**Mục đích:** thấy rõ central plane nằm trên system pool, tenant workload nằm trên shared pool hoặc dedicated node, và các endpoint public.

```mermaid
flowchart TB
  classDef node fill:#f8f9fa,stroke:#343a40,color:#000
  classDef ns fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef tenant fill:#e2f0d9,stroke:#2d6a4f,color:#000
  classDef dns fill:#fff3cd,stroke:#b58900,color:#000

  DNS["DNS trên Cloudflare (DNS only)<br/>host platform và *.mvp.tenantry.site<br/>→ IP public, NAT vào node control-plane"]:::dns

  subgraph CP["control-plane node"]
    APIS["kube-apiserver + etcd<br/>authn: OIDC issuer Keycloak<br/>admission: PodNodeSelector,<br/>PodTolerationRestriction"]:::node
    TR["ns ingress-system<br/>Traefik, hostPort 80/443"]:::ns
  end

  subgraph SYS["system pool — label tenantry.io/compute-pool=system, taint NoSchedule cho tenant"]
    NS1["ns tenantry-system<br/>Portal, API, Provisioning Worker,<br/>Group Sync Worker, PostgreSQL"]:::ns
    NS2["ns keycloak<br/>Keycloak (database riêng trên<br/>cùng instance PostgreSQL),<br/>Mailpit (hộp thư giả cho thư xác minh)"]:::ns
    NS3["ns capsule-system<br/>Capsule, Capsule Proxy,<br/>Workspace Controller"]:::ns
    NS4["ns headlamp-system<br/>Headlamp"]:::ns
    NS5["cert-manager, monitoring;<br/>csi-driver-nfs ở kube-system"]:::ns
  end

  subgraph SHARED["shared pool — tenantry.io/compute-pool=shared-general<br/>soft allocation, overcommit ratio"]
    S1["worker-a-01"]:::node
    S2["worker-a-02"]:::node
    T1["acme-solar-frontend<br/>acme-solar-backend"]:::tenant
    T2["globex-demo-web"]:::tenant
  end

  subgraph DED["dedicated node — tenantry.io/dedicated-org=org-01jabc (label + taint)"]
    D1["worker-a-05<br/>reservable 8 CPU / 30 GiB<br/>membership: ws-01jxyz, ws-01jgrn"]:::node
    T3["acme-water-*<br/>reserve 4 CPU"]:::tenant
    T4["acme-green-*<br/>reserve 2 CPU"]:::tenant
  end

  DNS --> TR
  TR -->|"portal.tenantry.site"| NS1
  TR -->|"auth.tenantry.site"| NS2
  TR -->|"kube-a.tenantry.site"| NS3
  TR -->|"console-a.tenantry.site"| NS4
  TR -->|"*.mvp.tenantry.site"| T1
  T1 --- S1
  T2 --- S2
  T3 --- D1
  T4 --- D1
```

Bảng endpoint public (TLS bởi cert-manager, Let's Encrypt). Mọi request đi qua Traefik; host của platform nằm ngoài apps domain `mvp.tenantry.site` nên không Route nào của tenant trùng hostname được:

| Hostname | Trỏ tới | Ai dùng | OIDC client |
|---|---|---|---|
| `portal.tenantry.site` | Portal Web + Platform API | mọi user, Admin | `portal` |
| `auth.tenantry.site` | Keycloak, chỉ `/realms/tenantry` và `/resources` | browser, kubelogin, Headlamp | — (issuer) |
| `kube-a.tenantry.site` | Capsule Proxy | kubectl, Headlamp | token của user từ `kubectl` hoặc `headlamp-cluster-a` |
| `console-a.tenantry.site` | Headlamp | browser | `headlamp-cluster-a` |
| `*.mvp.tenantry.site` | Route của tenant → app | người dùng cuối của tenant | — |

Không có hostname: admin console và realm `master` của Keycloak, Mailpit (`06-security-and-identity.md` mục 1).

**Điểm cần review**

- Traefik chạy trên node control-plane vì IP public được NAT vào đó (`05-network-and-scheduling.md` mục 7); mọi thành phần nền khác chạy trên system pool.
- Không trùng hostname không có nghĩa là tách site: Route của tenant cùng registrable domain `tenantry.site` với portal và Keycloak. Hệ quả và giới hạn: `06-security-and-identity.md` mục 1.
- System pool cần **taint** để tenant workload không rơi vào (Capsule Tenant nodeSelector không đủ nếu user bỏ được selector; taint là lớp bảo vệ thứ hai).
- Keycloak dùng **chung instance PostgreSQL, database riêng** để tiết kiệm tài nguyên lab; tách khi Phase 2.
- Phase 2 chỉ di chuyển khối `tenantry-system` + `keycloak` ra ngoài và thêm Cluster Agent vào `capsule-system`; các hostname theo Cluster (`kube-b`, `console-b`) được thêm theo mẫu.

## 4. Component view — bên trong Platform API

**Mục đích:** thấy ranh giới package của monolith và điểm đặt transaction reservation. Tên khối là tên package trong `backend/internal/`; danh sách package và quy tắc import định nghĩa ở IMPLEMENTATION-PLAN mục 2 và 2.1.

```mermaid
flowchart LR
  classDef edge fill:#fde2e4,stroke:#c9184a,color:#000
  classDef mod fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef infra fill:#e2f0d9,stroke:#2d6a4f,color:#000

  subgraph Edge["httpapi"]
    MW["middleware: request-id → phiên →<br/>requirePlatformAdmin (/api/v1/admin/) →<br/>rate limit ghi → CSRF →<br/>validate OpenAPI → Idempotency-Key"]:::edge
    HTTP["handler sinh từ OpenAPI<br/>+ /auth/login, /auth/callback, /auth/logout"]:::edge
  end

  subgraph Domain["Service theo domain"]
    Auth["auth<br/>OIDC login, upsert user,<br/>phiên portal"]:::mod
    User["user<br/>Active/Disabled"]:::mod
    Org["org<br/>xin tạo/duyệt, member, invitation,<br/>ownership transfer, group, entitlement"]:::mod
    WS["workspace<br/>validate, create/update/delete,<br/>namespaces, bindings, routes, state"]:::mod
    Res["reservation<br/>cluster registry, ClusterGrant,<br/>shared pool, DedicatedNodeGrant,<br/>capacity math, row lock"]:::mod
    IPAM["ipam<br/>NetworkPool, auto-allocate,<br/>overlap, quarantine"]:::mod
    Ops["operation<br/>máy trạng thái, get/retry"]:::mod
    Audit["audit"]:::mod
  end

  subgraph Infra["Adapter"]
    Store["store<br/>sqlc + pgx, store.InTx"]:::infra
    Outbox["outbox"]:::infra
    KCc["keycloak<br/>Admin API client"]:::infra
    K8sc["k8s<br/>chỉ CRD tenantry.io"]:::infra
    Prom["Prometheus client"]:::infra
  end

  MW --> HTTP
  MW -->|"phiên"| Auth
  HTTP --> Auth & User & Org & WS & Res & Ops
  Org --> Res
  WS --> Res
  WS --> IPAM
  WS --> Ops
  WS --> Outbox
  Org & WS & Ops --> Audit
  Auth & User & Org & WS & Res & IPAM & Ops & Audit & Outbox --> Store
  WS -.->|"đọc usage"| Prom

  subgraph Workers["tenantry-worker, process riêng"]
    PW["--loops=provisioning,housekeeping"]:::infra
    GS["--loops=groupsync"]:::infra
  end
  PW --> Outbox
  PW --> K8sc
  GS --> Store
  GS --> KCc
```

Quy tắc phụ thuộc (để giữ monolith không rối):

- `httpapi` chỉ gọi service của domain; không tự viết SQL, chỉ dùng kiểu dữ liệu của `store` để map ra response.
- Middleware chỉ kiểm `platform-admin` cho tiền tố `/api/v1/admin/`. Không có middleware kiểm role trong Organization: service resolve `org_id` từ row rồi kiểm membership (404) → role (403) → trạng thái Organization (409); `:force` và hành động admin khác nằm ngoài `/admin/` tự kiểm `platform-admin` (IMPLEMENTATION-PLAN mục 3.5).
- `workspace` là orchestrator của create/update/delete: nó gọi `reservation` và `ipam` **trong cùng một transaction** (`store.InTx`); hai package này nhận `*store.Queries` của transaction đang chạy, không tự mở transaction. Thứ tự lock: `02-data-model.md` mục 4.
- Service domain không gọi thẳng Keycloak hay Kubernetes, không import `keycloak` hay `k8s`. Thay đổi cho Cluster đi qua `outbox`; thay đổi cho Keycloak đi qua cột `sync_status` của `groups`/`group_members` (và trạng thái `users`) mà Group Sync Worker đọc. Cả hai được ghi cùng transaction với thay đổi nghiệp vụ.
- `org` gọi `reservation` để tạo grant khi Admin duyệt hoặc tạo thẳng Organization; `reservation` không import `org`.

**Điểm cần review**

- Cluster registry (trong `reservation`) ở MVP gần như tĩnh (một cluster) nhưng mọi service khác **phải** đọc endpoint/console URL từ đây, không hard-code, để Phase 2 chỉ thêm row.

## 5. Component view — bên trong Workspace Controller

**Mục đích:** một CR vào, nhiều output ra; mỗi output có Condition riêng để lỗi chỉ đúng chỗ. Sơ đồ chỉ minh họa: tên sub-reconciler, thứ tự chạy, Condition sở hữu và milestone định nghĩa ở IMPLEMENTATION-PLAN mục 4.1; thứ tự xóa (finalizer) ở mục 4.3; Condition bắt buộc để vào `Ready` ở `04-state-machines.md` mục 1.2.

```mermaid
flowchart TB
  classDef in fill:#fff3cd,stroke:#b58900,color:#000
  classDef rec fill:#d1e7ff,stroke:#0b5ed7,color:#000

  CR["PlatformWorkspace acme-solar<br/>spec + generation"]:::in
  Main["PlatformWorkspaceReconciler<br/>finalizer · observedGeneration · phase"]:::rec

  R1["compute<br/>Dedicated: membership label ws-id trên node"]:::rec
  R2["network<br/>Calico IPPool, NetworkPolicy baseline"]:::rec
  R3["tenant<br/>Capsule Tenant: owners, nodeSelector,<br/>namespaceOptions, StorageClass"]:::rec
  R4["namespaces<br/>spec.namespaces → Namespace,<br/>quan sát → status.namespaces"]:::rec
  R5["policy<br/>quota chung của Tenant, LimitRange,<br/>Pod Security label"]:::rec
  R6["rbac<br/>additionalRoleBindings<br/>Developer/Viewer"]:::rec
  R7["routes<br/>spec.routes → Ingress → status.routes"]:::rec
  R8["proxyaccess<br/>ProxySetting, synthetic check"]:::rec
  Status["status.conditions → phase"]:::in

  CR --> Main
  Main --> R1 & R2 & R3 & R4 & R5 & R6 & R7 & R8
  R1 & R2 & R3 & R4 & R5 & R6 & R7 & R8 --> Status

  subgraph Aux["CRD phụ, mỗi CRD một reconciler"]
    NG["PlatformNodeGrant<br/>label + taint dedicated-org,<br/>báo allocatable"]:::rec
    SP["PlatformSharedPool<br/>label compute-pool theo spec.nodes"]:::rec
    NI["PlatformNodeInventory<br/>báo danh sách node"]:::rec
  end
```

**Điểm cần review**

- Controller **không** biết gì về quota của ClusterGrant hay capacity node: những invariant đó đã được API kiểm tra trước khi CR tồn tại. Controller chỉ render. Nếu thấy controller cần truy vấn DB thì thiết kế đang lệch.
- `namespaces` phải phân biệt Namespace do portal khai báo (`spec.namespaces`) và do kubectl tạo (chỉ observed) để không xóa nhầm Namespace của user khi họ xóa khỏi portal list.
- ServiceAccount của operator nằm trong Capsule `users` (group `system:serviceaccounts:<namespace của operator>`) và được render làm owner của mọi Tenant, nhờ vậy tạo được Namespace của `spec.namespaces` qua webhook Capsule. Đã chạy trên Capsule 0.13 của lab cũ; kiểm lại trên 0.15.x ở M6.
- Label/taint **cấp Organization** trên node (`tenantry.io/dedicated-org`) phải có từ lúc Admin grant node, trước khi có Workspace nào — nên không thể nằm trong `PlatformWorkspace`. Vì vậy có CRD phụ `PlatformNodeGrant`: operator kiểm node sạch, reconcile label/taint và báo `allocatable` về status (`03-flows.md` mục 2b). Label `tenantry.io/compute-pool=<pool-id>` của shared pool do `PlatformSharedPool` gắn/gỡ theo `spec.nodes`; `PlatformNodeInventory` báo danh sách node (IMPLEMENTATION-PLAN mục 4.5). Sub-reconciler `compute` của `PlatformWorkspace` chỉ thêm/gỡ membership label per Workspace.
