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
  T -->|"apply PlatformWorkspace CR<br/>credential scoped"| K8s
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
- Mọi thứ Tenantry ghi vào Cluster đi qua đúng một cửa: CR `PlatformWorkspace`. Đây là contract cho Phase 2 (Cluster Agent) nên cần được xem là API công khai nội bộ.

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
    SPA["Portal Web<br/>React + TS, SPA tĩnh"]:::ui
    API["Platform API<br/>Go modular monolith<br/>REST /api/v1, BFF session"]:::svc
    PW["Provisioning Worker<br/>đọc outbox → apply CR<br/>watch CR status → DB"]:::svc
    GS["Group Sync Worker<br/>DB group → Keycloak"]:::svc
    PG[("PostgreSQL<br/>source of truth, outbox,<br/>phiên portal")]:::data
    KC["Keycloak<br/>realm tenantry"]:::ext
  end

  subgraph ClusterA["Cluster A — data plane"]
    APIS["kube-apiserver<br/>structured authn OIDC"]:::k8s
    CRD[("PlatformWorkspace CRs<br/>tenantry.io/v1alpha1")]:::data
    WC["Workspace Controller<br/>controller-runtime"]:::svc
    Capsule["Capsule Operator<br/>Tenant, ResourcePool, policy"]:::k8s
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
  WC -->|"tạo Tenant, RBAC, NetworkPolicy,<br/>Certificate, IPPool, node label"| APIS
  APIS --> Capsule
  APIS --> Calico
  APIS --> CM
  Capsule --> WL
  GS --> PG
  GS -->|"Admin API: group, member,<br/>logout user"| KC
  API -->|"PromQL usage"| Mon

  Kubectl -->|"exec plugin → browser"| KC
  Kubectl -->|"bearer token"| CP
  Browser -->|"console-cluster-a"| HL
  HL --> CP
  CP -->|"impersonation-free forward"| APIS
  Browser -->|"web.acme-solar.apps..."| Ing
  Ing --> WL
```

**Điểm cần review**

- **Hai worker là process riêng** (Provisioning, Group Sync) nhưng cùng codebase với API; scale/restart độc lập, không cần message broker: outbox trong PostgreSQL là hàng đợi.
- Provisioning Worker là **thành phần duy nhất** có credential vào Cluster, và credential đó chỉ có quyền trên các CRD `tenantry.io`. Controller (chạy trong cluster) mới là thứ có quyền tạo Tenant/IPPool/RBAC. Nếu API bị chiếm, kẻ tấn công không tạo được cluster-admin binding.
- Portal Web là SPA tĩnh, gọi API same-origin qua cookie → không có CORS, không giữ token trong JS.
- Cần xác nhận Headlamp nói với Capsule Proxy bằng **token của user** (không dùng ServiceAccount có quyền), đúng như plan 12.4.

## 3. Deployment view — MVP một Cluster

**Mục đích:** thấy rõ central plane nằm trên system pool, tenant workload nằm trên shared pool hoặc dedicated node, và các endpoint public.

```mermaid
flowchart TB
  classDef node fill:#f8f9fa,stroke:#343a40,color:#000
  classDef ns fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef tenant fill:#e2f0d9,stroke:#2d6a4f,color:#000
  classDef dns fill:#fff3cd,stroke:#b58900,color:#000

  DNS["DNS trên Cloudflare (DNS only)<br/>host platform và *.mvp.tenantry.site<br/>→ IP ingress"]:::dns

  subgraph CP["control-plane node(s)"]
    APIS["kube-apiserver + etcd<br/>authn: OIDC issuer Keycloak<br/>admission: PodNodeSelector"]:::node
  end

  subgraph SYS["system pool — label tenantry.io/compute-pool=system, taint NoSchedule cho tenant"]
    NS1["ns tenantry-system<br/>Portal, API, Provisioning Worker,<br/>Group Sync Worker, PostgreSQL"]:::ns
    NS2["ns keycloak<br/>Keycloak (database riêng trên<br/>cùng instance PostgreSQL)"]:::ns
    NS3["ns capsule-system<br/>Capsule, Capsule Proxy,<br/>Workspace Controller"]:::ns
    NS4["ns headlamp-system<br/>Headlamp"]:::ns
    NS5["ns ingress-system (Traefik), cert-manager,<br/>monitoring; csi-driver-nfs ở kube-system"]:::ns
  end

  subgraph SHARED["shared pool — tenantry.io/compute-pool=shared-general<br/>soft allocation, overcommit ratio"]
    S1["worker-a-01"]:::node
    S2["worker-a-02"]:::node
    T1["acme-solar-frontend<br/>acme-solar-backend"]:::tenant
    T2["globex-demo-web"]:::tenant
  end

  subgraph DED["dedicated node — tenantry.io/dedicated-org=org-01jabc (label + taint)"]
    D1["worker-a-05<br/>reservable 8 CPU / 30 GiB<br/>membership: ws-water, ws-green"]:::node
    T3["acme-water-*<br/>reserve 4 CPU"]:::tenant
    T4["acme-green-*<br/>reserve 2 CPU"]:::tenant
  end

  DNS -->|"portal.tenantry.site"| NS1
  DNS -->|"auth.tenantry.site"| NS2
  DNS -->|"kube-cluster-a.tenantry.site"| NS3
  DNS -->|"console-cluster-a.tenantry.site"| NS4
  DNS -->|"*.mvp.tenantry.site"| NS5
  T1 --- S1
  T2 --- S2
  T3 --- D1
  T4 --- D1
