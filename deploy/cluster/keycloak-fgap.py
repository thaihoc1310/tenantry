#!/usr/bin/env python3
"""Narrow the service account of tenantry-sync (the group sync worker) with
Keycloak's fine-grained admin permissions v2 (architecture/06 §7, M12).

The worker finds, creates and deletes top-level groups, adds and removes
members, and turns accounts on and off. Meant to run with realm-management
query-users and query-groups only, instead of manage-users, it gets exactly
that through permissions of the realm's admin-permissions client:

  tenantry-sync-groups   Groups, all: view, manage, manage-membership
  tenantry-sync-users    Users, all: view, manage, manage-group-membership
  tenantry-sync-not-platform-admins
                         Group tenantry:platform-admins: view, manage,
                         view-members, manage-members, manage-membership,
                         manage-membership-of-members, impersonate-members,
                         DENIED (the group can be neither changed nor
                         deleted, nor its members touched). Every user holding the realm role
                         platform-admin is put in that group: a deny on a
                         group's member scopes wins over user permissions.

No map-roles and no impersonate are granted. Two policies name the service
account: tenantry-sync-sa (allow) and tenantry-sync-sa-denied (negative).

Users manage, which turning accounts on and off needs, lets tenantry-sync
take over a plain user's account: reset or set the password, change the
email (known limitation, architecture/06 §7). It cannot be narrowed: the
admin-permissions resource server decides UNANIMOUS, and on the all-Users
resource a permission that does not grant one scope (a reset-password deny,
tried on Keycloak 26.8.2) refuses every Users scope, manage included.
Platform Admins stay out of reach through the marker group.

Before anything, it refuses to go on if any group carries a role mapping:
with manage-membership on every group, putting someone in such a group would
hand them its roles. The group tenantry:platform-admins itself holds none
(the role stays mapped to the users directly).

Usage (env: none; the Keycloak Pod has the bootstrap admin's credentials):
  keycloak-fgap.py apply    | kubectl -n keycloak exec -i keycloak-0 -- bash -s
  keycloak-fgap.py rollback | kubectl -n keycloak exec -i keycloak-0 -- bash -s
  keycloak-fgap.py check    | ...   (only the role-mapping and platform-admin checks)

rollback gives the service account manage-users back first (and maps it into
the client's scope if missing, or the token would not carry it), so the worker
works again at once, takes query-users away (back to manage-users and
query-groups, what verify.sh expects with admin permissions off), then
deletes the permissions, policies and the group, and turns admin permissions
off. realm-sync.py, run later, follows realm-tenantry.json for the roles.

apply ends by taking manage-users away from the service account (query-users
and query-groups stay), once the permissions are in place.

Run it again and nothing changes. It prints what it does, no secret.
"""
import json
import sys

REALM = "tenantry"
SA = "service-account-tenantry-sync"
MARKER = "tenantry:platform-admins"
# Denied to tenantry-sync on the marker group: the group itself (view,
# manage: no renaming, deleting or reading it) and its members.
STALE = "tenantry-sync-no-reset-password"
DENIED_SCOPES = ["view", "manage", "view-members", "manage-members", "manage-membership",
                 "manage-membership-of-members", "impersonate-members"]


def doc(d):
    """An unquoted here-document: $sa and $gid in it are the Pod's values."""
    text = json.dumps(d, sort_keys=True)
    assert "\nJSON\n" not in text and "`" not in text
    return "<<JSON\n" + text + "\nJSON"


PRELUDE = f"""set -euo pipefail
kcadm=/opt/keycloak/bin/kcadm.sh
cfg=$(mktemp); trap 'rm -f "$cfg"' EXIT
$kcadm config credentials --config "$cfg" --server http://localhost:8080 --realm master \\
  --user "$KC_BOOTSTRAP_ADMIN_USERNAME" --password "$KC_BOOTSTRAP_ADMIN_PASSWORD" >/dev/null
k() {{ $kcadm "$@" --config "$cfg" -r {REALM}; }}
say() {{ echo "keycloak-fgap: $*" >&2; }}
# field NAME CSV: the value of column 1 on the line whose column 2 is NAME.
field() {{ local i n; while IFS=, read -r i n; do [ "$n" = "$1" ] && {{ echo "$i"; return 0; }}; done <<< "$2"; return 0; }}
sa=$(k get users -q username={SA} -q exact=true --fields id --format csv --noquotes)
[ -n "$sa" ] || {{ say "no user {SA}"; exit 1; }}
"""

