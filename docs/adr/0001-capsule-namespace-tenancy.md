# ADR-0001: Dùng Capsule namespace tenancy cho người dùng chỉ cần data plane

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §1, §2.3, §2.4, §3](../PRODUCT-PLAN.md), [IMPLEMENTATION-PLAN §4.1, §11](../IMPLEMENTATION-PLAN.md), [05 mục 2, 6, 8](../architecture/05-network-and-scheduling.md), [benchmark](../report/benchmark.md), [isolation-cluster](../report/isolation-cluster.md)

## Bối cảnh

Người dùng đích của Tenantry chỉ cần chạy workload namespaced: Deployment, Service `ClusterIP`, PVC, Helm chart không cần tài nguyên cluster-scoped. Họ không cần control plane riêng, không cần quyền cluster-scoped, và muốn có môi trường trong vài phút với quota, network, node placement và kubeconfig sẵn dùng ([PRODUCT-PLAN §1](../PRODUCT-PLAN.md)).

Luận điểm của đồ án là với nhóm này, chia sẻ một control plane rẻ và nhanh hơn việc cấp một môi trường có control plane riêng. Virtual cluster và cluster riêng cho tenant nằm ngoài phạm vi ([§2.3](../PRODUCT-PLAN.md)). Lab bản đầu đã chạy Capsule với hai tenant Solar và Water trên cùng API server ([§3](../PRODUCT-PLAN.md)).

Cần một cơ chế cho tenant tự tạo Namespace bằng `kubectl` trong vùng tên của mình, có quota chung cho nhiều Namespace, có policy được phát xuống mọi Namespace, và chỉ thấy Namespace của mình khi `kubectl get ns`.

## Quyết định

Mỗi Cluster chạy Capsule 0.15.2 và Capsule Proxy 0.15.3. Tenant của Tenantry là một Capsule `Tenant` trên kube-apiserver dùng chung; tenant vào cụm qua Capsule Proxy bằng token OIDC của chính họ.

Capsule đảm nhận:
- vùng tên và số Namespace: `forceTenantPrefix`, namespace quota;
- quota chung của Tenant qua `spec.rules[].quota` (Capsule sinh `GlobalResourceQuota` và chặn ở admission);
- luật workload: registry cho phép, cấm Service `NodePort`/`LoadBalancer`/`ExternalName`, cấm QoS `BestEffort`;
- metadata managed của Namespace: node selector, toleration, annotation IPPool của Calico (tenant sửa thì Capsule đặt lại);
- replication `GlobalTenantResource` (NetworkPolicy baseline, LimitRange) tới mọi Namespace, kể cả Namespace tạo bằng `kubectl`;
- Capsule Proxy lọc danh sách Namespace theo Tenant, gate `ProxyClusterScoped` chặn đọc Node.

Phần Capsule không có thì dùng primitive có sẵn của Kubernetes và Calico: admission plugin `PodNodeSelector` và `PodTolerationRestriction`, Pod Security `baseline`, Calico IPPool, và sáu ValidatingAdmissionPolicy trong `deploy/charts/tenantry-operator/templates/policies.yaml` (`tenantry-node-labels`, `tenantry-tenant-node-name`, `tenantry-tenant-pod-network`, `tenantry-tenant-network-ready`, `tenantry-tenant-ingress-lockdown`, `tenantry-tenant-rolebinding-subjects`).

Product layer (Organization, ClusterGrant, reservation, CIDR) nằm ở PostgreSQL. Capsule không biết gì về grant hay capacity.

## Phương án đã cân nhắc

### Mỗi tenant một cluster riêng

Cách cô lập mạnh nhất. Không chọn vì nằm ngoài phạm vi ([§2.3](../PRODUCT-PLAN.md)), platform không tạo cluster ([§31](../PRODUCT-PLAN.md) quyết định 8), và chi phí mỗi tenant lớn. Baseline đo bằng kind trên máy dev ([baseline-kind-vcluster.csv](../report/baseline-kind-vcluster.csv)): 33,4 s tới khi sẵn sàng, khoảng 100m CPU và 655 MiB lúc rảnh, 9 Pod thêm cho mỗi tenant.

### vCluster

Cho mỗi tenant một control plane ảo trong cụm host. Không chọn vì người dùng đích không cần control plane, và mỗi tenant vẫn tốn tài nguyên thường trực. Đo vcluster 0.37.7 trên cùng máy dev: 24,8–26,9 s, khoảng 40m CPU và 303 MiB lúc rảnh, 2 Pod thêm ([benchmark.md](../report/benchmark.md), mục Baseline). Pod của vCluster vẫn chạy trên node của cụm host, nên quota, NetworkPolicy và node placement ở cụm host vẫn phải làm như với Capsule.

### HNC (Hierarchical Namespace Controller)

