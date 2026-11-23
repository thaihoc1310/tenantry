# ADR-0012: Mỗi Workspace một Calico IPPool, chống CIDR overlap bằng IPAM trong transaction và exclusion constraint

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §1, §7.1–7.5](../PRODUCT-PLAN.md), [02 mục 3, 4](../architecture/02-data-model.md), [04 mục 4](../architecture/04-state-machines.md), [05 mục 5, 6](../architecture/05-network-and-scheduling.md), [IMPLEMENTATION-PLAN §4.3](../IMPLEMENTATION-PLAN.md)

## Bối cảnh

Mỗi Workspace được cấp một Pod CIDR/IPPool nằm trong vùng địa chỉ Organization được phép dùng ([PRODUCT-PLAN §1](../PRODUCT-PLAN.md)). Lab bản đầu đã có IPPool riêng cho Solar và Water nhưng CIDR `/18` viết cứng trong YAML ([05 mục 5](../architecture/05-network-and-scheduling.md)).

Khi cấp động, cần:
- CIDR của các Workspace trong một Cluster không đè nhau, kể cả khi hai request đến cùng lúc;
- không đè Pod CIDR mặc định, Service CIDR, Node CIDR của Cluster (lab: `192.168.0.0/16`, `10.96.0.0/12`);
- tenant không tự lấy IP từ pool của Workspace khác;
- CIDR chỉ được cấp lại khi Calico đã thật sự nhả mọi địa chỉ.

## Quyết định

**Vùng địa chỉ.** NetworkPool thuộc một Cluster, riêng của một Organization hoặc dùng chung (`organizationId = null`, ví dụ `onboarding-pod-network` `10.240.0.0/18`), và được đưa vào ClusterGrant. Pool không đè nhau và không đè `clusters.reserved_cidrs`; service kiểm dưới lock `clusters` ([§7.1](../PRODUCT-PLAN.md)).

**Cấp phát trong API.** IPAM chạy trong transaction tạo Workspace, dưới lock `network_pools`: mặc định lấy block trống đầu tiên theo `defaultPrefixLength` (`/24`), mục Advanced cho chọn prefix hoặc CIDR cụ thể. Lớp bảo vệ cuối là exclusion constraint `network_allocations_no_overlap` `EXCLUDE USING gist (cluster_id WITH =, cidr inet_ops WITH &&) WHERE (state <> 'Released')` (`backend/migrations/0012_network_allocations.sql`); vi phạm trả `CIDR_OVERLAP`, không phải 500 ([02 mục 4](../architecture/02-data-model.md)).

**Vòng đời.** `Allocated → Releasing → Quarantined → Released`; overlap tính mọi state trừ `Released`; quarantine 10 phút ([04 mục 4](../architecture/04-state-machines.md)).

**Trong Cluster.** Operator render một IPPool mỗi Workspace (`operator/internal/render/network.go`): `assignmentMode: Manual`, `allowedUses: [Workload]`, `blockSize` bằng 26 hoặc bằng prefix nếu CIDR nhỏ hơn `/26`, `nodeSelector` theo shared pool (Shared) hoặc membership label của Workspace (Dedicated), `vxlanMode: CrossSubnet`, `natOutgoing: true`. Annotation `cni.projectcalico.org/ipv4pools` của Namespace là metadata managed của Capsule; VAP `tenantry-tenant-pod-network` từ chối Pod tenant mang annotation Calico ở mức Pod. kube-proxy nhận Pod local theo interface `cali` vì supernet tenant `10.240.0.0/16` nằm ngoài `clusterCIDR` ([05 mục 5](../architecture/05-network-and-scheduling.md)).

**Xóa.** Finalizer chờ không còn địa chỉ đang cấp trong các `IPAMBlock` thuộc CIDR, xóa IPPool qua `projectcalico.org/v3`, chờ block biến mất, rồi worker mới đưa CIDR vào quarantine. `:force` không bỏ qua bước này ([IMPLEMENTATION-PLAN §4.3](../IMPLEMENTATION-PLAN.md)).

Cô lập mạng giữa Workspace là việc của NetworkPolicy theo label tenant, không dựa vào CIDR ([05 mục 6](../architecture/05-network-and-scheduling.md)).

## Phương án đã cân nhắc

### Một pool Calico chung cho mọi tenant, chỉ dùng NetworkPolicy

Cô lập vẫn đạt vì NetworkPolicy chọn theo label. Không chọn vì mất vùng địa chỉ riêng cho Organization và Workspace mà [§1](../PRODUCT-PLAN.md) đã cam kết; IP của Pod không còn cho biết nó thuộc Workspace nào.

### Một IPPool cho mỗi Organization

Không chọn. `nodeSelector` của IPPool khác nhau theo mode: Workspace Shared chọn node của shared pool, Workspace Dedicated chọn node mang membership label của chính nó ([05 mục 5](../architecture/05-network-and-scheduling.md)). Một Organization có cả hai loại thì một pool không đủ.

### Một IPPool cho mỗi Namespace

Không chọn. Namespace có thể tạo bằng `kubectl` ([§4.5](../PRODUCT-PLAN.md)), không đi qua transaction của API, nên không cấp CIDR từ DB cho nó được. Số pool cũng tăng theo số Namespace.

