# Token size across the access path

IMPLEMENTATION-PLAN M11 item 3. Run on 2026-11-23 with `go test -tags lab -run TestTokenSize ./test/isolation`.

## Setup

- A throwaway account `optest-*` signs up through Keycloak. It owns an Organization made by the Platform
  Admin, creates 20 groups `team-01` to `team-20` and joins them all, so it holds 21 groups with `admins`.
- Once Keycloak has every group and membership, the account gets an ID token from the `kubectl` client
  (authorization code with PKCE, as kubelogin does).
- That token goes, as `Authorization: Bearer`, in a SelfSubjectReview through three paths:
  - kube-apiserver directly;
  - `kube-a` (Traefik → Capsule Proxy);
  - `console-a` (Traefik → Headlamp → Capsule Proxy).
- The same request is then sent with an extra 16 KiB and 64 KiB header, to see how much room is left.

Everything was cleaned up: the Organization (and its groups) deleted, the account disabled.

## Results

| | |
|---|---|
| groups in the token | 21 |
| JWT | 3,009 bytes |
| Authorization header | 3,016 bytes |

| Path | Token alone | + 16 KiB header | + 64 KiB header |
|---|---|---|---|
| kube-apiserver | 201, 22 `oidc:` names (user + 21 groups) | 201 | 201 |
| Traefik → Capsule Proxy | 201, 22 | 201 | 201 |
| Traefik → Headlamp → Capsule Proxy | 201, 22 | 201 | 201 |

No hop truncates or refuses the token. Go's servers (Traefik, Capsule Proxy, Headlamp, kube-apiserver)
accept headers up to 1 MiB by default, so a member of many more groups still fits. Each group adds about
45 bytes, since group names are `oidc:org_<ulid>:group_<ulid>`.

## Not covered here

Headlamp's browser session keeps the OIDC tokens in cookies. A browser limits one cookie to about 4 KiB,
which a 3 KiB ID token alone comes close to. This test sends the token as a header, not through the
browser login. The web e2e suite (Playwright) should log in to Headlamp as a member of about 20 groups to
cover that path. Nothing needs changing on the cluster side.
