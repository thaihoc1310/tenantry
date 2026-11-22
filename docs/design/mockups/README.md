# Mockup portal Tenantry

Mở `index.html` bằng trình duyệt. Không cần build, không cần mạng (trừ font chữ; thiếu mạng thì dùng font hệ thống).

- Mọi màn hình dựng bằng Vue 3 và Element Plus thật, dữ liệu mẫu trong `data.js`. Không gọi API, không lưu gì.
- Nút "Mockup screens" ở góc dưới phải mở danh sách mọi màn và mọi trạng thái. Nút đó không thuộc sản phẩm.
- Địa chỉ sau dấu `#` trùng với route thật ở `IMPLEMENTATION-PLAN.md` mục 6.4. Tham số sau `?` chọn trạng thái (`state=failed`), vai (`as=developer`), bước wizard (`step=3`) hoặc hộp thoại (`dialog=route`).
- `#/components` trưng bày bộ component dùng chung.

| File | Nội dung |
|---|---|
| `theme.css` | Màu, chữ, khung trang; biến màu của Element Plus |
| `components.js` | Bộ component dùng chung và router nhỏ theo `#` |
| `pages-onboarding.js` | Xin tạo Organization, lời mời, nhận quyền Owner |
| `pages-org.js` | Overview, Workspaces, Members, Groups, Clusters, Audit, Settings |
| `pages-workspace.js` | Wizard tạo Workspace, trang chi tiết Workspace và các tab |
| `pages-admin.js` | Khu Platform Admin |
| `app.js` | Bảng route, danh sách màn, trang trưng bày component |
| `vendor/` | Bản ghim của Vue 3.5.46, Element Plus 2.14.8, icons 2.3.2 |

Đây là bản tham chiếu về bố cục, nội dung và câu chữ. Code thật trong `web/` viết lại bằng SFC và TypeScript, không chép từ đây.
