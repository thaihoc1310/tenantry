#!/usr/bin/env python3
"""make demo-clean-e2e [CONFIRM=e2e]: take the leftovers of end-to-end runs
out of the Platform Admin's way before a demo, through the portal API as the
Platform Admin:

  1. requests still Pending whose owner is an e2e*/optest* account and whose
     slug starts with e2e or optest are rejected;
  2. Active organizations whose slug starts with e2e or optest lose their
     cluster grants (refused, and left alone, while one holds dedicated
     nodes), so they leave the Grants list and the organization choice of
     "Give a node".

The organizations themselves stay Active: only their owner can delete one,
and the e2e accounts' passwords are kept nowhere. Nothing else is touched,
acme and every other slug included. Without --confirm e2e it prints the plan
and changes nothing.

  deploy/demo/clean-e2e.py [--confirm e2e]
"""
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lab  # noqa: E402

CONFIRMED = "--confirm" in sys.argv and sys.argv[sys.argv.index("--confirm") + 1:][:1] == ["e2e"]
TEST_SLUG = re.compile(r"^(e2e|optest)[a-z0-9]*$")
TEST_EMAIL = re.compile(r"^(e2e|optest)[^@]*@")


def is_test(slug, email=None):
    """A test organization: its slug, and its owner's address when given,
    name a test run. acme or any other slug never is."""
    return bool(TEST_SLUG.match(slug or "")) and (email is None or bool(TEST_EMAIL.match(email or "")))


def main():
    pending = [r for r in lab.sql("""SELECT o.id, o.slug, u.email FROM organizations o JOIN users u ON u.id = o.owner_user_id
        WHERE o.status = 'Pending' ORDER BY o.created_at""") if is_test(r[1], r[2])]
    grants = [r for r in lab.sql("""SELECT o.id, o.slug, g.cluster_id FROM organizations o JOIN organization_cluster_grants g ON g.org_id = o.id
        WHERE o.status = 'Active' ORDER BY o.created_at""") if is_test(r[1])]
    held = {r[0] for r in lab.sql("SELECT DISTINCT org_id FROM dedicated_node_grants WHERE revoked_at IS NULL")}
    keep = [g for g in grants if g[0] in held]
    grants = [g for g in grants if g[0] not in held]
    verb = "" if CONFIRMED else "would "
    print(f"{verb}reject {len(pending)} pending test request(s): {', '.join(p[1] for p in pending) or 'none'}")
    print(f"{verb}remove {len(grants)} cluster grant(s) of Active test organizations")
    for g in keep:
        print(f"  left alone: {g[1]} holds a dedicated node on {g[2]}")
    if not CONFIRMED:
        print("\nnothing changed: run make demo-clean-e2e CONFIRM=e2e to do it")
        return
    admin = lab.Portal("admin", lab.secret("keycloak", "keycloak-realm", "PLATFORM_ADMIN_PASSWORD"))
    failed = 0
    for org_id, slug, _ in pending:
        status, body = admin.api("POST", f"/admin/organizations/{org_id}:reject", {"reason": "end-to-end test leftover"})
        if status != 200:
            failed += 1
            print(f"  reject {slug}: HTTP {status} {body}")
    for org_id, slug, cluster in grants:
        status, body = admin.api("DELETE", f"/admin/organizations/{org_id}/cluster-grants/{cluster}")
        if status not in (200, 204, 404):
            failed += 1
            print(f"  grant of {slug} on {cluster}: HTTP {status} {body}")
    print(f"done: {len(pending) + len(grants) - failed} changed, {failed} refused")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