```

Bảng endpoint public (TLS bởi cert-manager, Let's Encrypt). Mọi request đi qua Traefik; host của platform nằm ngoài apps domain `mvp.tenantry.site` nên không Route nào của tenant trùng được:

| Hostname | Trỏ tới | Ai dùng | OIDC client |
|---|---|---|---|
| `portal.tenantry.site` | Portal Web + Platform API | mọi user, Admin | `portal` |
| `auth.tenantry.site` | Keycloak | browser, kubelogin, Headlamp | — (issuer) |
| `kube-cluster-a.tenantry.site` | Capsule Proxy | kubectl, Headlamp | `kubectl` |
| `console-cluster-a.tenantry.site` | Headlamp | browser | `headlamp-cluster-a` |
| `*.mvp.tenantry.site` | Route của tenant → app | người dùng cuối của tenant | — |

**Điểm cần review**

- System pool cần **taint** để tenant workload không rơi vào (Capsule Tenant nodeSelector không đủ nếu user bỏ được selector; taint là lớp bảo vệ thứ hai).
- Keycloak dùng **chung instance PostgreSQL, database riêng** để tiết kiệm tài nguyên lab; tách khi Phase 2.
- Phase 2 chỉ di chuyển khối `tenantry-system` + `keycloak` ra ngoài và thêm Cluster Agent vào `capsule-system`; các hostname theo Cluster (`kube-cluster-b`, `console-cluster-b`) được thêm theo mẫu.

## 4. Component view — bên trong Platform API

**Mục đích:** thấy ranh giới module của monolith và điểm đặt transaction reservation.

```mermaid
flowchart LR
  classDef edge fill:#fde2e4,stroke:#c9184a,color:#000
  classDef mod fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef infra fill:#e2f0d9,stroke:#2d6a4f,color:#000

  subgraph Edge["Edge"]
    HTTP["HTTP router<br/>OpenAPI generated"]:::edge
    Sess["BFF session + OIDC callback<br/>CSRF"]:::edge
    AuthZ["AuthZ middleware<br/>org membership · platform-admin"]:::edge
  end

  subgraph Domain["Domain services"]
    Identity["identity<br/>OIDC login, user upsert,<br/>phiên portal"]:::mod
    Org["organization<br/>xin tạo/duyệt, member, invitation,<br/>ownership transfer, group"]:::mod
    Ent["entitlement<br/>ClusterGrant, shared pool,<br/>DedicatedNodeGrant, NetworkPool"]:::mod
    WS["workspace<br/>validate, create/update/delete,<br/>namespaces, bindings, state"]:::mod
    Res["reservation<br/>grant quota · node capacity<br/>row lock, invariant"]:::mod
    IPAM["ipam<br/>auto-allocate, overlap,<br/>quarantine"]:::mod
    Ops["operation + audit"]:::mod
    Cluster["cluster registry<br/>status, endpoints,<br/>console/proxy URL"]:::mod
  end

  subgraph Infra["Infrastructure adapters"]
    Repo["repository (sqlc + pgx)<br/>transaction manager"]:::infra
    Outbox["outbox writer/reader"]:::infra
    KCc["Keycloak Admin client"]:::infra
    K8sc["Kubernetes client<br/>chỉ CRD tenantry.io"]:::infra
    Prom["Prometheus client"]:::infra
  end

  HTTP --> Sess --> AuthZ
  AuthZ --> Identity & Org & Ent & WS & Cluster & Ops
  WS --> Res
  WS --> IPAM
  WS --> Ops
  Res --> Repo
  IPAM --> Repo
  WS --> Outbox
  Org --> Ent
  Ops --> Repo
  Org --> Repo
  Ent --> Repo
  Cluster --> Repo
  Outbox --> Repo
  WS -.->|"đọc usage"| Prom

  subgraph Workers["Process riêng, cùng module"]
    PW["Provisioning Worker"]:::infra
    GS["Group Sync Worker"]:::infra
  end
  PW --> Outbox
  PW --> K8sc
  GS --> Repo
  GS --> KCc
