# Nội dung slide bảo vệ đồ án — Kubernetes Tenant as a Service

> Khung ~26 slide, 5 phần theo cấu trúc học thuật: Problem Definition → Survey → Proposal → Experimental → Conclusion.
> Mỗi slide: tiêu đề + bullet (tối đa 5) + gợi ý hình. *Nói:* là ý thuyết trình, không đưa lên slide.
> `[đo sau]` = điền sau khi chạy benchmark.

---

## Slide 0 — Trang bìa

- Tên đề tài: **Tenantry — Xây dựng nền tảng Kubernetes Tenant as a Service dựa trên Capsule**
- Sinh viên / GVHD / khoa / năm

## Slide 1 — Outline

1. **Problem Definition** — domain, nhu cầu, Kubernetes as a Service
2. **Survey** — kiến trúc Kubernetes, các mô hình multi-tenancy hiện có
3. **Proposal** — nền tảng đề xuất: thiết kế hệ thống, Capsule làm building block
4. **Experimental** — môi trường, benchmark, sizing
5. **Conclusion** — đóng góp, hạn chế, hướng phát triển

---

# PHẦN 1 — PROBLEM DEFINITION (slides 2–4)

## Slide 2 — Domain: hạ tầng container dùng chung

- Container hóa là chuẩn triển khai phần mềm; Kubernetes là nền điều phối de-facto
- Một tổ chức (doanh nghiệp, trường, cloud provider nhỏ) vận hành hạ tầng chung cho **nhiều nhóm độc lập**: dev team, phòng ban, lớp học, khách hàng
- Câu hỏi của domain: **chia hạ tầng Kubernetes cho nhiều nhóm như thế nào** — an toàn, nhanh, tiết kiệm?
- Hình: một tổ chức ở giữa, nhiều nhóm xung quanh cùng cần "một môi trường K8s"

## Slide 3 — Nhu cầu thực tế của người dùng

- Khảo sát nhu cầu điển hình của một nhóm dev:
  - Deploy workload **namespaced**: Deployment, Service, PVC, Ingress, ConfigMap
  - Có quota rõ ràng, có kubeconfig và web console, tự phục vụ trong vài phút
- **Không** cần: quản trị Node, cài CRD/operator cluster-wide, đụng control plane, chọn version Kubernetes
- Nghĩa là: đa số người dùng chỉ cần **data plane**, không cần cả một cluster
- Hình: bảng 2 cột "Cần" / "Không cần"

*Nói: đây là quan sát nền tảng — mọi lựa chọn thiết kế phía sau đều bám vào persona này.*

## Slide 4 — Kubernetes as a Service và bài toán đặt ra

- **KaaS phổ biến hiện nay = cấp nguyên cluster** (EKS/GKE/AKS, cluster tự dựng):
  - Control plane riêng chạy 24/7 dù workload nhỏ → lãng phí
  - Provision chậm (chục phút–giờ), thường kèm quy trình xin–duyệt thủ công
  - Vận hành nhân bản theo số cluster: upgrade, backup, monitoring ("cluster sprawl")
- **Bài toán**: xây nền tảng **Tenant as a Service** — cấp "một phần cluster" thay vì cả cluster:
  self-service, có tenant trong vài phút, cô lập API/network/compute/quota, không sửa YAML tay
- **Luận điểm**: với người dùng chỉ cần data plane, tenant trên control plane dùng chung là **nhanh hơn, nhẹ hơn, rẻ hơn** môi trường có control plane riêng

---

# PHẦN 2 — SURVEY (slides 5–10)

## Slide 5 — Kiến trúc Kubernetes

- **Control plane**: API Server, etcd, Scheduler, Controller Manager — quản lý trạng thái
- **Data plane**: worker node chạy workload (kubelet, container runtime, CNI)
- Mô hình khai báo: user gửi desired state → controller reconcile
- Ranh giới control plane / data plane là **trục phân tích** của toàn bộ survey phía sau
- Hình: diagram 2 tầng, tô màu control plane vs data plane

## Slide 6 — Tenancy primitives có sẵn và khoảng trống

- Có sẵn: Namespace, RBAC, ResourceQuota, LimitRange, NetworkPolicy, taint/toleration
- Nhưng Kubernetes thiết kế cho **một chủ**: nhiều resource là cluster-scoped (Node, CRD, ClusterRole, StorageClass, PV)
- Không có khái niệm tenant/organization/user-quota ở tầng sản phẩm; không có self-service
- → Multi-tenancy phải tự xây, câu hỏi là **chia ở tầng nào**: hạ tầng, control plane, hay namespace?

