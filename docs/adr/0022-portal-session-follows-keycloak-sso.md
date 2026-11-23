# ADR-0022: Phiên portal sống đúng bằng SSO session của Keycloak, không có đồng hồ riêng

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §8.2, changelog v0.7 → v0.8 mục 1](../PRODUCT-PLAN.md), [02 mục 2](../architecture/02-data-model.md), [06 mục 3, 5](../architecture/06-security-and-identity.md), [IMPLEMENTATION-PLAN M12](../IMPLEMENTATION-PLAN.md), [ADR-0003](0003-groups-as-oidc-claims.md), [ADR-0015](0015-platform-admin-realm-role.md), [ADR-0021](0021-postgresql-without-redis.md)

## Bối cảnh

Portal là BFF: browser chỉ giữ cookie `__Host-tenantry_session`, token không xuống JavaScript (§8.2). Cùng một người còn đăng nhập Headlamp và kubelogin qua cùng realm. Bản plan trước v0.8 có hai bộ thời hạn mâu thuẫn nhau, một ở portal và một ở Keycloak (changelog v0.7 → v0.8 mục 1).

Những gì phải đúng:

- Gỡ `platform-admin` phải có hiệu lực trên portal trong một khoảng biết trước ([ADR-0015](0015-platform-admin-realm-role.md)).
- Admin disable user thì user mất phiên ngay.
- "Đăng xuất" phải có một nghĩa rõ ràng giữa portal, Headlamp và kubectl.

## Quyết định

- **Một bộ số duy nhất, đặt ở realm `tenantry`**, không override theo client, không bản sao ở portal ([06 mục 3.1](../architecture/06-security-and-identity.md); `deploy/keycloak/realm-tenantry.json`):

  | Tham số | Giá trị |
  |---|---|
  | Access Token Lifespan | 5 phút |
  | SSO Session Idle (cũng là hạn refresh token) | 8 giờ |
  | SSO Session Max | 24 giờ |
  | Revoke Refresh Token | tắt |

- **Phiên portal sống đúng bằng refresh token của nó** ([06 mục 3.2](../architecture/06-security-and-identity.md)). Row `sessions` giữ `sha256` của cookie, refresh token mã hóa AES-256-GCM, `realm_roles` và `access_expires_at`; `expires_at` lấy từ `refresh_expires_in` của Keycloak, không quá 24 giờ sau lần đăng nhập (`MaxSession`, `backend/internal/auth/auth.go`).
- **Refresh lười**: chỉ khi có request và access token đã hết hạn. Keycloak từ chối (`invalid_grant`: hết idle, hết max, logout, account disabled) thì xóa row, trả 401. Keycloak không trả lời thì trả 500, giữ row, request sau thử lại, không dùng tiếp role cũ.
- **Refresh đồng thời** (sửa ở M12): không giữ connection hay row lock trong lúc chờ Keycloak. Mỗi process gộp các request cùng phiên vào một lần refresh (`singleflight`); `UPDATE` chỉ ăn khi row còn giữ đúng refresh token lúc bắt đầu, để replica khác đã refresh trước thì không bị ghi đè; gặp `invalid_grant` thì đọc lại row trước khi kết thúc phiên ([IMPLEMENTATION-PLAN M12](../IMPLEMENTATION-PLAN.md), "Lỗi tìm thấy ở M12").
- **Logout portal là logout Keycloak**: xóa row và cookie trước, rồi gọi endpoint logout của Keycloak bằng refresh token hiện tại. Headlamp và kubelogin cũng phải đăng nhập lại.
- **Disable user** xóa mọi row `sessions` của user trong cùng transaction đặt `Disabled`; Group Sync Worker tắt account trên Keycloak sau đó.
- Quyền trong Organization không nằm trong phiên: mỗi request đọc DB ([ADR-0002](0002-keycloak-accounts-platform-db-organizations.md)).

## Phương án đã cân nhắc

### Portal có thời hạn phiên riêng

Ví dụ cookie portal sống 7 ngày độc lập với Keycloak. Không chọn: hai đồng hồ lệch nhau sinh ra trạng thái "portal còn đăng nhập nhưng Keycloak đã hết" (bấm Open Web Console lại bị hỏi password) hoặc ngược lại, và đó chính là mâu thuẫn bản v0.8 bỏ đi. Portal cũng phải tự lo idle, max và logout toàn cục.

### Token trong browser (SPA tự giữ access và refresh token)

Không cần bảng `sessions`. Không chọn: §8.2 chọn BFF để token không bao giờ xuống JavaScript; Route của tenant cùng site với portal ([06 mục 1](../architecture/06-security-and-identity.md)), nên giữ token ở nơi JavaScript đọc được là tăng rủi ro.

### Phiên stateless (JWT ký bởi portal trong cookie)

