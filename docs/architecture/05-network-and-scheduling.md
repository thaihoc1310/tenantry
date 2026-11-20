# 05 — Network, scheduling, ingress, access và storage

> Nơi định nghĩa cho node layout, capacity model, IP plan, NetworkPolicy, Ingress/TLS và storage; quy tắc nghiệp vụ nền ở `PRODUCT-PLAN.md` mục 6, 7, 11, 12, 18. Sơ đồ bám theo lab hiện có trong repo (Calico IPPool `Manual` per tenant, `PodNodeSelector`, NetworkPolicy replicate qua `GlobalTenantResource`) và thay tên lab (`solar`, `water`, `capsule.clastix.io/tenant=solar`) bằng tên Tenantry (`acme-solar`, `tenantry.io/...`).
> Chỗ plan chưa nói rõ được đánh dấu **(giả định)**.

Ví dụ dùng xuyên suốt:

| Thực thể | Giá trị |
|---|---|
| Cluster | `cluster-a`, wildcard domain `*.apps.cluster-a.tenantry.example` |
| Organization | slug `acme`, id `org-01jabc` |
| Workspace Shared | `acme-solar` (`ws-01jsol`), pool `shared-general`, CIDR `10.240.12.0/24` |
| Workspace Dedicated | `acme-water` (`ws-01jxyz`, reserve 4 CPU) và `acme-green` (`ws-01jgrn`, reserve 2 CPU) trên node `worker-a-05` |
| Dedicated node | `worker-a-05`, allocatable 9 CPU, systemReserve 1 CPU → reservable 8 CPU |
| NetworkPool | `onboarding-pod-network` (chung) và `acme-cluster-a-pod-network` (riêng), cùng supernet `10.240.0.0/16` |

---

## 1. Node layout của Cluster demo

**Mục đích:** cho thấy node nào chạy gì, label/taint nào tách system pool, shared pool và dedicated node, và membership label per Workspace cùng tồn tại trên một node.

```mermaid
flowchart TB
  subgraph CP["Control plane: cp-1"]
    CP1["kube-apiserver + PodNodeSelector<br/>etcd, scheduler, controller-manager<br/>taint node-role.kubernetes.io/control-plane:NoSchedule"]
  end

  subgraph SYS["System pool: sys-1, sys-2<br/>label tenantry.io/compute-pool=system (giả định)<br/>taint tenantry.io/compute-pool=system:NoSchedule (giả định)"]
    direction LR
    CENTRAL["Central plane<br/>Portal, Platform API, Provisioning Worker,<br/>Group Sync Worker, PostgreSQL, Keycloak"]
    TENANCY["Capsule Operator + webhook<br/>Capsule Proxy<br/>Workspace Controller"]
    EDGE["ingress-nginx (class platform-shared)<br/>cert-manager<br/>Headlamp"]
    OBS["Prometheus / Grafana<br/>NFS subdir provisioner"]
  end

  subgraph SHARED["Shared pool shared-general: wk-1, wk-2<br/>label tenantry.io/compute-pool=shared-general"]
    direction LR
    S1["Pod của acme-solar<br/>và Workspace Shared của Org khác"]
  end

  subgraph DED["Dedicated node worker-a-05 (Organization acme)<br/>label tenantry.io/dedicated-org=org-01jabc<br/>taint tenantry.io/dedicated-org=org-01jabc:NoSchedule<br/>label workspaces.tenantry.io/ws-01jxyz=true (Water)<br/>label workspaces.tenantry.io/ws-01jgrn=true (Green)"]
    direction LR
    W["Pod acme-water<br/>reserve 4 CPU"]
    G["Pod acme-green<br/>reserve 2 CPU"]
    DS["System DaemonSet<br/>calico-node, kube-proxy,<br/>node-exporter (toleration riêng)"]
  end

  CP1 --- SYS
  CP1 --- SHARED
  CP1 --- DED

  classDef sys fill:#eef,stroke:#446;
  classDef shared fill:#efe,stroke:#464;
  classDef ded fill:#fee,stroke:#644;
  class CENTRAL,TENANCY,EDGE,OBS sys;
  class S1 shared;
  class W,G,DS ded;
```

Ánh xạ với lab hiện tại: lab gắn `capsule.clastix.io/tenant=solar` lên `wk1,wk2` và `=water` lên `wk3` (scripts/40-platform.sh), tức mỗi tenant một pool cứng. Tenantry tách thành hai kiểu: pool chung `tenantry.io/compute-pool=shared-general` cho Shared mode và label/taint theo Organization + membership label theo Workspace cho Dedicated mode (plan 6.2, 6.4).

**Điểm cần review**

- System pool trong lab hiện chưa có node riêng; đề xuất label/taint `tenantry.io/compute-pool=system` là giả định để tenant workload không thể vào system node (plan 6.5 chỉ nói "không được schedule vào system/control-plane nodes").
- Cluster demo tối thiểu cần bao nhiêu node: 1 control-plane + 1–2 system + 2 shared + 1 dedicated = 5–6 node; đủ tài nguyên lab không?
- Trên dedicated node, system DaemonSet cần toleration cho taint `tenantry.io/dedicated-org` — phải thêm vào Helm values của Calico/monitoring/NFS client, và tính vào `systemReserve`.
- Có cho phép một node vừa là shared pool vừa là dedicated không? Plan hàm ý không (grant yêu cầu node sạch); nên enforce ở API.

