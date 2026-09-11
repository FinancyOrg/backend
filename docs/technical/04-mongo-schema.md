# MongoDB schema

Two databases of concern, one cluster. Names:

| Collection | Authority | Wiped by cache clear? |
|---|---|---|
| `store` | Yes (settings / UI) | **No** |
| `account_flags` | Yes (hidden/liquid UI flags) | **No** |
| `cache` | No (replica + snapshots) | CRDB-backed kinds **yes**; `ecb` **no** |

Use the Mongo API (`go.mongodb.org/mongo-driver/v2`), not Firestore Native. Collection `cache` is created on first write.

Documents use `_id` string keys. Ledger field names **mirror CRDB columns** (camelCase in JSON, same meaning) so an implementer can map row ↔ doc without a second mental model. Amounts remain **decimal integer strings**.

## `store`

Single config document plus optional extras.

```json
{
  "_id": "config",
  "commodityId": "EUR",
  "theme": "light",
  "timezone": "Europe/Berlin",
  "functionalChangeJob": "{\"status\":\"running\",\"from\":\"EUR\",\"to\":\"INR\"}",
  "ledgerRevision": 42
}
```

| Field | Default if missing |
|---|---|
| `commodityId` | Mirror of CRDB \(F\) for the onboarding blob. **Authority is CRDB** `app_config.functional_commodity_id`. Null until chosen. |
| `theme` | `light` |
| `timezone` | `Europe/Berlin` if unset; **Mongo is the authority**. Civil months/years only. See [14-time.md](14-time.md). |
| `functionalChangeJob` | JSON `{status,from,to,error}` for the async \(F\) restamp. Process memory is the live job; this field lets GET repair an interrupted restamp after a process death. |
| `ledgerRevision` | CRDB revision mirrored after a committed ledger mutation |

`PUT` functional currency:

- Must be an existing `commodities` row with `kind=currency`.
- First set: CRDB `functional_commodity_id`, provision fee accounts, audit `functional.set`.
- Later change: require `confirm`, start an async restamp of `cost_*`
  ([08-functional-currency.md](08-functional-currency.md)). PUT returns
  before the restamp commits.
- Mirror `commodityId` on `store.config` if Mongo is up.
- Invalidate **derived** cache kinds (`networth`, `retranslate`, `periods`, `view:*`). Do **not** invalidate catalogs, ECB, or posting replicas except the postings whose `cost_*` were restamped (write-through those journal docs).

Timezone / theme: Mongo `store.config` is the authority. \(F\) is not.

## `account_flags`

```json
{
  "_id": "<accountUuid>",
  "accountId": "<accountUuid>",
  "hidden": false,
  "liquid": true
}
```

Missing doc → hidden=false, liquid=false. Copy `ledger.AccountFlags`. Not a CRDB column.

## `cache` — replica of CRDB (no math)

Goal: a warm process serves catalogs, balances, and ECB **without SQL**. Shape is a snapshot per table (small personal ledger: full table in one doc), plus optional per-row docs if a table grows.

### Catalog snapshots (write-through)

| `_id` | `kind` | Body | Populate | Invalidate |
|---|---|---|---|---|
| `commodities` | `commodities` | `{ "items": [ { "id","code","name","minorUnits","kind","createdAt" } ] }` | After `CreateCommodity`; also on miss from `SELECT *` | Replace doc, do not delete-all |
| `accounts` | `accounts` | `{ "items": [ CRDB account fields ] }` | After create/patch/delete account | Replace |
| `balances` | `balances` | `{ "items": [ { "accountId","commodityId","minor" } ] }` | After journal commit (re-read balances or apply delta in Go **then** Put) | Replace |

Mirror names: `minor` = `units_minor` string. `items` order does not matter.

**Thrash rule:** do not `DeleteMany({kind: {$ne: "ecb"}})`. That was production `ClearLedger`. Replace **one** snapshot document whose contents changed. Derived snapshots are separate kinds.

### ECB cache

| `_id` | `kind` | Body |
|---|---|---|
| `ecb:USD:2026-09-04` | `ecb` | `{ "currencyId":"USD","observedDate":"2026-09-04","numerator":"…","denominator":"…" }` |

Mongo is the durable ECB cache. Historical prints do not change. Never deleted by ledger invalidation.

On lookback: read Mongo `ecb:{CCY}:{date}` first, then HTTP → Put Mongo. Do not store crosses; Go computes them. EUR identity `1/1` is never a Mongo doc.

Nil Mongo (tests, `make backend` without `MONGO_URI`): HTTP each miss; in-process `rateMemo` still applies for the life of the instance.

### Posting / journal point cache (optional, recommended)

| `_id` | `kind` | Body |
|---|---|---|
| `journal:{id}` | `journal` | Full entry + postings (CRDB column set) |

Write after successful post. Delete on `DeleteJournalEntry`. `GetJournalEntry` tries cache first. **Do not** cache filtered lists (`LIKE`, date range, pagination) — CRDB stays SoR for those.

## `cache` — derived snapshots (computed in Go)

Key **must** include current \(F\) (and civil as-of where the number is a closing-rate photo).

| `_id` | `kind` | When valid |
|---|---|---|
| `networth:{F}:{YYYY-MM-DD}` | `networth` | Today’s closing NW in \(F\) |
| `retranslate:{F}:{YYYY-MM-DD}` | `retranslate` | Today’s IE−NW preview in \(F\) |
| `periods:{F}:{tz}:{ledgerRevision}` | `periods` | Month/year rollups in \(F\) for ledger TZ |
| `view:dashboard:{F}:{YYYY-MM-DD}` | `view` | Optional bundle |

On journal/price/commodity/account mutation: delete derived kinds, **keep** catalog replicas (except rewrite the one that changed).

On \(F\) change: delete derived kinds; restamped journals overwrite `journal:{id}`. Old `{EUR}:…` snapshots must not be served as a JPY view.

`asOfDate` inside the doc must match the key. A hit for a **previous** UTC/ledger civil day is a miss (new closing rate).

## What Mongo is forbidden to store as source of truth

- Journal existence
- Any translated amount that was not produced by Go's view projection

## Indexing

- `cache.kind` (invalidation of derived kinds)
- `account_flags._id` (already PK)
