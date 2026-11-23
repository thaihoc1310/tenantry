# ADR-0019: Namespace UI của portal đi qua CR khai báo, không gọi Kubernetes API bằng token của user

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §0 (v0.4 → v0.5 điểm 7, v0.5 → v0.6 điểm 8), §4.5, §14.2, §14.3](../PRODUCT-PLAN.md), [03 mục 7a, 7b](../architecture/03-flows.md), [06 mục 1, 4, 7](../architecture/06-security-and-identity.md), [04 mục 9.2](../architecture/04-state-machines.md)

## Bối cảnh

Portal quản lý Namespace của Workspace: liệt kê, tạo, xóa, xem usage; workload bên trong để cho Headlamp và `kubectl` (§4.5). Namespace cũng tạo được bằng `kubectl` hay Headlamp, đi qua Capsule như mọi request của tenant.

Có hai đường để nút "Tạo Namespace" trên portal biến thành một Namespace thật:

- portal gọi kube-apiserver ngay trong request, bằng token của user hoặc bằng ServiceAccount của chính nó;
- portal ghi ý định vào DB, và đường provisioning sẵn có (outbox, `PlatformWorkspace`, controller) đưa ý định đó xuống Cluster.

Các ràng buộc đã có từ trước:

- Token của client `portal` có audience `portal-api`, kube-apiserver không nhận; Pod API không mount token ServiceAccount ([06 mục 1, 2](../architecture/06-security-and-identity.md)).
- Provisioning Worker là thành phần duy nhất có credential vào Cluster, và chỉ có quyền trên CRD `tenantry.io` ([06 mục 7](../architecture/06-security-and-identity.md)).
- Phase 2 thay worker bằng Cluster Agent kéo assignment qua outbound mTLS; central plane không cần đường mạng vào kube-apiserver của từng Cluster (§14.3, [ADR-0010](0010-crd-controller-provisioning.md)).
- Namespace quota là một con số của Workspace trong DB, và trần của grant là `Σ namespace_quota` (§5.3).

## Quyết định

- Danh sách Namespace mong muốn nằm trong `PlatformWorkspace.spec.namespaces` (chỉ hậu tố); xóa là bỏ khỏi `spec.namespaces` và ghi tên đầy đủ vào `spec.namespacesToDelete` (§14.2).
- API kiểm quyền và quota trong DB, ghi `workspace_namespaces` (`source=portal`), Operation `namespace.create`/`namespace.delete`, outbox event và audit trong một transaction rồi trả `202` ([03 mục 7a](../architecture/03-flows.md)).
- Operator tạo Namespace dưới Tenant. ServiceAccount của operator là Capsule user và owner của mọi Tenant, nên Namespace đi đúng đường owner của Capsule: tiền tố, quota Tenant, metadata, replication.
- Namespace tạo bằng `kubectl`/Headlamp hiện lên portal qua `status.namespaces` (`source=kubectl`); worker upsert về DB. Portal xóa được cả Namespace đó qua `namespacesToDelete`; operator chỉ xóa Namespace Capsule gắn với đúng Tenant.
- **Ai được thao tác** là quyết định của lớp 1 (portal): OrgOwner, OrgAdmin, hoặc member của group giữ `TenantOwner` tại Workspace. Developer/Viewer chỉ xem (§4.5). Đây là một trong hai chỗ portal đọc binding Kubernetes để quyết định quyền portal ([06 mục 4](../architecture/06-security-and-identity.md)).

## Phương án đã cân nhắc

### Portal gọi Kubernetes API bằng token của user

Không chọn:

- Portal phải xin và giữ token có audience `kubernetes` cùng group claim, trong khi client `portal` cố ý không có group mapper. Portal thành nơi giữ credential Kubernetes của mọi user.
- Quyền portal và quyền Kubernetes phải trùng nhau. OrgOwner không thuộc group nào được bind sẽ không tạo được Namespace trên portal, trái với quy tắc OrgOwner/OrgAdmin được thao tác.
- Token phản ánh group trễ tới 5 phút ([ADR-0003](0003-groups-as-oidc-claims.md)), nên portal sẽ hiện quyền lệch với DB.
- Platform API phải có đường mạng tới kube-apiserver của từng Cluster, không khớp với Cluster Agent kéo assignment ở Phase 2.

### Portal gọi Kubernetes API bằng ServiceAccount có quyền tạo Namespace

