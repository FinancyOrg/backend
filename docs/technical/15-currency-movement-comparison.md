# Currency movement: production vs target

How **unrealised FX** is handled in production `financy`, the previous v2
*presentation-currency* design, and this **functional-currency** target
([08-functional-currency.md](08-functional-currency.md)).

The identity is the same:

\[
\text{retranslation} = \mathrm{IE} - \mathrm{NW}
\]

The measurement currency and what happens when that currency changes are
what differ.

| | Production | Previous v2 design (superseded) | Target (this revision) |
|---|---|---|---|
| Measurement currency | Write-once CRDB `default_commodity_id` (de facto \(F\)) | None. Mongo presentation \(P\) is a view. | Changeable CRDB `functional_commodity_id` (\(F\)) |
| Foreign cash | FIFO lots, cost in default CCY | No lots; native only | FIFO lots, cost in \(F\) (copy production) |
| Spend | Consume lots; expense inherits lot cost | Native expense; view at txn-date ECB | Consume lots; expense inherits lot cost |
| Change of reporting CCY | Impossible (`DefaultCurrencyAlreadySet`) | `PUT` \(P\): zero ledger writes; history at original-date crosses into \(P\) | Confirmed `PUT` \(F\): restamp **all `cost_*`** at **change-date** rate. No FIFO replay. Native units unchanged. |
| Retranslation book | `Expenses:Forex Retranslation` vs `Equity:Retranslation` in default CCY | View line; booking discouraged / generic native journal | Same accounts as production, in **current** \(F\). Not auto-posted on \(F\) change. |
| Traveler “JPY for ten days” | No | Yes | No. That would be a future presentation overlay on already-measured \(F\) books. |

## IAS 21

| Behaviour | Standard? |
|---|---|
| Record / carry foreign cash in \(F\); close remaining monetary items at closing rate | Yes (21.21–23, 21.28) |
| Change \(F\) prospectively; change-date rate becomes new historical cost | Yes (21.35–37) |
| Replay every past FIFO consumption in the new \(F\), or show 2021 INR at 2021 INRJPY after an \(F\) switch | No |

## Worked example (Ma grocery)

Salary €1,000, €200 → Axis at 105 INR/EUR (₹21,000), spend ₹10,500, close 120.

Production / target while \(F\)=EUR: Ma expense keeps ~€100 lot cost; retranslation ~€12.50 on remaining ₹10,500.

Previous v2 view in EUR at today’s close for both IE and NW (current code, not the old design doc): Ma also at 120 → residual ~€25.

Target after confirmed EUR→JPY on date \(D\): €100 Ma cost becomes \(100 \times \text{EURJPY}_D\); remaining lot the same way; native rupees unchanged. Not \(10500 \times \text{INRJPY}_{2021}\).

## Current v2 code (until implementation)

Still the presentation path: no lot stamp, `cost_*` columns absent, `PUT`
currency does not restamp, `PostRetranslation` already books the production
accounts in whatever `commodityId` is set. Implementation should restore
production lots, move authority for \(F\) to CRDB, and add the change
procedure — not keep the view overlay.
