# Load test của platform API (ADR-0021)

Chạy ngày 2026-11-24, 03:47–04:04 UTC (10:47–11:04 +07), bằng `make load-test LOAD=full` từ laptop dev.
- Script: `test/load/api.js` (k6 `grafana/k6:1.1.0`, image pin trong `test/load/k6-image`).
- Runner: `web/e2e/load.spec.ts`.
- Bản đang chạy trên lab: backend `src-74a3593aa136` (API và hai worker), operator rev 17 (`src-4ddc77daaeb0`). Mỗi thành phần 1 replica.

Kết quả thô nằm ở `test/load/results/2026-11-24T03-47-05-full/`, không commit:
- `summary.json` của k6;
- `k6.log`;
- `metrics.jsonl`, lấy mẫu mỗi 15 s;
- `server.json`;
- `network.json`;
- `probe/`.

## Đường mạng và probe

| | |
|---|---|
| Đường tới portal | `wg0` (mạng nhà, WireGuard qua VPS), lấy từ `ip route get` |
| Probe | 10 vòng tạo, liệt kê rồi thu hồi một invitation, cách nhau 3 s, chạy trong cùng container k6 |
| Kết quả probe | 30 request, 0 lỗi, không có EOF. DELETE trung vị 90 ms, tối đa 114 ms. p95 của mọi request 94 ms |

Probe sẽ dừng cả lần chạy nếu có request lỗi hoặc một DELETE quá 3 s. Lỗi EOF của `docs/report/eof-path.md` không xuất hiện.

## Tải

- **Organization:** 10 thành viên OrgAdmin, 4 Shared Workspace nhỏ, mỗi Workspace có binding của group `toggled`.
- **Số VU:** tăng 10 → 25 → 50 → 100, mỗi mức gồm 1 phút tăng và 3 phút giữ, tổng 16 phút.
- **Mỗi vòng lặp của một VU là một trong ba việc:**
  - 70 %: một lần đọc (`/me`, danh sách Workspace, một Workspace, usage, một operation);
  - 25 %: đổi role của binding qua outbox, rồi chờ operation kết thúc;
  - 5 %: tạo rồi xoá một Namespace.
- **Ngưỡng dừng:**
  - p95 quá 5 s, hoặc lỗi quá 5 % (k6 tự dừng);
  - kết nối Postgres quá 80 % (runner dừng k6).

  Không ngưỡng nào bị chạm.

## Kết quả chính

| | Phía client (k6, qua WAN) | Phía server (histogram của API) |
|---|---|---|
| Request | 32 302 (33,2/s trung bình, 74/s ở mức 100 VU) | 32 302 request API, cùng số |
| p50 | 93 ms | dưới 5 ms |
| p95 | 1 322 ms | khoảng 5 ms (bucket thấp nhất là 5 ms) |
| p99 | 3 265 ms | 16–22 ms |
| Tối đa | 30 s (2 request timeout) | không request nào quá 5 s; 0 lỗi 5xx |
| Lỗi theo định nghĩa của script | 2 trên 32 302 (0,01 %), đều là timeout 30 s phía client | — |

Có 7 927 phản hồi 409 `WORKSPACE_NOT_READY`/`CONFLICT`. Đây không phải lỗi: 100 VU cùng đổi binding trên 4 Workspace, nên một Workspace thường đang bận một operation khác. Vì vậy `http_req_failed` của k6 báo 24,5 %. 429 và 503 `SERVICE_BUSY` đều bằng 0.

### Theo mức tải (phía server, delta giữa hai mẫu ở ranh giới mỗi mức)

| Mức | Request API | Request/s | p95 server | p99 server | 5xx | Chờ connection DB (tổng) | Kết nối Postgres tối đa | Outbox đã giao |
|---|---|---|---|---|---|---|---|---|
| 10 VU | 1 744 | 7,2 | 9 ms | 22 ms | 0 | 0 s | 15/100 | 125 |
| 25 VU | 4 549 | 19,0 | 6 ms | 19 ms | 0 | 0 s | 15/100 | 177 |
| 50 VU | 8 586 | 35,6 | 5 ms | 16 ms | 0 | 0,09 s | 22/100 | 198 |
| 100 VU | 17 681 | 74,0 | 5 ms | 16 ms | 0 | 0,76 s | 31/100 | 240 |

Trước khi bắt đầu, Postgres có 12 kết nối client.

CPU và bộ nhớ cao nhất (`kubectl top` mỗi 15 s):

| Pod | CPU | Bộ nhớ |
|---|---|---|
| `tenantry-api` | 86m | 25 Mi |
| `postgres-0` | 78m | 171 Mi |
| `tenantry-worker-provisioning` | 10m | 14 Mi |
| `tenantry-worker-groupsync` | 1m | 11 Mi |

