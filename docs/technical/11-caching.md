# Caching

Intent: **substitute CRDB reads as far as a replica can**, with **minimal thrash**, schema **similar to CRDB**, **no calculations in Mongo**.

## Rules

1. Compute in Go from CRDB-shaped structs. Mongo returns those structs or a finished snapshot.
2. Cache miss or Mongo error → SQL → Put (log Put errors).
3. Mutations always SQL commit **then** cache. Never cache-then-SQL.
4. ECB HTTP last; Mongo `ecb:*` first.
5. Do not calculate translations or aggregate postings in Mongo.
6. Do not `ClearLedger` the world on each grocery.

## Thrash model (replace production ClearLedger)

| Event | Mongo actions |
|---|---|
| Post / delete journal | Replace `balances`; Put/Delete `journal:{id}`; **DeleteMany** `kind in (networth, retranslate, periods, view)` |
| Create account / commodity | Replace that catalog snapshot only |
| Successful ECB HTTP | Put `ecb:{ccy}:{date}` only |
| Set / change \(F\) | DeleteMany derived kinds; write-through restamped `journal:{id}` |
| `DELETE /api/v1/cache` | DeleteMany `kind != ecb` (operator escape hatch, copy) |

Derived docs are cheap to drop: next dashboard GET recomputes **in Go** from
warm catalogs, balances, and ECB (often **zero SQL**
if those replicas are warm) and puts `networth:{F}:{day}`.

## Warm `GET /api/v1/view/dashboard` (target)

Hit `view:dashboard:{F}:{day}` or `networth:{F}:{day}` → **zero CRDB**.

Miss derived, hit catalogs+balances+ecb → **zero CRDB**,
compute NW/IE in process, Put derived.

Miss catalogs → SQL once, Put, compute.

Production already hits zero SQL for warm net-worth. Rebuild must keep that.
A change of \(F\) drops derived snapshots because lots/`cost_*` changed;
catalogs and `ecb:*` stay.

## Snapshot `rev` (optional)

`store.config.ledgerRev` integer incremented on every CRDB ledger mutation. Derived docs include `rev`. Hit only if `doc.rev == current`. Prevents a missed invalidate from serving a grocery-less net worth. Catalogs can omit rev if write-through replaced the whole `items` array.

Prefer write-through of `balances` over rev on catalogs.

## Parallelism

Single Cloud Run instance in production today; still make cache puts idempotent. Do not use Mongo for locks.

## Tests

- `memCache` fake (copy `ledger/cache_test.go`): hit, miss, fallback, \(F\)-keyed networth, journal post does not delete `ecb:*` or `commodities`.
- First `PUT` \(F\) does not restamp postings. Confirmed change of \(F\) updates `cost_*` and drops derived cache, not `ecb:*`.
