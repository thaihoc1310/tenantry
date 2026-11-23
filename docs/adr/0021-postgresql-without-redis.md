# ADR-0021: PostgreSQL kiêm kho phiên, hàng đợi và idempotency, không thêm Redis

- Trạng thái: Accepted, 2026-11-23
- Liên quan: [PRODUCT-PLAN §0 (v0.7 → v0.8 điểm 4, 5), §13.2, §14.3](../PRODUCT-PLAN.md), [06 mục 3.2, 3.3, 6](../architecture/06-security-and-identity.md), [02 mục 2, 4](../architecture/02-data-model.md), [IMPLEMENTATION-PLAN §3.4, §5.1, §5.3, §12 quyết định 8, 11](../IMPLEMENTATION-PLAN.md)

## Bối cảnh

Ngoài dữ liệu nghiệp vụ, backend cần ba thứ mà nhiều hệ thống đặt ở Redis hoặc message broker:

- **Kho phiên portal**: cookie phiên trỏ tới refresh token đã mã hóa, realm role, thời hạn ([06 mục 3.2](../architecture/06-security-and-identity.md)).
- **Hàng đợi** việc đẩy xuống Cluster: tạo, sửa, xóa `PlatformWorkspace` và các CRD phụ, giao at-least-once, đúng thứ tự trong từng resource (§14.3).
- **Kho idempotency**: `Idempotency-Key` của các `POST` tạo resource, để retry mù không tạo trùng (IMPLEMENTATION-PLAN §3.4).

Cả ba phải nhất quán với dữ liệu nghiệp vụ. Outbox event phải commit cùng transaction giữ chỗ quota; nếu không, DB đã commit mà Kubernetes không bao giờ nhận được việc (§14.3). Response đã lưu của một idempotency key phải commit cùng thay đổi nó mô tả. Mỗi request vốn đã đọc PostgreSQL để tra `users.status` và `organization_members`.

Quy mô MVP: một Cluster, một replica API, một provisioning worker mỗi Cluster; mục tiêu 20–50 Workspace (§23).

## Quyết định

- **Không dùng Redis, Kafka, RabbitMQ hay Temporal** (§13.2; IMPLEMENTATION-PLAN §12 quyết định 11).
- **Phiên** ở bảng `sessions`: khóa là `sha256` của cookie, refresh token mã hóa AES-256-GCM. Thu hồi mọi phiên của một user là một câu `DELETE ... WHERE user_id`. Hết hạn bằng vòng `housekeeping` mỗi phút thay cho TTL.
- **Hàng đợi** là `outbox_events`: worker poll mỗi 1s, batch 10, `FOR UPDATE SKIP LOCKED`, không cần lease (worker chết thì transaction rollback, lock tự nhả). Thứ tự theo `ordering_key = <resource_type>/<resource_id>`: event chỉ được lấy khi không còn event cũ hơn chưa giao cùng key. Backoff 1s → 5 phút; 10 lỗi liên tiếp thì Operation `Failed` (IMPLEMENTATION-PLAN §5.1, §12 quyết định 8). Group Sync Worker không dùng outbox: nó đọc thẳng `sync_status` của `groups`/`group_members`/`users`, cùng kiểu `SKIP LOCKED`.
- **Idempotency** ở bảng `idempotency_keys`, khóa `(user_id, key)`. Middleware giành key bằng `INSERT ... ON CONFLICT DO NOTHING` ở **đầu** transaction, handler chạy trong chính transaction đó, response lưu rồi commit cùng thay đổi. Request trùng đến đồng thời đứng chờ ở câu `INSERT` rồi nhận lại response của request trước. Giữ 24 giờ.
- **Kích thước pool đặt rõ cho từng tiến trình**: API 20, worker 6, đổi bằng `TENANTRY_DB_MAX_CONNS` (2–90). Metric `tenantry_db_pool_*` cho số connection đang dùng, số lần và thời gian chờ.

## Phương án đã cân nhắc

### Redis cho phiên

Redis làm tốt nhất việc tự hết hạn theo TTL. Không chọn ([06 mục 3.3](../architecture/06-security-and-identity.md)): mỗi request vẫn phải đọc PostgreSQL, nên tra phiên theo khóa chính không thêm chặng mạng nào; Redis thêm một thành phần stateful phải deploy, backup, giám sát; và thêm một kiểu sự cố (Redis mất dữ liệu thì mọi người bị đăng xuất). TTL thay bằng một câu `DELETE` định kỳ.

### Message broker (RabbitMQ, Kafka, NATS) cho hàng đợi

Không chọn: ghi DB rồi publish là dual-write; muốn không mất việc thì vẫn phải có outbox trong DB rồi relay sang broker, tức là thêm một chặng. Thứ tự theo resource và "event của Cluster `Unavailable` giữ nguyên, không tăng `attempts`" là vài điều kiện SQL trên cùng bảng. Lưu lượng của MVP là vài event cho mỗi thao tác của người dùng.

### Redis cho idempotency key

Không chọn: key phải commit hoặc rollback cùng thay đổi nghiệp vụ. Ghi ở kho khác thì có cửa sổ key đã lưu mà thay đổi mất, hoặc ngược lại; request trùng đồng thời cũng không còn chỗ để đứng chờ.

### Workflow engine (Temporal)

