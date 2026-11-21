# deploy/cluster — nền của Cluster

Dựng mọi thứ Tenantry cần trên một cụm kubeadm trần (IMPLEMENTATION-PLAN M1): storage, ingress, TLS,
PostgreSQL, Keycloak, OIDC cho kube-apiserver, Capsule, Capsule Proxy, Headlamp.

```sh
make cluster-up       # chạy lần lượt các script đánh số; chạy lại bao nhiêu lần cũng được
make cluster-verify   # kiểm tra nền làm đúng việc của nó; in PASS/FAIL từng mục
make deploy           # cài chart của Tenantry (hiện có tenantry-backend) lên nền này
```

Mọi giá trị riêng của cụm (tên node, IP public, domain, phiên bản chart) nằm ở [`env.sh`](env.sh) và
đổi được bằng biến môi trường cùng tên. Mọi lệnh `kubectl`/`helm` của script đều ghim vào context
`KUBE_CONTEXT`, bất kể `kubectl config current-context` đang trỏ đâu.

## Các bước

| Script | Dựng gì | Namespace |
|---|---|---|
| `00-preflight.sh` | Kiểm tra công cụ, quyền cluster-admin, các node | — |
| `10-nodes.sh` | Label + taint `tenantry.io/compute-pool=system` cho system pool, label `shared-general` cho shared pool | — |
| `20-storage.sh` | csi-driver-nfs, StorageClass `standard` (mặc định) trên NFS | `kube-system` |
| `30-cert-manager.sh` | cert-manager, ClusterIssuer `letsencrypt-staging` và `letsencrypt` (DNS-01 qua Cloudflare) | `cert-manager` |
| `40-traefik.sh` | Traefik trên node control-plane (hostPort 80/443), cert wildcard `*.mvp.tenantry.site` làm cert mặc định, app `demo` | `ingress-system`, `demo` |
| `50-postgres.sh` | PostgreSQL (chart [`charts/postgres`](charts/postgres)), database `tenantry` và `keycloak`, Secret `tenantry-db` | `tenantry-system` |
| `60-keycloak.sh` | Keycloak (chart keycloakx), realm [`../keycloak/realm-tenantry.json`](../keycloak/realm-tenantry.json) | `keycloak` |
| `70-apiserver.sh` | kube-apiserver nhận token Keycloak, bật `PodNodeSelector` và `PodTolerationRestriction` | static Pod trên cp1 |
| `80-capsule.sh` | Capsule, Capsule Proxy tại `kube-a.tenantry.site` | `capsule-system` |
| `85-headlamp.sh` | Headlamp tại `console-a.tenantry.site` | `headlamp-system` |

Mọi Pod của nền chạy trên system pool, trừ Traefik (node control-plane, nơi IP public đi vào) và
DaemonSet chạy trên mọi node. Mọi namespace ở mức Pod Security `restricted`, trừ `ingress-system`
(`privileged`, vì hostPort bị cấm ở cả `baseline` lẫn `restricted`).

## Làm tay một lần, trước `make cluster-up`

1. **Máy chạy script** có `kubectl`, `helm`, `openssl`, `curl`, `python3`, `ssh`, và một context
   kubeconfig có quyền cluster-admin (mặc định `hocbt@kubernetes`, đổi bằng `KUBE_CONTEXT`).
2. **SSH vào node control-plane** bằng key, có `sudo` không hỏi mật khẩu (`CONTROL_PLANE_SSH`, mặc
   định `fdp-k8s-cp1`). Chỉ `70-apiserver.sh` dùng: cờ của kube-apiserver chỉ sửa được trong static
   Pod manifest trên node đó.
3. **NFS server** (VM ngoài cụm) export một thư mục cho dải IP các node, ví dụ trong `/etc/exports`:

   ```
   /srv/tenantry 172.21.92.0/24(rw,sync,no_subtree_check,no_root_squash)
   ```

   `no_root_squash` là bắt buộc: driver tạo thư mục cho từng volume bằng root, rồi kubelet đổi
   group theo `fsGroup` để Pod không chạy root (PostgreSQL, Keycloak) ghi được.
4. **DNS trên Cloudflare**, kiểu A, chế độ *DNS only* (không bật proxy), tất cả trỏ về IP public
   (`PUBLIC_IP`): `*.mvp`, `portal`, `auth`, `kube-a`, `console-a` (dưới `tenantry.site`).
