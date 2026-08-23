# Capsule multi-tenant lab

Lab này biến cluster kubeadm hiện tại thành một Kubernetes-as-a-Service nhỏ, với Capsule là thành phần trung tâm.

## Kiến trúc

- Capsule Operator `0.13.10` quản lý Tenant, namespace delegation, RBAC, admission
  enforcement, replication và ResourcePool; Helm chart chính thức cài CRD, controller và webhook.
- Capsule Proxy `0.13.9` lọc các truy vấn cluster-scoped theo tenant.
- Calico `3.32.0` cấp IP từ pool riêng cho từng tenant:
  - Solar: `10.240.0.0/18`
  - Water: `10.240.64.0/18`
- Compute pool riêng: Solar dùng `wk1,wk2`, Water dùng `wk3`; Capsule Proxy chỉ
  hiển thị node thuộc tenant và `PodNodeSelector` buộc Pod chạy đúng pool.
- Keycloak `26.7.0` cung cấp OIDC cho Kubernetes và Headlamp.
- Headlamp `0.44.0` dùng token của user, không dùng ServiceAccount cluster-admin.
- kube-prometheus-stack `88.3.0` thu thập metrics và nạp dashboard Capsule.

## User demo

| User | Password | Nhóm | Vai trò |
|---|---|---|---|
| `alice` | `CapsuleDemo!2026` | `projectcapsule.dev`, `solar-owners` | Owner Solar |
| `joe` | `CapsuleDemo!2026` | `projectcapsule.dev`, `solar-developers` | Developer Solar |
| `bob` | `CapsuleDemo!2026` | `projectcapsule.dev`, `water-owners` | Owner Water |
| `mallory` | `CapsuleDemo!2026` | `projectcapsule.dev` | Không có tenant |

Đây là credential dành riêng cho lab. Không tái sử dụng ở môi trường thật.

## Triển khai

Kiểm tra context trước:

```bash
kubectl config current-context
```

Chạy toàn bộ:

```bash
./scripts/deploy.sh
```

Hoặc chạy từng bước:

```bash
./scripts/00-preflight.sh
./scripts/10-cert-manager.sh
./scripts/15-capsule-core.sh
./scripts/20-monitoring.sh
./scripts/30-capsule.sh
./scripts/40-platform.sh
./scripts/50-keycloak.sh
./scripts/60-enable-oidc.sh
./scripts/70-headlamp.sh
./scripts/80-demo-namespaces.sh
./scripts/90-verify.sh
```

Bước `60-enable-oidc.sh` thêm `--authentication-config` và bật admission
`PodNodeSelector` trong static manifest của kube-apiserver. Script tạo backup trên
control-plane tại `/etc/kubernetes/backups/capsule-lab/` trước khi thay đổi.
API server sẽ restart trong thời gian ngắn.

## Truy cập UI

Giữ terminal này mở:

```bash
./scripts/access.sh
```

Sau đó mở:

- Keycloak: <https://keycloak.127.0.0.1.nip.io:8443>
- Headlamp: <http://headlamp.127.0.0.1.nip.io:8444>
- Grafana: <http://grafana.127.0.0.1.nip.io:3000>

Grafana dùng `admin` / `CapsuleGrafana!2026`. Trang quản trị Keycloak dùng
`admin` / `CapsuleKeycloak!2026`.

CA của Keycloak là CA nội bộ của lab, nên trình duyệt sẽ cảnh báo certificate. Có thể tải CA bằng:

```bash
kubectl -n identity-system get secret capsule-lab-root-ca -o jsonpath='{.data.tls\.crt}' | base64 -d > capsule-lab-ca.crt
```

### Đổi user Headlamp

Nút logout của Headlamp chỉ xóa token cục bộ; Keycloak vẫn giữ browser SSO cookie.
Để đổi user trong demo:

1. Logout khỏi Headlamp.
2. Mở <https://keycloak.127.0.0.1.nip.io:8443/realms/capsule/protocol/openid-connect/logout>
   và xác nhận logout.
3. Quay lại Headlamp và bấm Sign in; Keycloak sẽ hiện lại form đăng nhập.

Có thể dùng cửa sổ ẩn danh riêng cho từng user khi demo song song.

## Flow demo đề xuất

1. Đăng nhập Headlamp bằng Alice và tạo namespace `solar-demo`.
2. Kiểm tra namespace có annotation Calico pool Solar và các resource do Capsule replicate.
3. Tạo workload hợp lệ; IP pod phải thuộc `10.240.0.0/18`.
4. Thử workload từ registry không cho phép hoặc Burstable pod; Capsule phải từ chối.
5. Alice thử truy cập `water-prod` và `kube-system`; Kubernetes phải từ chối.
6. Đăng nhập bằng Bob; chỉ thấy Water và pod có IP thuộc `10.240.64.0/18`.
7. Mở Grafana, tìm dashboard Capsule và xem tenant/proxy metrics.

## Rollback OIDC control-plane

Nếu cần bỏ cấu hình OIDC nhưng giữ các workload khác:

```bash
./scripts/rollback-oidc.sh
```

Script chỉ xóa đúng flag `--authentication-config=/etc/kubernetes/pki/capsule-lab-authentication-config.yaml`; các cơ chế certificate và ServiceAccount hiện tại vẫn giữ nguyên.

## Lưu ý về dữ liệu

Cluster hiện không có StorageClass, nên Keycloak, Prometheus và Grafana đang dùng
storage tạm thời phù hợp cho lab. Cấu hình realm được tự import lại khi Keycloak
khởi tạo, nhưng lịch sử metrics và các thay đổi thủ công trên UI có thể mất khi
pod bị tạo lại. Nên gắn PVC trước khi dùng ngoài phạm vi demo.