---

## 2. Chuỗi enforcement scheduling: Shared và Dedicated

**Mục đích:** theo dõi một Pod từ Tenant spec tới scheduler, chỉ ra lớp nào inject selector/toleration và lớp nào từ chối các thao tác vượt rào của user.

```mermaid
flowchart LR
  subgraph DESIRED["Desired state (Workspace Controller render)"]
    CR["PlatformWorkspace acme-solar<br/>compute.mode = Shared"]
    CRD["PlatformWorkspace acme-water<br/>compute.mode = Dedicated<br/>nodeName = worker-a-05"]
    TS["Capsule Tenant acme-solar<br/>spec.nodeSelector:<br/>tenantry.io/compute-pool=shared-general"]
    TD["Capsule Tenant acme-water<br/>spec.nodeSelector:<br/>workspaces.tenantry.io/ws-01jxyz=true"]
    NL["Node worker-a-05<br/>+ label/taint dedicated-org=org-01jabc<br/>+ label workspaces.tenantry.io/ws-01jxyz=true"]
    CR --> TS
    CRD --> TD
    CRD --> NL
  end

  subgraph NS["Namespace (Capsule inject, tenant không sửa được)"]
    NSS["acme-solar-frontend<br/>annotation scheduler.alpha.kubernetes.io/node-selector<br/>= tenantry.io/compute-pool=shared-general"]
    NSD["acme-water-api<br/>annotation scheduler.alpha.kubernetes.io/node-selector<br/>= workspaces.tenantry.io/ws-01jxyz=true<br/>annotation scheduler.alpha.kubernetes.io/defaultTolerations<br/>= dedicated-org=org-01jabc (giả định)"]
    TS --> NSS
    TD --> NSD
  end

  subgraph ADM["kube-apiserver admission"]
    A1["RBAC + Capsule webhook<br/>registry, QoS, Service type,<br/>protected label/annotation"]
    A2["PodNodeSelector<br/>merge selector từ Namespace,<br/>conflict → reject"]
    A3["PodTolerationRestriction (giả định)<br/>inject default toleration của Org,<br/>toleration ngoài whitelist → reject"]
    A4["Workspace Controller validating webhook (giả định)<br/>reject spec.nodeName"]
    A1 --> A2 --> A3 --> A4
  end

  NSS --> A1
  NSD --> A1
  A4 --> SCHED["kube-scheduler<br/>match nodeSelector + tolerate taint"]
  SCHED -->|"Shared"| WK["wk-1 / wk-2"]
  SCHED -->|"Dedicated"| WA["worker-a-05"]
  SCHED -.->|"không match label / taint system"| SYSN["sys-1, cp-1: không bao giờ được chọn"]

  subgraph REJ["Thao tác của user bị từ chối"]
    R1["spec.nodeName: worker-a-05<br/>bỏ qua scheduler"] --x A4
    R2["nodeSelector: compute-pool=shared-general<br/>trong Namespace Dedicated"] --x A2
    R3["toleration dedicated-org=org-khac"] --x A3
    R4["kubectl label node ... / sửa annotation Namespace"] --x A1
    R5["nodeSelector: compute-pool=system"] --x A2
  end
```

Bảng tương ứng với lab hiện tại:

| Lớp | Lab (Solar/Water) | Tenantry |
|---|---|---|
| Tenant nodeSelector | `capsule.clastix.io/tenant: solar` | Shared: `tenantry.io/compute-pool: shared-general`; Dedicated: `workspaces.tenantry.io/<ws-id>: "true"` |
| Node label | script gắn tay `capsule.clastix.io/tenant=solar` lên wk1,wk2 | Admin gắn pool label một lần; Workspace Controller gắn/gỡ org label+taint và membership label |
| Admission | `PodNodeSelector` bật trong `60-enable-oidc.sh`; test `tests/wrong-node-pod.yaml` | Giữ `PodNodeSelector`; thêm toleration injection và chặn `nodeName` |
| Node visibility | Capsule Proxy lọc Node theo `Tenant.spec.nodeSelector` | Shared: không grant Node `List` (portal chỉ show pool summary); Dedicated: Proxy chỉ trả node có membership label |

**Điểm cần review**

- Cơ chế inject toleration: `PodTolerationRestriction` (admission plugin có sẵn của kube-apiserver, cấu hình qua annotation Namespace) là lựa chọn đơn giản nhất cho kubeadm lab; thay thế là mutating webhook riêng trong Workspace Controller. Cần chốt một trong hai.
- `spec.nodeName` bỏ qua scheduler và kubelet không kiểm tra taint `NoSchedule`, nên **bắt buộc** có lớp reject `nodeName` (A4); Capsule không làm việc này.
- Nếu `PodNodeSelector`/`PodTolerationRestriction` bị tắt, enforcement hỏng im lặng — plan 6.4 yêu cầu test bắt buộc; test nên chạy trong `90-verify.sh`.
- Với Shared mode, nếu Organization có nhiều Workspace cùng pool, `PodNodeSelector` không tách Workspace với nhau — đúng ý định (cô lập bằng Namespace/NetworkPolicy/quota), cần nói rõ trong luận văn.

