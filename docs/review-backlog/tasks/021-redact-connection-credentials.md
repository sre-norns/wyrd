# 021: Keep Connection Credentials Out of dbstore Errors

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P0` |
| Workstream | Queries |
| Depends on | — |
| Likely conflicts | 004 |
| Owner | Claude Code |

## Why This Matters

`dbstore.Config` holds the database password, either in `URL` (`DB_URL`) or
in `Password` (`DB_PASS`). On a malformed configuration, the errors that
`Config.Dialector()` returns contain the password in clear text. So do the
errors that `gorm.Open` returns for the Dialector that `Dialector()` built.
Every downstream service logs these errors at startup and exits, so the
password ends up in the process log and in any log pipeline.

This happened in production. On 2026-10-31 the Exp-Bench API on Cloud Run
logged its Cloud SQL password to Cloud Logging. The password in `DB_URL`
contained a `/` that was not percent-encoded. The database password had to be
rotated.

The trigger is common. `urllib.parse.quote` and similar helpers leave `/`
unencoded by default. A password manager often generates `/`, `#`, `?`, or
`@`.

## Architecture Assessment

`Config.Dialector()` is the only Interface that turns operator input into a
connection. It is the right place to own the trust boundary. Today it passes
raw parser text through. It wraps the `*url.Error` from `dburl.Parse` with
`%v`, and it hands pgx a DSN string that pgx may reject later, quoting the
string in its error. Callers cannot redact reliably. They do not know which
parts of an opaque error string are secret, and the pgx error surfaces from
`gorm.Open` in caller code, outside wyrd.

The deletion test: if `Dialector()` did not exist, each consumer (Exp-Bench
api-server and system-operator, Urth api-server) would have to parse URLs,
assemble DSNs, and redact errors itself, and would get it wrong in the same
way. The Module earns its place only if it guarantees two things. Its errors
never contain credential material. The Dialector it returns never makes the
driver fail on DSN syntax.

## Evidence

Reproduced on `main` at `c4bf9c8` (`xo/dburl v0.40.0`, `pgx/v5 v5.11.0`) with
a throwaway test that calls `Dialector()` and then `gorm.Open`:

| Config | Error text (fake password `Sup3r/Secret` or `Sup3rSecret`) |
|---|---|
| `URL: "postgres://svc:Sup3r/Secret@localhost/db"` | `invalid DB URL: parse "postgres://svc:Sup3r/Secret@localhost/db": invalid port ":Sup3r" after host` |
| `URL: "postgres://svc:Sup3r#Secret@localhost/db"` | `invalid DB URL: parse "postgres://svc:Sup3r": invalid port ":Sup3r" after host` |
| `URL: "postgres://127.0.0.1:5432/db", User: "svc", Password: "Sup3rSecret"` | `gorm.Open`: ``cannot parse `postgres://127.0.0.1:5432/db user=svc password=Sup3rSecret`: failed to parse as URL (unexpected spaces …)`` |

- `pkg/dbstore/config.go:40-42`: `dburl.Parse` delegates to `net/url.Parse`.
  `*url.Error` formats as `parse "<whole input>": <cause>`, and the cause
  quotes input fragments too (`invalid port ":Sup3r"`). Removing only the URL
  still leaks a password prefix.
- `pkg/dbstore/config.go:46-63`: `u.DSN` for Postgres is URL form
  (`postgres://…`). The `user=`, `password=`, `dbname=`, and `port=`
  overrides are space-joined after it. pgx cannot parse a URL followed by
  keyword/value pairs. pgx redacts passwords only in DSNs that it can parse,
  so its error message prints the password. `port=%v` formats the `*int`
  pointer, not the value.
- `pkg/dbstore/config.go:73`: `postgres.Open(dsn)` defers DSN parsing to
  `gorm.Open`, which runs in caller code.
- `pkg/dbstore/config_test.go`: the cases `pgURL+username` and similar assert
  only the Adapter name. No test opens the Dialector, and none checks error
  text for secrets.

