# ADR-0009: MVP chạy một Cluster, data model multi-cluster từ đầu

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §0 (v0.4 → v0.5 điểm 1), §1, §2.2, §6.1, §25, §31](../PRODUCT-PLAN.md), [IMPLEMENTATION-PLAN §5.1, §7 Phase 2](../IMPLEMENTATION-PLAN.md), [01 mục 3, 4](../architecture/01-system-context.md), [02 mục 3, 4](../architecture/02-data-model.md)

## Bối cảnh

Tenantry là một control plane trung tâm quản lý các Kubernetes cluster có sẵn do Platform Admin đăng ký ([PRODUCT-PLAN §1](../PRODUCT-PLAN.md)). Đồ án do một người làm, và lab chỉ có một cụm bốn node.

Nếu data model chỉ biết một cụm, thêm cụm thứ hai về sau phải sửa mọi bảng gắn với Workspace, mọi invariant (CIDR, tên Namespace, capacity), API và wizard. Nếu làm multi-cluster đầy đủ ngay thì cần cụm thứ hai, Cluster Agent, heartbeat và tách central plane ra ngoài cụm.

## Quyết định

MVP vận hành đúng một Cluster (`cluster-a`), nhưng mọi thứ đã theo Cluster ([§31](../PRODUCT-PLAN.md) quyết định 10):
- Cluster registry `clusters` giữ trạng thái, `proxy_endpoint`, `console_url`, `apps_domain`, `reserved_cidrs`. Mọi service đọc endpoint và console URL từ registry, không hard-code ([01 mục 4](../architecture/01-system-context.md));
- ClusterGrant theo cặp (Organization, Cluster), `UNIQUE (org_id, cluster_id)`, đúng một grant mặc định mỗi Organization;
- shared pool, NetworkPool, dedicated node grant, Workspace và các row con đều mang `cluster_id`, nối bằng FK ghép ([02 mục 3, 4](../architecture/02-data-model.md));
- outbox event có `target_cluster_id` bắt buộc; worker provisioning chạy theo Cluster (`TENANTRY_CLUSTER_ID`); CR do worker tạo mang nhãn `tenantry.io/cluster-id` ([IMPLEMENTATION-PLAN §5.1](../IMPLEMENTATION-PLAN.md));
- vòng đời Cluster `Registered/Ready/Maintenance/Unavailable`, MVP do Admin đặt tay ([§6.1](../PRODUCT-PLAN.md));
- wizard có bước chọn Cluster, MVP tự preselect vì chỉ có một grant.

Central plane (portal, API, worker, PostgreSQL, Keycloak) chạy trên system pool của chính `cluster-a` ở MVP. Phase 2 là M13 (Cluster thứ hai), M14 (Cluster Agent, tách central plane), M15 (đánh giá multi-cluster) ([IMPLEMENTATION-PLAN §7](../IMPLEMENTATION-PLAN.md)).

## Phương án đã cân nhắc

### Chỉ một cụm, thêm multi-cluster khi cần

Bỏ `cluster_id`, grant gắn thẳng Organization. Không chọn vì Phase 2 sẽ phải migrate schema của mọi bảng, đổi API và viết lại các invariant đang kiểm "trong Cluster". Đây chính là điều changelog v0.4 → v0.5 điểm 1 muốn tránh.

### Multi-cluster đầy đủ ngay trong MVP

Không chọn vì quá sức một người trong MVP: cần cụm thứ hai trên lab, Cluster Agent mTLS và heartbeat ([ADR-0010](0010-crd-controller-provisioning.md)), tách central plane ra ngoài. M14 riêng được ước lượng XL, 8 tuần trong sơ đồ milestone ([IMPLEMENTATION-PLAN §7, §8](../IMPLEMENTATION-PLAN.md)). Theo [§26.2](../PRODUCT-PLAN.md), phần không được cắt là isolation, reservation, group mapping và delete cleanup; multi-cluster không nằm trong đó.

## Hệ quả

Mặt tốt:
- Phase 2 dự kiến chỉ thêm row (Cluster, grant, pool), không đổi schema. Đây là thiết kế, chưa được chứng minh bằng một Cluster thứ hai thật.
- Các trạng thái Cluster đã hoạt động: Cluster `Maintenance` chặn tạo grant, duyệt Organization, tạo và resize Workspace; Cluster `Unavailable` giữ event trong outbox.

Mặt xấu và giới hạn:
- Phức tạp hơn ngay từ đầu: FK ghép, `cluster_id` trong mọi truy vấn, bước Cluster trong wizard thường bị ẩn.
- Id của shared pool và NetworkPool là DNS label duy nhất toàn platform, không theo Cluster; Cluster thứ hai phải đặt tên pool khác ([02 mục 3](../architecture/02-data-model.md)).
- Còn vài chỗ mặc định một Cluster: link sâu Headlamp giả định `inClusterContextName` bằng cluster id ([known-limitations](../report/known-limitations.md) mục 4); một provisioning worker mỗi Cluster, không leader election (mục 19); OIDC client Headlamp đặt tên theo Cluster (`headlamp-cluster-a`), Cluster mới cần client mới ([§2.2](../PRODUCT-PLAN.md)).
- Central plane nằm trong `cluster-a`: cụm này hỏng thì portal hỏng theo. Kịch bản "Cluster A unavailable, portal và Cluster B vẫn chạy" chỉ có ở Phase 2.
- Trạng thái Cluster đặt tay, không có heartbeat; worker chỉ ghi `last_health_check`.

## Kiểm chứng

Các test dưới đây dùng một `cluster-b` trong DB test:
- `backend/internal/httpapi/admin_test.go` (`TestApproveRefused`): duyệt Organization với grant trên Cluster `Maintenance` trả `409 CLUSTER_NOT_READY`.
- `backend/internal/httpapi/admin_clusters_test.go` (`TestClusterRegistry`, `TestClusterGrants`): grant mới trên Cluster `Maintenance` trả 409; `clusterId` trong body khác đường dẫn trả 400.
- `backend/internal/httpapi/catalog_test.go` (`TestCatalog`): Cluster chưa được grant trả 404 cho OrgOwner.
- `backend/internal/httpapi/workspaces_test.go` (`TestCreateWorkspaceRefusals`): tạo Workspace trên Cluster chưa grant trả `409 CLUSTER_NOT_GRANTED`.
- `backend/internal/outbox/outbox_test.go` (`TestClusterScopeAndUnavailable`): worker chỉ giao event của Cluster của nó; Cluster `Unavailable` không bị giao event.
- Chưa kiểm: chưa đăng ký Cluster thứ hai thật, chưa chạy hai worker cho hai Cluster, chưa có Cluster Agent.