## Slide 7 — Mô hình 1: Cluster riêng per tenant

- Mỗi tenant một cluster đầy đủ — cô lập bằng hạ tầng
- ✅ Cô lập mạnh nhất (control plane, etcd, node đều riêng); tenant có cluster-admin thật
- ❌ Chi phí cố định lớn per tenant; provision tính bằng giờ
- ❌ Chi phí vận hành nhân theo số cluster
- Hình: N cluster xếp cạnh nhau, mỗi cái một control plane

## Slide 8 — Mô hình 2–3: Control plane riêng dạng nhẹ

- **Kamaji — hosted control plane**: control plane tenant chạy như pod trong management cluster, etcd riêng; tenant gắn **worker node riêng**
  - ✅ cô lập cứng, full API · ❌ vẫn tốn node riêng per tenant, vận hành phức tạp
- **vcluster — virtual cluster**: API server + data store ảo chạy như pod, workload sync xuống host cluster
  - ✅ tenant có cluster-admin ảo, tự cài CRD, provision nhanh
  - ❌ overhead per tenant ~100–500m CPU / 256Mi–1Gi RAM; thêm tầng sync phải debug
- Hình: 2 diagram mini cạnh nhau

## Slide 9 — Mô hình 4: Namespace-based với Capsule

- **Capsule** (CNCF Sandbox 2022): gom nhiều Namespace thành **Tenant** — một CR nhẹ
- Policy engine (admission webhook): quota, RBAC, registry, node selector, network kế thừa xuống mọi namespace của tenant
- **Capsule Proxy**: lọc resource cluster-scoped theo quyền tenant
- ✅ overhead per tenant ≈ 0, provision tính bằng giây · ❌ chung control plane: không CRD riêng, không chọn version
- Ghi chú: các lựa chọn namespace-based khác đã ngừng phát triển (HNC archived 4/2025, kiosk archived)

## Slide 10 — Bảng so sánh và gap analysis

| Tiêu chí | Cluster riêng | Kamaji | vcluster | **Capsule** |
|---|---|---|---|---|
| Cô lập control plane | Riêng | Riêng | Ảo riêng | Chung |
| Cô lập node | Riêng | Riêng | Chung | Chung/riêng theo policy |
| Overhead per tenant | Rất lớn | Trung bình | 0.1–0.5 CPU / 0.25–1Gi | **≈ 0** |
| Thời gian provision | Giờ | Phút | Phút | **Giây** |
| Tenant cài CRD / chọn version | ✅ | ✅ | ✅ | ❌ |

- Với persona "chỉ cần data plane" → **Capsule là điểm tối ưu chi phí/tốc độ**; trade-off được chấp nhận có chủ đích
- **Gap**: Capsule chỉ là *engine* — Tenant vẫn do admin viết YAML tay; không có user/org, entitlement, self-service, cấp phát network/node động
- → Đóng góp của đồ án: **xây tầng platform trên engine này**

---

# PHẦN 3 — PROPOSAL (slides 11–18)

## Slide 11 — Nền tảng đề xuất

- Tên: **Tenantry** — dùng xuyên suốt code (API group `tenantry.io`), UI và slide
- Một câu: *nền tảng self-service cấp Kubernetes Tenant trong vài phút trên cluster dùng chung*
- Ba vai trò: **Platform Admin** (cấp tài nguyên) · **Org Owner** (tự phục vụ) · **Member** (sử dụng)
- Đơn vị cấp phát: **Workspace** = nhiều Namespace + quota + network + node placement + access
- Hình: user journey — Sign up → Create Workspace → kubectl/console → Deploy app (URL thật `web.acme-solar.apps.cluster-a.tenantry.example`)

## Slide 12 — Mô hình khái niệm

- **Account** (Keycloak giữ credential) → **Organization** (boundary quản lý, tự tạo khi first login) → **Group** → role vào Workspace: Owner / Developer / Viewer
- **ClusterGrant**: Admin cấp cho Organization quota + compute pool + dedicated node + network pool, theo từng cluster
- **Workspace ↔ Capsule Tenant (1:1)**, sống trọn trong một cluster
- Onboarding: user mới được auto-grant quota nhỏ → trải nghiệm ngay như free tier
- Hình: ERD Account–Org–Group–Workspace–ClusterGrant

## Slide 13 — Kiến trúc hệ thống

