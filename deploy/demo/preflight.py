#!/usr/bin/env python3
"""make demo-preflight: is the lab ready for the defence demo (PRODUCT-PLAN
§28)? Read only, apart from a Keycloak session for the admin's check and, with
--verify, the throwaway objects cluster-verify makes and removes.

Prints one line per check, PASS or FAIL, with what to do about a FAIL, and
exits 1 when anything failed.

  deploy/demo/preflight.py [--verify]   (--verify also runs make cluster-verify, ~5 min)
"""
import os
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lab  # noqa: E402

results = []


def check(name, fix):
    """Decorator: run the check now; it returns a short detail or raises."""
    def run(f):
        try:
            detail = f()
            results.append(("PASS", name, detail or "", ""))
        except Exception as e:  # a check that cannot even run fails too
            results.append(("FAIL", name, str(e) if name == "cluster-verify" else str(e)[:160], fix))
    return run


def need(cond, msg):
    if not cond:
        raise AssertionError(msg)


for host in (lab.PORTAL, lab.AUTH, lab.KUBE, lab.CONSOLE):
    @check(f"https://{host} answers, certificate valid", f"kubectl get certificate -A | grep {host.split('.')[0]}; cert-manager renews it, or see deploy/cluster/30-cert-manager.sh")
    def _(host=host):
        days = lab.cert_days(host)
        need(days >= 7, f"certificate expires in {days} days")
        return f"certificate valid {days} more days"


@check("Platform API ready", "kubectl -n tenantry-system logs deploy/tenantry-api; make deploy")
def _():
    out = lab.kubectl("get", "--raw", "/api/v1/namespaces/tenantry-system/services/tenantry-api:http/proxy/readyz")
    need(out.strip() == "ok", out[:80])


@check("Keycloak realm tenantry answers", "kubectl -n keycloak get pods; deploy/cluster/60-keycloak.sh")
def _():
    status, _ = lab.get(f"https://{lab.AUTH}/realms/tenantry/.well-known/openid-configuration")
    need(status == 200, f"HTTP {status}")


@check("platform Pods Running", "kubectl get pods -A | grep -v Running; look at the one that is not")
def _():
    bad = []
    for ns in ("tenantry-system", "tenantry-operator", "keycloak", "capsule-system", "headlamp-system", "ingress-system"):
        for line in lab.kubectl("-n", ns, "get", "pods", "--no-headers").splitlines():
            parts = line.split()
            if len(parts) >= 3 and parts[2] not in ("Running", "Completed"):
                bad.append(f"{ns}/{parts[0]} {parts[2]}")
    need(not bad, ", ".join(bad))
    return "all Running"


@check("outbox delivered (provisioning worker)", "kubectl -n tenantry-system logs deploy/tenantry-worker-provisioning")
def _():
    m = lab.metrics("tenantry-worker-provisioning-metrics")
    pending = m.get(f'tenantry_outbox_pending_events{{cluster="{lab.CLUSTER}"}}', 0)
    lag = m.get(f'tenantry_outbox_oldest_pending_seconds{{cluster="{lab.CLUSTER}"}}', 0)
    need(pending == 0 or lag < 60, f"{pending:.0f} events waiting, oldest {lag:.0f}s")
    return f"{pending:.0f} waiting"


@check("Keycloak in step with groups (group sync worker)", "kubectl -n tenantry-system logs deploy/tenantry-worker-groupsync")
def _():
    m = lab.metrics("tenantry-worker-groupsync-metrics")
    late = {k: v for k, v in m.items() if k.startswith("tenantry_groupsync_oldest_pending_seconds") and v > 120}
    need(not late, f"waiting over 2 min: {late}")
    return "nothing waiting over 2 min"


@check("wk2 and wk3 in shared-general, dedicated to nobody", "admin portal: Clusters → shared-general → add the node; Organizations → take dedicated nodes back (or make demo-reset CONFIRM=acme)")
def _():
    for node in lab.SHARED_NODES:
        labels = lab.kubectl("get", "node", node, "-o", "jsonpath={.metadata.labels}")
        need(f'"tenantry.io/compute-pool":"{lab.POOL}"' in labels, f"{node} is not labelled for {lab.POOL}")
        need("tenantry.io/dedicated-org" not in labels, f"{node} is dedicated to an organization")
    live = lab.sql("SELECT node_name FROM dedicated_node_grants WHERE revoked_at IS NULL")
    need(not live, f"live node grants: {[r[0] for r in live]}")