Không chọn: API là tiến trình public-facing; cho nó quyền ghi Namespace (hoặc impersonate user) trên Cluster là mở rộng đúng thứ [06 mục 1](../architecture/06-security-and-identity.md) muốn tránh. Hiện API không cầm credential Kubernetes nào; bị chiếm thì chỉ đi vòng được qua DB và worker, vẫn trong khuôn CR mà controller giữ ([01 mục 2](../architecture/01-system-context.md)).

### Không có Namespace UI, chỉ `kubectl`/Headlamp

Đã loại ở v0.5 (PRODUCT-PLAN §0, điểm 7): người không dùng `kubectl` cần tạo được Namespace và thấy usage theo Namespace trên portal.

## Hệ quả

- Tạo Namespace là bất đồng bộ: `202` kèm Operation, portal theo dõi qua SSE hoặc polling. Một Workspace chỉ có một Operation chạy tại một thời điểm, nên tạo Namespace khi Workspace chưa `Ready`/`Degraded` hoặc đang có Operation bị `409 WORKSPACE_NOT_READY`.
- Namespace quota kiểm hai lớp: DB đếm cả Namespace `source=kubectl` (vượt trả `409 CONFLICT`, `details.limit=namespaceQuota`), Capsule kiểm lại ở admission. Đường `kubectl` chỉ có lớp Capsule, trễ khoảng 1 giây ([known-limitations](../report/known-limitations.md) mục 6).
- Pod trong Namespace vừa tạo bị từ chối vài giây tới khi NetworkPolicy baseline có mặt ([known-limitations](../report/known-limitations.md) mục 16).
- Hai lớp quyền có thể lệch có chủ ý: OrgOwner không thuộc group được bind tạo được Namespace trên portal nhưng `kubectl` bị từ chối ([06 mục 4](../architecture/06-security-and-identity.md)).
- Owner của Workspace có quyền admin trong Namespace nên sửa được ProxySetting của Namespace mình ([known-limitations](../report/known-limitations.md) mục 5); không liên quan đường portal nhưng là hệ quả của việc Namespace cũng tạo được bằng `kubectl`.

## Kiểm chứng

- `backend/internal/httpapi/workspaces_test.go`, `TestWorkspaceNamespaces`: tạo khi Workspace đang Provisioning bị `409 WORKSPACE_NOT_READY`; Developer thấy danh sách nhưng tạo bị `403`, tạo được sau khi group của mình giữ `TenantOwner`; CR mang Namespace mới trong `spec.namespaces` ở version mới; vượt `namespaceQuota` (đếm cả Namespace kubectl) bị `409`; xóa Namespace kubectl ghi tên đầy đủ vào `namespacesToDelete`.
- `web/e2e/workspace.spec.ts` trên lab: Namespace xin trên portal có mặt trên cụm; Namespace do member group TenantOwner tạo bằng `kubectl` hiện lên portal với `source` kubectl.
- [e2e-personas](../report/e2e-personas.md): Namespace `frontend` tạo trên portal, Operation `Succeeded`; Bob (Viewer) tạo Namespace trên portal bị `403`.
- Lab, [isolation-cluster](../report/isolation-cluster.md) (T19): Namespace thứ tư trên quota 3 bị Capsule từ chối; Namespace mang tiền tố của Workspace khác bị `forceTenantPrefix` từ chối.
- `web/e2e/access.spec.ts` (kết quả M11 ghi ở IMPLEMENTATION-PLAN M12): Developer không tạo được Namespace qua proxy.
- NFR "Namespace create p95 dưới 15 giây" (PRODUCT-PLAN §23), đo 20 Namespace trên lab ([namespace-latency](../report/namespace-latency.md)) qua bốn lần:
  - Bản đầu (operator a8928a8): phía người dùng (Operation) p95 72,6 s, vì probe Capsule Proxy chờ 30 s mỗi lần Proxy chưa thấy Namespace mới. Đã sửa (e35daa1: thử lại sau 2 s).
  - Sau bản sửa đó: p95 13,8 s và 15,0 s ở hai lần đo, nhưng có Operation xong trước khi NetworkPolicy có mặt.
  - Bản hiện tại (d96f980, operator rev 15): Operation chỉ xong khi mọi Namespace đã có đủ bản sao LimitRange và NetworkPolicy. Phía cụm p95 9 s; phía người dùng p95 16,1 s (p50 11,9 s, max 18,1 s), tức **không đạt** 15 s ở phía người dùng.
  - Tạo Workspace: phần lớn 3–5 s, nhưng 65–69 s khi GlobalTenantResource giữ Ready=False sau race của Capsule tới lần resync 60 s. Bản sửa 3f15433 dựa vào bản sao có thật khi status chỉ chậm vì race; chưa deploy và chưa đo lại.
