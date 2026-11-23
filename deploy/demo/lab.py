"""What demo-preflight and demo-reset share: kubectl on the lab, read-only
SQL, the Keycloak realm's demo accounts, and the portal API as a person who
logs in through Keycloak like a browser. Python 3 standard library only.

Never prints a secret: passwords are read from the Secret keycloak-realm and
kept in memory.
"""
import base64
import html
import http.cookiejar
import json
import os
import re
import socket
import ssl
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from datetime import datetime, timezone

CONTEXT = os.environ.get("KUBE_CONTEXT", "hocbt@kubernetes")
ZONE = os.environ.get("DNS_ZONE", "tenantry.site")
PORTAL = f"portal.{ZONE}"
AUTH = f"auth.{ZONE}"
KUBE = f"kube-a.{ZONE}"
CONSOLE = f"console-a.{ZONE}"
CLUSTER = "cluster-a"
POOL = "shared-general"
NETWORK_POOL = "onboarding-pod-network"
SHARED_NODES = ("fdp-k8s-wk2", "fdp-k8s-wk3")
DEMO_ORG = "acme"
DEMO_USERS = ("alice", "joe", "bob", "mallory")


def kubectl(*args, input=None, check=True):
    r = subprocess.run(["kubectl", "--context", CONTEXT, *args], input=input, capture_output=True, text=True)
    if check and r.returncode != 0:
        raise RuntimeError(f"kubectl {' '.join(args[:3])}…: {r.stderr.strip()[:300]}")
    return r.stdout


def secret(ns, name, key):
    return base64.b64decode(kubectl("-n", ns, "get", "secret", name, "-o", f"jsonpath={{.data.{key}}}")).decode()


def sql(query):
    """One read-only query on the platform database; rows as lists of strings."""
    out = kubectl("-n", "tenantry-system", "exec", "postgres-0", "--", "sh", "-c",
                  'psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-tenantry}" -v ON_ERROR_STOP=1 '
                  '-At -F "\t" -c "SET default_transaction_read_only = on" -c "$0"', query)
    lines = out.splitlines()[1:]  # the first is the SET's own answer
    return [line.split("\t") for line in lines if line]


def psql_script(script):
    """Run an SQL script on the platform database as written (it opens and
    ends its own transaction; this one can write); its -At output lines,
    tab-separated, as lists."""
    out = kubectl("-n", "tenantry-system", "exec", "-i", "postgres-0", "--", "sh", "-c",
                  'psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-tenantry}" -v ON_ERROR_STOP=1 -q -At -F "	" -f -',
                  input=script)
    return [line.split("\t") for line in out.splitlines() if line]


def kcadm(script):
    """Run kcadm commands as the bootstrap admin inside the Keycloak Pod (read
    with get only, by the callers here); k is kcadm on the tenantry realm."""
    prelude = """set -euo pipefail
kcadm=/opt/keycloak/bin/kcadm.sh; cfg=$(mktemp); trap 'rm -f "$cfg"' EXIT
$kcadm config credentials --config "$cfg" --server http://localhost:8080 --realm master \\
  --user "$KC_BOOTSTRAP_ADMIN_USERNAME" --password "$KC_BOOTSTRAP_ADMIN_PASSWORD" >/dev/null
k() { $kcadm "$@" --config "$cfg" -r tenantry; }
"""
    return kubectl("-n", "keycloak", "exec", "-i", "keycloak-0", "--", "bash", "-s", input=prelude + script)


def metrics(service):
    """The /metrics of a platform Service, through the API server's proxy."""
    text = kubectl("get", "--raw", f"/api/v1/namespaces/tenantry-system/services/{service}:metrics/proxy/metrics")
    out = {}
    for line in text.splitlines():
        if line and not line.startswith("#"):
            name, _, value = line.rpartition(" ")
            try:
                out[name] = float(value)
            except ValueError:
                pass
    return out


def cert_days(host):
    """Days until the TLS certificate of host expires; raises when it does not verify."""
    ctx = ssl.create_default_context()
    with socket.create_connection((host, 443), timeout=10) as s, ctx.wrap_socket(s, server_hostname=host) as t:
        end = datetime.strptime(t.getpeercert()["notAfter"], "%b %d %H:%M:%S %Y %Z").replace(tzinfo=timezone.utc)
    return (end - datetime.now(timezone.utc)).days


