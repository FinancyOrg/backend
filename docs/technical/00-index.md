# Rebuild backend — index

These files specify the Go backend for Financy. Read **in order**.

**Review this revision before implementation:** measurement is IAS 21
**functional currency** \(F\) with FIFO carrying amounts on foreign cash.
Changing \(F\) is prospective (21.35–37): restamp stored `cost_*` at the
change-date rate. Do not replay history. There is no presentation overlay.

| File | Topic |
|---|---|
| [01-principles.md](01-principles.md) | Accounting standards, what we copy, what we change |
| [02-architecture.md](02-architecture.md) | Process, two stores, request flow |
| [03-crdb-schema.md](03-crdb-schema.md) | CockroachDB DDL (ledger of record) |
| [04-mongo-schema.md](04-mongo-schema.md) | MongoDB collections (settings, UI, cache) |
| [05-money-weights.md](05-money-weights.md) | Minors, rationals, journal weights, `cost_*` |
| [06-ecb.md](06-ecb.md) | ECB as the only FX source |
| [07-lots-fifo.md](07-lots-fifo.md) | FIFO functional carrying for foreign cash |
| [08-functional-currency.md](08-functional-currency.md) | \(F\), onboarding, change procedure |
| [09-retranslation.md](09-retranslation.md) | Unrealised FX in \(F\); optional booking |
| [10-journals.md](10-journals.md) | Post, delete, exchange, transaction cost, lots |
| [11-caching.md](11-caching.md) | Cache-aside that substitutes CRDB reads without thrash |
| [12-http-api.md](12-http-api.md) | HTTP surface |
| [13-invariants.md](13-invariants.md) | Properties tests must never break |
| [14-time.md](14-time.md) | UTC journals; Mongo ledger timezone; month/year buckets |
| [15-currency-movement-comparison.md](15-currency-movement-comparison.md) | Production vs previous view-design vs this target |

## Product intent

A single-user, double-entry personal ledger. The owner picks a **functional
currency** \(F\) (home currency). Foreign accounts stay in their native
currency. Spend and remaining cash carry an \(F\) amount via FIFO lots.
Unrealised FX (lots vs close) is the retranslation preview and can be booked
in \(F\).

If the owner later **changes** \(F\) (move to Japan for real), remaining
carrying amounts and stored functional costs convert at **that day’s** ECB
rate. Historical native journals are not rewritten. History is not replayed
as if the new \(F\) had always been in force.

ECB daily spots are **mandatory**. The backend embeds an allowlist of
ECB-supported currency codes; unsupported currencies are rejected.

## Two databases

| Store | Role |
|---|---|
| **CockroachDB** | Ledger of record. Journals, postings (`price_*` and `cost_*`), balances, **`functional_commodity_id`**. |
| **MongoDB** | Theme, timezone, UI flags, **cache**, ECB historical prints (`ecb:{CCY}:{date}`). Never where a number is computed. Never where a journal is born. |

Auth stays as in production: allowlisted Google sign-in, `__session` HMAC cookie.

## Non-goals

- Multi-tenant / per-user ledgers
- A second presentation currency that translates native history at original-date rates without lots
- Replaying FIFO from inception when \(F\) changes
- Securities/inventory cost-flow lots (`kind=security` reserved)
- Calculating inside Mongo aggregations
- Docker (Podman locally; tests on the host)
