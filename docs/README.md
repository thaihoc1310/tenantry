# Tenantry — Bản đồ tài liệu

Mỗi chủ đề có **đúng một nơi định nghĩa**. File khác chỉ trỏ về nơi đó, không chép lại. Khi sửa một quyết định, sửa ở nơi định nghĩa; nếu thấy hai file nói khác nhau thì nơi định nghĩa thắng và file kia phải sửa.

| Chủ đề | Nơi định nghĩa |
|---|---|
| Nghiệp vụ, phạm vi, khái niệm, quy tắc, FR/NFR, roadmap, demo | [`PRODUCT-PLAN.md`](PRODUCT-PLAN.md) |
| Danh sách endpoint | [`PRODUCT-PLAN.md`](PRODUCT-PLAN.md) mục 16; hình dạng request/response và mã lỗi: `backend/openapi/tenantry.v1.yaml` |
| Bảng, cột, constraint, thứ tự lock | [`architecture/02-data-model.md`](architecture/02-data-model.md); DDL thật: `backend/migrations/` |
| Trạng thái và cạnh chuyển của mọi entity | [`architecture/04-state-machines.md`](architecture/04-state-machines.md) |
| Phiên portal, thời hạn token, OIDC client, thu hồi quyền, rate limit, credential nội bộ, ma trận quyền | [`architecture/06-security-and-identity.md`](architecture/06-security-and-identity.md) |
| Node, pool, capacity, IP plan, NetworkPolicy, Ingress/TLS, storage | [`architecture/05-network-and-scheduling.md`](architecture/05-network-and-scheduling.md) |
| Cấu trúc repo, layering, transaction, worker, tham số tuning, milestone, test | [`IMPLEMENTATION-PLAN.md`](IMPLEMENTATION-PLAN.md) |

Các file còn lại là **góc nhìn**, không định nghĩa gì mới:

| File | Góc nhìn |
|---|---|
| [`architecture/01-system-context.md`](architecture/01-system-context.md) | Context → Container → Deployment → Component: ai gọi ai, cái gì chạy ở đâu |
| [`architecture/03-flows.md`](architecture/03-flows.md) | Sequence diagram từng luồng nghiệp vụ, đánh số bước để trích dẫn |
| [`architecture/07-ux-wireflow.md`](architecture/07-ux-wireflow.md) | Sitemap, wireflow theo persona, wizard, màn hình lỗi |
| [`architecture/REVIEW-CHECKLIST.md`](architecture/REVIEW-CHECKLIST.md) | Các câu hỏi **còn mở**; câu nào chốt xong thì ghi vào nơi định nghĩa rồi xóa khỏi checklist |

Sơ đồ là Mermaid, render trực tiếp trên GitHub/VS Code. Số trong ngoặc vuông như [5.3] trỏ về mục của `PRODUCT-PLAN.md`.

Thứ tự đọc lần đầu: PRODUCT-PLAN mục 1–4 → 01 → 03 → 04 → 02 → 06 → 05 → 07 → IMPLEMENTATION-PLAN.
