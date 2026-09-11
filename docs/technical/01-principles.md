# Accounting principles

Implement these as law. When a convenience fights a principle, the principle wins.

## Standards this design follows

Personal books are not a listed company, but the **measurement rules** are IAS/IFRS, not a homegrown FX story.

| Topic | Rule | Practical effect |
|---|---|---|
| **IAS 21** functional currency \(F\) | Currency of the primary economic environment; measurement currency of the books. | One \(F\) per ledger (`app_config.functional_commodity_id`). UI reports in \(F\). |
| **IAS 21** presentation currency | Currency in which a particular statement is presented. | Not a separate overlay in this version. Reports use \(F\). A later read-only presentation mode would translate already-measured \(F\) amounts without restamping lots. |
| **IAS 21.23** monetary items | Closing rate on the balance sheet. | Foreign A/L → ECB closing (lookback) into **\(F\)**. |
| **IAS 21.21–22** transactions | Record at the rate on the transaction date. | Journals keep native units. Foreign cash lots store the \(F\) carrying amount from that acquisition (and FIFO consume on spend). |
| **IAS 21.28** exchange differences | Recognised in P&L according to policy. | Remaining lots vs close → retranslation preview; optional book to `Expenses:Forex Retranslation`. Not capital gains. |
| **IAS 21.35–37** change of functional currency | **Prospective** from the change date. Rate on that day becomes the new historical cost. | Restate stored `cost_*` at one change-date ECB cross. Do **not** replay FIFO or revalue native history at original-date new-\(F\) rates. |
| **IAS 2 / cost flow** | FIFO (or other methods) where units have distinct costs. | FIFO for **foreign cash carrying in \(F\)**. Securities lots are a future module. |
| **Double-entry** | Sum of weights per weight-commodity is zero. | Unbalanced journals never commit. |
| **Native currency facts** | A currency account remains denominated in its own currency. | Axis remains INR regardless of \(F\). Fees stay in the currency actually charged. |

Copy the current repo’s explicit policies where they already match this:

- A bank fee or FX margin is an ordinary expense posting in the currency
  actually charged or explicitly selected at posting time. Changing \(F\)
  restates `cost_*` only, never native fee units.
- Bank/ECB residuals are explicit transaction-cost postings.
- A price on a posting is the journal weight/settlement relation. Functional
  carrying is `cost_*`, not `price_*`.
- No future-dated journals (ledger timezone civil date).
- Opening balances against `Equity:Opening:{commodity}`.

## Ledger currency vs native currency

CRDB stores `functional_commodity_id`. It is changeable with confirmation
([08-functional-currency.md](08-functional-currency.md)).

Each account has an immutable `native_commodity_id`. Each posting's units
must match that account. A cross-currency journal uses explicit prices and
weights to relate its legs. Foreign monetary inflows/outflows also receive
`cost_*` in \(F\) via FIFO lots ([07-lots-fifo.md](07-lots-fifo.md)).

Mongo stores theme, timezone, and a mirror of `commodityId` for onboarding.
\(F\) itself is a ledger fact, not a view setting.

## What “no assumptions” means

Do not assume:

- The user has only one foreign cash account.
- \(F\) equals every account’s native currency.
- ECB published on the journal's calendar date (weekends / TARGET — lookback).
- Mongo is available (cache miss → CRDB; settings miss → defaults).
- A transaction fee has to be in \(F\) (it often is; it is not required).

Do assume:

- Exactly one current \(F\) once onboarding has finished.
- Every currency commodity is in the embedded ECB allowlist (or is EUR).
- Cross rates are **only** ECB EUR legs: \(B/A = (B/\text{EUR}) / (A/\text{EUR})\).
- One allowlisted user, one ledger.
- Change of \(F\) is rare and confirmed.

## Exactness vs rounding

Copy `apps/backend/internal/domain/money.go`:

- **Journal weights and explicit prices** must multiply with **zero remainder** (`MultiplyUnitsByRational`). If it cannot, reject the post.
- **Closing-rate conversion and \(F\) restamp** use `ConvertMinorAtRate` + `RoundHalfAwayFromZero` (commercial / Kaufmännisch). Never use that rounding to invent a weight.

## What not to copy

- Write-once `default_commodity_id` that can never change.
- Presentation-only \(P\) that translates native history at original-date
  crosses without lots (the previous v2 view design).
- Replaying all historical FIFO consumptions when \(F\) changes.
- `ClearLedger` wiping the entire cache on every journal (thrash). Replace with per-kind write-through ([11-caching.md](11-caching.md)).
- Putting FX translation, P&L aggregation, or accounting math in Mongo.

## Report reconciliation

In **functional** currency \(F\):

\[
\text{retranslation}_F = \mathrm{IE}_F - \mathrm{NW}_F
\]

- \(\mathrm{NW}_F\): assets − liabilities at **closing** ECB into \(F\) (equity excluded).
- \(\mathrm{IE}_F\): income − expense using **lot / functional cost** when present.

This pending amount may be booked with `POST /api/v1/retranslate` in \(F\).
Changing \(F\) restamps costs; it does not auto-book the plug.