---

## 3. Mô hình capacity của dedicated node

**Mục đích:** minh họa phép tính `reservable = allocatable − systemReserve`, hai reservation Water 4 CPU + Green 2 CPU, và request thứ ba 3 CPU bị từ chối.

```mermaid
flowchart LR
  subgraph NODE["DedicatedNodeGrant nodegrant-a1 · worker-a-05 · Organization acme"]
    direction TB
    ALLOC["allocatable<br/>9 CPU / 34Gi / 110 pods<br/>(Node.status.allocatable snapshot)"]
    SR["systemReserve<br/>1 CPU / 4Gi / 10 pods<br/>(DaemonSet + safety margin)"]
    RES["reservable = allocatable − systemReserve<br/>8 CPU / 30Gi / 100 pods"]
    ALLOC --> RES
    SR --> RES
    subgraph RESV["reserved = Σ NodeReservation (Pending/Active/Releasing)"]
      direction LR
      WR["acme-water<br/>4 CPU / 8Gi / 40 pods<br/>Active"]
      GR["acme-green<br/>2 CPU / 4Gi / 20 pods<br/>Active"]
    end
    RES --> RESV
    REM["remaining = max(reservable − reserved, 0)<br/>2 CPU / 18Gi / 40 pods<br/>capacityState = PartiallyAllocated"]
    RESV --> REM
  end

  REQ["Request thứ ba<br/>Dedicated, nodegrant-a1<br/>requestsCpu = 3, limits = requests"]
  REQ -->|"lock row dedicated_node_grants<br/>6 + 3 > 8"| DENY["409 DEDICATED_NODE_INSUFFICIENT_CAPACITY<br/>không ghi gì"]
  REQ2["Request 2 CPU"] -->|"6 + 2 ≤ 8"| OK["NodeReservation Pending<br/>capacityState = FullyAllocated<br/>remaining = 0"]
  REM -.-> REQ
  REM -.-> REQ2

  classDef bad fill:#fdd,stroke:#a33;
  classDef good fill:#dfd,stroke:#3a3;
  class DENY bad;
  class OK good;
```

Quy tắc đi kèm (plan 5.4, 6.3):

- Dedicated bắt buộc Guaranteed QoS: API đặt `limits = requests`, từ chối nếu client gửi limits khác; reservation dùng chính `requests.cpu/memory` và `pods`.
- Capsule ResourcePool của Workspace không được lớn hơn NodeReservation; controller render `hard` bằng đúng reservation.
- `requests.storage` được trừ vào ClusterGrant, **không** trừ vào node.
- Xóa `acme-water` release 4 CPU; `acme-green` và org label/taint trên node không bị ảnh hưởng.

**Điểm cần review**

- `systemReserve` do Admin nhập tay hay tính từ tổng request của DaemonSet đang chạy trên node cộng margin? Plan nói "bao gồm request của system DaemonSet dự kiến", nên có thể tự gợi ý rồi Admin xác nhận.
- Snapshot `allocatable` refresh khi nào; nếu node reboot với ít CPU hơn thì đi vào nhánh `Overcommitted` (xem 04-state-machines mục 5).
- Pods reservation: `pods` của Workspace là trần ResourcePool, nhưng Pod thực tế còn bị giới hạn bởi `max-pods` của kubelet — có nên cảnh báo khi Σ pods reserved > allocatable pods không?

---

## 4. Shared pool soft allocation vs Dedicated hard reservation

**Mục đích:** đặt cạnh nhau hai công thức để thấy Shared là trần có overcommit, Dedicated là giữ chỗ thật.

```mermaid
flowchart TB
  subgraph SHAREDP["Shared pool shared-general (soft allocation)"]
    direction TB
    SA["allocatable snapshot<br/>24 CPU / 96Gi / 330 pods"]
    SS["systemReserve<br/>2 CPU / 8Gi / 30 pods"]
    SRV["reservable = 22 CPU / 88Gi / 300 pods"]
    RATIO["allocationOvercommitRatio = 3.0<br/>(Admin đặt per pool)"]
    CAP["allocation ceiling = ratio × reservable<br/>66 CPU / 264Gi / 900 pods"]
    SUM["Σ allocation của mọi Workspace trên pool<br/>(mọi Organization)<br/>phải ≤ ceiling"]
    SA --> SRV
    SS --> SRV
    SRV --> CAP
    RATIO --> CAP
    CAP --> SUM
    SUMOK["Workspace mới 4 CPU:<br/>OK nếu Σ + 4 ≤ 66"]
    SUMBAD["Vượt ceiling →<br/>COMPUTE_POOL_CAPACITY_EXCEEDED"]
    SUM --> SUMOK
    SUM --> SUMBAD
    PEND["Hệ quả: khi mọi Workspace dùng hết quota,<br/>Pod có thể Pending — chấp nhận ở shared tier;<br/>portal cảnh báo allocated / (ratio × reservable)"]
    SUMOK --> PEND
  end

  subgraph DEDP["Dedicated node worker-a-05 (hard reservation)"]
    direction TB
    DA["allocatable snapshot<br/>9 CPU / 34Gi / 110 pods"]
    DSR["systemReserve<br/>1 CPU / 4Gi / 10 pods"]
    DRV["reservable = 8 CPU / 30Gi / 100 pods"]
    DRATIO["ratio cố định = 1<br/>(không cấu hình được)"]
    DCAP["reservation ceiling = reservable<br/>8 CPU / 30Gi / 100 pods"]
    DSUM["Σ NodeReservation trên node<br/>(chỉ Workspace của Organization acme)<br/>phải ≤ ceiling"]
    DA --> DRV
    DSR --> DRV
    DRV --> DCAP
    DRATIO --> DCAP
    DCAP --> DSUM
    DOK["Workspace 2 CPU: OK nếu Σ + 2 ≤ 8"]
    DBAD["Vượt →<br/>DEDICATED_NODE_INSUFFICIENT_CAPACITY"]
    DSUM --> DOK
    DSUM --> DBAD
    DGUAR["Hệ quả: requests = limits (Guaranteed),<br/>Pod không Pending vì thiếu capacity đã reserve"]
    DOK --> DGUAR
  end

  classDef bad fill:#fdd,stroke:#a33;
  class SUMBAD,DBAD bad;
```