# Every group's role mappings must be empty (kcadm prints "{ }" for none).
CHECK = f"""say "checking that no group carries a role mapping"
bad=0
# One read of every group with its roles: an empty mapping prints as "[ ]" or "{{ }}".
while IFS= read -r line; do
  case "$line" in
    *'"realmRoles" : [ ]'*|*'"clientRoles" : {{ }}'*) ;;
    *'"realmRoles"'*|*'"clientRoles"'*) say "a group carries a role mapping: $line"; bad=1;;
  esac
done <<< "$(k get groups -q briefRepresentation=false -q max=-1)"
[ "$bad" = 0 ] || {{ say "refusing: a group with roles would let tenantry-sync hand them out"; exit 1; }}
fgap=$(k get realms/{REALM} --fields adminPermissionsEnabled --format csv --noquotes)
if [ -z "${{APPLYING:-}}" ] && [ "$fgap" != true ]; then
  say "admin permissions are off: tenantry-sync has manage-users (rollback state); nothing more to check"
else
say "checking that every platform-admin is in {MARKER}, and that the group is out of tenantry-sync's reach"
gid=$(field "{MARKER}" "$(k get groups -q search="{MARKER}" -q exact=true --fields id,name --format csv --noquotes)")
if [ -z "${{APPLYING:-}}" ]; then
  [ -n "$gid" ] || {{ say "no group {MARKER}"; exit 1; }}
  members=$(k get "groups/$gid/members" --fields id --format csv --noquotes)
  for uid in $(k get roles/platform-admin/users --fields id --format csv --noquotes); do
    in=0; while IFS= read -r m; do [ "$m" = "$uid" ] && in=1; done <<< "$members"
    [ "$in" = 1 ] || {{ say "platform-admin $uid is not in {MARKER}: tenantry-sync could manage it"; bad=1; }}
  done
  # The denying permission: on the group, with the scopes that keep it and
  # its members out of reach, decided by the negative policy.
  ap=$(k get clients -q clientId=admin-permissions --fields id --format csv --noquotes)
  base="clients/$ap/authz/resource-server"
  pid=$(field tenantry-sync-not-platform-admins "$(k get "$base/permission/scope" --fields id,name --format csv --noquotes)")
  if [ -z "$pid" ]; then say "no permission tenantry-sync-not-platform-admins"; bad=1
  else
    res=$(k get "$base/policy/$pid/resources" --fields name --format csv --noquotes)
    in=0; while IFS= read -r r; do [ "$r" = "$gid" ] && in=1; done <<< "$res"
    [ "$in" = 1 ] || {{ say "tenantry-sync-not-platform-admins is not on {MARKER}"; bad=1; }}
    scopes=$(k get "$base/policy/$pid/scopes" --fields name --format csv --noquotes)
    for want in {" ".join(DENIED_SCOPES)}; do
      in=0; while IFS= read -r x; do [ "$x" = "$want" ] && in=1; done <<< "$scopes"
      [ "$in" = 1 ] || {{ say "tenantry-sync-not-platform-admins lacks the scope $want"; bad=1; }}
    done
    pols=$(k get "$base/policy/$pid/associatedPolicies" --fields name --format csv --noquotes)
    [ "$pols" = tenantry-sync-sa-denied ] || {{ say "tenantry-sync-not-platform-admins is not decided by tenantry-sync-sa-denied alone"; bad=1; }}
  fi
fi
fi
[ "$bad" = 0 ] || exit 1
"""


def upsert(kind, path, name, body):
    """Create or update a policy or permission named name under $base."""
    return f"""id=$(field {json.dumps(name)} "$(k get "$base/{kind}" --fields id,name --format csv --noquotes)")
if [ -z "$id" ]; then say "create {kind} {name}"; k create "$base/{path}" -f - >/dev/null {doc(body)}
else say "update {kind} {name}"; k update "$base/{path}/$id" -s "id=$id" -f - {doc(body)}
fi
"""


