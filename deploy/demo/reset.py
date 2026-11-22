#!/usr/bin/env python3
"""make demo-reset CONFIRM=acme: bring the lab back to where the defence demo
starts (PRODUCT-PLAN §28), touching only the organization acme and what it
was given. Everything goes through the portal API, as the people would do it:

  1. acme still a request (Pending): the Platform Admin rejects it, which
     gives the slug back.
  2. acme Active: its owner (a demo account) deletes its Workspaces, a stuck
     delete is forced by the Platform Admin; the Admin takes its dedicated
     nodes back; the owner deletes acme once the Workspaces' address blocks
     are out of quarantine (up to 10 minutes). Its grants, groups,
     memberships, invitations and Routes go with it.
  3. wk2 and wk3 back in the shared pool shared-general.
  4. The onboarding policy back to the seed's defaults.

Accounts are never deleted: the demo accounts are only left without acme.
Without CONFIRM=acme it says what it would do and changes nothing.

  deploy/demo/reset.py [--confirm acme]
"""
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lab  # noqa: E402

CONFIRMED = "--confirm" in sys.argv and sys.argv[sys.argv.index("--confirm") + 1:][:1] == [lab.DEMO_ORG]

# The seed's onboarding policy (backend/cmd/tenantry-seed, policyTemplates).
SEED_POLICY = {
    "entitlement": {"maxMembers": 20, "maxGroups": 10, "maxWorkspacesTotal": 8},
    "clusterGrant": {
        "clusterId": lab.CLUSTER, "maxWorkspaces": 2, "maxNamespacesTotal": 6,
        "resources": {"requestsCpu": "2", "requestsMemory": "4Gi", "limitsCpu": "4", "limitsMemory": "8Gi",
                      "pods": 20, "requestsStorage": "20Gi"},
        "sharedPoolIds": [lab.POOL], "networkPoolIds": [lab.NETWORK_POOL], "allowedStorageClasses": ["standard"],
    },
}


def say(msg):
    print(("" if CONFIRMED else "would: ") + msg, flush=True)


def ok(status, body, *want):
    if status not in want:
        raise RuntimeError(f"HTTP {status}: {body}")
    return body


def finished(portal, op_id, admin):
    """Wait for an operation; a Blocked Workspace delete is forced."""
    forced = False
    while True:
        op = ok(*portal.api("GET", f"/operations/{op_id}"), 200)
        if op["state"] == "Succeeded":
            return
        if op["state"] == "Blocked" and not forced:
            say(f"  operation {op_id} Blocked ({op.get('errorMessage', '')}): the Platform Admin forces it")
            ok(*admin.api("POST", f"/operations/{op_id}:force"), 200)
            forced = True
        elif op["state"] == "Failed":
            raise RuntimeError(f"operation {op_id} failed: {op.get('errorCode')} {op.get('errorMessage')}")
        time.sleep(5)


