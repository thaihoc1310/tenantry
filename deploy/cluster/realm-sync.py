#!/usr/bin/env python3
"""Bring the running tenantry realm in line with deploy/keycloak/realm-tenantry.json
(IMPLEMENTATION-PLAN §10). Keycloak imports that file only when the realm does
not exist yet; this applies later changes of it to the realm in place.

What it changes, from the file:
  - realm settings (token lifetimes, email verification, SMTP, brute force...);
  - realm roles: created when missing, description updated;
  - clients: created when missing, otherwise their settings and protocol
    mappers updated;
  - a client's default and optional client scopes, when the file lists them
    (defaultClientScopes, optionalClientScopes): exactly those;
  - a client's scope mappings to other clients' roles (clientScopeMappings):
    added when missing. One left over grants nothing beyond what the user
    holds, and the service accounts' roles below are exact;
  - the client roles of a client's service account, when the file's users
    list that service account (serviceAccountClientId) with clientRoles:
    exactly those roles of those clients, added when missing, any other role
    of the same clients taken away. Only that service account is touched.

What it never touches:
  - client secrets of existing clients (the file only names a placeholder);
  - users (other than the service accounts above), groups and memberships
    (people's accounts and their sub);
  - the realm itself: never deleted and imported again, which would change
    every user's sub and orphan the portal's users rows.

Run it again and nothing changes. It prints the commands it runs, no values.

Usage: realm-sync.py REALM_FILE   (env: PORTAL_HOST, CONSOLE_HOST)
Writes to stdout a bash script for the Keycloak Pod:
  realm-sync.py realm.json | kubectl -n keycloak exec -i keycloak-0 -- bash -s
"""
import json
import os
import re
import shlex
import sys

PLACEHOLDER = re.compile(r"\$\{([A-Z0-9_]+)\}")
# Placeholders the Pod resolves itself (from the keycloak-realm Secret), only
# ever when a client is created.
SECRET = re.compile(r"^\$\{([A-Z0-9_]+(SECRET|PASSWORD))\}$")
HOSTS = ("PORTAL_HOST", "CONSOLE_HOST")


def hosts(value):
    """Fill in the host placeholders; leave secret placeholders alone."""
    if isinstance(value, str):
        return PLACEHOLDER.sub(lambda m: os.environ[m.group(1)] if m.group(1) in HOSTS else m.group(0), value)
    if isinstance(value, list):
        return [hosts(v) for v in value]
    if isinstance(value, dict):
        return {k: hosts(v) for k, v in value.items()}
    return value


def heredoc(doc):
    text = json.dumps(doc, indent=1, sort_keys=True)
    assert "\nJSON\n" not in text
    return "<<'JSON'\n" + text + "\nJSON"