```

Quy tắc phụ thuộc (để giữ monolith không rối):

- `Edge` chỉ gọi `Domain`; không gọi `Infra` trực tiếp.
- `workspace` là orchestrator của create/update/delete: nó gọi `reservation` và `ipam` **trong cùng một transaction** do `Repo.transaction manager` cấp; hai module này không được tự mở transaction.
- `Domain` không gọi thẳng Keycloak hay Kubernetes, không module domain nào import `KCc` hay `K8sc`. Thay đổi cho Cluster đi qua `outbox`; thay đổi cho Keycloak đi qua cột `sync_status` của `groups`/`group_members` mà Group Sync Worker đọc. Cả hai được ghi cùng transaction với thay đổi nghiệp vụ.
- `organization` gọi `entitlement` để tạo grant khi Admin duyệt hoặc tạo thẳng Organization; `entitlement` không biết `organization`.

**Điểm cần review**

- Có nên tách `reservation` và `ipam` thành module riêng ngay từ M2 hay để trong `workspace`? Đề xuất tách ngay vì đây là hai chỗ có unit test nặng nhất (quota/capacity math, CIDR math) và cần test độc lập với HTTP.
- `Cluster registry` trong MVP gần như tĩnh (một cluster) nhưng mọi service khác **phải** đọc endpoint/console URL từ đây, không hard-code, để Phase 2 chỉ thêm row.

## 5. Component view — bên trong Workspace Controller

**Mục đích:** một CR vào, nhiều output ra; mỗi output có Condition riêng để lỗi chỉ đúng chỗ.

```mermaid
flowchart TB
  classDef in fill:#fff3cd,stroke:#b58900,color:#000
  classDef rec fill:#d1e7ff,stroke:#0b5ed7,color:#000
  classDef out fill:#e2f0d9,stroke:#2d6a4f,color:#000

  CR["PlatformWorkspace acme-solar<br/>spec + generation"]:::in
  Main["Reconciler chính<br/>finalizer · observedGeneration · phase"]:::rec

  R1["node reconciler<br/>Dedicated: membership label ws-id<br/>(label/taint org: PlatformNodeGrant)"]:::rec
  R2["tenant reconciler<br/>Capsule Tenant acme-solar<br/>owners, nodeSelector, quota,<br/>allowed StorageClass"]:::rec
  R3["namespace reconciler<br/>spec.namespaces → Namespace<br/>watch Namespace của Tenant → status.namespaces"]:::rec
  R4["resource reconciler<br/>ResourcePool, LimitRange,<br/>object quota"]:::rec
  R5["network reconciler<br/>Calico IPPool, annotation,<br/>NetworkPolicy baseline"]:::rec
  R6["rbac reconciler<br/>Developer/Viewer additional<br/>RoleBinding → tenantry ClusterRoles"]:::rec
  R7["route reconciler<br/>spec.routes → Ingress<br/>→ status.routes"]:::rec
  R8["verify reconciler<br/>synthetic check → ProxyAccessReady"]:::rec

  O1["Node labels/taints"]:::out
  O2["Tenant"]:::out
  O3["Namespaces"]:::out
  O4["ResourcePool / LimitRange"]:::out
  O5["IPPool / NetworkPolicy"]:::out
  O6["RoleBindings"]:::out
  O7["Certificate / Secret"]:::out
  Status["status.conditions<br/>ComputeReady · CapsuleTenantReady ·<br/>NetworkReady · PolicyReady ·<br/>IdentityBindingsReady ·<br/>ProxyAccessReady → phase Ready"]:::in

  CR --> Main
  Main --> R1 --> O1
  Main --> R2 --> O2
  Main --> R3 --> O3
  Main --> R4 --> O4
  Main --> R5 --> O5
  Main --> R6 --> O6
  Main --> R7 --> O7
  Main --> R8
  R1 & R2 & R3 & R4 & R5 & R6 & R7 & R8 --> Status
```

Thứ tự khi **tạo**: node → tenant → namespaces → resource → network → rbac → ingress-cert → verify. Thứ tự khi **xóa** (finalizer): ngược lại, và network chỉ được xóa IPPool sau khi Calico báo hết workload endpoint; node chỉ gỡ membership label sau khi không còn Pod của Workspace.

**Điểm cần review**

- Controller **không** biết gì về quota của ClusterGrant hay capacity node: những invariant đó đã được API kiểm tra trước khi CR tồn tại. Controller chỉ render. Nếu thấy controller cần truy vấn DB thì thiết kế đang lệch.
- `namespace reconciler` phải phân biệt Namespace do portal khai báo (`spec.namespaces`) và do kubectl tạo (chỉ observed) để không xóa nhầm Namespace của user khi họ xóa khỏi portal list.
- `verify reconciler` là bước tùy chọn nhưng đáng giữ: nó biến "Ready" thành lời khẳng định đã test, không chỉ là "đã apply xong".
- Label/taint **cấp Organization** trên node (`tenantry.io/dedicated-org`) phải có từ lúc Admin grant node, trước khi có Workspace nào — nên không thể nằm trong `PlatformWorkspace`. Vì vậy có CRD phụ `PlatformNodeGrant` do worker apply; controller kiểm node sạch, reconcile label/taint và báo `allocatable` về status (`03-flows.md` mục 2b). Capacity của shared pool đi cùng mẫu qua CRD phụ `PlatformSharedPool` (IMPLEMENTATION-PLAN mục 4.5). `node reconciler` của `PlatformWorkspace` chỉ thêm/gỡ membership label per Workspace.
