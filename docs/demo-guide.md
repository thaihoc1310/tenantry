# Hướng dẫn tập và chạy demo (PRODUCT-PLAN §28)

Bản này viết cho người đứng demo, làm theo từ trên xuống. Mọi bước đều đã chạy thật trên lab, bằng e2e hoặc bằng tay; chỗ nào cần cẩn thận có ghi **Lưu ý**. Thời gian cả kịch bản khoảng 35–45 phút, chưa tính phần nói.

Tình trạng lúc viết (2026-11-23): M0–M12 đã làm, e2e persona §24.3 qua toàn bộ trên lab, isolation 8/8 + Dedicated 4/4, `cluster-verify` 37/37. Những gì chưa làm hoặc chỉ làm được một phần: `docs/report/known-limitations.md`.

---

## 0. Chuẩn bị một lần trên máy demo

Cần có:

- `kubectl`, `helm`, và plugin `kubelogin` (`kubectl oidc-login`). Máy dev này đã có cả ba.
- Context admin của lab: `hocbt@kubernetes`. Chỉ dùng cho các lệnh của Platform Admin (drain node, xem IP Pod), **không** dùng thay cho tenant.
- Repo `tenantry` (để có `deploy/demo-app` và `make demo-*`).

**Lấy mật khẩu**, không dán vào đâu khác:

```sh
kubectl --context hocbt@kubernetes -n keycloak get secret keycloak-realm \
  -o jsonpath='{.data.DEMO_USER_PASSWORD}' | base64 -d; echo      # alice, joe, bob, mallory (một mật khẩu chung)
kubectl --context hocbt@kubernetes -n keycloak get secret keycloak-realm \
  -o jsonpath='{.data.PLATFORM_ADMIN_PASSWORD}' | base64 -d; echo  # user admin
```

**Trình duyệt: mỗi người một profile riêng.** Keycloak giữ phiên SSO theo cookie, nên cùng một profile thì đăng nhập người này sẽ thành người kia. Tạo 4 profile (hoặc 4 cửa sổ container riêng): **Admin**, **Alice**, **Joe**, **Bob**.

**kubectl cho Alice, Joe và Bob trên cùng một máy.** `kubelogin` cache token theo issuer và client, không theo người, và mở trình duyệt mặc định. Nếu không tách, người sau dùng nhầm token của người trước. Khi tải kubeconfig của từng người (tab **Connect**):

1. **Đổi tên ngay**: mọi file tải về đều tên `acme-solar.kubeconfig`, file sau sẽ thành `acme-solar (1).kubeconfig`. Đổi thành `alice.kubeconfig`, `joe.kubeconfig`, `bob.kubeconfig`.
2. Sửa khối `args` của `exec` cho đúng như dưới (giữ **đúng 6 dấu cách** trước dấu `-`, thẳng cột với các dòng có sẵn; thụt sai thì YAML gộp hai dòng mới vào dòng trước, cache lại dùng chung và kubelogin mở trình duyệt mặc định):

```yaml
      args:
      - oidc-login
      - get-token
      - --oidc-issuer-url=https://auth.tenantry.site/realms/tenantry
      - --oidc-client-id=kubectl
      - --token-cache-dir=/home/thaihoc/.kube/cache/oidc-login-joe
      - --skip-open-browser
```

(Thay `joe` bằng `alice`/`bob` ở file tương ứng. Chỉ thêm hai dòng cuối, các dòng trên giữ nguyên như file tải về.)

Với `--skip-open-browser`, kubelogin in ra một URL; mở URL đó trong **đúng profile** của người đó để đăng nhập. Mỗi người một file kubeconfig (`KUBECONFIG=...`), mở mỗi người một terminal cho dễ.

---

## 1. Trước giờ demo (T-30 phút)