def main():
    rows = lab.sql(f"""SELECT o.id, o.status, u.email FROM organizations o JOIN users u ON u.id = o.owner_user_id
        WHERE o.slug = '{lab.DEMO_ORG}' AND o.status NOT IN ('Rejected', 'Deleted')""")
    admin = lab.Portal("admin", lab.secret("keycloak", "keycloak-realm", "PLATFORM_ADMIN_PASSWORD")) if CONFIRMED else None

    if not rows:
        say(f"nothing to do for {lab.DEMO_ORG}: no such organization")
    else:
        org_id, status, owner_email = rows[0]
        if status == "Pending":
            say(f"reject the request {lab.DEMO_ORG} ({org_id}) of {owner_email}")
            if CONFIRMED:
                ok(*admin.api("POST", f"/admin/organizations/{org_id}:reject", {"reason": "reset before the demo"}), 200)
        else:
            username, _, domain = owner_email.partition("@")
            if username not in lab.DEMO_USERS or domain != lab.ZONE:
                raise SystemExit(f"{lab.DEMO_ORG} belongs to {owner_email}, not a demo account: delete it as that person, or ask before going further")
            workspaces = lab.sql(f"SELECT id, slug, state FROM workspaces WHERE org_id = '{org_id}' AND state <> 'Deleted' ORDER BY id")
            nodes = lab.sql(f"SELECT node_name FROM dedicated_node_grants WHERE org_id = '{org_id}' AND revoked_at IS NULL")
            say(f"as {username}, delete the Workspaces of {lab.DEMO_ORG}: {[w[1] for w in workspaces] or 'none'}")
            say(f"as the Platform Admin, take back its dedicated nodes: {[n[0] for n in nodes] or 'none'}")
            say(f"as {username}, delete {lab.DEMO_ORG} once its address blocks are out of quarantine")
            if CONFIRMED:
                owner = lab.Portal(username, lab.secret("keycloak", "keycloak-realm", "DEMO_USER_PASSWORD"))
                for ws_id, slug, _ in workspaces:
                    status, body = lab.wait(f"the delete of {slug} accepted", lambda: (
                        lambda r: None if r[0] == 409 and r[1].get("code") == "WORKSPACE_NOT_READY" else r)(
                        owner.api("DELETE", f"/workspaces/{ws_id}?confirmName={slug}")), timeout=600)
                    if status == 202:
                        finished(owner, body["operation"]["id"], admin)
                    elif status != 404:
                        raise RuntimeError(f"delete {slug}: HTTP {status} {body}")
                    print(f"  Workspace {slug} deleted", flush=True)
                if nodes:
                    r = ok(*admin.api("PUT", f"/admin/organizations/{org_id}/cluster-grants/{lab.CLUSTER}/dedicated-nodes", {"nodes": []}), 200)
                    for op in r.get("operations") or []:
                        finished(admin, op["id"], admin)
                    print("  dedicated nodes taken back", flush=True)

                def delete_org():
                    status, body = owner.api("DELETE", f"/organizations/{lab.DEMO_ORG}")
                    if status == 204:
                        return True
                    if status == 409 and body.get("code") == "ORGANIZATION_NOT_EMPTY" and not body["details"].get("workspaces") \
                            and not body["details"].get("dedicatedNodes"):
                        print(f"  waiting: {body['message']}", flush=True)
                        return None
                    raise RuntimeError(f"delete {lab.DEMO_ORG}: HTTP {status} {body}")
                lab.wait(f"{lab.DEMO_ORG} deletable", delete_org, timeout=15 * 60, every=30)
                print(f"  {lab.DEMO_ORG} deleted", flush=True)

    # The shared pool, as the seed has it.
    admin_pools = None
    if CONFIRMED:
        admin_pools = ok(*admin.api("GET", f"/admin/clusters/{lab.CLUSTER}/shared-pools"), 200)["items"]
        pool = next(p for p in admin_pools if p["id"] == lab.POOL)
        missing = [n for n in lab.SHARED_NODES if n not in pool["nodes"]]
    else:
        live = lab.sql(f"SELECT node_name FROM cluster_nodes WHERE cluster_id = '{lab.CLUSTER}' AND shared_pool_id = '{lab.POOL}'")
        missing = [n for n in lab.SHARED_NODES if [n] not in live]
    say(f"put back in {lab.POOL}: {missing or 'none, both are in'}")
    if CONFIRMED and missing:
        body = {"name": pool["name"], "allocationOvercommitRatio": pool["allocationOvercommitRatio"],
                "systemReserve": pool["systemReserve"], "nodes": sorted(set(pool["nodes"]) | set(missing))}
        r = ok(*admin.api("PUT", f"/admin/clusters/{lab.CLUSTER}/shared-pools/{lab.POOL}", body), 200)
        if r.get("operation"):
            finished(admin, r["operation"]["id"], admin)

    # The onboarding policy, as the seed has it.
    if CONFIRMED:
        current = ok(*admin.api("GET", "/admin/onboarding-policy"), 200)
        if current != SEED_POLICY:
            say("reset the onboarding policy to the seed's defaults")
            ok(*admin.api("PUT", "/admin/onboarding-policy", SEED_POLICY), 200)
        else:
            say("onboarding policy already the seed's")
    else:
        say("reset the onboarding policy to the seed's defaults if it differs")

    if not CONFIRMED:
        print(f"\nnothing changed: run make demo-reset CONFIRM={lab.DEMO_ORG} to do it")


if __name__ == "__main__":
    main()
