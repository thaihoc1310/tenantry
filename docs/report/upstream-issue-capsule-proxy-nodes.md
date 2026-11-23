# Capsule Proxy: đọc Node bỏ qua proxySettings

Ngày 2026-11-23 đã báo riêng cho maintainer qua GitHub private vulnerability reporting (GHSA-2v66-cjj2-h2v5, trạng thái triage), theo `SECURITY.md` của projectcapsule: lỗi bảo mật không mở issue công khai. Chi tiết kỹ thuật và cách tái hiện để trong báo cáo đó, chờ maintainer công bố advisory.

Ảnh hưởng tới Tenantry: không có. Lab chạy capsule-proxy với `--feature-gates=ProxyClusterScoped=true`, nên tenant không đọc được Node qua Proxy (`isolation-cluster.md` T13/T14, `known-limitations.md` mục 2).
