#!/usr/bin/env python3
"""Bring the running tenantry realm in line with deploy/keycloak/realm-tenantry.json
(IMPLEMENTATION-PLAN §10). Keycloak imports that file only when the realm does
not exist yet; this applies later changes of it to the realm in place.

What it changes, from the file:
  - realm settings (token lifetimes, email verification, SMTP, brute force...);
  - realm roles: created when missing, description updated;
  - clients: created when missing, otherwise their settings and protocol
    mappers updated.

What it never touches:
  - client secrets of existing clients (the file only names a placeholder);
  - users, groups and memberships (people's accounts and their sub);
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
    ]

    # Realm settings: everything but the collections, handled below or not at all.
    settings = {k: v for k, v in realm.items() if k not in ("roles", "clients", "users", "groups")}
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
        settings = {k: v for k, v in client.items() if k not in ("secret", "protocolMappers")}
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

    print("\n".join(out))


if __name__ == "__main__":
    main()