1. Kiểm lab, chỉ đọc:

   ```sh
   make demo-preflight VERIFY=1      # khoảng 2–3 phút; phải in "16 passed, 0 failed" (15 check + một dòng cluster-verify)
   ```

   Mỗi dòng FAIL có gợi ý cách sửa ngay bên dưới.
2. Nếu lab còn org `acme` từ lần tập trước (preflight báo FAIL ở dòng org `acme`):

   ```sh
   make demo-reset CONFIRM=acme      # tới ~15 phút: phải chờ hết quarantine IP (10 phút) mới xóa được org
   make demo-preflight               # chạy lại cho tới khi PASS hết
   ```

   **Lưu ý**: lần tập cuối nên xong trước giờ demo ít nhất 20 phút, để kịp reset.
3. Mở Grafana (tùy chọn, để chiếu ở Phần 6). Lệnh port-forward chiếm terminal, chạy ở một terminal riêng:

   ```sh
   kubectl --context hocbt@kubernetes -n observability port-forward svc/grafana 3000:80
   kubectl --context hocbt@kubernetes -n observability get secret grafana -o jsonpath='{.data.admin-password}' | base64 -d; echo
   ```

   Vào http://localhost:3000 → Dashboards → Tenantry → **Tenantry**. Ý nghĩa 20 panel: `docs/report/observability.md`. Panel thời gian tạo Workspace (2, 15) chỉ có số khi đã có Workspace tạo trong 1 giờ gần nhất.
4. Mở sẵn các tab: profile Admin ở `https://portal.tenantry.site/admin`, profile Alice ở `https://portal.tenantry.site`.

---

## 2. Phần 1 — Bài toán (chỉ nói, 3–5 phút)

- Người dùng chỉ cần data plane (Namespace, workload, URL), không cần control plane riêng.
- Số liệu so sánh (`docs/report/benchmark.md`): cụm kind riêng mất khoảng 33 giây và 655 MiB RAM khi idle; vcluster khoảng 26 giây và 303 MiB mỗi instance; một Workspace của Tenantry 10–20 giây và không tốn thêm Pod nào. Nói là **so bậc độ lớn**: kind/vcluster đo trên máy dev 16 CPU, Workspace đo trên lab (3 CPU mỗi worker), như ADR-0001.
- Giới hạn trung thực của shared control plane: `docs/report/known-limitations.md` (chọn 3–4 mục, ví dụ thu hồi quyền trễ tối đa 5 phút, Capsule Proxy có quyền rộng, Node không xem được qua kubectl).

---

## 3. Phần 2 — Self-service onboarding (5 phút)

| # | Ai (profile) | Làm | Kỳ vọng |
|---|---|---|---|
| 2.1 | Alice | Mở `https://portal.tenantry.site` → portal chuyển thẳng sang trang đăng nhập Keycloak → đăng nhập `alice` | Màn trống "Request an Organization" (lần đăng nhập đầu tạo tài khoản portal) |
| 2.2 | Alice | Điền tên `Acme`, slug `acme`, mục đích, gửi | Org ở trạng thái **Pending** |
| 2.3 | Admin | `https://portal.tenantry.site/admin` → Organizations → tab chờ duyệt → **Review** `acme` → giữ grant mặc định → **Approve** | `acme` **Active** |
| 2.4 | Alice | Tải lại trang | Vào được `acme`; có 3 group `admins`, `developers`, `viewers`; Alice nằm trong `admins` |
| 2.5 | Alice | **Members → Invite**: `joe@tenantry.site` (OrgMember), copy link mời. Lặp lại cho `bob@tenantry.site` | Link hiện trong dialog. **Không có email gửi đi**, phải copy link |
| 2.6 | Joe | Mở link mời trong profile Joe, đăng nhập `joe`, **Accept invitation** | Joe là member của `acme` |
| 2.7 | Bob | Như trên với link của Bob | Bob là member |
| 2.8 | Alice | **Groups**: thêm Joe vào `developers`, Bob vào `viewers` | Group hiện **Synced** sau vài giây (đồng bộ sang Keycloak) |