Downstream evidence (do not modify as part of this task):

- `../experibench/cmd/api-server/main.go:100-108` and `:199-203`: logs
  `err.Error()` of both `Dialector()` and `gorm.Open` as JSON, then exits.
- `../experibench/cmd/system-operator/main.go:27-29`, `../urth/cmd/api-server/main.go:46-50`:
  the same pattern.
- Both consumers pin `github.com/sre-norns/wyrd v0.7.0`.

## Failure Sequence

1. An operator sets `DB_URL` with a password that has an unencoded `/`, or
   sets `DB_URL` without credentials plus `DB_USER`/`DB_PASS`.
2. `Dialector()` returns the parser error with the raw input, or returns a
   Dialector with a DSN that pgx cannot parse.
3. The service logs the error and exits. The log has the password. The
   operator must rotate the database password and cannot delete the log
   entries.

## Required Outcome

1. **No credential material in errors.** No error returned by `Dialector()`,
   and no error returned by `gorm.Open` on its Dialector due to DSN syntax,
   contains the password or any substring of it of 3 or more characters. This
   holds for passwords from `URL` and from `Password`, for every supported
   dialect. For `URL`, also treat the whole userinfo as secret: the user
   part may be a token.
2. **Errors stay useful and classifiable.** `errors.Is(err, ErrInvalidDBUrl)`
   keeps working for unparseable URLs. The message says what to fix without
   quoting input, for example:
   `invalid DB URL: cannot parse the authority; percent-encode reserved characters (such as / # ? @ :) in the user name and password`.
   When the scheme parses, the message may include the scheme. It may include
   `url.URL.Redacted()` of a successfully parsed URL, with userinfo fully
   removed (see 1). Do not wrap the raw `*url.Error` or a pgx parse error: a
   caller that logs with `%+v` or unwraps with `errors.As` would recover the
   secret.
3. **Overrides produce a valid DSN.** For Postgres, `User`, `Password`,
   `DBName`, and `Port` merge into one DSN that pgx parses. Values with
   spaces, quotes, backslashes, and URL-reserved characters must survive
   exactly. `Port` uses the integer value. `Dialector()` validates the final
   Postgres DSN eagerly (`pgconn.ParseConfig` or `pgx.ParseConfig`), so a
   syntax problem fails in `Dialector()` with a sanitized error, not later in
   `gorm.Open`. Validation must not open a network connection.
4. **No behavior change for valid input.** Every config that connects today
   still connects to the same host, port, database, and user, with the same
   query parameters (`sslmode`, `host=/cloudsql/…` Unix socket, and others).

## Implementation Options and Trade-offs

### Preferred: Dialector owns parsing, assembly, and sanitizing

- Add one unexported helper, for example
  `sanitizeURLError(scheme string, err error) error`. It maps parse failures
  to fixed messages wrapped with `ErrInvalidDBUrl`. It never formats the
  input or the cause. The `dburl.Error` constants (`ErrInvalidDatabaseScheme`
  and others) are fixed strings and safe to include. `*url.Error` and its
  cause are not.
- For Postgres, apply overrides to the parsed `*url.URL` and render it with
  `String()`:
  - `url.UserPassword(user, password)`, where an empty override keeps the URL
    value;
  - the path for `DBName`;
  - `Host` with `Port`.

  `net/url` then encodes every value correctly, and query parameters stay
  untouched. A non-empty `Config.DSN` cannot be appended to a URL. Either
  render keyword/value form when `DSN` is set (quote values per libpq rules:
  `'…'` with `\'` and `\\`), or reject `URL`+`DSN` with a clear error.
  Document the choice. Task 004 owns the final URL/DSN contract.
- Validate the final Postgres DSN with `pgconn.ParseConfig` and sanitize its
  error the same way.
- Leave the MySQL and SQLite paths as they are, except for error sanitizing.
  Overrides being ignored for them is task 004 scope.

