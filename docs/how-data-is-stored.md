# How data is stored

Financy uses two databases on purpose. **CockroachDB** is the official books. **MongoDB** holds preferences and a cache so everyday screens can answer quickly. Arithmetic always happens in the Go service. MongoDB stores copies and finished pictures; it does not add up the ledger.

## The two stores

| Store | What it is for |
|---|---|
| **CockroachDB** | The ledger of record. Journals, postings, account balances, the home-currency choice. If it is written here, it happened. |
| **MongoDB** | Theme, timezone, hidden/liquid account flags, cached copies of catalogs and balances, stored ECB prints, and precomputed dashboard snapshots. |

Think of CockroachDB as the bound paper ledgers in a cupboard, and MongoDB as the sticky notes and photocopies on the desk. If a photocopy disagrees with the cupboard, the cupboard wins. The next read repairs the copy.

## What lives in CockroachDB

- **Commodities** — currencies you have added (EUR, INR, JPY, …).
- **Accounts** — checking, expenses, opening equity, and the rest, each with one native currency.
- **Journal entries and postings** — the balanced stories, including prices, weights, and home-currency costs on foreign cash.
- **Account balances** — running totals, kept in step by the database whenever a posting is inserted or removed.
- **App config** — including the current home currency.
- **Audit events** — a trail of posts, currency changes, and similar facts.

Amounts are integers in minor units (cents, or whole yen). Prices and costs are exact fractions stored as text. Timestamps are UTC instants.

## What lives in MongoDB

Three collections matter:

**`store`** — your preferences. Theme, timezone, a mirror of home currency for the welcome screen, and (while a restatement is running) a small job status. Timezone lives here on purpose: it is a calendar setting, not a rewrite of the journals. Wiping the cache never touches this document.

**`account_flags`** — whether an account is hidden or treated as liquid in the UI. Missing means visible and not marked liquid.

**`cache`** — copies and snapshots.

| Kind of cache document | Role |
|---|---|
| Catalogs (`commodities`, `accounts`, `balances`) | A replica of the matching CockroachDB table, replaced as a whole when that table changes |
| Journals (`journal:{id}`) | A copy of one posted journal, written after a successful post, removed on delete |
| ECB prints (`ecb:USD:2026-09-04`) | The official rate for that currency on that day, stored after the first fetch. Historical prints stay. |
| Derived snapshots (`networth:…`, `retranslate:…`, `periods:…`, `view:…`) | Finished pictures already computed in Go, keyed by home currency and day (and timezone / ledger revision where that matters) |

## How the cache cuts database load

The rule is **cache-aside**:

1. A read tries MongoDB first.
2. On a hit, CockroachDB is not consulted.
3. On a miss (or if MongoDB is briefly unavailable), Financy reads CockroachDB, computes in Go if needed, and writes the copy back.

Writes always commit in CockroachDB **first**, then update the cache. The cache is never treated as more recent than the ledger.

A warm dashboard is the goal. If today’s net-worth snapshot is present, the screen can load with **no SQL**. If the snapshot is absent but catalogs, balances, and ECB prints are warm, Financy computes net worth and income in process — still no SQL — and stores the snapshot for next time. SQL runs when a replica is cold, then that replica is filled.

## Keeping the cache tidy

Each kind of change updates only what changed:

| You do this | Cache does this |
|---|---|
| Post or delete a journal | Replace the balances replica; write or delete that journal copy; drop derived snapshots (net worth, retranslation, months, dashboard) |
| Create an account or currency | Replace that catalog replica only |
| Fetch an ECB rate | Store that one print |
| Change home currency | Drop derived snapshots; refresh restated journal copies. ECB prints stay. |
| Clear cache (operator action) | Drop CRDB-backed copies. ECB prints stay. |

Derived snapshots are cheap to drop. The next dashboard rebuilds them from warm catalogs, balances, and rates.

Snapshots are keyed by home currency and civil day. Yesterday’s net worth is not served as today’s. After a change from euros to yen, a leftover euro snapshot is not served as a yen view.

A ledger revision number is mirrored after mutations. Derived documents can carry that revision so a snapshot from before the last grocery is not reused.

## What MongoDB is trusted with, and what it is not

MongoDB is the authority for theme, timezone, and account flags. It is the durable memory of ECB prints. It is a fast replica of catalogs and a shelf of already-computed pictures.

It is not where a journal is born, and it is not where amounts are added up. There is no MongoDB aggregation that translates currencies or sums postings. Go does that from CockroachDB-shaped data, whether that data arrived from SQL or from a replica.

## If one store is quiet

- CockroachDB commit succeeded and a cache write did not: the request still succeeds. The next read misses and repairs the copy.
- A cache read fails: Financy reads CockroachDB and continues.
- An ECB rate is required and neither MongoDB nor the ECB HTTP API can supply it: posting that needs the rate waits for a rate. The books stay consistent.

Tests and some local runs can omit MongoDB. Theme and timezone then use code defaults (`light`, `Europe/Berlin`). Home currency still lives in CockroachDB. The cache becomes a no-op; every read goes to SQL. Production and `make up` run both stores together.

## Why this split fits a household ledger

The ledger is small enough that a full catalog or balance list fits in one MongoDB document. Copying that document is cheaper than repeating SQL on every dashboard open. ECB prints are immutable after publication, so remembering them is both correct and kind to the ECB. Preferences change independently of journals, so they live beside the cache without being swept away when you post groceries.