### Theo endpoint

| Endpoint | Request (server) | p95 server | p95 client | Tối đa client |
|---|---|---|---|---|
| `GET /me` | 4 095 | khoảng 5 ms | 1 490 ms | 30 001 ms (timeout) |
| `GET /organizations/{org}/workspaces` | 4 026 | khoảng 5 ms | 1 528 ms | 17 839 ms |
| `GET /workspaces/{id}` | 4 016 | khoảng 5 ms | 1 353 ms | 14 463 ms |
| `GET /workspaces/{id}/usage` | 4 057 | khoảng 5 ms | 1 372 ms | 29 912 ms |
| `GET /operations/{id}` | 6 960 | khoảng 5 ms | 1 349 ms | 22 617 ms |
| `PUT /workspaces/{id}/bindings/{group}` | 7 330 | 9 ms | 882 ms | 30 003 ms (timeout) |
| `POST /workspaces/{id}/namespaces` | 1 463 | 11 ms | 306 ms | 4 318 ms |
| `DELETE /workspaces/{id}/namespaces/{name}` | 355 | 23 ms | 1 186 ms | 10 565 ms |

Thời gian từ lúc ghi tới lúc operation kết thúc (`operation_wait` của k6, gồm outbox, operator và worker đồng bộ status): trung vị 3,9 s, p95 10,1 s, tối đa 18,1 s.

Thời gian phía client, tách theo pha của k6:
- `http_req_waiting` (TTFB) chiếm gần hết: p95 1 321 ms;
- `connecting`, `tls_handshaking`, `sending` và `receiving` ở p95 đều khoảng 0 đến 1,5 ms, vì kết nối được dùng lại.

## Kết luận

1. **API và Postgres không phải nút thắt ở 100 VU** (74 request/s):
   - p95 phía server giữ quanh 5 ms ở mọi mức, p99 dưới 25 ms, không có 5xx;
   - tổng thời gian chờ connection DB là 0,76 s trên 17 681 request;
   - Postgres dùng tối đa 31/100 kết nối;
   - API dùng 86m CPU và 25 Mi bộ nhớ.
2. **p95 1,3 s mà k6 đo được nằm ở đường giữa laptop và process API, không nằm trong API.**
   - Đường này gồm `wg0`, VPS, Traefik và kube-proxy.
   - Ở 1 VU (probe), p95 là 94 ms, tức là cỡ RTT.
   - Ở tải cao, trung vị vẫn 93 ms, nhưng đuôi kéo dài tới vài giây, và có 2 request chờ quá 30 s.
   - Mình chưa tách được phần này giữa tunnel và Traefik, vì không có metric của Traefik trong lần chạy.
3. **Outbox theo kịp.** Số event giao mỗi mức tăng theo tải (125 → 240). Operation p95 là 10 s, chủ yếu là thời gian operator reconcile và chu kỳ sync status.

## Giới hạn của phép đo

- **Chạy qua `wg0`, không phải mạng trực tiếp.** Số phía client vì vậy là số của đường này. Số phía server không phụ thuộc đường mạng.
- **Bucket histogram của API bắt đầu ở 5 ms**, nên "khoảng 5 ms" chỉ có nghĩa là phần lớn request nằm dưới 5 ms. p99 là nội suy trong các bucket.
- **k6 không xuất số client theo từng mức tải**, nên bảng theo mức tải chỉ có số phía server.
- **Chỉ một replica API, và tải chỉ đến từ một máy.**

## Dọn dẹp

- Organization thử `e2eloadu5k5x7` bị giữ lại tới 04:14 UTC, vì khối địa chỉ của các Workspace đang trong quarantine 10 phút.
- Sau giờ đó, `make demo-purge-test-data CONFIRM=e2e` (chạy plan trước) đã xoá nó cùng 11 tài khoản thử: 2 685 dòng DB, 11 tài khoản và 4 group Keycloak.
- Trên cụm không còn object nào mang tên thử. `make demo-preflight` 15/15.

## Chạy lại

```sh
make load-test-check     # offline: k6 against a stand-in API (no lab)
make load-test LOAD=dry  # 1 VU, 30 s, after the probe
make load-test LOAD=full # 10 -> 100 VUs, about 20 minutes with setup and cleanup
```

Bảng theo mức tải được tính từ `metrics.jsonl`: lấy hai mẫu gần ranh giới mỗi mức (0, 4, 8, 12, 16 phút), trừ bộ đếm của mẫu sau cho mẫu trước, rồi nội suy p95 như `histogram_quantile`.
