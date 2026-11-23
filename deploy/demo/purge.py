#!/usr/bin/env python3
"""make demo-purge-test-data [CONFIRM=e2e]: remove the fixtures end-to-end
runs leave on the lab, for good: organizations whose slug starts with e2e or
optest (any status) and accounts whose email does, with everything theirs.

  1. The database, in one transaction (purge.sql): without --confirm e2e it
     runs and is rolled back, so the plan is exactly what would go, table by
     table. An organization the cluster may still hold (a Workspace not
     Deleted, an address block in quarantine, a live dedicated node) is
     refused and kept; so is an account a kept organization refers to.
  2. Keycloak: the purged accounts and the groups of the purged
     organizations, plus test accounts Keycloak has without a portal row.
  3. The cluster: read only. Anything left with an e2e or optest name is
     listed, for a person to look at; nothing is deleted there.

admin, alice, joe, bob, mallory and every other real account, every other
organization, the cluster registry, the pools and the policy are never
touched; the real accounts are listed as kept.

  deploy/demo/purge.py [--confirm e2e]
"""
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lab  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
CONFIRMED = "--confirm" in sys.argv and sys.argv[sys.argv.index("--confirm") + 1:][:1] == ["e2e"]
TEST = re.compile(r"^(e2e|optest)")
PROTECTED = {"admin", "alice", "joe", "bob", "mallory"}


def is_test_account(username, email):
    """A Keycloak account made by a test run: its username or email starts
    with e2e or optest, and it is none of the protected accounts."""
    local = (email or "").split("@")[0]
    if (username or "") in PROTECTED or local in PROTECTED:
        return False
    return bool(TEST.match(username or "") or TEST.match(email or ""))


def database():
    with open(os.path.join(HERE, "purge.sql")) as f:
        body = f.read()
    rows = lab.psql_script("BEGIN;\n" + body + ("\nCOMMIT;\n" if CONFIRMED else "\nROLLBACK;\n"))
    by = {}
    for r in rows:
        by.setdefault(r[0], []).append(r[1:])
    return by


def keycloak(groups):
    """Keycloak accounts and groups to delete: the purged ones, and test
    accounts Keycloak holds without a portal row."""
    out = lab.kcadm("k get users -q max=-1 --fields id,username,email --format csv --noquotes; echo ---; "
                    "k get groups -q max=-1 --fields id,name --format csv --noquotes")
    users_csv, _, groups_csv = out.partition("---\n")
    users = []
    for line in users_csv.splitlines():
        uid, username, email = (line.split(",") + ["", ""])[:3]
        if is_test_account(username, email):
            users.append((uid, username, email))
    names = set(groups)
    kc_groups = [line.split(",", 1) for line in groups_csv.splitlines() if line.count(",") >= 1]
    kc_groups = [(gid, name) for gid, name in kc_groups if name in names]
    return users, kc_groups


def cluster_leftovers():
    found = []
    for kind in ("tenants.capsule.clastix.io", "platformworkspaces.tenantry.io", "namespaces",
                 "ippools.projectcalico.org", "platformnodegrants.tenantry.io"):
        out = lab.kubectl("get", kind, "-o", "name", check=False)
        found += [f"{kind}: {n.split('/', 1)[-1]}" for n in out.splitlines() if TEST.search(n.split("/", 1)[-1])]
    out = lab.kubectl("get", "rolebindings", "-A", "--no-headers", "-o", "custom-columns=NS:.metadata.namespace,NAME:.metadata.name", check=False)
    found += [f"rolebinding: {' '.join(l.split())}" for l in out.splitlines() if TEST.search(l)]
    return found


def main():
    by = database()
    verb = "deleted" if CONFIRMED else "would delete"
    print(f"Database ({'committed' if CONFIRMED else 'plan: rolled back'}):")
    for table, n in by.get("delete", []):
        if n != "0":
            print(f"  {verb} {n:>6}  {table}")
    total = sum(int(n) for _, n in by.get("delete", []))
    print(f"  {total} rows in all")
    for what, why in by.get("refuse", []):
        print(f"  refused: {what}: {why}")
    keep = [k[0] for k in by.get("keep", [])]
    print(f"Kept accounts ({len(keep)}): {', '.join(keep)}")

    groups = [g[0] for g in by.get("kc-group", [])]
    users, kc_groups = keycloak(groups)
    refused = {w.split(" ", 1)[1] for w, _ in by.get("refuse", []) if w.startswith("account ")}
    users = [u for u in users if u[2] not in refused and u[2] not in keep]
    print(f"Keycloak: {'deleting' if CONFIRMED else 'would delete'} {len(users)} account(s) and {len(kc_groups)} group(s)")
    if CONFIRMED and (users or kc_groups):
        script = "".join(f"k delete users/{uid}\n" for uid, _, _ in users) + "".join(f"k delete groups/{gid}\n" for gid, _ in kc_groups)
        lab.kcadm(script)

    left = cluster_leftovers()
    print(f"Cluster, read only: {len(left)} object(s) with a test name")
    for x in left:
        print(f"  {x}")
    if not CONFIRMED:
        print("\nnothing changed: run make demo-purge-test-data CONFIRM=e2e to do it")


if __name__ == "__main__":
    main()
