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

`/api/*` returns a plain `401` JSON error when called without valid
credentials (a session cookie or a bearer token). `/healthz` and `/version`
are the only endpoints that bypass the login, so container health probes keep
working.

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
