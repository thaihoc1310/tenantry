# Capsule Proxy: báo cáo bảo mật gửi riêng

Ngày 2026-11-23 đã báo riêng cho maintainer qua GitHub private vulnerability reporting (GHSA-2v66-cjj2-h2v5, trạng thái triage). Lý do không mở issue công khai: `SECURITY.md` của projectcapsule yêu cầu báo riêng mọi lỗi bảo mật. Chi tiết kỹ thuật chỉ ghi vào tài liệu này sau khi maintainer công bố advisory.

Ảnh hưởng tới Tenantry: không có. Lab chạy capsule-proxy với `--feature-gates=ProxyClusterScoped=true` (`architecture/05-network-and-scheduling.md` mục 8, `isolation-cluster.md` T13/T14, `known-limitations.md` mục 2).