### IPPool `assignmentMode: Automatic`

Không chọn: pool `Automatic` được Calico dùng cho Pod không chỉ định pool. `Manual` chỉ cấp cho Namespace nào gọi đúng tên pool.

### Chỉ kiểm overlap trong code

Không đủ khi hai request cùng xin một block. IPAM vẫn tự tính block trống dưới lock, nhưng exclusion constraint là lưới an toàn cuối ([02 mục 4](../architecture/02-data-model.md)).

### Đổi Pod CIDR của cả cụm sang dải tenant thay vì sửa kube-proxy

Không chọn: phải sửa `podSubnet`, `--cluster-cidr`, tạo lại pool mặc định và khởi động lại mọi Pod hệ thống, mà `clusterCIDR` vẫn chỉ nhận một dải nên mọi NetworkPool về sau buộc nằm trong dải đó ([05 mục 5](../architecture/05-network-and-scheduling.md)).

## Hệ quả

Mặt tốt:
- IP của Pod nằm đúng CIDR của Workspace và cho biết Workspace của nó.
- Thêm CIDR vào NetworkPool chỉ là sửa DB; IPPool tạo riêng cho từng Workspace lúc cấp ([§7.1](../PRODUCT-PLAN.md)).

Mặt xấu và giới hạn:
- Kích thước IPPool cố định, không đổi prefix sau khi tạo; xóa Workspace thì block vào quarantine 10 phút, giữ luôn slug, và xóa Organization bị chặn tới khi quarantine cuối cùng hết ([known-limitations](../report/known-limitations.md) mục 8).
- IP rò (Calico còn giữ địa chỉ không có workload) làm xóa kẹt, phải gỡ tay bằng `calicoctl` (mục 14).
- Pod mới trong Namespace mới bị từ chối vài giây tới khi NetworkPolicy baseline có mặt (mục 16).
- Internet egress mở cho mọi Workspace, trừ dải private và metadata endpoint (mục 18).
- Phụ thuộc cấu hình kube-proxy `detectLocalMode: InterfaceNamePrefix`; thiếu thì traffic tới Service bị SNAT và NetworkPolicy "cùng Workspace" chặn nhầm (risk 13, [IMPLEMENTATION-PLAN §11](../IMPLEMENTATION-PLAN.md)).
- Pool chung được nhiều Organization dùng; UI chỉ hiện phần của Organization đó và số block còn trống ([04 mục 4](../architecture/04-state-machines.md)).

## Kiểm chứng

- Backend: `TestNetworkAllocation` (`backend/internal/httpapi/workspaces_test.go`): tự cấp `10.240.0.0/24` từ `onboarding-pod-network`; block đang dùng trả `CIDR_OVERLAP`; ngoài pool trả `CIDR_OUTSIDE_ALLOWED_POOL`; hai create cùng xin `10.240.5.0/24`, đúng một 202 và một 409; không bớt được pool hay CIDR đang có block. `TestNetworkPools` (`admin_pools_test.go`): pool đè pool khác hoặc đè `reserved_cidrs` trả `CIDR_OVERLAP`. `TestCheckPoolCIDRs` (`backend/internal/reservation/capacity_test.go`), `TestNextFreeAndWithin` (`ipam_test.go`).
- Operator envtest `TestWorkspaceNetwork` (`operator/internal/controller/network_test.go`): IPPool, annotation Namespace, policy baseline; khi xóa, pool chỉ đi khi Calico không còn cấp địa chỉ nào.
- Isolation trên lab (`operator/test/isolation/isolation_test.go`; [isolation-cluster.md](../report/isolation-cluster.md)): T15 A ↔ B bị chặn, cùng Workspace khác Namespace gọi được; T16 owner đổi `ipv4pools` sang pool của B bị Capsule đặt lại, Pod mang `ipv4pools` hoặc `ipAddrs` bị VAP từ chối (403).
- Persona: Pod của Solar `10.240.0.129` trong `10.240.0.0/24`, Pod của Water `10.240.1.128` trong `10.240.1.0/24`, hai chiều bị chặn ([e2e-personas.md](../report/e2e-personas.md)). Web e2e `web/e2e/network.spec.ts`.
- `make cluster-verify`: "kube-proxy detects local Pods by interface, not by clusterCIDR" và "a Pod outside clusterCIDR calls a Service on another node without SNAT" (`deploy/cluster/verify.sh`).
- Benchmark N=50: mọi Workspace xóa sạch, không còn IPPool hay IPAM block nào ([benchmark.md](../report/benchmark.md)).
- Chưa kiểm: `calicoctl ipam release` cho IP rò, vì chưa có IP rò thật (known-limitations, mục "Chưa kiểm hết").

## Câu hỏi cần soát

- Docs nói Workspace "được cấp" một CIDR nhưng không ghi rõ vì sao cần CIDR riêng khi cô lập đã do NetworkPolicy lo. Lý do ở phương án "pool chung" (vùng địa chỉ của Organization, IP cho biết Workspace) là suy ra từ §1 và §7.1; cần tác giả xác nhận hoặc bổ sung lý do gốc (ví dụ firewall phía ngoài theo dải của Organization).
