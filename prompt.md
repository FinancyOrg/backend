# Retry Cockroach allowlist injection on `codeProxyRefusedConnection`

Implement runtime recovery for the production 500 on `GET /api/config`. Do not redesign networking, Terraform IP lists, or health checks. Do not open the cluster to `0.0.0.0/0`.

## Production failure (already confirmed)

Cloud Run `backend` in `financy-v1` / `europe-west3` serves the SPA at https://financy-v1.web.app/. Unauthenticated `GET /api/health` is 200. Authenticated `GET /api/config` is 500. App logs:

```
internal error: failed to connect to `user=financy database=defaultdb`:
34.141.92.82:26257 (financy-prod-20657.jxf.gcp-europe-west3.cockroachlabs.cloud):
server error: FATAL: codeProxyRefusedConnection: connection refused (SQLSTATE 08C00)
```

That is the Cockroach Cloud SQL proxy refusing this instance’s current SNAT `/32`. The cluster is up (`CREATED`). The Cloud allowlist only has `cloudrun` + `laptop` `/32`s. Cloud Run SNAT rotates (min instances = 0).

`GET /api/config` is session-gated (`public: false`). Handler:

`internal/httpapi/server.go` `getConfig` → `ledger.Service.AppConfig` → `GetDefaultCommodityID` (`SELECT value FROM app_config …`) → `mapError` logs the pgx error and returns generic `"internal error"` 500.

## Gap in current code

`internal/crdballowlist` already does the right startup sequence in `Run` / `Ensure`:

1. Discover public egress IPv4 (`api.ipify.org`)
2. `AddAllowlistEntry2` named `cloudrun`, SQL only, `/32` (409 Conflict is OK)
3. Poll Cloud API until `propagating == false` and the `/32` is listed
4. Wait until TCP `:26257` accepts
5. Wait until `SELECT now()` succeeds — comments and tests already name `codeProxyRefusedConnection` / `08C00`
6. Prune stale `cloudrun*` entries; never delete `laptop` or `0.0.0.0`

`cmd/server/main.go` calls `crdballowlist.Run` once with a 100s timeout, then `store.Connect` (`pgxpool.New`, no ping), then serves HTTP.

After `ListenAndServe`, nothing re-runs `Ensure`. A live instance whose SNAT changed (or whose startup ping raced ahead of proxy enforcement) keeps handing every CRDB acquire this FATAL. `waitSQL` retries only during startup. Request-time failures become 500s forever on that instance.

Local/dev (`CRDB_API_KEY` and `CRDB_CLUSTER_ID` both unset) must stay a no-op.

## What to build

Retry **allowlist injection** on this **exact** reachability failure, then retry the failed DB operation once recovery succeeds.

### 1. Classify the error

Add `IsProxyRefused(err error) bool` in `internal/crdballowlist`.

True only when the cause is Cockroach proxy deny:

- `errors.As` to `*pgconn.PgError` with `Code == "08C00"`, and/or
- error text contains `codeProxyRefusedConnection`

False for unique violations, `pgx.ErrNoRows`, cancelled context, generic connection refused without that SQLSTATE/code, Mongo errors, domain errors.

Use `errors.As` / `errors.Is` so wrapped pgx errors from `QueryRow.Scan` still match (this is how `isUniqueViolation` works in `internal/ledger/accounts.go`).

Tests: `08C00` PgError, wrapped `fmt.Errorf("%w")`, the log-shaped string `FATAL: codeProxyRefusedConnection: connection refused (SQLSTATE 08C00)`, 23505, `context.Canceled`, nil.

### 2. Keep a recoverable `Ensure` after startup

Today `Run` builds API+config, calls `Ensure`, and drops them. Production needs the same `Ensure` again on `08C00`.

