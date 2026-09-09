[← Documentation index](README.md) · [almanaut](../README.md)

# Authentication & access control

Almanaut **requires a login** — every page and the whole JSON API sit behind a
session-cookie auth check, with no way to run it open.

## Roles

Every user has one of three built-in roles (there are no custom roles):

| Role | Can do |
|---|---|
| **admin** | Everything, including managing users, webhooks, custom fields, and integrations |
| **editor** | Create, edit, and delete inventory |
| **viewer** | Read-only access to everything |

Admins manage accounts at **Users** (`/users`): create users (viewer by
default), change roles, reset passwords, and delete accounts. The last
remaining user cannot be deleted, so you can't lock yourself out entirely.
Each user changes their own password at `/account/password`.

## Sessions & login throttling

Sessions are server-side (stored in SQLite), cookie-based, and last 30 days;
use the **Logout** button to end one early. Failed logins are throttled
per-username: after 5 consecutive failures, further attempts for that username
are refused for 15 minutes (state is in-memory and resets on restart).

[`/api/*`](api.md) returns a plain `401` JSON error when called without valid
credentials (a session cookie or a bearer token).

[`/healthz` and `/version`](monitoring.md#health--version) bypass the login so
container health probes keep working, and the login pages themselves plus the
stylesheet they need (`/static/app.css`) are public by necessity. Nothing that
reads or writes inventory is: every other route requires a session or a token.

## Lockout recovery

Locked out? Set `ALMANAUT_RESET_ADMIN=true` and restart — almanaut resets the
admin's password (using `ALMANAUT_AUTH_PASS` if set, otherwise a fresh random
one) and logs the new value the same way as the first-run banner. **Unset
`ALMANAUT_RESET_ADMIN` afterwards**, or every restart will reset the password
again.

Note that `ALMANAUT_AUTH_USER` / `ALMANAUT_AUTH_PASS` only seed the *initial*
admin (or feed `ALMANAUT_RESET_ADMIN`); they are **not** HTTP Basic auth
credentials and are not checked on every request. The `_FILE` convention
applies to `ALMANAUT_AUTH_PASS` (see [Secrets from files](configuration.md#secrets-from-files)).

## Two-factor authentication (TOTP)

Each user can add a second factor to their own account at **Two-factor
authentication** (`/account/2fa`). Enrollment shows a QR code to scan with any
TOTP app, and is only completed once you enter a first valid code — so a
mis-scanned secret cannot lock you out of your own account.

Confirming enrollment shows **10 single-use recovery codes, once**. Copy them
then; only their hashes are stored. Each one works exactly once, and the page
shows how many you have left.

Turning 2FA off requires a current TOTP code or a recovery code, not just your
session. That is deliberate: a hijacked session should not be able to quietly
remove the second factor it is supposed to be protected by.

With 2FA on, logging in becomes two steps — the password form, then a challenge
at `/login/2fa` reached through a short-lived cookie that is only valid between
the two. A correct password alone creates no session.

Admins can clear a user's second factor at **Users** (`/users`, see
[Roles](#roles)) for a lost phone with no recovery codes left. Doing so also
revokes that user's sessions and is recorded in the audit log, so it can never
be a silent downgrade of someone's account.

## Reverse-proxy SSO

If an authenticating proxy already sits in front of almanaut (Authelia,
Authentik, oauth2-proxy, …), it can assert the identity instead. All four
settings below are in the [configuration table](configuration.md):

- `ALMANAUT_PROXY_AUTH_HEADER` — the header carrying the username, e.g.
  `Remote-User`. Empty (the default) disables header auth entirely.
- `ALMANAUT_PROXY_AUTH_ALLOWLIST` — the proxy IPs or CIDRs the header is
  trusted from.
- `ALMANAUT_PROXY_AUTH_AUTOPROVISION` — create a local account the first time
  an unknown identity arrives (off by default).
- `ALMANAUT_PROXY_AUTH_DEFAULT_ROLE` — the role such an account gets
  (`viewer` by default).

**The allowlist is the security boundary, and it is checked against the request's
direct peer**, not against any forwarded-for header. A client that is not the
configured proxy cannot forge an identity by setting the header itself, because
its own address is what gets checked. Setting the header without an allowlist
that matches your proxy therefore fails closed rather than trusting everyone.

On any miss — header absent, peer not allowlisted, identity unknown with
auto-provisioning off — the request falls through to ordinary session auth, so
the normal login form keeps working. Auto-provisioned accounts are created with
an unusable password, since they are only ever meant to authenticate through the
proxy.

Roles still apply exactly as they do for local accounts: SSO decides *who* you
are, never *what you may do*.

## Authentication audit log

Admins get an append-only trail of authentication events at **Audit**
(`/audit`):

| Event | Recorded when |
|---|---|
| `login_success` | A password (and second factor, if enabled) was accepted |
| `login_failure` | A password was rejected, or the attempt was rate-limited |
| `logout` | A session was ended from the Logout button |
| `2fa_success` / `2fa_failure` | A second factor was accepted or rejected |
| `sso_login` | A proxy-asserted identity was accepted |
| `token_used` | An [API token](api.md#api-tokens) authenticated a request |
| `session_revoked` | Sessions were invalidated (a user deleted, or their 2FA reset) |

Two details make this trail trustworthy rather than decorative:

- **The IP recorded is the direct peer**, never `X-Forwarded-For`. A forged
  header cannot poison the log. The flip side is that behind a reverse proxy
  every event shows the proxy's address, so correlate with the proxy's own logs
  for the original client.
- **Auditing never breaks authentication.** A failed audit write is logged and
  swallowed rather than propagated, so a full disk degrades the trail instead of
  locking everyone out.

`token_used` is coalesced — each token is recorded at most once per window — so
a script polling the API every few seconds leaves a usage trail without
flooding the log. That state is in memory and resets on restart.

Events older than
[`ALMANAUT_AUTH_AUDIT_RETENTION_DAYS`](configuration.md) (default 90) are pruned
opportunistically on successful logins, which keeps the table bounded without a
dedicated job. Set it to `0` to keep events forever.

---

**See also:** [Configuration](configuration.md) · [JSON API](api.md)
