# Tenantry — Kubernetes Tenant as a Service trên Capsule

Đồ án: biến một cluster kubeadm thành dịch vụ multi-tenant self-service. Người dùng
tự tạo Workspace trên portal; Capsule, Calico và cert-manager thực thi cách ly.

| | |
|---|---|
| Bản đồ tài liệu | [`docs/README.md`](docs/README.md) — chủ đề nào định nghĩa ở file nào |
| Spec sản phẩm | [`docs/PRODUCT-PLAN.md`](docs/PRODUCT-PLAN.md) |
| Kế hoạch triển khai | [`docs/IMPLEMENTATION-PLAN.md`](docs/IMPLEMENTATION-PLAN.md) |
| Kiến trúc | [`docs/architecture/`](docs/architecture/) |

Code đang làm theo milestone, bắt đầu từ `backend/`. Nền của cụm (`deploy/cluster/`) được viết mới ở M1; lab Capsule bản đầu chỉ còn ở
nhánh `feat/tenantry-scaffold` để tra cứu.
