> **Đã báo upstream: projectcapsule/capsule#2318 (2026-11-23).**
> - projectcapsule/capsule#2266 (merge 2026-11-05, chưa có bản phát hành sau v0.15.2) đã cho hai controller đọc object đích không qua cache và retry khi Conflict.
> - Theo mã `main` (6f14b77, đọc chứ chưa chạy) còn lại một trường hợp: `createTarget` trả `already exists` khi hai bên cùng tạo, mà `retryableApplyConflict` chỉ bắt Conflict.
> - Khi lên bản có sửa, đo lại race như `benchmark.md`, rồi bỏ khoảng 2 phút mà operator đang coi là Progressing.
>
> Bản nháp ban đầu giữ dưới đây.

Title: GlobalTenantResource replication into a new Namespace fails with AlreadyExists when namespace-trigger and the GlobalTenantResource controller race

Version: Capsule v0.15.2, Kubernetes 1.36.2.

What happens
- When a Namespace is created in a Tenant, two controllers replicate the same GlobalTenantResource into it at the same time:
  - `capsule/replications/namespace-trigger` (internal/controllers/resources/namespace_trigger.go), For Namespace with the tenant label, created after start;
  - `capsule/replications/globaltenantresource` (internal/controllers/resources/global.go), through its Tenant watch (TenantSelectionChangedPredicate) as Tenant.status.namespaces changes.
- For an object that does not exist yet, ssa.Manager.Apply takes the creation path (`if snapshot[0] == nil { createTarget(...) }`, pkg/runtime/ssa/manager.go), and createTarget (pkg/runtime/ssa/condition.go) returns `creating target: <err>` on AlreadyExists. The snapshot is read through the cached client, so a stale cache leads there too.
- The loser of the race records, on the items it lost, either "apply failed for item …: creating target: … already exists" (both created the object)
  or "apply failed for item …: applying object failed: Operation cannot be fulfilled on … the object has been modified" (both updated it, the second with a stale resourceVersion),
  and, while a Namespace is being deleted, "creating target: … is forbidden: unable to create new content in namespace … because it is being terminated"
  (the terminating check reads a cached Namespace), and the GlobalTenantResource goes Ready=False, "applying of N resources failed", until its next reconcile succeeds through the patch path. Controllers watching the Ready condition see a failure that fixes itself.

Reproduce: a GlobalTenantResource with a LimitRange in rawItems selecting a Tenant; create Namespaces in that Tenant one after another; watch the GlobalTenantResource's status: some creations flip it to Ready=False "applying of 1 resources failed" for a moment. The controller log shows "cannot replicate the GlobalTenantResource on the Namespace" with that error.

Expected: AlreadyExists on creation means another writer created it: take the existing object and go on with the update path (ownership upgrade and SSA patch), as a later reconcile does; a Conflict on the update is retried on a fresh read. Or one controller only replicates into a new Namespace.
