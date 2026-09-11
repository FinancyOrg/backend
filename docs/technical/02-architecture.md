# Architecture

## Process

One Go HTTP service (Cloud Run). Same handler chain as production: security headers → per-IP rate limit → session cookie → mux.

```
HTTP
  → auth.RequireSession
  → httpapi.Server
       → ledger.Service
            mutations  → CRDB (pgx)  then  cache write-through / invalidate
            reads      → cache hit (Mongo)  else  CRDB  then Put cache
            FX         → Mongo `ecb:{CCY}:{date}` then ECB HTTP; Go crosses in memory
                         in-process memo for the life of the instance
            settings   → Mongo `store` (theme, timezone, flags); F in CRDB app_config
```

Empty `MONGO_URI`: Mongo is nil. Theme and timezone fall back to code defaults. Functional currency is still CRDB `app_config` (or an in-memory override in SQL-only tests). Cache is a no-op. Tests and `make backend` stay SQL-only.

## Authority

| Question | Answer |
|---|---|
| Did this grocery happen? | CRDB `journal_entries` + `postings` |
| How many INR on Axis? | CRDB `account_balances` (trigger-maintained) |
| In what currency and at what price was it recorded? | CRDB posting units, `price_*` / `weight_*`, and `cost_*` (functional carrying) |
| What is \(F\)? | CRDB `app_config.functional_commodity_id` |
| What did ECB print on 2021-06-15 for USD? | Mongo `ecb:USD:2021-06-15` (filled once from ECB HTTP; historical prints do not change). Nil Mongo → HTTP again. |
| What figure do we show today? | Computed in Go in **\(F\)**; may be stored as a Mongo snapshot keyed by \(F\) + as-of |
| Dark mode / timezone? | Mongo `store` |
| Hidden / liquid account chips? | Mongo `account_flags` |

If Mongo and CRDB disagree on a **ledger** field, CRDB wins. Repair the cache on the next miss. Never “fix” CRDB from Mongo.

## Calculation boundary

**All arithmetic** (weights, ECB crosses, FIFO lots, net worth, functional
P&L, retranslation, \(F\) restamp, and period rollups) runs in Go in
`internal/domain` and `internal/ledger`.

Mongo may store:

- byte-for-byte replicas of CRDB catalog/balance rows
- ECB daily spots (`ecb:{CCY}:{date}`), which are not CRDB rows
- **already computed** JSON snapshots (`networth:{F}:2026-09-10`, …)

Mongo must not:

- `$sum` postings
- apply FX in an aggregation
- decide a transaction price or fee currency

A snapshot is an output cache, like a rendered image. Recompute from CRDB on miss.

## Package layout (copy + rename)

```
apps/backend/
  cmd/server/main.go
  internal/auth/          # copy
  internal/domain/        # money, posting, valuation, time
  internal/ledger/        # service; native journals, lots, views in F
  internal/store/         # CRDB migrate + pgx
  internal/mongo/         # cache + store settings (today: internal/firestore)
  internal/httpapi/
```

CockroachDB `schema_locked=true` (v26.1+): migrations unlock, DDL, re-lock. Copy `AGENT.md`.

## Time

Decisions and tests: [14-time.md](14-time.md).

- Storage: UTC nanosecond `2006-01-02T15:04:05.000000000Z` (`domain.UTCLayout`) on every journal timestamp. No civil date and no IANA zone on the row.
- Ledger timezone: Mongo `store.config.timezone` (onboarding). Default `Europe/Berlin` only if unset. Not CRDB.
- Months and years: Go buckets UTC instants with that zone (`ViewPeriods` / `viewEntryDate`). Changing TZ rebuckets history without rewriting journals.
- Civil day for ECB “today” and future-journal rejection: the same ledger timezone.
- ECB publication gate: 16:00 `Europe/Berlin` before today’s spot exists. Copy `ledger/ecb.go`. That gate is the ECB calendar, not the user's ledger timezone.

## Identifiers

- Journal / account / posting / audit IDs: UUID strings (copy `newID()`).
- Commodity IDs: ISO 4217 (`EUR`, `JPY`, `INR`). The catalog starts empty. Currencies are created with `POST /api/commodities` when the user picks them (onboarding does this if the code is missing). EUR is not seeded. It is the ECB quote unit in Go (`domain.EcbQuoteCurrency`): identity `1/1`, never an HTTP request, never a Mongo `ecb:*` doc.

## Currency-specific accounts

Accounts have one native commodity. \(F\) is ledger-wide. Copy production
system accounts in **current** \(F\); keep per-native opening equity:

| Code | Type | Native commodity |
|---|---|---|
| `Expenses:Transaction Cost` or `…:{F}` | expense | \(F\) |
| `Expenses:Forex Retranslation` | expense | \(F\) |
| `Equity:Retranslation` | equity | \(F\) |
| `Expenses:Bank Fees:{CCY}` | expense | `{CCY}` when the bank charges that CCY |
| `Income:Capital Gains:{CCY}` | income | `{CCY}` (future securities) |
| `Equity:Opening:{CCY}` | equity | that CCY |

Changing \(F\) provisions new \(F\) fee/retranslation accounts. It does not
rewrite old native fee lines. Retranslation **preview** is derived;
**booking** uses the two accounts above ([09-retranslation.md](09-retranslation.md)).

## Failure policy (copy)

1. CRDB commit OK, Mongo write/invalidate fails → log, return success. Next read misses and repairs.
2. Mongo read fails → log, read CRDB, do not 500.
3. ECB HTTP fails and no Mongo `ecb:*` hit → `MissingEcbRate` / `MissingValuationPrice`.
4. Do not fail closed on cache. Do fail closed on CRDB and on missing ECB when a rate is required to post.