Không cần DB, nhưng không thu hồi được trước hạn: disable user không cắt được phiên ngay, logout chỉ xóa cookie ở một trình duyệt. Không chọn.

### Kiểm token với Keycloak ở mỗi request (introspection)

Thu hồi tức thì, nhưng mọi request portal phụ thuộc Keycloak và thêm một chặng mạng. Không chọn: refresh mỗi 5 phút đã cho trần thu hồi đủ dùng, và quyền Organization đã đọc DB ở mỗi request.

### Refresh token xoay vòng một lần (Revoke Refresh Token bật)

An toàn hơn nếu refresh token lộ. Không chọn: hai request đồng thời cùng refresh sẽ có một bên dùng token đã bị thu hồi và làm hỏng phiên ([06 mục 3.1](../architecture/06-security-and-identity.md)). Refresh token chỉ nằm trong DB đã mã hóa, không xuống browser.

## Hệ quả

- Hết idle 8 giờ, hết max 24 giờ, logout ở đâu đó, hay bị disable trên Keycloak thì phiên portal chết ở lần refresh kế tiếp, chậm nhất 5 phút.
- Gỡ `platform-admin` có hiệu lực trên portal chậm nhất 5 phút ([06 mục 5](../architecture/06-security-and-identity.md)).
- Logout portal đăng xuất người dùng khỏi mọi nơi dùng cùng SSO session; không có "chỉ thoát portal".
- Portal phụ thuộc Keycloak mỗi 5 phút cho mỗi phiên đang dùng: Keycloak chết quá 5 phút thì người đang dùng nhận 500 dù chưa bị đăng xuất.
- Đổi khóa mã hóa phiên là mọi người đăng nhập lại ([06 mục 7](../architecture/06-security-and-identity.md)).
- Phiên nằm trong PostgreSQL thay vì Redis ([ADR-0021](0021-postgresql-without-redis.md)); housekeeping xóa row hết hạn.
- Bài học M12: bản đầu giữ connection và `SELECT … FOR UPDATE` trong lúc gọi Keycloak (timeout 10s) với pool mặc định 4 connection; vài request song song của một phiên làm cạn pool, mọi request khác treo, đã gặp 2 lần "awaiting headers" quá 30s trên lab. Pool giờ đặt rõ (API 20, worker 6, `TENANTRY_DB_MAX_CONNS`) và có metric `tenantry_db_pool_*`.
- `singleflight` chỉ gộp trong một process. Nhiều replica API có thể cùng gọi Keycloak cho một phiên; `UPDATE` có điều kiện giữ row đúng, chỉ tốn thêm một lần refresh.

## Kiểm chứng

- `backend/internal/httpapi/refresh_test.go` (Keycloak giả):
  - `TestRefresh`: dùng role của lần đăng nhập tới khi access token hết hạn, refresh đúng một lần, lưu refresh token mới, phiên kéo dài theo `refresh_expires_in` (8 giờ);
  - `TestRefreshOnce`: 8 request đồng thời, 1 lần refresh;
  - `TestRefreshHoldsNoConnection`: Keycloak bị giữ, số request chờ bằng 3 lần `MaxConns`, phiên khác vẫn trả lời trong 5 giây; fail trên code cũ, pass trên code mới (IMPLEMENTATION-PLAN M12);
  - `TestRefreshRefused`: SSO session hết thì 401 và row bị xóa; `TestRefreshWhileKeycloakDown`: 500, row giữ, request sau thành công;
  - `TestRefreshKeepsSessionMax`: không kéo quá 24 giờ; `TestRefreshWithRotatedKey`: đổi khóa thì phiên kết thúc;
  - `TestLogout`, `TestLogoutNeedsCSRF`, `TestLogoutWithNothingToEnd`: logout gọi Keycloak bằng refresh token hiện tại và kết thúc SSO session.
- Disable xóa mọi phiên, kể cả phiên trên thiết bị khác: `TestDisableEnableUser` (`backend/internal/httpapi/admin_orgs_test.go`).
- Trên lab: `web/e2e/onboarding.spec.ts` "the session outlives the access token" chờ 5 phút 20 giây rồi kiểm `GET /api/v1/me` vẫn 200; chỉ chạy khi đặt `E2E_SLOW=1`, chưa thấy report ghi kết quả.
- Logout portal kéo theo Headlamp và kubelogin phải đăng nhập lại: chỉ kiểm với Keycloak giả (`TestLogout`), trên lab chưa kiểm.

## Câu hỏi cần soát

- [06 mục 3.2](../architecture/06-security-and-identity.md) (sequence diagram và gạch "Refresh lười") vẫn mô tả `SELECT ... FOR UPDATE` trong lúc gọi Keycloak; code từ commit `41713e6` đã đổi sang cách không giữ lock. Cần sửa 06 cho khớp.