**Lưu ý**: §28 ghi "Alice đăng ký mới". Trên lab, alice/joe/bob đã có sẵn trong Keycloak, nên "đăng ký" thay bằng lần đăng nhập đầu tiên. Muốn đăng ký thật thì phải dùng tài khoản mới và lấy link xác minh từ Mailpit (port-forward `svc/mailpit` 8025 trong namespace `keycloak`); không nên làm trong buổi demo.

---

## 4. Phần 3 — Workspace Shared (5 phút)

| # | Ai | Làm | Kỳ vọng |
|---|---|---|---|
| 3.1 | Alice | **Workspaces → Create Workspace** (wizard 7 bước). Slug `solar`; compute **Shared**, pool `shared-general`; requests 2 CPU / 4 GiB, limits 4 CPU / 8 GiB; network để auto; quyền: `admins` = TenantOwner, `developers` = Developer, `viewers` = Viewer | Mỗi bước được server kiểm khi bấm tiếp; bước review hiện block IP sẽ cấp |
| 3.2 | Alice | **Create** | Trang Workspace hiện Operation chạy trực tiếp (SSE): Provisioning → **Ready**, thường dưới 30 giây |

**Lưu ý**: trong lúc chờ, Operation có thể hiện dòng message của Capsule (lỗi replicate tạm thời, đã biết). Cứ chờ, nó tự hết; chỉ Failed nếu quá 10 phút.

---

## 5. Phần 4 — Data plane (10 phút)

**4.1 Namespace.** Alice → tab **Namespaces** → tạo `frontend` → Namespace `acme-solar-frontend` Active.

**4.2 Kubeconfig của Joe.** Joe → Workspace `solar` → tab **Connect** → tải kubeconfig, sửa như mục 0, rồi:

```sh
export KUBECONFIG=~/Downloads/joe.kubeconfig
kubectl get ns                      # mở URL kubelogin in ra trong profile Joe, đăng nhập joe
# chỉ thấy acme-solar-frontend
```

**4.3 Deploy app có PVC** (Joe):

```sh
helm install web deploy/demo-app -n acme-solar-frontend
kubectl -n acme-solar-frontend get pods,pvc,svc        # Pod Running, PVC Bound
```

**Lưu ý**:
- Namespace mới tạo từ chối Pod trong vài giây đầu (422 nhắc `tenantry-tenant-network-ready`) vì NetworkPolicy chưa kịp vào. Với helm/Deployment, lỗi này không in ra terminal mà nằm trong Event của ReplicaSet (`kubectl -n acme-solar-frontend describe rs`); Deployment tự thử lại, khoảng 6 giây là lên.
- Developer tạo được Secret nhưng không đọc/liệt kê được, nên `helm install` của Joe chạy được còn **`helm list`, `helm upgrade`, `helm uninstall` bằng Joe sẽ bị Forbidden**. Đừng chạy ba lệnh đó bằng Joe; cần thì dùng Alice (TenantOwner).

**4.4 Route.** Alice → tab **Routes → Create**: Namespace `acme-solar-frontend`, tên `web`, Service `web`, port `80` → chờ **Active** → mở `https://web-acme-solar.mvp.tenantry.site` (HTTPS thật, cert wildcard).

**4.5 Headlamp.** Alice hoặc Joe → tab **Connect → Open web console** → Headlamp hiện trang **Sign In**, bấm một lần, Keycloak trả về ngay mà **không hỏi lại mật khẩu** (dùng phiên SSO của portal) → thấy Namespace của Workspace. Trang Nodes trống (cố ý, xem known-limitations 2).

**4.6 Bob chỉ xem** (kubeconfig của Bob, cache riêng):

```sh
kubectl -n acme-solar-frontend get pods                 # được
kubectl -n acme-solar-frontend delete deploy web        # Forbidden
kubectl -n acme-solar-frontend get secrets              # Forbidden
```