def get(url, timeout=10):
    req = urllib.request.Request(url, headers={"User-Agent": "tenantry-demo"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def password_ok(username, password):
    """Whether Keycloak takes the password (direct grant on the realm's
    built-in admin-cli client); leaves no row in the portal."""
    body = urllib.parse.urlencode({"grant_type": "password", "client_id": "admin-cli",
                                   "username": username, "password": password}).encode()
    req = urllib.request.Request(f"https://{AUTH}/realms/tenantry/protocol/openid-connect/token", data=body)
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status == 200
    except urllib.error.HTTPError:
        return False


class Portal:
    """The portal API as one person, logged in through Keycloak's own form."""

    def __init__(self, username, password):
        self.jar = http.cookiejar.CookieJar()
        self.http = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))
        with self.http.open(f"https://{PORTAL}/auth/login", timeout=20) as r:
            page = r.read().decode()
        m = re.search(r'<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"', page) or \
            re.search(r'<form[^>]*action="([^"]+)"[^>]*id="kc-form-login"', page)
        if not m:
            raise RuntimeError("no Keycloak login form")
        form = urllib.parse.urlencode({"username": username, "password": password, "credentialId": ""}).encode()
        with self.http.open(html.unescape(m.group(1)), data=form, timeout=20) as r:
            landed = urllib.parse.urlparse(r.geturl())
        if landed.hostname != PORTAL or self.csrf() == "":
            raise RuntimeError(f"login of {username} did not come back to the portal")

    def csrf(self):
        return next((c.value for c in self.jar if c.name == "__Host-tenantry_csrf"), "")

    def api(self, method, path, body=None, idempotent=False):
        """One call; (status, body). GET, PUT and DELETE are tried again on a
        network failure (retried), the others are not."""
        data = None if body is None else json.dumps(body).encode()
        key = str(uuid.uuid4()) if idempotent else None

        def call():
            req = urllib.request.Request(f"https://{PORTAL}/api/v1{path}", data=data, method=method)
            req.add_header("Content-Type", "application/json")
            req.add_header("X-CSRF-Token", urllib.parse.unquote(self.csrf()))
            if key:
                req.add_header("Idempotency-Key", key)
            try:
                with self.http.open(req, timeout=30) as r:
                    raw = r.read()
                    return r.status, (json.loads(raw) if raw else None)
            except urllib.error.HTTPError as e:
                raw = e.read()
                try:
                    return e.code, json.loads(raw)
                except ValueError:
                    return e.code, {"message": raw.decode(errors="replace")[:200]}

        return retried(f"{method} {path}", call) if method in SAFE_TO_REPEAT else call()


# Methods the portal API answers the same way when repeated: a lost answer
# can be asked again. POST (reject, force) is not repeated.
SAFE_TO_REPEAT = {"GET", "PUT", "DELETE"}


def retried(what, call, tries=3, backoff=2.0, sleep=time.sleep):
    """call(), tried again after a network failure (timeout, reset, refused):
    at most tries times, waiting backoff, then twice as long. An HTTP answer,
    whatever its status, is not a failure here."""
    for attempt in range(tries):
        try:
            return call()
        except urllib.error.HTTPError:
            raise
        except (urllib.error.URLError, OSError) as e:
            if attempt == tries - 1:
                raise
            wait_s = backoff * 2 ** attempt
            print(f"  {what}: {e}; trying again in {wait_s:.0f}s", flush=True)
            sleep(wait_s)


def verify_failures(out):
    """What cluster-verify says failed: each FAIL line (the check and the
    first line of its error), or its last line when it printed none."""
    fails = [line.strip() for line in out.splitlines() if line.startswith("FAIL")]
    if fails:
        return f"{len(fails)} failed: " + " | ".join(fails)
    lines = out.strip().splitlines()
    return lines[-1] if lines else "failed"


def wait(what, check, timeout=600, every=5):
    """Call check until it returns something other than None; that value."""
    end = time.time() + timeout
    while True:
        v = check()
        if v is not None:
            return v
        if time.time() > end:
            raise RuntimeError(f"timed out waiting for {what}")
        time.sleep(every)
