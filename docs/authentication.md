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
default), then open **Manage access** on an account to change its role, reset
its password, or delete it. Role descriptions sit beside the create form. The last
remaining user cannot be deleted, and the last admin cannot be demoted or
deleted, so you can't lock yourself out of user management.
Each user changes their own password at `/account/password`.

A password change ends the user's other sessions, so a session stolen before
the change does not survive it; the session you changed it from stays signed
in. An admin resetting someone's password signs that user out everywhere.
Both actions invalidate pending second-factor challenges, so a login that
verified the previous password must start again with the new password.
[API tokens](api.md#api-tokens) are separate credentials and are not revoked
by a password change — delete them at `/account/tokens` if they may be
compromised too.

## Sessions & login throttling

Password verification, password changes, account creation, and SSO provisioning
share a budget of four concurrent password operations per server. Extra work is
rejected immediately with HTTP `503` and `Retry-After: 1`, without recording a
failed password attempt. This bounds CPU work even when requests use different
usernames; the username-based failure limit below still applies independently.

Sessions are server-side (stored in SQLite), cookie-based, and last 30 days;
use the **Logout** button to end one early. Failed logins are throttled
per-username: after 5 consecutive failures, further attempts for that username
are refused for 15 minutes (state is in-memory and resets on restart).

UI mutations recheck the authenticated session and current role after reading
the form or upload. Revocation, expiry or demotion during that read therefore
blocks the action. SSO requests refresh the same local identity without
recreating an account deleted during the read. This is a check before the
handler runs, not an atomic permission check through its eventual commit.

UI forms require the CSRF token in the submitted body, matching the CSRF cookie,
alongside the browser origin checks. A query-string token cannot supply a missing
form token or override one in a multipart upload. This applies to scripted UI
clients too; send the hidden `csrf_token` field in the body.

Changing your password has a separate per-user limit: after five incorrect
current-password attempts, further attempts return HTTP `429` with `Retry-After`
for 15 minutes. The limit is shared across that user's sessions, and signing in
again does not reset it. A successful password change clears its failure count;
server restarts clear this in-memory limit. Password verification consistently
rejects inputs longer than bcrypt's 72-byte limit instead of ignoring a suffix.

[`/api/*`](api.md) returns a plain `401` JSON error when called without valid
credentials (a session cookie or a bearer token).

[`/healthz` and `/version`](monitoring.md#health--version) bypass the login so
container health probes keep working, and the login pages themselves plus the
stylesheet and enhancement script they need (`/static/app.css` and
`/static/app.js`) are public by necessity. Nothing that
reads or writes inventory is: every other route requires a session or a token.

## Lockout recovery

Locked out? Set `ALMANAUT_RESET_ADMIN=true` and restart — almanaut resets the
admin's password (using `ALMANAUT_AUTH_PASS` if set, otherwise a fresh random
one) and logs the new value the same way as the first-run banner. The account
reset is `ALMANAUT_AUTH_USER` (default `admin`) or, if no such user exists, the
oldest admin (the oldest account when no admin is left); it is given the admin
role back if it had lost it. **Unset
`ALMANAUT_RESET_ADMIN` afterwards**, or every restart will reset the password
again.

The recovery reset also revokes the account's sessions and pending second-factor
challenges in the same transaction as the password and role changes. Enrolled
2FA and API tokens remain unchanged; this option recovers a password, not a lost
authenticator.

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

Each TOTP code is accepted once. A code stays valid for about 90 seconds to
tolerate clock skew, but once it has been used — to log in, confirm enrollment
or turn 2FA off — it is refused for the rest of that window, so a code seen
over someone's shoulder cannot be replayed.

Turning 2FA off requires a current TOTP code or a recovery code, not just your
session. That is deliberate: a hijacked session should not be able to quietly
remove the second factor it is supposed to be protected by.

Enabling, disabling, or resetting a second factor revokes pending login
challenges and the user's other sessions. The session performing its own
factor change remains signed in. The factor change and these revocations
commit together; a storage failure rolls them all back. API tokens remain
independent of the second-factor setting.

After five failed removal attempts, further attempts are refused for 15 minutes.
This per-user limit is separate from login throttling and resets on server
restart. If saving the removal fails, the submitted one-time credential remains
usable for a retry.

With 2FA on, logging in becomes two steps — the password form, then a challenge
at `/login/2fa` reached through a short-lived cookie that is only valid between
the two. A correct password alone creates no session.
Logging out revokes both the current browser's session and its unfinished
second-factor challenge, including a login started in another tab. Both cookies
are cleared after the revocation commits. Other browsers' sessions and challenges
remain valid; a storage failure leaves this browser's credentials intact for a
retry and does not record a successful logout.

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

With proxy authentication configured, the login page displays **Sign in with
SSO** alongside password login. The button opens `/login/sso` and returns to the
originally requested page after the trusted proxy supplies a recognized identity.
Configure the proxy to require authentication for `/login/sso` and the application
routes; if you exempt `/login` for local password access, do not extend that
exception to `/login/sso`. The proxy handles the identity-provider redirect and
sign-in flow. Almanaut does not initiate an OIDC or SAML flow itself.

If the request reaches Almanaut without an accepted proxy identity, it shows a
login error with SSO retry and password sign-in available. SSO continues to use
the proxy identity on subsequent requests rather than creating a local session.

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
| `role_changed` | An admin changed a user's role; details identify the previous/new roles and acting user ID |
| `token_created` / `token_revoked` | A user created or actually removed an API token |
| `token_rotation_started` / `token_rotation_completed` | A replacement token was prepared or its original was retired after successful use |
| `session_revoked` | Sessions were invalidated (a user deleted, their password changed or reset, or their 2FA reset) |

Role and token lifecycle events describe successful mutations only. Repeated
requests that make no change, rejected requests, and rolled-back transactions
do not add success events. Token details use numeric IDs, scope and expiration
metadata; token values, hashes and free-form labels are never included.

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
on the first request after startup and at most once per hour afterward, without
a dedicated job. This also covers SSO, API, 2FA and health-check requests; it
does not require a successful password login. Set it to `0` to keep events forever.

Login identities are limited to 256 bytes, passwords to bcrypt's 72-byte limit,
and login request bodies to 8 KiB. Oversized login fields are rejected before
password verification or audit insertion. Audit identity, source and detail
fields also have byte limits to prevent an oversized event from inflating the
database.

---

**See also:** [Configuration](configuration.md) · [JSON API](api.md)