Không chọn (§13.2): luồng provisioning là apply một CR rồi đọc status; state machine của Operation và Workspace đã nằm trong DB ([04](../architecture/04-state-machines.md)).

## Hệ quả

- PostgreSQL là điểm hỏng chung của API, hai worker và cả Keycloak (database riêng trên cùng instance, [01 mục 3](../architecture/01-system-context.md)). Thêm Redis không giảm điểm hỏng này.
- Tranh chấp connection thành vấn đề thật. M12 tìm thấy: refresh phiên giữ một connection và `SELECT ... FOR UPDATE` trong lúc gọi Keycloak (timeout 10s), pool mặc định chỉ 4 connection, vài request song song cùng một phiên làm cạn pool và mọi request khác treo; gặp 2 lần "awaiting headers" quá 30s trên lab (IMPLEMENTATION-PLAN M12; commit `41713e6`). [e2e-personas](../report/e2e-personas.md) cũng ghi portal hai lần không trả lời trong 30s. Sửa: đọc row rồi trả connection, gọi Keycloak, `UPDATE ... WHERE refresh_token_enc = <cũ>` (optimistic), singleflight mỗi phiên trong một process, pool rõ ràng và metric. Bài học: không giữ connection DB khi chờ dịch vụ ngoài.
- Hết hạn phiên và idempotency key chậm tới một vòng housekeeping (1 phút); args của worker từng không quote trong chart làm housekeeping không chạy suốt từ M9 tới khi phát hiện (IMPLEMENTATION-PLAN §5.1).
- Một provisioning worker mỗi Cluster, không leader election ([known-limitations](../report/known-limitations.md) mục 19). Nhiều replica an toàn nhờ `SKIP LOCKED` nhưng chưa cần.
- Rate limit là token bucket trong bộ nhớ, đúng cho một replica API. Điều kiện xem lại quyết định này: chạy nhiều replica API và cần rate limit chính xác toàn cục ([06 mục 3.3, 6](../architecture/06-security-and-identity.md)).
- API và hai worker dùng chung một role PostgreSQL đọc-ghi, nên chiếm một tiến trình là điều khiển được cả hai worker qua DB ([06 mục 1](../architecture/06-security-and-identity.md)). Đây là hệ quả của việc dùng DB làm hàng đợi chung mà không tách role.
- Metric scrape qua Service: nhiều replica API thì mỗi lần scrape chỉ thấy một Pod ([known-limitations](../report/known-limitations.md) mục 20). Độ trễ hàng đợi đọc được qua `tenantry_outbox_pending_events`, `tenantry_outbox_oldest_pending_seconds`, `tenantry_groupsync_pending`.

## Kiểm chứng

- Idempotency, `backend/internal/httpapi/idempotency_test.go`: `TestIdempotentCreate`, `TestIdempotentFailureIsNotKept`, `TestIdempotentConcurrentRepeats` (request lặp đồng thời chờ request đầu và nhận response của nó, không nhận `409` vì vấp slug vừa giữ), `TestIdempotentRetry`; `idempotency_internal_test.go`, `TestIdempotentChangeAndResponseCommitTogether` (không lưu được response thì thay đổi bị rollback theo).
- Outbox, `backend/internal/outbox/outbox_test.go`: `TestDeliversInOrderAndMarksOperationRunning`, `TestFailingEventHoldsBackOnlyItsOwnResource`, `TestFailuresBackOffThenFailTheOperation`, `TestConcurrentWorkersDeliverEachEventOnce`, `TestClusterScopeAndUnavailable`, `TestPermanentFailure`.
- Giao lại sau sự cố, `backend/internal/provisioning/status_test.go`: `TestRedeliveryCreatesNoDuplicate` (worker dừng sau apply, trước khi ghi nhận: giao lại, cụm vẫn một CR cùng generation), `TestClusterDownAfterCommit`, `TestTimeoutThenRetry`.
- Phiên, `backend/internal/httpapi/refresh_test.go`: `TestRefreshOnce`, `TestRefreshHoldsNoConnection` (fail trên code cũ, pass trên code mới theo IMPLEMENTATION-PLAN M12), `TestRefreshRefused`, `TestRefreshWhileKeycloakDown`, `TestRefreshKeepsSessionMax`.
- Hàng đợi Group Sync, `backend/internal/keycloak/sync_test.go`: `TestSyncWorkersShareTheQueue`, `TestSyncAfterCrash`, `TestSyncRetriesWithBackoff`.
- Housekeeping, `backend/internal/housekeeping/housekeeping_test.go`: `TestHousekeeping`. Pool, `backend/internal/config/config_test.go`: `TestDBMaxConns`.
- Thông lượng outbox dưới tải: chưa kiểm. [benchmark](../report/benchmark.md) tạo `PlatformWorkspace` trực tiếp, không đi qua API và outbox.
- Pool trên lab sau bản sửa, đọc từ Prometheus lúc lab rảnh ([observability](../report/observability.md)): api 0/20, worker-groupsync 1/6, worker-provisioning 3/6 connection, không có lần chờ nào. Pool dưới tải (nhiều phiên refresh cùng lúc trên lab): chưa kiểm.
- PostgreSQL restart không mất account/config (NFR §23), backup và HA của PostgreSQL: chưa kiểm.
