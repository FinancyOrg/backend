# Retranslation (unrealised FX in \(F\))

Retranslation is the IAS 21.28 exchange difference on remaining foreign
monetary items: lot carrying in \(F\) versus closing-rate \(F\).

Copy the identity and the production booking accounts. Do not treat this as
a presentation-currency report that can be recomputed in EUR, USD, and JPY
from the same untouched lots.

## Identity

\[
\text{retranslation}_F = \mathrm{IE}_F - \mathrm{NW}_F
\]

- **NW**: `ComputeNetWorth` — assets + liabilities (equity excluded), native
  balances at **closing** ECB into **current** \(F\).
- **IE**: `IncomeExpenseFromPostings` — income and expense at **functional
  cost** when `cost_*` is present (lot / restated carrying), including
  \(F\)-native P&L that inherited FIFO from a foreign cash leg. Foreign
  lines without cost are an error (`MissingFunctionalCost`), not today’s
  close.

Positive = unrealised loss (IE higher than NW). Negative = unrealised gain.

Same-currency salary in \(F\) on the transaction date → 0.

When remaining foreign A/L native is 0, remaining lot carrying must be 0,
so preview is 0 (no leftover shorts on empty Revolut pockets). Booking
`POST /retranslate` is not how that zero is obtained.

## Why a gap exists

P&L keeps lot cost (acquisition / last restamp). The balance sheet uses
today’s close on the remaining native cash. Spending consumes lots and
realises that slice into the expense. Leftover cash’s close-vs-cost sits
here until booked. Preview is that leftover **iff** IE equals \(F\) net
assets plus remaining lot carrying.

The required audit identity is:

\[
\mathrm{IE}_F = F\text{-native A/L}+\text{remaining foreign lot carrying}
\]

Therefore:

\[
\mathrm{IE}_F-\mathrm{NW}_F
= \text{remaining foreign lot carrying}
- \text{remaining foreign native balance at close}
\]

The first term is the exact FIFO carrying amount, not a per-row integer
approximation of `units × cost numerator / denominator`.

Foreign cash that exits into an \(F\)-native asset or liability is not
remaining cash. Its consumed lot cost and the \(F\) balance-sheet inflow are
recognized on that journal through a realized FX transaction-cost posting.
Covering an overdraft does the same with the `CoverInflow` realized amount.
Those postings keep `IE = F_AL + remaining foreign lot carrying`; they are
not a `POST /api/v1/retranslate` plug and never use today's close.
For a foreign inflow funded by \(F\)-native income or a refund, the lot uses
the functional P&L amount net of its transaction cost.
An exchange between different foreign currencies likewise realizes the
source-lot versus destination-lot difference in \(F\).
All of these adjustments are stamped into the original journal. They must
not be repaired by booking a retranslation journal, which would only add an
equity plug.

Opening equity is excluded from NW, so an opening-only ledger shows
\(\mathrm{IE}-\mathrm{NW} = -\mathrm{NW}\) until income appears. That is
reconciliation, not FX ([10-journals.md](10-journals.md)).

## Preview

`GET /api/v1/view/retranslation` and `GET /api/v1/retranslate`:

```json
{
  "reportingCommodityId": "EUR",
  "retranslationMinor": "…",
  "netWorthMinor": "…",
  "incomeExpenseMinor": "…",
  "asOf": "…"
}
```

All minors are in **current \(F\)**. Cache key
`retranslate:{F}:{civilDay}` plus `ledgerRevision`; reject a stale rev.

A change of \(F\) invalidates these keys. There is no meaningful
`retranslate:JPY:…` snapshot while \(F\) is still EUR: lots are EUR-costed.
Changing \(F\) restamps the existing ledger in place and seeds costs for
old-\(F\)-native monetary/P&L postings that were previously costless. A
restore replay is not part of the normal change procedure.

## Booking

`POST /api/v1/retranslate` is an explicit user action (confirm in the UI).
Copy production:

- `Expenses:Forex Retranslation` debit/credit the pending amount in \(F\)
- `Equity:Retranslation` offset (equity, so it does not wash out of IE)
- Not capital gains
- Optional `amountMinor` (must not exceed pending, same sign) and
  `datetime` (backdated, still ≤ ledger-TZ today)

Zero pending → no journal. After a full book, preview is 0 until rates
move. Changing \(F\) does **not** call this automatically.

## Change of \(F\) vs retranslation

| Event | Lots | Retranslation journal |
|---|---|---|
| Spend / acquire foreign cash | Stamp / consume in current \(F\) | Unchanged; preview moves with close vs lots |
| `POST /retranslate` | Unchanged | Books pending into P&L vs equity in current \(F\) |
| `PUT` \(F\) with confirm | Restate all `cost_*`; seed old-\(F\)-native monetary/P&L lines at the same change-date rate | **Not** auto-posted; preview recomputed in new \(F\) |

## Historical expense

A Ma grocery keeps the functional cost it got from FIFO (then scaled once
per later \(F\) change at that change’s rate). It is not revalued at
today’s INR\(F\) merely because the dashboard opened. Remaining Axis is
valued at close for NW.

`ReportingMinor` uses a saved functional `cost_*` for every foreign P&L line.
If that cost is absent, preview returns `MissingFunctionalCost`; it never
converts the line at today’s close or at an unrelated posting-date rate.
Old-\(F\)-native P&L lines that had no cost while they were same-\(F\) lines
receive a saved current-\(F\) cost during the confirmed currency change, so
the dashboard can report them without a replay or a close-rate fallback.

## Cache

Invalidate cached journal postings plus `retranslate:*`, `networth:*`, and
period/view snapshots on journal post/delete, lot restamp, and \(F\) change.
The journal cache must be dropped on \(F\) change because existing `cost_*`
fields were restamped. Catalogs and `ecb:*` stay.