- Export a small `Recoverer` (name as you like) held by `cmd/server` and the DB wrapper: `Recover(ctx) error`.
- `Recover` re-discovers egress IPv4 and calls existing `Ensure` (PUT, wait propagate, wait TCP, wait `SELECT now()`, prune).
- **Singleflight** (or equivalent mutex + in-flight flag): concurrent `/api/config` 500s must share one Cloud API update, not stampede `AddAllowlistEntry2`.
- After a successful recover, call `pgxpool.Pool.Reset()` so idle conns opened under the old deny are not reused.
- If `LoadConfig` is nil (local), `Recover` is a no-op success — tests and `make backend` stay SQL-only.
- Do not cancel Cloud API work just because the HTTP request context died. Use a bounded background/process context (e.g. 100s, same order as startup) for `Ensure`. Retry the **query** with the original request context.
- Log one line on recover start/success/failure, including the `/32`, matching existing `crdb allowlist:` prefix.

Do not duplicate PUT/poll/SQL wait. Reuse `Ensure` / `Options`.

### 3. Intercept at the pool, not in every ledger method

Do **not** sprinkle `if IsProxyRefused` through `internal/ledger`. `*pgxpool.Pool` is used in many places; `QueryRow.Scan` is where connect FATALS surface (`GetDefaultCommodityID` is the `/api/config` path).

Wrap the pool in `internal/store` (or `internal/crdballowlist` if you keep store thin) so `Query`, `QueryRow`, `Exec`, and `Begin` retry **once** after `Recover`:

1. Run the operation.
2. If `!IsProxyRefused(err)`, return it.
3. `Recover(ctx)`. If that fails, return the original SQL error (`fmt.Errorf("%w", orig)` is fine; do not hide `08C00`).
4. `pool.Reset()` then run the operation once more.
5. Do not loop forever. One recover + one retry per call.

`QueryRow` must wrap `Scan`: pgx defers the connect error until `Scan`. A naive `QueryRow` wrapper that only checks the constructor will miss this 500.

Keep `ledger.Service.db` working. Prefer a small interface (`Query` / `QueryRow` / `Exec` / `Begin`) that `*pgxpool.Pool` already satisfies, with the recovering wrapper as the production type. Tests that pass `*pgxpool.Pool` into `ledger.New` should still compile — either keep `New(*pgxpool.Pool)` and wrap inside `store.Connect`, or accept the interface and let `*pgxpool.Pool` remain valid.

`store.RunMigrations` should go through the same recover-on-`08C00` path (startup still needs it if SNAT flips between `Run` and migrate).

### 4. Wire-up

`cmd/server/main.go`:

- `crdballowlist.Run` stays first (fail-fast if injection cannot complete at boot).
- The recoverer used at runtime is the same config/API as `Run`.
- Pass it into `store.Connect` (signature change is OK).
- `GET /api/health` must remain CRDB-free.

No Terraform, Firebase, or frontend changes.

## Tests (required)

Package `internal/crdballowlist` and the wrapper:

- `IsProxyRefused` cases above.
- Fake API (existing `fakeAPI` in `allowlist_test.go`): first `PingSQL` / `Query`/`Scan` returns `08C00`; `Ensure` PUT+wait runs; second attempt succeeds. Assert `AddAllowlistEntry2` ran.
- Concurrent wrappers: N goroutines hit `08C00` together → `Ensure` once (singleflight).
- Non-`08C00` error → zero Cloud API calls.
- Recoverer nil/disabled → `08C00` returned unchanged (local).
- `QueryRow.Scan` path is covered, not only `Query`/`Exec`.
- Existing tests keep passing: `TestEnsureRetriesSQLPingAfterTCP`, prune-keeps-laptop, conflict 409, rate-limit retry.

No live Cockroach Cloud calls in tests.

## Done when

- `go test ./internal/crdballowlist/... ./internal/store/...` pass (and `./internal/ledger/...` if you changed `New`).
- A process that already passed startup `Ensure` can survive a later `08C00` by re-allowlisting the current egress `/32` and completing `GET /api/config` without a container restart.
- Laptop allowlist entries are still never pruned.
- Unauthenticated health stays 200 when CRDB is denied.