- **Central plane**: Portal (React) + Platform API (Go) + PostgreSQL (source of truth) + Keycloak (OIDC)
- **Provisioning**: API ghi DB + outbox trong 1 transaction → worker apply CR `PlatformWorkspace` → **Workspace Controller** (controller-runtime) reconcile
- Worker chỉ có quyền trên CRD platform, không cluster-admin; retry idempotent, tự phục hồi
- Thiết kế sẵn multi-cluster: mọi entity gắn `cluster_id`
- Hình: architecture diagram đầy đủ — slide quan trọng nhất, vẽ kỹ

## Slide 14 — Capsule làm building block

- Nguyên tắc: **không phát minh lại tenancy — compose các building block CNCF**:
  - **Capsule**: Tenant, namespace quota, policy engine, ResourcePool
  - **Capsule Proxy**: endpoint truy cập duy nhất, lọc cluster-scoped resource
  - **Calico**: IPPool per workspace; **Keycloak**: OIDC identity; **Headlamp**: web console
- Phần đồ án xây: Portal/API/DB, controller, IPAM, reservation, group sync — tầng mà engine không có
- Hình: layer diagram — [tầng tự xây] đè trên [building blocks có sẵn] đè trên [Kubernetes]

*Nói: chỉ rõ ranh giới "dùng lại vs tự làm" — hội đồng luôn hỏi câu này.*

## Slide 15 — Identity và hai lớp authorization

- OIDC Authorization Code + PKCE; portal không lưu password
- **Lớp platform**: API kiểm tra Organization membership/role mọi endpoint
- **Lớp Kubernetes**: group claim `oidc:org_x:group_y` → RBAC/Capsule; role catalog cố định (custom ClusterRole, không dùng built-in `edit`)
- kubeconfig không chứa token tĩnh: exec plugin (kubelogin) → SSO → token ngắn hạn ~5 phút
- Revocation contract: mất quyền chậm nhất khi token hết hạn — trung thực, đo được

## Slide 16 — Resource model: entitlement và reservation

- Quota theo tầng: Org entitlement → ClusterGrant → Workspace allocation → ResourcePool enforce trong cluster
- **Invariant**: Σ allocation ≤ grant, kiểm tra + lock trong một DB transaction
- **Shared = soft** (Σ quota ≤ ratio × capacity, Admin đặt ratio per pool) · **Dedicated = hard** (Σ reservation ≤ capacity thật)
- Dedicated node: node thuộc **Organization**, nhiều Workspace chia capacity qua **reservation** (requests = limits, Guaranteed QoS); `reservable = allocatable − systemReserve`
- Row-lock chống overbook khi hai request tạo đồng thời — Kubernetes không tự làm được việc này
- Hình: node 8 CPU → Water 4 + Green 2 → request 3 CPU bị từ chối

## Slide 17 — Network isolation

- Mỗi Workspace một Pod CIDR cấp động từ NetworkPool (IPAM chống overlap bằng DB constraint) → Calico IPPool riêng
- Baseline NetworkPolicy: default-deny cross-workspace; allow nội bộ workspace + DNS + ingress
- Tenant không sửa được annotation/label làm cơ sở cô lập (admission chặn)
- Nhận diện tenant theo dải IP → phục vụ firewall/audit; delete có quarantine CIDR
- Hình: 2 workspace 2 màu CIDR, traffic cross bị chặn

## Slide 18 — Workspace lifecycle và demo flow