def main():
    for h in HOSTS:
        if not os.environ.get(h):
            sys.exit(f"{h} is not set")
    realm = hosts(json.load(open(sys.argv[1])))
    name = realm["realm"]

    out = [
        "set -euo pipefail",
        "kcadm=/opt/keycloak/bin/kcadm.sh",
        "cfg=$(mktemp); trap 'rm -f \"$cfg\"' EXIT",
        # The bootstrap admin's credentials are in the container's environment.
        '$kcadm config credentials --config "$cfg" --server http://localhost:8080 --realm master '
        '--user "$KC_BOOTSTRAP_ADMIN_USERNAME" --password "$KC_BOOTSTRAP_ADMIN_PASSWORD" >/dev/null',
        f'k() {{ $kcadm "$@" --config "$cfg" -r {shlex.quote(name)}; }}',
        'say() { echo "realm-sync: $*" >&2; }',
        # has LINE TEXT: TEXT has LINE as one of its lines (no grep in the image).
        'has() { local l; while IFS= read -r l; do [ "$l" = "$1" ] && return 0; done <<< "$2"; return 1; }',
        # scope_id NAME: the id of the client scope NAME, from $scopes.
        'scope_id() { local i n; while IFS=, read -r i n; do [ "$n" = "$1" ] && { echo "$i"; return 0; }; done <<< "$scopes"; say "no client scope $1"; exit 1; }',
    ]
    service_accounts = {u["serviceAccountClientId"]: u for u in realm.get("users", []) if u.get("serviceAccountClientId")}

    # Realm settings: everything but the collections, handled below or not at all.
    settings = {k: v for k, v in realm.items() if k not in ("roles", "clients", "users", "groups", "clientScopeMappings")}
    out += [f"say update realm {name} settings", f"$kcadm update realms/{name} --config \"$cfg\" -f - {heredoc(settings)}"]

    for role in realm.get("roles", {}).get("realm", []):
        r = shlex.quote(role["name"])
        out += [
            f"if k get roles/{r} >/dev/null 2>&1; then say update role {r}; k update roles/{r} -f - {heredoc(role)}",
            f"else say create role {r}; k create roles -f - {heredoc(role)}",
            "fi",
        ]

    for client in realm.get("clients", []):
        cid = shlex.quote(client["clientId"])
        mappers = client.get("protocolMappers", [])
        settings = {k: v for k, v in client.items() if k not in ("secret", "protocolMappers", "defaultClientScopes", "optionalClientScopes")}
        secret = SECRET.match(client.get("secret", "") or "")
        create = f"k create clients -f - {heredoc({k: v for k, v in client.items() if k != 'secret'})}"
        if secret:  # resolved by the Pod, never written here
            create = f'k create clients -s "secret=${{{secret.group(1)}}}" -f - {heredoc({k: v for k, v in client.items() if k != "secret"})}'
        out += [
            f"id=$(k get clients -q clientId={cid} --fields id --format csv --noquotes)",
            f'if [ -z "$id" ]; then say create client {cid}; {create}',
            f"else say update client {cid}; k update \"clients/$id\" -f - {heredoc(settings)}",
            '  maps=$(k get "clients/$id/protocol-mappers/models" --fields id,name --format csv --noquotes)',
        ]
        for m in mappers:
            mn = shlex.quote(m["name"])
            out += [
                # No awk in the Keycloak image: plain bash.
                f'  mid=""; while IFS=, read -r i n; do [ "$n" = {mn} ] && mid=$i; done <<< "$maps"',
                f'  if [ -n "$mid" ]; then say update mapper {cid}/{mn}; k update "clients/$id/protocol-mappers/models/$mid" -s "id=$mid" -f - {heredoc(m)}',
                f'  else say create mapper {cid}/{mn}; k create "clients/$id/protocol-mappers/models" -f - {heredoc(m)}',
                "  fi",
            ]
        out.append("fi")

        # The client may have been created just above.
        out.append(f"id=$(k get clients -q clientId={cid} --fields id --format csv --noquotes)")

        kinds = [kind for kind in ("default", "optional") if f"{kind}ClientScopes" in client]
        if kinds:
            out.append("scopes=$(k get client-scopes --fields id,name --format csv --noquotes)")
        for kind in kinds:  # removals first: a scope may move from one kind to the other
            want = "\n".join(client[f"{kind}ClientScopes"])
            out += [
                f'have=$(k get "clients/$id/{kind}-client-scopes" --fields name --format csv --noquotes)',
                f"want=$'{want}'",
                'while IFS= read -r s; do [ -z "$s" ] || has "$s" "$want" || { say "remove ' + kind + ' scope $s from client ' + client["clientId"] + '"; '
                f'k delete "clients/$id/{kind}-client-scopes/$(scope_id "$s")"; }}; done <<< "$have"',
            ]
        for kind in (k for k in kinds if client[f"{k}ClientScopes"]):
            out.append(f'have=$(k get "clients/$id/{kind}-client-scopes" --fields name --format csv --noquotes)')
            for sc in client[f"{kind}ClientScopes"]:
                q = shlex.quote(sc)
                out.append(f'has {q} "$have" || {{ say add {kind} scope {q} to client {cid}; '
                           f'k update "clients/$id/{kind}-client-scopes/$(scope_id {q})" --no-merge -b "{{}}"; }}')

        for owner, entries in sorted(realm.get("clientScopeMappings", {}).items()):
            names = [r for e in entries if e["client"] == client["clientId"] for r in e["roles"]]
            if not names:
                continue
            o = shlex.quote(owner)
            out += [
                f"oid=$(k get clients -q clientId={o} --fields id --format csv --noquotes)",
                'have=$(k get "clients/$id/scope-mappings/clients/$oid" --fields name --format csv --noquotes)',
            ]
            for n in names:
                q = shlex.quote(n)
                out.append(f'has {q} "$have" || {{ say map {o}/{q} into the scope of client {cid}; '
                           f'rid=$(k get "clients/$oid/roles/"{q} --fields id --format csv --noquotes); '
                           f'k create "clients/$id/scope-mappings/clients/$oid" -b \'[{{"id":"\'"$rid"\'","name":"{n}"}}]\'; }}')

        roles = service_accounts.get(client["clientId"], {}).get("clientRoles", {})
        if roles:
            assert client.get("serviceAccountsEnabled"), client["clientId"]
            out += [
                'uid=$(k get "clients/$id/service-account-user" --fields id --format csv --noquotes)',
                '[ -n "$uid" ] || { say "client ' + client["clientId"] + ' has no service account"; exit 1; }',
            ]
            for owner, names in sorted(roles.items()):
                o = shlex.quote(owner)
                want = "\n".join(names)
                out.append(f'have=$(k get-roles --uid "$uid" --cclientid {o} --fields name --format csv --noquotes)')
                for n in names:
                    q = shlex.quote(n)
                    out.append(f'has {q} "$have" || {{ say grant {o}/{q} to the service account of {cid}; '
                               f'k add-roles --uid "$uid" --cclientid {o} --rolename {q}; }}')
                out += [
                    f"want=$'{want}'",
                    'while IFS= read -r r; do [ -z "$r" ] || has "$r" "$want" || { say "revoke ' + owner + '/$r from the service account of ' + client["clientId"] + '"; '
                    f'k remove-roles --uid "$uid" --cclientid {o} --rolename "$r"; }}; done <<< "$have"',
                ]

    print("\n".join(out))


if __name__ == "__main__":
    main()