5. **API token Cloudflare** chỉ có quyền *Zone → DNS → Edit* trên zone `tenantry.site` (cert-manager
   dùng để tạo bản ghi TXT cho DNS-01). Lưu ở `~/.config/tenantry/cloudflare-token`, quyền `600`
   (đổi đường dẫn bằng `CLOUDFLARE_TOKEN_FILE`). Script đưa nó thẳng vào Secret qua pipe; không chép
   vào repo.
6. **Mở cổng ở hạ tầng**: NAT `PUBLIC_IP:80` và `:443` về cổng 80/443 của node control-plane, **giữ
   nguyên IP nguồn** (rate limit theo IP ở API cần IP thật; kiểm bằng `https://demo.mvp.tenantry.site`,
   app whoami in ra `X-Real-Ip`). Cổng 6443 chỉ để quản trị: giới hạn theo IP nguồn ở firewall.

## Sau khi dựng

- **Mật khẩu**, sinh lúc cài, nằm trong Secret, không ở đâu khác:

  ```sh
  kubectl -n keycloak get secret keycloak-realm -o jsonpath='{.data.DEMO_USER_PASSWORD}' | base64 -d      # alice, joe, bob, mallory
  kubectl -n keycloak get secret keycloak-realm -o jsonpath='{.data.PLATFORM_ADMIN_PASSWORD}' | base64 -d # admin (platform-admin)
  kubectl -n keycloak get secret keycloak-admin -o jsonpath='{.data.password}' | base64 -d                # admin của realm master
  ```

- **Trang admin Keycloak** không ra internet:
  `kubectl -n keycloak port-forward svc/keycloak-http 8080:80`, mở `http://localhost:8080/admin/`.
- **Image backend** chưa có registry: build bằng `make image`, nạp vào containerd của từng node
  (`docker save tenantry/backend:dev | ssh <node> sudo ctr -n k8s.io images import -`), rồi `make deploy`.

## Chuyển Let's Encrypt từ staging sang production

Cert staging không được trình duyệt tin; dùng khi đang dựng thử, vì production giới hạn số cert mỗi
domain mỗi tuần. Khi mọi thứ đã chạy ổn:

```sh
CLUSTER_ISSUER=letsencrypt make cluster-up     # hoặc đổi mặc định trong env.sh
make cluster-verify
```

Certificate đổi issuer thì cert-manager xin lại cert; cấu hình xác thực của kube-apiserver bỏ phần CA
staging và được nó tự nạp lại (không restart); Headlamp nhận bộ root mới và khởi động lại.

## Rollback kube-apiserver

`70-apiserver.sh` là bước duy nhất khởi động lại kube-apiserver, và chỉ lần đầu. Nó lưu manifest cũ
vào `/etc/kubernetes/tenantry-backups/` trên node (ngoài `manifests/`), và nếu kube-apiserver mới không
sẵn sàng trong 3 phút thì tự đặt lại manifest cũ. Nhật ký ở `apiserver-patch.log` cùng thư mục.

Gỡ hẳn OIDC và hai admission plugin, trả về manifest kubeadm đã viết:

```sh
deploy/cluster/rollback-apiserver.sh
```

Xem trước cấu hình xác thực mà không thay đổi gì: `deploy/cluster/70-apiserver.sh --print`.

## Giới hạn đã biết

- **`kubeadm upgrade` viết lại manifest của kube-apiserver** và làm mất `--authentication-config` cùng
  hai admission plugin. Sau mỗi lần upgrade, chạy lại `70-apiserver.sh`.
- **Realm chỉ được import một lần** (`--import-realm` bỏ qua realm đã có): sửa
  `realm-tenantry.json` sau đó không tự áp vào Keycloak. Sửa trong trang admin rồi export về file.
- **NFS không ép dung lượng** (PVC chỉ là con số tính quota) và là một điểm chết; tenant ghi đầy
  export thì central plane cũng hết chỗ (architecture/05 §9).
- **Một Pod Traefik** giữ hostPort: mỗi lần nâng cấp Traefik mất vài giây truy cập.
- **kube-apiserver lấy cấu hình OIDC qua IP public của chính cụm** (hairpin NAT). Nếu hạ tầng đổi và
  hairpin không còn, thêm vào `/etc/hosts` của node control-plane một dòng trỏ `auth.tenantry.site`
  về IP nội bộ của node đó.
- **Admin `master` của Keycloak là admin tạm** (`KC_BOOTSTRAP_ADMIN_*`), Keycloak cảnh báo trong log.