def apply():
    out = [PRELUDE, "APPLYING=1", CHECK]
    out.append(f"""say "admin permissions on"
k update realms/{REALM} -s adminPermissionsEnabled=true
ap=$(k get clients -q clientId=admin-permissions --fields id --format csv --noquotes)
[ -n "$ap" ] || {{ say "no admin-permissions client"; exit 1; }}
base="clients/$ap/authz/resource-server"
say "group {MARKER} with every platform-admin"
gid=$(field "{MARKER}" "$(k get groups -q search="{MARKER}" -q exact=true --fields id,name --format csv --noquotes)")
[ -n "$gid" ] || gid=$(k create groups -s name="{MARKER}" -i)
for uid in $(k get roles/platform-admin/users --fields id --format csv --noquotes); do
  k update "users/$uid/groups/$gid" -s realm={REALM} -s userId="$uid" -s groupId="$gid" -n
done
""")
    out.append(upsert("policy", "policy/user", "tenantry-sync-sa",
                      {"name": "tenantry-sync-sa", "logic": "POSITIVE", "users": ["__SA__"]}))
    out.append(upsert("policy", "policy/user", "tenantry-sync-sa-denied",
                      {"name": "tenantry-sync-sa-denied", "logic": "NEGATIVE", "users": ["__SA__"]}))
    perms = [
        {"name": "tenantry-sync-groups", "resourceType": "Groups", "scopes": ["view", "manage", "manage-membership"],
         "policies": ["tenantry-sync-sa"]},
        {"name": "tenantry-sync-users", "resourceType": "Users", "scopes": ["view", "manage", "manage-group-membership"],
         "policies": ["tenantry-sync-sa"]},
        {"name": "tenantry-sync-not-platform-admins", "resourceType": "Groups", "resources": ["__GROUP__"],
         "scopes": DENIED_SCOPES,
         "policies": ["tenantry-sync-sa-denied"]},
    ]
    for p in perms:
        out.append(upsert("permission/scope", "permission/scope", p["name"], p))
    # An earlier version's reset-password deny refused tenantry-sync every
    # Users scope: gone.
    out.append(f"""id=$(field {STALE} "$(k get "$base/permission/scope" --fields id,name --format csv --noquotes)")
[ -z "$id" ] || {{ say "delete permission {STALE}"; k delete "$base/permission/scope/$id"; }}
""")
    # Last, with the permissions in place, so the worker is never without
    # rights: the service account's roles become what realm-tenantry.json
    # gives it with the permissions on.
    out.append(f"""say "{SA}: query-users and query-groups, manage-users away"
k add-roles --uusername {SA} --cclientid realm-management --rolename query-users --rolename query-groups
k remove-roles --uusername {SA} --cclientid realm-management --rolename manage-users
""")
    out.append("say done; unset APPLYING\n" + CHECK)
    # The service account's and the group's ids are known in the Pod only.
    return "\n".join(out).replace("__SA__", "$sa").replace("__GROUP__", "$gid")


def rollback():
    return PRELUDE + f"""say "manage-users back to {SA} first"
k add-roles --uusername {SA} --cclientid realm-management --rolename manage-users
# The token carries a role only if the client's scope maps it too
# (fullScopeAllowed is off): a realm made from the file with the permissions
# on has no manage-users there.
cid=$(k get clients -q clientId=tenantry-sync --fields id --format csv --noquotes)
rm=$(k get clients -q clientId=realm-management --fields id --format csv --noquotes)
mapped=0; while IFS= read -r r; do [ "$r" = manage-users ] && mapped=1; done <<< "$(k get "clients/$cid/scope-mappings/clients/$rm" --fields name --format csv --noquotes)"
if [ "$mapped" = 0 ]; then
  say "map manage-users into the scope of tenantry-sync"
  rid=$(k get "clients/$rm/roles/manage-users" --fields id --format csv --noquotes)
  k create "clients/$cid/scope-mappings/clients/$rm" -f - <<JSON
[{{"id": "$rid", "name": "manage-users"}}]
JSON
fi
say "query-users away from {SA}: manage-users and query-groups, as before apply"
k remove-roles --uusername {SA} --cclientid realm-management --rolename query-users
ap=$(k get clients -q clientId=admin-permissions --fields id --format csv --noquotes)
if [ -n "$ap" ]; then
  base="clients/$ap/authz/resource-server"
  for name in tenantry-sync-groups tenantry-sync-users tenantry-sync-no-reset-password tenantry-sync-not-platform-admins; do
    id=$(field "$name" "$(k get "$base/permission/scope" --fields id,name --format csv --noquotes)")
    [ -z "$id" ] || {{ say "delete permission $name"; k delete "$base/permission/scope/$id"; }}
  done
  for name in tenantry-sync-sa tenantry-sync-sa-denied; do
    id=$(field "$name" "$(k get "$base/policy" --fields id,name --format csv --noquotes)")
    [ -z "$id" ] || {{ say "delete policy $name"; k delete "$base/policy/$id"; }}
  done
fi
gid=$(field "{MARKER}" "$(k get groups -q search="{MARKER}" -q exact=true --fields id,name --format csv --noquotes)")
[ -z "$gid" ] || {{ say "delete group {MARKER}"; k delete "groups/$gid"; }}
say "admin permissions off"
k update realms/{REALM} -s adminPermissionsEnabled=false
say done
"""


if __name__ == "__main__":
    mode = sys.argv[1] if len(sys.argv) > 1 else ""
    if mode == "apply":
        print(apply())
    elif mode == "rollback":
        print(rollback())
    elif mode == "check":
        print(PRELUDE + CHECK)
    else:
        sys.exit(__doc__)