**4.7 Vi phạm policy** (dùng kubeconfig của Joe), mỗi lệnh phải bị từ chối kèm message. Với các lệnh `run`, kubectl in trước một dòng dài "Warning: would violate PodSecurity restricted:latest…" (mức cảnh báo là `restricted`), dòng lỗi thật nằm sau nó. Riêng lệnh `create ingress`: với Joe (Developer) nó bị RBAC chặn trước (`cannot create resource "ingresses"`); muốn chiếu message của VAP `tenantry-tenant-ingress-lockdown` thì chạy lại lệnh đó bằng kubeconfig của **Alice** (TenantOwner):

```sh
kubectl -n acme-solar-frontend run bad --image=nginx --privileged          # Pod Security baseline
kubectl -n acme-solar-frontend create service nodeport np --tcp=80         # NodePort bị cấm
kubectl -n acme-solar-frontend create ingress x --rule="a.example.com/*=web:80"   # tenant không được tạo Ingress
kubectl -n acme-solar-frontend run big --image=nginx \
  --overrides='{"spec":{"containers":[{"name":"big","image":"nginx","resources":{"requests":{"cpu":"5"},"limits":{"cpu":"5"}}}]}}'
                                                                            # vượt quota chung của Workspace (limits 4 CPU)
kubectl get nodes                                                           # rỗng hoặc Forbidden
```

---

## 6. Phần 5 — Admin và Dedicated (10–12 phút)

| # | Ai | Làm | Kỳ vọng |
|---|---|---|---|
| 5.1 | Admin | **Organizations → Active → `acme`** → grant trên `cluster-a`: `maxWorkspaces` 4, `maxNamespacesTotal` **12**, `requestsStorage` 50Gi (giữ nguyên CPU/RAM/Pod). Đừng vào qua trang **Grants**: trang đó liệt kê mọi grant (có nhiều org e2e tên "E2E Acme …"), dễ mở nhầm | Lưu được |
| 5.2 | Admin | **Clusters → Shared pools** → sửa `shared-general`, bỏ `fdp-k8s-wk3` | Dialog nhắc lệnh drain |
| 5.3 | Admin (terminal) | **Chờ** nhãn pool rời wk3: `kubectl --context hocbt@kubernetes get node fdp-k8s-wk3 -L tenantry.io/compute-pool` phải trống cột pool. Rồi `kubectl --context hocbt@kubernetes drain fdp-k8s-wk3 --ignore-daemonsets --delete-emptydir-data` và `kubectl --context hocbt@kubernetes uncordon fdp-k8s-wk3` | Pod Shared dời sang wk2. **Bắt buộc**: node còn Pod tenant thì không cấp được (NodeNotClean). Drain dừng vì Pod "trần" (tạo bằng `kubectl run`) thì thêm `--force` |
| 5.4 | Admin | **Clusters → Dedicated nodes → Give a node to an Organization** → Node `fdp-k8s-wk3`, Organization **`Acme (acme)`** (danh sách còn có các "E2E Acme …", chọn đúng dòng có slug `acme`) → **Give node** (system reserve mặc định 500m / 1536Mi) | Dialog ghi "may reserve 2.5 cores and 10 Gi"; node Ready sau khoảng 1 giây |
| 5.5 | Alice | Create Workspace `water`: **Dedicated**, chọn node wk3, 1 CPU / 4 GiB (limits = requests), storage ≥ 1 GiB. Bước Namespaces: gõ `app` rồi **Enter** (hiện chip và dòng "Created as acme-water-app"). Bước Access: chỉ giữ `admins` = TenantOwner, **bỏ `developers`** (wizard chọn sẵn nó) | Ready sau khoảng 30 giây |
| 5.6 | Alice | Create Workspace `green`: như water (Dedicated, cùng node, 1 CPU / 4 GiB, storage ≥ 1 GiB, Namespace `app`, chỉ `admins` = TenantOwner) | Ready; node card còn 0,5 CPU |
| 5.7 | Alice | Tạo Workspace thứ ba Dedicated 1 CPU trên wk3, **namespace limit 1** | Tới bước Review: "Only 0.5 cores left on node fdp-k8s-wk3. (DEDICATED_NODE_INSUFFICIENT_CAPACITY)", dấu `!` ở bước Resources, nút Create bị khóa. (Wizard không dừng ngay ở bước compute; lỗi hiện ở Review.) |

