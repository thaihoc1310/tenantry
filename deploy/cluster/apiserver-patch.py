#!/usr/bin/env python3
"""Run as root on the control-plane node by 70-apiserver.sh and
rollback-apiserver.sh (piped through ssh). Python 3 standard library only.

    python3 - apply CONFIG_BASE64 < apiserver-patch.py
    python3 - rollback < apiserver-patch.py

(The script itself comes on stdin, so the authentication config, which holds
no secret, comes as a base64 argument.)

apply: install the structured authentication config and make sure the static
Pod manifest of kube-apiserver has --authentication-config and the two
scheduling admission plugins. Only an actual change to the manifest restarts
kube-apiserver; then the new one must become ready within TIMEOUT seconds, or
the previous manifest is put back. A change to the authentication config
alone needs no restart: kube-apiserver reloads that file by itself.

rollback: put back the manifest kubeadm wrote, saved before the first change.
"""
import base64
import os
import shutil
import signal
import subprocess
import sys
import time

KUBE = "/etc/kubernetes"
MANIFEST = f"{KUBE}/manifests/kube-apiserver.yaml"
# Inside /etc/kubernetes/pki, which kubeadm already mounts into the Pod.
AUTH_CONFIG = f"{KUBE}/pki/tenantry-authentication-config.yaml"
# Never under manifests/: the kubelet runs every file there as a static Pod.
BACKUPS = f"{KUBE}/tenantry-backups"
ORIGINAL = f"{BACKUPS}/kube-apiserver.yaml.original"

AUTH_FLAG = f"--authentication-config={AUTH_CONFIG}"
ADMISSION_PREFIX = "--enable-admission-plugins="
PLUGINS = ["PodNodeSelector", "PodTolerationRestriction"]
TIMEOUT = 180


LOG = f"{BACKUPS}/apiserver-patch.log"


def log(msg):
    """Print to the SSH session and append to LOG on the node. Losing the
    session must not stop the script half-way, least of all while it waits to
    restore a manifest: a write to a closed session is ignored, and the log
    file keeps the whole story."""
    line = f"{time.strftime('%Y-%m-%d %H:%M:%S')} [{os.uname().nodename}] {msg}"
    try:
        print(line, flush=True)
    except (BrokenPipeError, OSError):
        pass
    try:
        os.makedirs(BACKUPS, mode=0o700, exist_ok=True)
        with open(LOG, "a", encoding="utf-8") as f:
            f.write(line + "\n")
    except OSError:
        pass


def apiserver_pid():
    out = subprocess.run(["pgrep", "-xo", "kube-apiserver"], capture_output=True, text=True).stdout.split()
    return out[0] if out else None


def ready():
    # /readyz answers anonymous requests; -k because only liveness matters here.
    r = subprocess.run(["curl", "-sk", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5",
                        "https://127.0.0.1:6443/readyz"], capture_output=True, text=True)
    return r.stdout == "200"


def write_atomic(path, text, mode):
    # A dot file in the same directory is ignored by the kubelet until renamed.
    tmp = os.path.join(os.path.dirname(path), "." + os.path.basename(path) + ".tmp")
    with open(tmp, "w", encoding="utf-8") as f:
        f.write(text)
        f.flush()
        os.fsync(f.fileno())
    os.chmod(tmp, mode)
    os.replace(tmp, path)


def wait_for_new_apiserver(old_pid):
    """True once a kube-apiserver process other than old_pid answers /readyz."""
    deadline = time.time() + TIMEOUT
    while time.time() < deadline:
        pid = apiserver_pid()
        if pid and pid != old_pid and ready():
            return True
        time.sleep(3)
    return False


def patched(manifest):
    lines = manifest.splitlines(keepends=True)
    cmd = next(i for i, l in enumerate(lines) if l.strip() == "- kube-apiserver")
    indent = lines[cmd][: len(lines[cmd]) - len(lines[cmd].lstrip())]
    if not any(l.strip() == f"- {AUTH_FLAG}" for l in lines):
        lines.insert(cmd + 1, f"{indent}- {AUTH_FLAG}\n")
    for i, l in enumerate(lines):
        if l.strip().startswith(f"- {ADMISSION_PREFIX}"):
            plugins = l.strip()[len(f"- {ADMISSION_PREFIX}"):].split(",")
            plugins += [p for p in PLUGINS if p not in plugins]
            lines[i] = f"{indent}- {ADMISSION_PREFIX}{','.join(plugins)}\n"
            break
    else:
        lines.insert(cmd + 1, f"{indent}- {ADMISSION_PREFIX}NodeRestriction,{','.join(PLUGINS)}\n")
    return "".join(lines)


def replace_manifest(new, reason):
    """Swap the manifest, wait for the new kube-apiserver, restore on failure."""
    os.makedirs(BACKUPS, mode=0o700, exist_ok=True)
    current = open(MANIFEST, encoding="utf-8").read()
    if not os.path.exists(ORIGINAL):
        shutil.copy2(MANIFEST, ORIGINAL)
    backup = f"{BACKUPS}/kube-apiserver.yaml.{time.strftime('%Y%m%d-%H%M%S')}"
    shutil.copy2(MANIFEST, backup)
    old_pid = apiserver_pid()
    log(f"{reason}; previous manifest saved as {backup}")
    write_atomic(MANIFEST, new, 0o600)
    log(f"kubelet is restarting kube-apiserver; waiting up to {TIMEOUT}s for it to be ready")
    if wait_for_new_apiserver(old_pid):
        log("kube-apiserver is ready")
        return True
    log("kube-apiserver did not become ready: restoring the previous manifest")
    old_pid = apiserver_pid()
    write_atomic(MANIFEST, current, 0o600)
    if wait_for_new_apiserver(old_pid):
        log("previous manifest restored, kube-apiserver is ready again")
    else:
        log(f"kube-apiserver is still not ready; inspect it on this node (manifest {MANIFEST}, backups in {BACKUPS})")
    return False


def apply(config_b64):
    config = base64.b64decode(config_b64).decode("utf-8")
    old = open(AUTH_CONFIG, encoding="utf-8").read() if os.path.exists(AUTH_CONFIG) else None
    if config != old:
        write_atomic(AUTH_CONFIG, config, 0o600)
        log(f"authentication config written to {AUTH_CONFIG}")
    manifest = open(MANIFEST, encoding="utf-8").read()
    new = patched(manifest)
    if new == manifest:
        log("manifest already has the authentication config and the admission plugins")
        return True
    return replace_manifest(new, "adding the authentication config and the admission plugins")


def rollback():
    if not os.path.exists(ORIGINAL):
        log(f"no {ORIGINAL}: nothing was ever changed")
        return True
    if open(ORIGINAL, encoding="utf-8").read() == open(MANIFEST, encoding="utf-8").read():
        log("manifest is already the original one")
        return True
    return replace_manifest(open(ORIGINAL, encoding="utf-8").read(), "restoring the manifest kubeadm wrote")


if __name__ == "__main__":
    # A dropped SSH session would otherwise end the script with SIGHUP.
    signal.signal(signal.SIGHUP, signal.SIG_IGN)
    action = sys.argv[1] if len(sys.argv) > 1 else ""
    if action == "apply" and len(sys.argv) == 3:
        sys.exit(0 if apply(sys.argv[2]) else 1)
    if action == "rollback":
        sys.exit(0 if rollback() else 1)
    sys.exit(__doc__)
