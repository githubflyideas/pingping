# HTTP API

pingping's console, its notification-area icon and the Zabbix template talk to
pingping through this API. The same rules hold in fogping and vrf-ping, so a
client written for one reads the others the same way.

## Rules

These are enforced by tests (`api_test.go`): code that breaks one does not pass CI.

1. **Version.** This is API version 1. Every `/api/` response carries the header
   `API-Version: 1`. Within version 1, changes are only additive: new endpoints,
   new optional request fields, new response fields. Removing or renaming a field,
   or changing what it means, needs a new prefix `/api/v2/`, and `/api/` keeps
   answering as before.
2. **Methods.** Every endpoint names its methods. Reads are `GET`, creates
   `POST`, replacements `PUT`, deletions `DELETE`, actions `POST`. Any other
   method gets `405` with an `Allow` header.
3. **JSON in, JSON out.** A request with a body sends
   `Content-Type: application/json` (anything else gets `415`). Every response,
   errors included, is `application/json; charset=utf-8`. Three exceptions, by
   nature: the targets export is a JSON file sent as a download, and the custom
   logo is uploaded and served as the image itself.
4. **One error shape.** Every error is
   `{"error": "<message for a person>", "code": "<code for a program>"}`.
   Programs branch on `code` (or the status); people read `error`. The codes:

   | Status | `code` |
   |---|---|
   | 400 | `bad_request` |
   | 401 | `unauthorized` |
   | 403 | `forbidden` |
   | 404 | `not_found` |
   | 405 | `method_not_allowed` |
   | 409 | `conflict` |
   | 413 | `too_large` |
   | 415 | `unsupported_media_type` |
   | 429 | `rate_limited` (with `Retry-After`) |
   | 500 | `internal` |
   | 503 | `unavailable` |

   An unknown path under `/api/` is a `404` in the same shape.
5. **Names.** JSON keys are `snake_case`, in requests and responses. Times are
   Unix seconds; a duration or size says its unit in its name (`interval_sec`,
   `window_sec`, `retention_days`); a percentage ends in `_pct`. The one
   exception is the compact sample format of `/api/series` (below), which predates
   these rules and is kept for compatibility.
6. **Authentication.** The console signs in with `POST /api/login` and then sends
   the `pingping_session` cookie. An account is an `admin` or a `viewer`; a
   viewer gets `403` on anything that changes the instance. Machine clients use a
   bearer token (`Authorization: Bearer …`) where an endpoint says so. A missing
   or bad credential is `401`. Writes also refuse a cross-origin browser request
   (`403`).
7. **Documented.** Every endpoint is listed below. A test compares this table with
   the routes the code registers, both ways.

## Endpoints

| Method | Path | Auth | What it does |
|---|---|---|---|
| POST | /api/setup | none | First run only: create the admin account. Body `{"user", "pass"}`. `409` once an account exists. |
| POST | /api/login | none | Sign in. Body `{"user", "pass"}`; sets the session cookie. Repeated failures from one address get `429`. |
| POST | /api/logout | none | Sign out: the session ends on the server too. Safe to call without one. |
| POST | /api/password | session | Change your own password. Body `{"old", "new"}`. Ends all your sessions. |
| GET | /api/me | session | Who you are: `user`, `role`, and the console settings you may see. |
| GET | /api/users | session | Accounts and their roles. |
| POST | /api/users | admin | Create an account. Body `{"name", "pass", "role"}`. |
| POST | /api/users/{name}/password | admin | Set another account's password. Body `{"pass"}`. |
| POST | /api/users/{name}/role | admin | Change an account's role. Body `{"role"}`. |
| DELETE | /api/users/{name} | admin | Delete an account (not the last admin). |
| POST | /api/settings/port | admin | Console port and bind address for the next start. Body `{"port", "bind"}`. |
| GET | /api/health | loopback | Counts for the notification-area icon. Answers only on this machine and never through a proxy; `404` otherwise. |
| GET | /api/brand/logo | session | The custom logo image. `404` when there is none. |
| POST | /api/brand/logo | admin | Upload a custom logo; the body is the image file. |
| DELETE | /api/brand/logo | admin | Remove the custom logo. |
| GET | /api/i18n | none | Console strings for `?lang=` (or the browser's language): a dictionary keyed by message id, so its keys are data, not field names. |
| GET | /api/version | none / session | `{"version"}` for everyone; with a session also the instance settings. |
| GET | /api/probe-types | session | The probe types this build can run, in display order: `[{"type", "label", "uses_port", "options"}]`. The editor builds its type filter and form from it. |
| GET | /api/targets | session | Every target with its last-hour and last-day summary. |
| POST | /api/targets | admin | Create a target. Unknown fields are refused. |
| PUT | /api/targets/{id} | admin | Replace a target. |
| DELETE | /api/targets/{id} | admin | Delete a target (its history is kept). |
| GET | /api/targets/export | session | The target list as a `netping-targets` file ([format](targets-format.md)). |
| POST | /api/targets/import | admin | Import a `netping-targets` file. `?dry_run=1` reports without applying. |
| GET | /api/series | session | Rounds for one target: `?target=NAME` and either `?minutes=N` or `?from=&to=` (Unix seconds). |
| GET | /api/stats | bearer token | Per-target summary of the last 5 minutes, for Zabbix. Exists only with `--zabbix`. |

"session" means any signed-in account; "admin" means a signed-in admin.

## `/api/series` format

```json
{"tier": "raw", "rounds": [{"t": 1700000000, "s": 20, "r": 19, "ms": [12.1, 12.4], "b": true, "z": 3.8}]}
{"tier": "hourly", "buckets": [{"t": 1700000000, "q": [10.2, 12.0, 14.1, 19.5, 30.0], "loss": 1.5, "n": 60, "bursts": 1}]}
```

Up to a day the answer is `raw`: one entry per round — `t` time (Unix seconds),
`s` packets sent, `r` received, `ms` every reply's round-trip time in
milliseconds, `b` true when the round was flagged as a loss burst, `z` the robust
z-score behind that flag. Longer windows are `hourly`: one bucket per hour —
`q` min/p50/p90/p99/max in ms (absent when every packet was lost), `loss`
percent of packets lost, `n` rounds in the bucket, `bursts` flagged rounds.
