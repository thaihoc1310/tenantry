# Capsule Proxy: đọc Node bỏ qua proxySettings

Đã báo upstream: **projectcapsule/capsule-proxy#1158** (issue công khai, 2026-11-23). Bản đầu gửi riêng qua private vulnerability reporting (GHSA-2v66-cjj2-h2v5), sau khi trao đổi với maintainer thì đăng công khai để ai cũng sửa được.

Tóm tắt (capsule-proxy v0.15.3, gate `ProxyClusterScoped` tắt):
- `internal/modules/node/list.go` và `get.go` lọc Node theo `Tenant.spec.nodeSelector` (`utils.GetNodeSelectors`) mà không gọi `ProxyTenant.RequestAllowed(req, NodesProxy)`. Module StorageClass thì có gọi.
- Request đã lọc đi bằng ServiceAccount của Proxy, nên RBAC của người gọi không áp vào.
- Owner của Tenant hay subject của ProxySetting đọc được toàn bộ object Node của pool: nhãn, IP nội bộ, `status.images` (gồm image của tenant khác).

Ảnh hưởng tới Tenantry: không có. Lab chạy capsule-proxy với `--feature-gates=ProxyClusterScoped=true` (`architecture/05-network-and-scheduling.md` mục 8, `isolation-cluster.md` T13/T14, `known-limitations.md` mục 2).
