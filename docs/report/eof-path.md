# "unexpected EOF" khi xoá qua k8s-api: đo trên lab

Đo ngày 2026-11-23, 13:15–13:40 (+07), từ laptop dev, ở mạng công ty (Wi-Fi `FCI_Guest`). Chỉ dùng namespace thử `optest-eof` với ConfigMap và một Pod pause; namespace đã xoá khi xong. Không đổi WireGuard, VPS hay cấu hình cụm. Tóm tắt kết luận nằm trong `failure-cluster.md`, mục "unexpected EOF".

Ba điều kiện mạng:

| # | Mạng | Khi nào | Đường tới 203.29.16.150:6443 |
|---|---|---|---|
| 1 | mạng nhà + WireGuard | đêm 2026-11-22 và sáng 2026-11-23 | laptop → `wg0` (10.66.66.2, MTU 1280) → VPS 10.66.66.1 (NAT) → lab |
| 2 | mạng công ty + WireGuard còn bật | khoảng 13:15–13:34 | như trên, nhưng chặng vào tunnel đi từ mạng công ty |
| 3 | mạng công ty, không tunnel | từ khoảng 13:35 (user tắt `wg0`) | laptop → `wlo1` → thẳng tới lab (MSS 1448, PMTU 1500) |

Lỗi chỉ xảy ra ở điều kiện 1. Phép đo dưới đây chạy ở điều kiện 2 và 3.

## Hiện tượng đã ghi lại trước đó

- Sáng 2026-11-23 (khoảng 08:10–08:30), A thấy mọi DELETE qua `k8s-api` mất khoảng 10 s, kể cả một ConfigMap trong `default`. Cái nào quá khoảng 10,25 s thì kết thúc bằng `unexpected EOF` và **không được áp**; chạy lại thì được.
- Cùng lúc, GET mất 0,3 s. 1600 probe ngắn, watch 12 phút, `kubectl wait` 700 s và port-forward 690 s đều không sao.
- Hai lần cluster-verify cùng buổi sáng FAIL vì lỗi này: lần ở bước xoá Pod `postgres-probe`, lần ở `service_path` (`Unable to connect to the server: unexpected EOF`). Lần chạy lại vài phút sau thì qua hết.

## Kết quả đo

| Phép đo | Kết quả |
|---|---|
| `kubectl delete configmap` từ laptop, 12 lần (`-v=6`) | 0,30–0,73 s mỗi DELETE, không lần nào lỗi |
| `kubectl delete -f` một manifest (ConfigMap và Pod) | DELETE ConfigMap 0,51 s, DELETE Pod 0,08 s |
| DELETE ConfigMap trong `default` | 0,35 s |
| curl HTTP/1.1 và HTTP/2, cert admin của kubeconfig: GET, PATCH, DELETE không body, DELETE có body `DeleteOptions` (16 request) | 0,27–0,66 s, cả hai giao thức như nhau |
| PATCH body 1, 4, 16, 64 KiB (HTTP/2 và HTTP/1.1) | 0,5–1,0 s; 1,0–1,8 s; 2,6–2,8 s ở 64 KiB. Tăng đều theo kích thước (uplink chậm), không có bước nhảy hay chỗ đứng 10 s |
| DELETE có body độn 1, 4, 16 KiB | 0,68–0,81 s |
| Từ trong cụm: Pod với ServiceAccount chỉ có quyền trên `optest-eof`, 10 vòng POST, GET, PATCH, DELETE | DELETE trung bình 16 ms, tối đa 64 ms; các method khác tối đa 0,26 s |
| Lấy mẫu nền, điều kiện 2 (13:26–13:33, 20 vòng mỗi 20 s: create, GET, DELETE) | DELETE 0,38–1,16 s, POST tới 1,66 s, GET tới 1,49 s, không có EOF |
| Lấy mẫu nền, điều kiện 3 (13:33–13:35 và 13:36–13:40, 14 vòng) và lặp lại các dạng DELETE ở trên | DELETE 0,16–0,62 s, curl 0,33–0,44 s, không có EOF |

Phía kube-apiserver, từ Prometheus (`apiserver_request_duration_seconds`, chart prometheus chạy từ 07:00):

| Giờ | Số DELETE | Quá 1 s | Quá 5 s |
|---|---|---|---|
| 08:00–08:59 | 1654 | 0 | 0 |
| 09:00–09:59 | 765 | 0 | 0 |
| 10:00–12:59 | khoảng 2030 | 1 (12:xx) | 0 |
| 13:00–13:59 | 510 | 0 | 0 |

Lưu ý khi tự truy vấn: Prometheus 3 chuẩn hoá nhãn `le` thành số thực, nên phải viết `le="5.0"`, không phải `le="5"`. Viết sai thì truy vấn trả rỗng, rất dễ đọc nhầm thành "không có request chậm".

Đường mạng ở điều kiện 2:
- MTU 1280 đi trọn: ping DF payload 1252 (gói 1280) qua, 1253 không qua. TCP tới apiserver có MSS 1228, pmtu 1280.
- RTT tới lab 67–252 ms (trung bình 162 ms), tới VPS 59–228 ms, dao động mạnh, không mất gói trong 25 ping.
- Kết nối sống lâu có retransmit: port-forward 7 phút có 3 lần retransmit.

## Kết luận

1. **10 s không nằm trong cụm.** Trong giờ lỗi xảy ra (08:00–08:59), apiserver xử lý 1654 DELETE và không cái nào quá 1 s. Từ trong cụm, DELETE mất 16 ms. Những DELETE kết thúc bằng EOF chưa từng tới apiserver.
2. **Tunnel và VPS không phải nguyên nhân khi đi từ mạng công ty.** Ở điều kiện 2, cùng `wg0` và cùng VPS, mọi dạng DELETE đều dưới 1,2 s. Ở điều kiện 3, đường thẳng, cũng vậy.
3. **Nghi phạm còn lại là chặng từ mạng nhà vào tunnel** (ISP hoặc Wi-Fi nhà tới VPS). Chưa chứng minh được, vì điều kiện 1 chỉ tái hiện được ở nhà. Request đi trong TLS, nên đường mạng không thấy method. Việc chỉ DELETE dính lỗi nhiều khả năng đến từ thời điểm các DELETE chạy (lệnh đầu tiên sau một lúc chờ, trên một kết nối mới), không phải vì chúng là DELETE.

## Đề xuất

- **Đã làm:** `cluster-verify` thử lại một `kubectl delete` bị cắt (`unexpected EOF`, `connection reset by peer`, `http2: client connection lost`) tối đa 3 lần, kèm `--ignore-not-found`, và in ra rằng đã thử lại. Một lời từ chối vẫn FAIL ngay (`deploy/cluster/verify.sh`, hàm `kdelete`).
- **Cho user:** ở công ty thì vào lab thẳng, không cần `wg0`. Ở nhà với `wg0`, một lệnh admin kết thúc bằng `unexpected EOF` là chưa được áp: chạy lại.
- **Để xác nhận ở nhà**, khi `wg0` bật:
  - chạy lại bộ lấy mẫu (mỗi 20 s một create, GET, DELETE ConfigMap trong một namespace thử, ghi `ss -ti` khi có request chậm);
  - đo MTU qua tunnel: `ping -M do -s <size> 203.29.16.150`, size 1252 (gói 1280) phải qua, xem từ đâu thì không qua;
  - chạy `tracepath 203.29.16.150`, và tới IP public của VPS (ngoài tunnel);
  - nếu có quyền root, ghi `wg show wg0 latest-handshakes` theo thời gian để đối chiếu với giờ EOF.