@check("Pod address blocks left in onboarding-pod-network", "wait for quarantined blocks (10 min after a delete), or ask a Platform Admin for a bigger pool")
def _():
    m = lab.metrics("tenantry-worker-provisioning-metrics")
    free = m.get(f'tenantry_network_pool_free_blocks{{pool="{lab.NETWORK_POOL}",prefix_length="24"}}', 0)
    need(free >= 4, f"{free:.0f} free /24 blocks")
    return f"{free:.0f} free /24 blocks"


@check(f"no organization {lab.DEMO_ORG} left from a previous run", "make demo-reset CONFIRM=acme")
def _():
    rows = lab.sql(f"SELECT id, status FROM organizations WHERE slug = '{lab.DEMO_ORG}' AND status NOT IN ('Rejected', 'Deleted')")
    need(not rows, f"{rows[0][0]} is {rows[0][1]}" if rows else "")
    ws = lab.sql(f"SELECT w.k8s_name FROM workspaces w JOIN organizations o ON o.id = w.org_id WHERE o.slug = '{lab.DEMO_ORG}'")
    need(not ws, f"Workspaces left: {[r[0] for r in ws]}")


@check("demo accounts alice, joe, bob, mallory: in Keycloak, enabled, password works", "deploy/cluster/60-keycloak.sh creates them; the password is DEMO_USER_PASSWORD in the Secret keycloak-realm (namespace keycloak)")
def _():
    out = lab.kcadm("for u in " + " ".join(lab.DEMO_USERS) +
                    "; do echo \"$u $(k get users -q username=$u -q exact=true --fields enabled --format csv --noquotes)\"; done")
    state = dict(line.split(" ", 1) for line in out.splitlines() if line)
    for u in lab.DEMO_USERS:
        need(state.get(u, "").strip() == "true", f"{u}: {state.get(u, 'missing') or 'missing'}")
    password = lab.secret("keycloak", "keycloak-realm", "DEMO_USER_PASSWORD")
    for u in lab.DEMO_USERS:
        need(lab.password_ok(u, password), f"Keycloak refuses the demo password for {u}")


@check("demo accounts belong to no organization in the portal", "make demo-reset CONFIRM=acme (an organization of another owner: leave it from its Settings page)")
def _():
    emails = ", ".join(f"'{u}@{lab.ZONE}'" for u in lab.DEMO_USERS)
    rows = lab.sql(f"""SELECT u.email, o.slug, o.status FROM users u
        JOIN organization_members m ON m.user_id = u.id JOIN organizations o ON o.id = m.org_id
        WHERE u.email IN ({emails}) AND o.status NOT IN ('Deleted', 'Rejected')
        UNION SELECT u.email, o.slug, o.status FROM users u JOIN organizations o ON o.owner_user_id = u.id
        WHERE u.email IN ({emails}) AND o.status = 'Pending'""")
    need(not rows, "; ".join(f"{r[0]} in {r[1]} ({r[2]})" for r in rows))
    disabled = lab.sql(f"SELECT email FROM users WHERE email IN ({emails}) AND status <> 'Active'")
    need(not disabled, f"disabled in the portal: {[r[0] for r in disabled]} (admin portal: Users → Enable)")
    seen = lab.sql(f"SELECT count(*) FROM users WHERE email IN ({emails})")[0][0]
    return f"{seen} of 4 have logged in before, none in an organization"


@check("Platform Admin logs in to the portal", "the password is PLATFORM_ADMIN_PASSWORD in the Secret keycloak-realm (namespace keycloak)")
def _():
    admin = lab.Portal("admin", lab.secret("keycloak", "keycloak-realm", "PLATFORM_ADMIN_PASSWORD"))
    status, me = admin.api("GET", "/me")
    need(status == 200 and me.get("isPlatformAdmin"), f"/me {status}")


if "--verify" in sys.argv:
    @check("cluster-verify", "make cluster-verify, then read the failing step")
    def _():
        root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..")
        r = subprocess.run(["make", "-s", "cluster-verify"], cwd=root, capture_output=True, text=True)
        need(r.returncode == 0, lab.verify_failures(r.stdout + r.stderr))

width = max(len(n) for _, n, _, _ in results)
for status, name, detail, fix in results:
    print(f"{status}  {name.ljust(width)}  {detail}")
    if status == "FAIL":
        print(f"      {' ' * width}  → {fix}")
failed = sum(1 for r in results if r[0] == "FAIL")
print(f"\n{len(results) - failed} passed, {failed} failed")
sys.exit(1 if failed else 0)