| Tiêu chí | Shared | Dedicated |
|---|---|---|
| Ý nghĩa allocation | Trần ResourcePool (quota) | Giữ chỗ trên node cụ thể |
| Ceiling | `ratio × (allocatable − systemReserve)`, ratio per pool | `allocatable − systemReserve`, ratio = 1 |
| Ai chia sẻ | Mọi Organization có pool trong grant | Chỉ Workspace của Organization được grant node |
| QoS | `requests < limits` theo ratio policy của pool | `requests = limits` bắt buộc |
| Khi vượt | `COMPUTE_POOL_CAPACITY_EXCEEDED` | `DEDICATED_NODE_INSUFFICIENT_CAPACITY` |
| Rủi ro runtime | Pod Pending khi mọi tenant dùng hết quota | Không có, nếu snapshot đúng; drift → `Overcommitted` |

**Điểm cần review**

- Shared ceiling tính trên `requests` hay `limits`? Tài liệu giả định trên `requests` (khớp reservation invariant 5.3 dùng "allocation"); `limits` được kiểm bằng ratio `limits/requests` riêng của pool.
- Khi Admin hạ `allocationOvercommitRatio` xuống dưới Σ hiện tại: chặn (đối xứng với ClusterGrant) hay cho phép và hiển thị cảnh báo? Plan chỉ chặn với ClusterGrant.
- Pool usage thực (Prometheus) nên hiển thị bên cạnh ceiling để Admin có cơ sở điều chỉnh ratio — cần metric per pool label.

---

## 5. Kế hoạch địa chỉ IP

**Mục đích:** cho thấy supernet Cluster, hai loại NetworkPool, `/24` per Workspace được cấp tự động, và cách CIDR biến thành Calico IPPool + Namespace annotation.

```mermaid
flowchart TB
  subgraph CL["Cluster A: supernet 10.240.0.0/16 (Pod CIDR tenant)"]
    direction TB
    subgraph ONB["NetworkPool onboarding-pod-network<br/>organizationId = null (pool chung)<br/>cidrs 10.240.0.0/18 (giả định chia supernet)<br/>defaultPrefixLength 24, allowedComputeModes Shared"]
      P1["10.240.1.0/24 → Workspace globex-demo"]
      P2["10.240.2.0/24 → Workspace initech-lab"]
      PF["... free /24 ..."]
    end
    subgraph ACME["NetworkPool acme-cluster-a-pod-network<br/>organizationId = org-01jabc<br/>cidrs 10.240.64.0/18 (giả định)<br/>allowedPrefixLengths 20–24, Shared + Dedicated"]
      A1["10.240.64.0/24 → acme-solar (Shared)"]
      A2["10.240.65.0/24 → acme-water (Dedicated)"]
      A3["10.240.66.0/24 → acme-green (Dedicated)"]
      AQ["10.240.67.0/24 Quarantined<br/>(Workspace vừa xóa, quarantine_until)"]
      AF["... free ..."]
    end
    RESERVED["Vùng không cấp:<br/>Service CIDR 10.96.0.0/12 (kubeadm default)<br/>Calico default pool 192.168.0.0/16 cho system Pod<br/>Node CIDR"]
  end

  subgraph IPAM["IPAM (Platform API, transaction + exclusion constraint)"]
    I1["Auto: chọn /24 trống đầu tiên trong pool của grant"]
    I2["Advanced: prefix trong allowedPrefixLengths<br/>hoặc requestedCidr"]
    I3["Check: trong pool đúng Org + Cluster,<br/>không overlap Pod/Service CIDR,<br/>không overlap Allocated/Releasing/Quarantined"]
    I1 --> I3
    I2 --> I3
  end

  I3 -->|"NetworkAllocation Allocated<br/>cidr 10.240.64.0/24"| CRN["PlatformWorkspace acme-solar<br/>spec.network.allocationRef / cidr"]

  subgraph CALICO["Trong Cluster (Workspace Controller render)"]
    POOL["Calico IPPool acme-solar<br/>cidr 10.240.64.0/24<br/>assignmentMode Manual<br/>allowedUses [Workload]<br/>blockSize 26<br/>nodeSelector tenantry.io/compute-pool == 'shared-general'<br/>(Dedicated: workspaces.tenantry.io/ws-01jxyz == 'true')<br/>vxlanMode CrossSubnet, natOutgoing true"]
    NSA["Namespace acme-solar-frontend<br/>annotation cni.projectcalico.org/ipv4pools = ['acme-solar']<br/>(Capsule additionalMetadata inject,<br/>forbiddenAnnotations denied)"]
    POD["Pod nhận IP trong 10.240.64.0/24<br/>Pod-level override ipv4pools bị policy cấm"]
    CRN --> POOL
    CRN --> NSA
    POOL --> POD
    NSA --> POD
  end

  A1 -.-> I1
  AQ -.->|"quarantine hết hạn → Available"| AF

  classDef q fill:#ffe,stroke:#aa3;
  classDef r fill:#eee,stroke:#666;
  class AQ q;
  class RESERVED r;
```