**5.8 IP và placement.** Cài app vào water bằng kubeconfig của **Alice** (Joe không có quyền ở water; Alice cài được vì `admins` là TenantOwner ở water). Không cần tải file kubeconfig mới: mọi Workspace cùng server `kube-a` và cùng danh tính, nên file solar của Alice dùng được với `-n acme-water-app`. Namespace Dedicated bắt limits = requests (Guaranteed), nên phải hạ limits của chart xuống bằng requests, nếu không Pod bị LimitRange từ chối:

```sh
helm install web deploy/demo-app -n acme-water-app \
  --set resources.limits.cpu=50m --set resources.limits.memory=32Mi
```

Xem IP và node (terminal admin):

```sh
kubectl --context hocbt@kubernetes get pods -A -o wide | grep acme-
# Pod của solar ở wk2, IP trong CIDR của solar; Pod của water/green ở wk3, IP trong CIDR riêng
```

CIDR của từng Workspace xem ở tab **Network**.

**5.9 Solar ↔ Water bị chặn** (terminal admin, chạy Pod thử trong Namespace của solar):

```sh
kubectl --context hocbt@kubernetes -n acme-solar-frontend run probe --rm -it --restart=Never \
  --image=busybox:1.36 -- wget -T 3 -qO- http://<IP-Pod-của-water>:8080   # timeout: bị chặn
# (LimitRange của Workspace tự điền requests/limits mặc định cho Pod này)
```

Đối chứng: cùng lệnh nhưng gọi `http://web.acme-solar-frontend.svc` (Service cổng 80) hoặc `http://<IP-Pod-của-solar>:8080` thì trả trang. App nghe cổng **8080** (nginx-unprivileged); gọi IP Pod mà quên `:8080` sẽ ra "connection refused" chứ không phải timeout, và ca thử mất ý nghĩa.

---

## 7. Phần 6 — Lifecycle và kết quả (5–8 phút)

1. **Thu hồi quyền**: Alice gỡ Joe khỏi `developers` (hoặc khỏi org).
   - Portal: Joe mất quyền ngay (tải lại trang).
   - kubectl: token cũ của Joe vẫn dùng được tới khi hết hạn, **tối đa 5 phút** (đo trên lab: đúng 300 giây). Sau đó `kubectl get pods` bị Forbidden. Gỡ binding của cả group khỏi Workspace thì mất ngay.
2. **Cordon**: Workspace `green` → **Danger zone → Cordon** → tenant không tạo/sửa được gì, workload vẫn chạy. Cordon của Capsule chặn cả operator, nên mọi thay đổi khác trên `green` (Namespace, resize, xóa) đứng tới khi **Uncordon**: uncordon xong rồi mới làm gì khác với green.
3. **Xóa Water**: Danger zone → **Delete**, gõ lại tên `water` → sau khoảng 20 giây trang báo "No such Workspace" (đã xóa xong); `green` vẫn Ready; node card của wk3 trả lại 1 CPU (còn 1,5 / 2,5).
4. **Kết quả đánh giá** (chiếu file hoặc Grafana):
   - `docs/report/isolation-cluster.md`: 8/8 + Dedicated 4/4, mỗi threat ghi lớp nào chặn.
   - `docs/report/benchmark.md`: 10/20/50 Workspace, trước và sau khi chỉnh Capsule; baseline kind/vcluster.
   - `docs/report/failure-cluster.md`, `docs/report/e2e-personas.md`, `docs/report/token-size.md`.
   - `docs/report/known-limitations.md` và `docs/adr/` (22 ADR).

