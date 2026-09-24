# Tenantry — Kubernetes Tenant as a Service trên Capsule

Đồ án: biến một cluster kubeadm thành dịch vụ multi-tenant self-service. Người dùng
tự tạo Workspace trên portal; Capsule, Calico và cert-manager thực thi cách ly.

| | |
|---|---|
| Spec sản phẩm | [`docs/PRODUCT-PLAN.md`](docs/PRODUCT-PLAN.md) (v0.6 — single source of truth) |
| Kế hoạch triển khai | [`docs/IMPLEMENTATION-PLAN.md`](docs/IMPLEMENTATION-PLAN.md) |
| Kiến trúc | [`docs/architecture/`](docs/architecture/README.md) |

Chưa có code. Lab Capsule bản đầu (`scripts/`, `manifests/`, `values/`, `tests/`,
`CAPSULE-LAB-DEEP-DIVE.md`) nằm ở nhánh `feat/tenantry-scaffold`.