So với lab: lab dùng `/18` cứng cho Solar (`10.240.0.0/18`) và Water (`10.240.64.0/18`) với `nodeSelector: capsule.clastix.io/tenant == "solar"`; Tenantry giữ nguyên mọi field IPPool (`Manual`, `Workload`, `blockSize 26`, `vxlanMode CrossSubnet`, `natOutgoing`) nhưng CIDR đến từ NetworkAllocation và selector theo pool/Workspace.

**Điểm cần review**

- Cách chia supernet giữa pool chung và pool riêng (`/18` mỗi loại) là giả định; plan chỉ đưa ví dụ một pool `10.240.0.0/16`. Cần chốt để tránh pool chung và pool riêng overlap ngay trong cấu hình Admin.
- `blockSize 26` với `/24` cho 4 block: trên shared pool 2 node là đủ; Workspace Shared có nhiều node hơn số block sẽ hết block dù còn IP — cân nhắc `blockSize 27/28` hoặc `defaultPrefixLength 23`.
- `natOutgoing: true` nghĩa là egress Internet được SNAT; kết hợp với NetworkPolicy "internet egress theo profile" (mục 6) — hai lớp cần khớp.
- Service CIDR và Node CIDR được kiểm tra trong IPAM bằng cấu hình `clusters` (cần thêm cột hoặc lấy từ `kubeadm-config`).

---

## 6. Network policy baseline per Namespace

**Mục đích:** liệt kê rule baseline mà mọi Namespace nhận qua replication, và minh họa traffic được phép/bị chặn giữa Workspace `acme-solar` và `acme-water`.

```mermaid
flowchart LR
  subgraph SOLAR["Workspace acme-solar<br/>label capsule.clastix.io/tenant=acme-solar (Capsule set, immutable với tenant)"]
    SF["Namespace acme-solar-frontend<br/>Pod web"]
    SB["Namespace acme-solar-backend<br/>Pod api"]
  end

  subgraph WATER["Workspace acme-water<br/>label capsule.clastix.io/tenant=acme-water"]
    WA["Namespace acme-water-api<br/>Pod api"]
  end

  DNS["kube-system / kube-dns<br/>UDP+TCP 53"]
  ING["ingress-nginx<br/>namespace ingress-system (giả định)"]
  NET["Internet<br/>0.0.0.0/0 trừ private + metadata"]
  META["Metadata endpoint<br/>169.254.169.254/32"]

  SF -->|"allow: cùng tenant label"| SB
  SB -->|"allow: cùng tenant label"| SF
  SF -->|"allow egress DNS"| DNS
  SB -->|"allow egress DNS"| DNS
  ING -->|"allow ingress từ ingress-nginx<br/>tới Pod được expose"| SF
  SF -->|"allow egress Internet<br/>nếu profile cho phép"| NET
  SF -. "deny: khác tenant label" .-x WA
  WA -. "deny: khác tenant label" .-x SB
  SF -. "deny metadata" .-x META
  ING -. "deny: Ingress của Water không trỏ tới Solar" .-x SB

  classDef deny stroke:#a33,stroke-dasharray: 5 5;
  class META deny;
```

Bộ rule replicate (kế thừa `manifests/tenant-resources.yaml`, mở rộng theo plan 7.4), tất cả qua `GlobalTenantResource`/`TenantResource` với `resyncPeriod` để controller restore khi bị xóa tay:

| # | NetworkPolicy | Nội dung | Nguồn label |
|---|---|---|---|
| 1 | `tenant-default-deny` | `podSelector: {}`, `policyTypes: [Ingress, Egress]`, không rule → deny all | — |
| 2 | `tenant-allow-same-workspace` | ingress + egress `namespaceSelector: capsule.clastix.io/tenant = {{tenant.name}}` | Label do Capsule gắn khi tạo Namespace; tenant không sửa được (Capsule webhook) |
| 3 | `tenant-allow-dns` | egress tới `kube-system` `k8s-app=kube-dns` UDP/TCP 53 | Lab đã có |
| 4 | `tenant-allow-ingress-controller` | ingress từ `namespaceSelector: kubernetes.io/metadata.name=ingress-system` (giả định tên) | Namespace label bất biến của Kubernetes |
| 5 | `tenant-allow-internet-egress` | egress `ipBlock 0.0.0.0/0` `except [10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.169.254/32]` — chỉ replicate khi policy profile của Workspace bật (giả định: profile `internet-egress` mặc định bật cho MVP) | — |
| 6 | (ẩn trong #5) deny metadata | `169.254.169.254/32` nằm trong `except` nên không có rule nào cho phép | — |

Nguyên tắc "không tin label tenant tự sửa": mọi selector chỉ dùng `capsule.clastix.io/tenant` (Capsule set), `kubernetes.io/metadata.name` (Kubernetes set) hoặc label trong namespace system mà tenant không có quyền — không dùng Pod label do user tự đặt.

**Điểm cần review**

- Lab hiện không có rule Internet egress (Pod tenant không ra ngoài được); Tenantry thêm rule #5 theo profile — chốt mặc định bật hay tắt cho onboarding Organization.
- Rule #4 cho phép mọi Pod trong Namespace nhận traffic từ ingress-nginx, không chỉ Pod "được expose"; muốn chặt hơn cần Pod label do user đặt (không tin được) hoặc chấp nhận trade-off này.
- Ingress của Workspace Water trỏ backend sang Service của Solar bị chặn ở lớp Capsule (Ingress chỉ tham chiếu Service cùng Namespace theo Kubernetes) — không cần rule mạng riêng, nhưng nên ghi thành test.
- Calico NetworkPolicy (CRD) có thể thay Kubernetes NetworkPolicy để dùng `GlobalNetworkPolicy` một lần thay vì replicate — plan chọn replicate qua Capsule để gắn với Tenant lifecycle; giữ hay đổi?

---

## 7. Ingress và TLS

**Mục đích:** theo dõi request HTTPS từ browser tới Pod, và luồng cấp wildcard Certificate per Workspace từ CA lab rồi replicate Secret vào mọi Namespace.

```mermaid
flowchart TB
  subgraph DNSL["DNS"]
    D1["Wildcard *.apps.cluster-a.tenantry.example<br/>→ IP ingress-nginx<br/>(CoreDNS/dnsmasq lab hoặc sslip.io)"]
  end

  subgraph EDGE["System pool"]
    ING["ingress-nginx<br/>IngressClass platform-shared"]
    CM["cert-manager"]
    CI["ClusterIssuer capsule-lab-ca<br/>(CA nội bộ của lab)"]
  end

  subgraph CTRL["Workspace Controller"]
    CERT["Certificate acme-solar-wildcard<br/>dnsNames *.acme-solar.apps.cluster-a.tenantry.example<br/>issuerRef ClusterIssuer capsule-lab-ca<br/>secretName acme-solar-wildcard-tls<br/>namespace tenantry-system (giả định)"]
    TR["TenantResource (Tenant acme-solar)<br/>replicate Secret acme-solar-wildcard-tls<br/>vào mọi Namespace của Workspace"]
    TEN["Tenant acme-solar<br/>ingressOptions.allowedClasses [platform-shared]<br/>allowedHostnames.allowedRegex<br/>^[a-z0-9-]+\\.acme-solar\\.apps\\.cluster-a\\.tenantry\\.example$<br/>hostnameCollisionScope Cluster"]
  end

  subgraph WS["Namespace acme-solar-frontend"]
    IR["Ingress web<br/>ingressClassName platform-shared<br/>host web.acme-solar.apps.cluster-a.tenantry.example<br/>tls.secretName acme-solar-wildcard-tls"]
    SEC["Secret acme-solar-wildcard-tls<br/>(bản replicate)"]
    SVC["Service web ClusterIP"]
    POD["Pod web"]
  end

  U["Browser<br/>(đã import CA lab một lần)"] -->|"1. resolve"| D1
  U -->|"2. HTTPS SNI web.acme-solar.apps..."| ING
  ING -->|"3. TLS terminate bằng Secret của Ingress"| IR
  IR --> SVC --> POD
  SEC --- IR

  CERT -->|"CertificateRequest"| CM
  CM --> CI
  CI -->|"Secret acme-solar-wildcard-tls"| CERT
  CERT --> TR
  TR --> SEC

  subgraph REJ["Bị Capsule từ chối"]
    X1["Ingress host api.acme-water.apps...<br/>trong Namespace Solar → ngoài regex"]
    X2["Ingress host web.acme-solar.apps... lần hai<br/>ở Namespace khác → collision Cluster"]
    X3["ingressClassName nginx-other<br/>→ không trong allowedClasses"]
    X4["Service NodePort/LoadBalancer<br/>→ serviceOptions cấm"]
  end
  X1 --x TEN
  X2 --x TEN
  X3 --x TEN
  X4 --x TEN
```

Chuỗi bảo vệ hostname: (1) regex `allowedHostnames` khóa tenant trong vùng `*.acme-solar.apps...`; (2) `hostnameCollisionScope: Cluster` chặn hai Ingress cùng host ở bất kỳ Namespace nào; (3) `allowedIngressClasses` của ClusterGrant → `allowedClasses` của Tenant; (4) Service `NodePort`/`LoadBalancer`/`ExternalName` bị cấm nên Ingress là đường expose duy nhất.

**Điểm cần review**

- Certificate đặt ở Namespace của controller (`tenantry-system`) rồi replicate Secret, hay đặt trực tiếp trong Namespace đầu tiên của Workspace? Đặt ở controller namespace tránh tenant xóa/sửa Certificate; đây là giả định.
- Capsule `TenantResource` replicate Secret kèm private key vào Namespace tenant — TenantOwner đọc được key wildcard của chính Workspace mình; chấp nhận (key chỉ bao vùng của họ) hay dùng cert-manager per-Ingress annotation thay thế?
- Regex `allowedHostnames` phải escape dấu chấm; controller render từ `k8s_name` và wildcard domain của Cluster — cần unit test render.
- DNS wildcard trong lab: CoreDNS `hosts`/`template` plugin hay sslip.io; ảnh hưởng tới hostname thật dùng trong demo.
- cert-manager trong lab hiện dùng cho Keycloak (`capsule-lab-root-ca` trong `identity-system`); ClusterIssuer `capsule-lab-ca` cần được `10-cert-manager.sh` tạo theo M1.

---

## 8. Đường truy cập Kubernetes API

**Mục đích:** cho thấy kubectl (kubelogin) và Headlamp đều đi qua Capsule Proxy với OIDC client/audience riêng, Proxy lọc gì, và token portal không bao giờ chạm Kubernetes API.

```mermaid
flowchart LR
  subgraph KC["Keycloak realm capsule"]
    C1["client kubectl<br/>public + PKCE, localhost callback<br/>audience kubernetes<br/>group mapper: có"]
    C2["client headlamp-cluster-a<br/>confidential, callback console-cluster-a<br/>audience headlamp-cluster-a<br/>group mapper: có"]
    C3["client portal<br/>confidential BFF + PKCE<br/>audience portal-api<br/>group mapper: không"]
  end

  U1["kubectl + kubelogin<br/>kubeconfig từ portal:<br/>server https://kube-cluster-a.tenantry.example<br/>exec kubectl oidc-login"]
  U2["Browser → Headlamp<br/>https://console-cluster-a.tenantry.example<br/>SSO từ session portal"]
  U3["Browser → Portal"]

  U1 -->|"browser login"| C1
  U2 -->|"Authorization Code"| C2
  U3 -->|"Authorization Code"| C3

  C1 -->|"ID token aud=kubernetes<br/>groups [org_01jabc:group_01jadm, tenantry:tenant-users]"| U1
  C2 -->|"ID token aud=headlamp-cluster-a<br/>groups ..."| U2
  C3 -->|"token aud=portal-api"| U3

  subgraph CLUSTERA["Cluster A"]
    PX["Capsule Proxy<br/>public HTTPS (kubectl)<br/>+ ClusterIP (Headlamp)<br/>authPreferredTypes BearerToken"]
    API["kube-apiserver<br/>AuthenticationConfiguration:<br/>issuer id.tenantry.example/realms/tenantry<br/>audiences [kubernetes, headlamp-cluster-a]<br/>username claim sub, prefix oidc:<br/>groups claim groups, prefix oidc:"]
    RBAC["RBAC + Capsule webhook<br/>owner group → Tenant owner<br/>Developer/Viewer → custom ClusterRole<br/>oidc:tenantry:tenant-users = Capsule user group"]
    PAPI["Platform API"]
    HL["Headlamp<br/>không có cluster-admin SA<br/>kubernetes host = Capsule Proxy service"]
  end

  U1 -->|"Bearer token"| PX
  U2 -->|"Bearer token"| HL
  HL -->|"Bearer token của user"| PX
  PX -->|"forward token, lọc response"| API
  API --> RBAC
  U3 -->|"session cookie"| PAPI
  PAPI -. "không bao giờ gọi kube-apiserver bằng token user;<br/>Provisioning Worker dùng SA scoped chỉ trên CRD tenantry.io" .-> API

  subgraph FILTER["Capsule Proxy lọc (cluster-scoped LIST)"]
    F1["Namespace: chỉ Namespace của Tenant user sở hữu/được bind"]
    F2["Node: theo Tenant.spec.nodeSelector<br/>(Shared: không grant List → rỗng; Dedicated: node membership)"]
    F3["StorageClass / IngressClass: theo allow-list của Tenant"]
    F4["PersistentVolume, PriorityClass, RuntimeClass: theo Tenant"]
    F5["Namespaced resource: không lọc, RBAC thuần quyết định"]
  end
  PX --- FILTER
```

| Đường | OIDC client | Audience | Đi qua | Ghi chú |
|---|---|---|---|---|
| kubectl | `kubectl` | `kubernetes` | kubeconfig → Capsule Proxy public endpoint | Không static token; kubelogin cache/refresh |
| Headlamp | `headlamp-cluster-a` | `headlamp-cluster-a` | Headlamp → Capsule Proxy ClusterIP | Một instance per Cluster; SSO nhờ session Keycloak từ portal |
| Portal | `portal` | `portal-api` | Portal BFF → Platform API | Token không hợp lệ với kube-apiserver vì audience không khớp |
| Provisioning Worker | (ServiceAccount) | — | apiserver trực tiếp | Chỉ quyền trên CRD `tenantry.io`; Phase 2 thay bằng Cluster Agent |

**Điểm cần review**

- Lab hiện dùng `username.claim: preferred_username`, `prefix: ""`, audiences `[headlamp, kubernetes]`; plan yêu cầu `sub` + prefix `oidc:` và audience `headlamp-{clusterId}`. Đổi prefix làm mọi RoleBinding/Tenant owner phải mang `oidc:`; cần cập nhật `60-enable-oidc.sh`, `tenants.yaml`, `values/capsule.yaml` (`userGroups`) cùng lúc.
- Với Shared mode "không grant Node List" (plan 6.2) nhưng Proxy vẫn lọc theo nodeSelector: Headlamp trang Nodes sẽ rỗng hoặc lỗi 403 — chọn hành vi nào cho UX.
- Capsule Proxy cần hai Service/Ingress (public cho kubectl, ClusterIP cho Headlamp) hay một public endpoint cho cả hai? Sơ đồ vẽ hai để Headlamp không đi vòng ra ngoài.
- Thời hạn token và contract thu hồi quyền: `06-security-and-identity.md` mục 3 và 5.

---

## 9. Storage

**Mục đích:** theo dõi PVC của tenant từ StorageClass `standard` (NFS subdir provisioner) tới ba lớp giới hạn: allow-list StorageClass của Tenant, `requests.storage` của ResourcePool, và `requests.storage` của ClusterGrant.

```mermaid
flowchart LR
  subgraph INFRA["Hạ tầng (system pool)"]
    NFS["NFS server<br/>export /srv/tenantry (giả định: VM lab hoặc Pod trong system pool)"]
    PROV["nfs-subdir-external-provisioner<br/>chạy trên system pool"]
    SC["StorageClass standard<br/>provisioner nfs-subdir<br/>reclaimPolicy Delete<br/>volumeBindingMode Immediate<br/>accessModes RWO/RWX"]
    SC2["StorageClass local-path (tùy chọn)<br/>ghim Pod vào node, không nằm trong<br/>allowedStorageClasses mặc định"]
    NFS --- PROV --- SC
  end

  subgraph GRANT["Platform DB"]
    CG["ClusterGrant acme @ cluster-a<br/>resources.requests.storage 500Gi<br/>allowedStorageClasses [standard]"]
    WSA["Workspace acme-solar<br/>requests.storage 100Gi<br/>(trừ vào ClusterGrant, không trừ vào node)"]
    CG --> WSA
  end

  subgraph TENANT["Tenant acme-solar (Workspace Controller render)"]
    TSC["storageClasses.allowed [standard]<br/>(Capsule từ chối PVC với class khác)"]
    RP["ResourcePool acme-solar<br/>quota.hard.requests.storage 100Gi<br/>+ persistentvolumeclaims (object count, tùy chọn)"]
    WSA --> TSC
    WSA --> RP
  end

  subgraph NSB["Namespace acme-solar-backend"]
    CLAIM["ResourcePoolClaim → ResourceQuota<br/>requests.storage 40Gi"]
    PVC["PVC data<br/>storageClassName standard<br/>20Gi RWX"]
    PV["PV pvc-... (subdir acme-solar-backend-data-pvc-...)"]
    POD["Pod api mount /data<br/>không ghim node"]
    RP --> CLAIM
    CLAIM --> PVC
    PVC --> PV
    PV --> POD
  end

  SC --> PVC
  PROV -->|"tạo subdir + PV"| PV
  TSC --> PVC

  subgraph REJ["Bị từ chối"]
    R1["PVC storageClassName local-path<br/>→ Capsule: class không được phép"]
    R2["PVC 80Gi khi quota còn 20Gi<br/>→ ResourceQuota exceeded"]
    R3["Workspace tăng requests.storage lên 600Gi<br/>→ CLUSTER_GRANT_..._QUOTA_EXCEEDED"]
  end
  R1 --x TSC
  R2 --x CLAIM
  R3 --x CG
```

Lifecycle khi xóa Workspace (plan 17.4): xóa Namespace → PVC bị xóa theo → `reclaimPolicy Delete` → provisioner xóa subdir (hoặc archive theo cấu hình `archiveOnDelete` — giả định tắt để "xóa hết" đúng nghĩa). PVC kẹt finalizer (`kubernetes.io/pvc-protection` khi Pod còn dùng, hoặc NFS server không phản hồi) → Operation `Blocked`, chỉ Platform Admin `:force`.

**Điểm cần review**

- Capsule Proxy lọc StorageClass theo allow-list, nên Headlamp chỉ hiện `standard`; cần xác nhận với Capsule 0.13.x rằng `storageClasses.allowed` hỗ trợ cả tên và regex.
- `archiveOnDelete` của NFS provisioner: tắt (mất dữ liệu thật, khớp cảnh báo UI) hay bật (giữ subdir để cứu, nhưng tốn dung lượng NFS và mâu thuẫn với "xóa hết")?
- Lab hiện không có StorageClass (README "Lưu ý về dữ liệu"): Keycloak/Prometheus/PostgreSQL của central plane cũng nên dùng `standard` hay một class riêng để tenant và system không chung một NFS export?
- `requests.storage` không có ở LimitRange mặc định; có nên đặt `max` per PVC trong LimitRange (type `PersistentVolumeClaim`) để một PVC không nuốt cả quota Workspace?