- State machine: Draft → Validating → Reserving → Provisioning → Ready (→ Degraded/Cordoned/Deleting); mọi bước có condition, operation theo dõi được trên portal
- Delete sạch: workload → IPPool → reservation → quarantine CIDR → release quota
- **Demo (live/video 5–7'):** Alice đăng ký có ngay quota → tạo Workspace < 2' → Joe deploy app PVC + Ingress URL thật → Viewer bị chặn sửa → 2 Dedicated Workspace chia node, request vượt capacity **bị từ chối** → cross-network bị chặn → xóa release đúng phần
- Backup: video quay sẵn

---

# PHẦN 4 — EXPERIMENTAL (slides 19–23)

## Slide 19 — Môi trường thí nghiệm và sizing

- Cluster kubeadm: [N] node — control plane [spec], system pool [spec], shared pool [spec], dedicated node [spec]
- Thành phần: Capsule 0.13.x, Capsule Proxy, Calico 3.32, Keycloak 26.x, Headlamp, kube-prometheus-stack
- Sizing platform: tài nguyên dành cho central plane + per-cluster components: [đo sau] CPU/RAM
- Mật độ tenant mục tiêu: 20–50 Workspace / cluster; công thức ước lượng: số tenant ≈ (capacity node − system reserve) / allocation trung bình
- Hình: sơ đồ node layout của lab

## Slide 20 — Phương pháp đánh giá

- **Isolation suite**: tenant A không đọc/gọi được API, network, node, IP của tenant B
- **Concurrency suite**: cặp request đồng thời sát quota / sát capacity node / cùng dải CIDR
- **E2E persona test**: tự động hóa toàn bộ kịch bản demo
- **Benchmark**: đo lặp N lần trên cluster thật, báo cáo p50/p95

## Slide 21 — Benchmark 1: chi phí cấp một môi trường

| Chỉ số (cùng đầu ra: user deploy được app) | Cluster mới | vcluster | **Platform** |
|---|---|---|---|
| Thời gian tới khi deploy được | [đo sau] | [đo sau] | **[đo sau] (mục tiêu < 2')** |
| CPU/RAM nền per tenant | [đo sau] | ~0.1–0.5 CPU / 0.25–1Gi | **≈ 0** |
| Số thao tác admin | nhiều | trung bình | **0 (self-service)** |

- Số liệu trực tiếp chứng minh luận điểm — so sánh công bằng trên cùng một kết quả đầu ra

## Slide 22 — Benchmark 2: scale và độ trễ vận hành

- Provision p95 khi hệ thống có 10 / 20 / 50 Workspace: [đo sau]
- API server latency trước/sau 50 tenant (overhead webhook/proxy): [đo sau]
- Thời gian revoke quyền có hiệu lực (≈ token TTL): [đo sau]
- Overhead từng thành phần platform (Portal, Keycloak, Capsule, Proxy, controller): [đo sau]
- Kết quả suite: isolation X/X pass, concurrency X/X pass (không overbook, không overlap CIDR)

## Slide 23 — Phân tích kết quả và giới hạn

- [Điền sau khi có số] — đối chiếu từng số liệu với luận điểm: nhanh hơn ~[k] lần, overhead ≈ 0
- Giới hạn trung thực của mô hình:
  - Chung control plane → blast radius khi API server sự cố
  - Không CRD/operator riêng, không chọn version K8s per tenant
  - Revocation trễ tối đa bằng token TTL; node lỗi không tự thay
- **Định vị**: bổ sung chứ không thay thế cluster riêng — đúng cho persona data-plane-only

---

# PHẦN 5 — CONCLUSION (slides 24–25)

## Slide 24 — Đóng góp

- Tầng **product/platform hoàn chỉnh** trên Capsule: identity, org/group, entitlement, self-service portal, kubeconfig/console động
- **Capacity reservation** cho dedicated node chia sẻ trong Organization, chống overbook bằng DB transaction
- **IPAM động** per-workspace trên Calico: chống overlap, quarantine khi xóa
- Provisioning **outbox → CRD → controller** idempotent, tự phục hồi
- Bộ đánh giá isolation/concurrency/benchmark tái lập được

## Slide 25 — Hướng phát triển + Q&A

- **Phase 2 — Multi-cluster**: nhiều cluster, Cluster Agent outbound mTLS (central không giữ credential), demo cô lập sự cố giữa cluster
- Migration workspace giữa node/cluster; CLI riêng; email invitation; custom domain
- Kết luận một câu: *tenant trong **giây thay vì giờ**, overhead **≈ 0 thay vì một control plane** — cho đúng nhóm người dùng chỉ cần data plane*
- Q&A — backup slides: ERD, state machine, chi tiết admission/policy, kết quả từng test, so sánh chi tiết từng giải pháp

---

## Ghi chú soạn slide

- Mỗi slide ≤ 5 bullet, bullet ≤ 2 dòng; ưu tiên hình và số thay chữ.
- Ba slide cần đầu tư nhất: **10 (bảng so sánh + gap), 13 (kiến trúc), 21 (benchmark)**.
- Slide 23 chủ động nói giới hạn *trước* khi hội đồng hỏi.
- Nguồn đối chiếu phần Survey: [CNCF Capsule](https://www.cncf.io/projects/capsule/) · [Project Capsule docs](https://projectcapsule.dev/) · [vcluster — comparing multi-tenancy options](https://www.vcluster.com/blog/comparing-multi-tenancy-options-in-kubernetes) · [vcluster vs Kamaji isolation models](https://www.vcluster.com/blog/tenant-isolation-models-kubernetes) · [Kamaji docs](https://kamaji.clastix.io/) · [HNC archived](https://github.com/kubernetes-retired/hierarchical-namespaces) · [Kubernetes multi-tenancy](https://kubernetes.io/docs/concepts/security/multi-tenancy/)
