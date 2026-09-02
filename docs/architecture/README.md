# Tenantry — Bộ sơ đồ kiến trúc

Nguồn sự thật là `../PRODUCT-PLAN.md` (v0.6). Các file dưới đây chỉ **vẽ lại** plan theo từng góc nhìn để review luồng; khi có mâu thuẫn, plan thắng và sơ đồ phải sửa.

Mọi sơ đồ là Mermaid, render trực tiếp trên GitHub/VS Code. Sau mỗi sơ đồ có mục **Điểm cần review** — đó là các câu hỏi/rủi ro mà người vẽ thấy khi chuyển plan thành hình; hãy đọc chúng như checklist review.

| File | Góc nhìn | Đọc khi muốn review |
|---|---|---|
| [01-system-context.md](01-system-context.md) | Context → Container → Deployment → Component (API, Controller) | Ai gọi ai, cái gì chạy ở đâu, ranh giới module |
| [02-data-model.md](02-data-model.md) | ERD theo vùng + bảng invariant và cách enforce | Schema, constraint, thứ tự lock, ánh xạ DB → CR → K8s |
| [03-flows.md](03-flows.md) | Sequence diagram cho 13 luồng nghiệp vụ | Từng bước của signup, invite, tạo/xóa Workspace, access, revoke… |
| [04-state-machines.md](04-state-machines.md) | State machine của Workspace, Operation, Reservation, CIDR, Node, Cluster, Invitation… | Trạng thái nào được phép đi đâu, side effect của từng cạnh |
| [05-network-and-scheduling.md](05-network-and-scheduling.md) | Node layout, taint/label, capacity model, IP plan, NetworkPolicy, Ingress/TLS, storage | Cô lập compute/network/ingress được enforce thế nào |
| [06-security-and-identity.md](06-security-and-identity.md) | Trust boundary, OIDC client/audience, hai lớp authz, revocation, credential nội bộ, ma trận quyền | Token nào đi đâu, ai được làm gì |
| [07-ux-wireflow.md](07-ux-wireflow.md) | Sitemap, wireflow theo persona, wizard, màn hình lỗi, khu Admin | Trải nghiệm portal từ đầu tới cuối |

| [REVIEW-CHECKLIST.md](REVIEW-CHECKLIST.md) | ~55 điểm mở gom từ mọi sơ đồ, mỗi điểm có đề xuất | Duyệt một lượt và đánh ✅/✏️ |

Kế hoạch thực hiện (repo layout, kiến trúc code, milestone M0–M15, test, rủi ro): [`../IMPLEMENTATION-PLAN.md`](../IMPLEMENTATION-PLAN.md).

Thứ tự đọc gợi ý cho lần review đầu: 01 → 03 → 04 → 02 → 05 → 06 → 07 → IMPLEMENTATION-PLAN.
