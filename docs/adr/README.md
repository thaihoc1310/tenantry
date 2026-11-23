# Architecture Decision Records

Mỗi ADR trả lời một câu hỏi trong [PRODUCT-PLAN §29](../PRODUCT-PLAN.md), số ADR trùng số câu hỏi. Mỗi file có năm mục: Bối cảnh, Quyết định, Phương án đã cân nhắc, Hệ quả, Kiểm chứng. Mục Kiểm chứng chỉ dẫn test và report có thật; chỗ chưa có bằng chứng ghi "chưa kiểm". Mục "Câu hỏi cần soát" ở cuối một số file là điểm còn mở.

Giới hạn đã biết của từng quyết định nằm ở [known-limitations](../report/known-limitations.md); các ADR trỏ tới đúng mục.

| # | Quyết định |
|---|---|
| [0001](0001-capsule-namespace-tenancy.md) | Dùng Capsule namespace tenancy cho người dùng chỉ cần data plane |
| [0002](0002-keycloak-accounts-platform-db-organizations.md) | Account nằm ở Keycloak, Organization và Group nằm ở platform database |
| [0003](0003-groups-as-oidc-claims.md) | Group đi vào OIDC claim, Kubernetes tự authorize request |
| [0004](0004-workspace-is-one-capsule-tenant.md) | Mỗi Workspace là đúng một Capsule Tenant |
| [0005](0005-fixed-role-templates.md) | Workspace dùng ba role template cố định, không có RBAC tùy ý |
| [0006](0006-headlamp-separate-service-sso.md) | Headlamp là service riêng của mỗi Cluster, vào bằng SSO của portal |
| [0007](0007-cluster-grant-per-organization.md) | ClusterGrant và dedicated node gắn với Organization, không gắn với account |
| [0008](0008-workspace-fixed-to-one-cluster.md) | Workspace nằm cố định trong đúng một Cluster |
| [0009](0009-single-cluster-mvp-multi-cluster-model.md) | MVP chạy một Cluster, data model multi-cluster từ đầu |
| [0010](0010-crd-controller-provisioning.md) | Provisioning qua outbox, CRD và controller; Cluster Agent mTLS để Phase 2 |
| [0011](0011-dedicated-node-per-organization.md) | Dedicated node thuộc Organization, mỗi Workspace giữ một phần capacity |
| [0012](0012-per-workspace-calico-ippool.md) | Mỗi Workspace một Calico IPPool, chống CIDR overlap bằng IPAM trong transaction và exclusion constraint |
| [0013](0013-kubelogin-oidc-per-cluster-capsule-proxy.md) | Kubeconfig dùng kubelogin exec OIDC và trỏ vào Capsule Proxy của từng Cluster |
| [0014](0014-organization-requires-admin-approval.md) | Organization phải được Platform Admin duyệt, không tự tạo kèm quota mặc định |
| [0015](0015-platform-admin-realm-role.md) | Platform Admin là account cùng realm, phân biệt bằng realm role `platform-admin` |
| [0016](0016-kubernetes-names-from-slugs.md) | Tên Kubernetes-visible là `<org-slug>-<ws-slug>`, hai slug immutable |
| [0017](0017-shared-soft-dedicated-hard.md) | Shared là soft allocation có overcommit ratio, Dedicated là hard reservation |
| [0018](0018-gated-organizations-single-owner.md) | User không tự tạo Organization, và mỗi Organization có đúng một OrgOwner |
| [0019](0019-namespaces-through-declarative-cr.md) | Namespace UI của portal đi qua CR khai báo, không gọi Kubernetes API bằng token của user |
| [0020](0020-routes-and-shared-wildcard-cert.md) | Tenant khai báo Route thay vì tự viết Ingress, mọi Route dùng chung một wildcard cert |
| [0021](0021-postgresql-without-redis.md) | PostgreSQL kiêm kho phiên, hàng đợi và idempotency, không thêm Redis |
| [0022](0022-portal-session-follows-keycloak-sso.md) | Phiên portal sống đúng bằng SSO session của Keycloak, không có đồng hồ riêng |

Trạng thái: cả 22 ADR là Accepted, ngày 2026-11-23 (M12). Một quyết định đổi thì viết ADR mới thay thế, ghi "Superseded by ADR-NNNN" vào ADR cũ, không sửa nội dung cũ.