---

## 8. Sau demo

```sh
make demo-reset CONFIRM=acme        # xóa acme, trả wk3 về shared-general, đặt lại onboarding policy (tới ~15 phút)
make demo-preflight                 # phải PASS hết
```

Nếu `demo-reset` dừng giữa chừng vì timeout (đã gặp một lần: request treo trên đường mạng tới lab), **chạy lại đúng lệnh đó**; nó làm tiếp từ chỗ dừng, lần chạy lại thường xong trong vài chục giây.

Nếu đã drain wk3 mà reset không trả về được, kiểm `kubectl --context hocbt@kubernetes get nodes -L tenantry.io/compute-pool,tenantry.io/dedicated-org`.

---

## 9. Khi có sự cố

| Hiện tượng | Nguyên nhân thường gặp | Xử lý |
|---|---|---|
| Đăng nhập người này lại thành người kia | Cùng profile trình duyệt, hoặc cache kubelogin chung | Dùng profile riêng; `--token-cache-dir` riêng (mục 0); xóa cache của đúng người: `kubectl oidc-login clean --token-cache-dir=/home/thaihoc/.kube/cache/oidc-login-<tên>` |
| Pod bị 422 nhắc `tenantry-tenant-network-ready` | Namespace vừa tạo, NetworkPolicy chưa vào | Chờ 5–10 giây; Deployment tự lên |
| Operation đứng Running kèm message Capsule | Race replicate nội bộ Capsule | Chờ; quá 10 phút mới Failed, khi đó bấm Retry |
| Group không Synced | Group sync worker đang chạy | Chờ vài giây; Admin → **Health** xem hàng đợi |
| Tạo lại Workspace cùng tên bị `SLUG_TAKEN` | Tên còn trong quarantine 10 phút sau khi xóa | Dùng tên khác hoặc chờ |
| Xóa org bị `ORGANIZATION_NOT_EMPTY` kèm giờ | Workspace đã xóa còn trong quarantine | Chờ tới giờ ghi trong message |
| Cấp node bị `NodeNotClean` | Node còn Pod tenant | Drain rồi uncordon (bước 5.3) |
| Lệnh admin (`kubectl --context hocbt@kubernetes …`: cordon, drain, port-forward) báo `unexpected EOF` | Chỉ gặp khi ở mạng công ty mà vẫn bật WireGuard (`wg0`, vòng qua server main): lệnh xóa mất khoảng 10 giây rồi đứt. Ở nhà, có hay không có `wg0`, lệnh xóa đều dưới 1 giây; apiserver không ghi lỗi | Ở mạng công ty thì **tắt `wg0`** (`sudo wg-quick down wg0`), vì mạng đó vào thẳng được lab. Đang demo mà gặp thì chạy lại đúng lệnh đó (cordon/drain/uncordon chạy lại vô hại); port-forward chết thì mở lại |
| Portal hoặc API trả `503 SERVICE_BUSY` (kèm `Retry-After`) | Pool DB của API không có connection trống trong 5 giây | Chờ vài giây rồi thử lại; Grafana panel 19–20; Admin → **Health** (có từ bản deploy sau 113e937) |
| Portal chậm hoặc treo hơn 10 giây | Đường mạng hoặc API (lỗi cạn pool DB đã sửa) | Tải lại trang; Admin → **Health**; Grafana panel 19–20 |
| `kubectl` qua kube-a báo lỗi lạ hàng loạt | Capsule Proxy CrashLoop (gate Alpha, khi một APIService hỏng) | `kubectl --context hocbt@kubernetes get apiservice`, sửa APIService không Available (05 §8) |

Kiểm nhanh toàn bộ bất kỳ lúc nào: `make demo-preflight`.