Locality: all credential handling lives in `config.go`. Leverage: every
consumer becomes safe by bumping wyrd, with no code change.

### Alternative: Redact strings after formatting

Format the error as today, then replace every occurrence of the password and
the userinfo with `REDACTED`. This is less code, but fragile. The `#` case
truncates the input, so the error holds only a password prefix; the
percent-encoded and decoded forms differ; and fragments cut at any position
must be found. It does not fix the invalid-DSN bug either, so the pgx path
still fails, with a redacted message. Use it only as a defense-in-depth
check in tests, not as the mechanism.

## Implementation Constraints

- Keep the exported API: `Config`, its fields and kong tags, `Dialector()`,
  and the three `Err*` sentinels. This is a patch release (`v0.7.1`).
- Do not log inside wyrd. Return errors only.
- Do not add a dependency. `pgx/v5` is already in the module graph through
  `gorm.io/driver/postgres`. Promote it from `// indirect` if you import it.
- Do not broaden into the dialect, override, or schema contract of task 004.
  If you find more problems there, add them to task 004, not here.

## Suggested Implementation Sequence

1. Add the failing table test from Required Tests.
2. Add the sanitizing helper and route the `dburl.Parse` error through it.
3. Rework Postgres override assembly on `*url.URL`, add eager
   `pgconn.ParseConfig` validation, and fix `Port`.
4. Update `pkg/dbstore/README.md`: how overrides combine with `URL`, that the
   URL needs percent-encoded userinfo, and that errors never contain
   credentials.
5. Tag `v0.7.1`. Add a short "Downstream" note to the completion record: bump
   Exp-Bench and Urth.

## Non-Goals

- The full URL/DSN/override contract for MySQL and SQLite, and the DSN-only
  guard (task 004).
- A `slog.LogValuer` or `String()` on `Config` that redacts `Password` when
  the struct itself is logged. This is worth a separate small task; record it
  as a follow-up.
- Changing how consumers log startup errors.
- Connection-time errors such as authentication failures or unreachable
  hosts. pgx already reports these without the password. Verify this once
  with the live Postgres test, and record the result.

## Acceptance Criteria / Definition of Done

- [x] Required Outcome 1–4 hold.
- [x] The secret-leak table test fails on `c4bf9c8` and passes after the change.
- [x] Existing `TestConfig_Dialector` cases still pass, unchanged.
- [x] `errors.Is(err, ErrInvalidDBUrl)` holds for every unparseable URL case.
- [x] `pkg/dbstore/README.md` documents encoding, overrides, and error redaction.
- [x] Task 004 notes that its connection-contract work must keep the 021 tests.
- [x] No unrelated changes.
- [x] Full validation passes.

## Required Tests

In `pkg/dbstore/config_test.go`, add a table test, for example
`TestConfig_DialectorNeverLeaksCredentials`. For each case, call
`Dialector()`. If that succeeds, call `gorm.Open` with
`DisableAutomaticPing: true`, so that no network access is needed. Then
assert:

- that `err.Error()` and `fmt.Sprintf("%+v", err)` contain neither the secret
  nor any 3-character substring of it;
- that `errors.As(err, new(*url.Error))` is false.

Use a distinctive secret such as `Zq9/Xk#Pw?Lm@Vb%:Ty`. Cases:

- the secret unencoded in a `postgres://` URL password, with each of
  `/ # ? @ :` alone;
- the secret unencoded in a `mysql://` URL and a `sqlite:` URL;
- `URL` without credentials plus `User`/`Password` overrides, where the
  password contains spaces, `'`, `\`, `=`, and URL-reserved characters. Also
  assert success: `pgconn.ParseConfig` on the resulting DSN gives the exact
  user, password, database, host, and port;
- `URL` with `?host=/cloudsql/project:region:instance` plus `Password`: the
  socket host is preserved;
- `Port` override: the port is the integer, not a pointer address;
- an unsupported scheme whose URL holds credentials: the error has no
  credentials.

If `WYRD_TEST_POSTGRES_URL` is set, add one live case. Connect with a wrong
password, and assert that the error has no password.

## Validation

```sh
go test -race -count=1 -run 'TestConfig_' ./pkg/dbstore/
go mod verify
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