Cho tạo namespace con và truyền RBAC, policy từ namespace cha xuống. Không chọn vì phần Tenantry cần nhất lại nằm ngoài nó: không có lớp lọc `kubectl get ns` theo tenant, không ép node selector, toleration hay annotation IPPool cho mọi Namespace của một tenant, và quota chung trên cả cây namespace không phải phần lõi. Chỉ so chức năng, không đo.

### Kiosk

Có khái niệm account, space và template để tenant tự tạo namespace với quota theo account. Không chọn vì không có proxy lọc tài nguyên cho tenant, không có cơ chế node placement hay metadata managed của Namespace như Capsule, và Tenantry đã giữ account, quota ở platform database (ADR-0002, ADR-0007). Chỉ so chức năng, không đo.

### Rancher Projects

Gom namespace thành project có quota và RBAC chung. Không chọn vì project là khái niệm của Rancher: phải chạy Rancher management server và dùng auth, API của nó, tức thêm một control plane quản lý cụm nằm giữa Tenantry và kube-apiserver; Tenantry sẽ phải điều khiển Rancher thay vì render object Kubernetes. Chỉ so chức năng, không đo.

### Namespace và RBAC tự quản, không dùng Capsule

Controller của Tenantry tự tạo Namespace, ResourceQuota, RoleBinding. Không chọn vì phải tự viết lại những gì Capsule đã có và lab đã kiểm: cho tenant tự tạo Namespace bằng `kubectl` mà vẫn ép tiền tố và số lượng, quota chung trải nhiều Namespace, phát policy xuống Namespace tạo ngoài portal, và lọc `kubectl get ns` theo tenant.

## Hệ quả

Mặt tốt:
- Một Workspace không tốn Pod nào, không tốn CPU hay RAM lúc rảnh; một Workspace tạo một mình Ready sau 10–20 s ([benchmark.md](../report/benchmark.md)).
- `kubectl` và `helm` dùng như bình thường qua Capsule Proxy: kịch bản persona cài được Helm chart và PVC `standard` Bound ([e2e-personas.md](../report/e2e-personas.md)).

Mặt xấu và giới hạn:
- Workspace không phải một cluster: không CRD, ClusterRole, webhook ([§2.4](../PRODUCT-PLAN.md)). Portal phải nói rõ giới hạn này.
- `ProxyClusterScoped` là gate Alpha: `kubectl get nodes` và StorageClass qua Proxy rỗng hoặc 403 với mọi tenant; Proxy CrashLoop khi một APIService không Available lúc khởi động ([known-limitations](../report/known-limitations.md) mục 2). Owner sửa được ProxySetting trong Namespace của mình (mục 5).
- Phụ thuộc hành vi của Capsule 0.15.2: race replication `GlobalTenantResource` (mục 10), Capsule đặt nhịp tạo Workspace (mục 12), `namespace-cleanup` gỡ finalizer của mọi object trừ Pod (mục 13), `:force` bỏ lại Tenant Terminating (mục 14), namespace quota trễ khoảng 1 giây (mục 6).
- Nhiều field của Tenant đã deprecated ở 0.15, Tenantry đã chuyển hết sang `spec.rules[]` ([IMPLEMENTATION-PLAN §4.1](../IMPLEMENTATION-PLAN.md)); version phải pin trong chart (risk 1, §11).
- Chung một kube-apiserver: tải control plane của tenant này ảnh hưởng tenant khác. Mỗi Workspace tốn khoảng 900 request của Capsule và 180–225 request của operator khi tạo. Ở N=50 không có 5xx mới của apiserver ([benchmark.md](../report/benchmark.md)). Cô lập compute mạnh hơn là việc của Dedicated ([ADR-0011](0011-dedicated-node-per-organization.md), [ADR-0017](0017-shared-soft-dedicated-hard.md)).

## Kiểm chứng

- Isolation suite trên lab: 8/8 subtest pass, 31 lần từ chối như mong muốn, 0 rò rỉ (`operator/test/isolation/isolation_test.go`, `TestIsolation`; [isolation-cluster.md](../report/isolation-cluster.md)). Bảng trong report ghi lớp nào từ chối: RBAC, Capsule webhook, VAP, PodNodeSelector, Pod Security.
- Kịch bản persona end to end pass trên lab (`test/e2e/personas_test.go`, `TestPersonas`; [e2e-personas.md](../report/e2e-personas.md)).
- Baseline kind/vCluster đo trên máy dev 16 CPU, còn số của Workspace đo trên lab (3 CPU mỗi worker). Hai môi trường khác nhau nên các số chỉ dùng để so bậc độ lớn (giây và trăm MiB thường trực so với 0), không so trực tiếp.
- Benchmark control plane N=10/20/50 và baseline kind/vCluster (`operator/test/isolation/bench_test.go`, `TestBenchmark`; [benchmark.md](../report/benchmark.md), [benchmark-control-plane.csv](../report/benchmark-control-plane.csv)).
- `make cluster-verify` kiểm `proxy_view`: qua Capsule Proxy, member chỉ thấy Namespace của Workspace mình và không thấy Node (`deploy/cluster/verify.sh`).