Fill this in before marking the task `done`:

- **Implemented:** `Config.Dialector()` replaces every `dburl.Parse` error
  with a fixed message wrapped in `ErrInvalidDBUrl`
  (`sanitizeURLError`). A `dburl.Error` constant is kept, because it is a
  fixed string. A `*url.Error` is never formatted or wrapped. For Postgres,
  `User`, `Password`, `DBName` and `Port` are set on the parsed dburl DSN URL
  (`url.UserPassword`, path, `net.JoinHostPort` with the integer port, or a
  `port=` query option when there is no single URL host). Each override drops
  the query option that would win over it in pgx. Other query options keep
  their encoding. With no override, the dburl DSN is used byte for byte. The
  final DSN is validated eagerly with `pgx.ParseConfig`, the same parse that
  `gorm.Open` runs. It opens no connection. Its error is replaced by a fixed
  message. A Postgres `URL` combined with `DSN` is rejected: it always failed
  in pgx before. Task 004 owns the final contract. MySQL and SQLite are
  unchanged except for error sanitizing. `pgx/v5` was promoted to a direct
  dependency.
- **Tests added/updated:** `pkg/dbstore/config_secrets_test.go`:
  `TestConfig_DialectorNeverLeaksCredentials` (each of `/ # ? @ :` alone, the
  full secret in postgres, postgresql, mysql and sqlite URLs, unsupported and
  unknown schemes with credentials, a bad query option, URL plus DSN, and a URL
  plus `User`/`Password`). It checks `Error()`, `%+v` and `%#v` for any
  3-character substring of the secret and of the user, and checks that neither
  `*url.Error` nor `*pgconn.ParseConfigError` can be unwrapped.
  `TestConfig_DialectorPostgresOverrides` parses the final DSN with
  `pgconn.ParseConfig` and asserts the exact user, password, database, host
  and port for these cases: awkward values (space, `'`, `\`, `=`, `"`,
  `/ # ? @ : % +`), partial overrides, an IPv6 port override, the Cloud SQL
  socket host, preserved query options, and overrides winning over query
  options. `TestConfig_DialectorLiveWrongPassword` runs when
  `WYRD_TEST_POSTGRES_URL` is set. The existing `TestConfig_Dialector` is
  unchanged. 19 of the new cases failed before the fix, on `main` at `08b816b`
  plus the dependency bump (dburl v0.55.2, pgx v5.12.0).
- **Documentation updated:** `pkg/dbstore/README.md` has a new "Connecting"
  section covering encoding, override precedence, URL plus DSN, and redacted
  errors. The note in task 004 now names both tests and records the
  empty-host socket URL finding.
- **Compatibility/migration:** No exported API change. Valid configs produce
  the same DSN as before when there are no overrides. Postgres overrides used
  to produce a DSN that pgx could not parse, so they now work for the first
  time. Error messages for invalid URLs changed text, and
  `errors.Is(err, ErrInvalidDBUrl)` still holds.
- **Validation evidence:** `go test -race -count=1 -run 'TestConfig_'
  ./pkg/dbstore/`, `go mod verify`, `go test -race -count=1 ./...`,
  `go vet ./...`, `git diff --check`, and `govulncheck ./...` all pass
  locally. The live wrong-password case was skipped locally (no Postgres).
  It runs in CI against `postgres:18`.
- **Downstream:** Tag `v0.7.1`. Bump `github.com/sre-norns/wyrd` in Exp-Bench
  (api-server, system-operator) and Urth (api-server). They need no code
  change.
- **Follow-ups:** a `slog.LogValuer`/`String()` on `Config` that redacts
  `Password` when the struct itself is logged. The empty-host Postgres socket
  URL that dburl rewrites (recorded in task 004).
