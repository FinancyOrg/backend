# Currency lots (FIFO carrying amount in \(F\))

Foreign-currency cash is a monetary item (IAS 21.23). The native balance is
still INR on Axis. The **functional carrying amount** of those rupees lives
on FIFO lots, copied from production `domain/inventory.go` and
`ledger/inventory.go` (`stampReportingLots`, `loadMonetaryLots`,
`ConsumeLots`).

This is not securities inventory. Lots exist so spend and closing
remeasurement have an \(F\) carrying amount to compare to. Do not build a
separate `asset_lots` table for bank cash; reconstruct lots from posting
`cost_*` as production does.

## Cost columns

Restore on `postings` (production names):

- `cost_per_unit_numerator`, `cost_per_unit_denominator`
- `cost_commodity_id` — the **functional** currency at the time of the
  restamp (after a change of \(F\), this is the current \(F\))
- `cost_date`, `cost_label`

`price_*` remains the journal weight/settlement relation. Cost is the IAS 21
carrying amount in \(F\). An identity `price` of `1/1` may sit on a foreign
line so weight stays native while cost is in \(F\) (`AttachReportingCost` /
`EnsureNativeWeight`).

Same-currency \(F\) postings do not need cost.
Every foreign income/expense line does need a saved `cost_*`; reporting must
fail with `MissingFunctionalCost` rather than inventing a close-rate or
posting-date conversion.

## Acquisition

N26 €1,000 → Axis ₹90,000:

```
Assets:Axis       +₹90,000
Assets:N26        -€1,000
price: 90 INR/EUR   (weight)
cost on Axis: €1,000 of F carrying (if F is EUR)
```

If the bank residual vs ECB is booked as transaction cost, Axis’s lot cost
is the \(F\) given **minus** that residual (`functionalNetGiven`). Then
\(\mathrm{EUR}_{\text{given}} = \text{lot} + \text{txn}\), so empty remaining
foreign cash can make \(\mathrm{IE}=\mathrm{NW}\).

`stampReportingLots` on the inflow: add a lot `{ native units, F cost }`,
or cover an overdraft short of the same account first.

The same net-given rule applies when the foreign inflow is funded by an
\(F\)-native income or refund line rather than an \(F\)-native asset. The
functional P&L amount less the transaction-cost line is the foreign lot cost;
the fee is not counted twice.

## Spend (event date)

Spending ₹10,000 on Ma **consumes lots FIFO**. The expense’s functional
amount is the **carrying amount of the consumed lots**, not a fresh close
rate and not a replay into a later \(F\).

```
Expenses:Food     +₹10,000   cost = F carrying of consumed lots
Assets:Axis       -₹10,000
```

That is `ConsumeLots` + `AttachReportingCost` on the expense. The grocery
is measured in \(F\) from the cash that left. Unrealised FX stays on
**remaining** lots until retranslation or a change of \(F\).

Spend of foreign cash onto an \(F\)-native expense (USD card billed in EUR)
still puts the **consumed lot** on that expense’s `cost_*`. IE uses that
cost, not the EUR bill and not the spend-date ECB residual. The residual
posting stays on `Expenses:Transaction Cost` for weights; its `cost_*` is
0 so it does not leak into retranslation.

Spending more native than on-hand records a **negative lot** (overdraft at
spot). A later inflow covers shorts FIFO and drops both costs so native 0
has carrying 0. `loadMonetaryLots` uses the same `ApplyLotDelta`.

Same-currency unequal transfer: the residual booked in \(F\) is the FIFO
slice of the leftover native units, not a second ECB conversion of those
units.

When foreign cash leaves for a functional-currency asset or liability, the
foreign lot cost is realized on that journal. The source price is restated to
the consumed carrying amount and the difference between that amount and the
functional balance-sheet inflow is an additional
`Expenses:Transaction Cost:<F>` posting. Its `cost_*` equals its \(F\) amount.
This preserves journal weights without leaving an FX difference after the
foreign account is empty; it does not use today's close.

When an inflow covers an overdraft, `CoverInflow` returns the realized
difference between the inflow cost allocated to the short and the short's
carrying amount. That difference gets the same kind of transaction-cost
posting. The original ECB/actual exchange-cost posting remains separate, and
the source price is adjusted only to keep the journal weights exact.

An exchange between two different foreign cash currencies also realizes the
difference between the consumed source lot cost and the destination lot cost
in \(F\). Same-currency transfers use the FIFO residual rule above; the
cross-currency case adds a separate realized transaction-cost posting.
These realized postings are appended to the stamped posting slice and are
persisted by `PostJournalEntry`; they are part of the original transaction,
not a later retranslation plug.

Do not select lots using the current UI setting independently of stored
cost. Do not restamp a settled expense when \(F\) changes except for the
**single change-date rate** applied to all stored `cost_*`
([08-functional-currency.md](08-functional-currency.md)).

## Closing rate vs lots

Net worth values native A/L at **closing** ECB into \(F\). Remaining lots
carry historical \(F\) cost. The difference is the retranslation preview
([09-retranslation.md](09-retranslation.md)).

For an exact audit, replay foreign monetary postings in
`effective_date`, `posted_at`, `created_at`, and `line_order` order. Apply the
same rational multiplication as `MultiplyUnitsByRational` when reading each
stored `cost_*`; do not independently integer-divide `units × numerator /
denominator` per row. A zero native balance must leave zero `LotCarrying`,
including after an overdraft is covered.

## Change of \(F\)

IAS 21.35–37: **reset remaining carrying amounts** at the change-date rate.
Do **not** replay every historical consumption in the new \(F\). Restamp
existing `cost_*` once from old \(F\) to new \(F\) at the single change-date
ECB cross. For old-\(F\)-native asset, liability, income, and expense
postings that had no cost because they were same-\(F\) lines, seed `cost_*`
from their native units at that same cross. Native units, prices, weights,
journal count, and FIFO order are unchanged. Equity and other non-monetary
postings are not seeded.

See [08-functional-currency.md](08-functional-currency.md) for the procedure.

## Delete / backdated journals

Deleting or reinserting a currency journal changes native balances **and**
the lot queue for later postings. Production already rebuilds lots from
history on each stamp (`loadMonetaryLots` before `effective_date`). Keep
that. Invalidate derived views. A backdated insert in the middle of the
queue affects subsequent lot consumption the next time those later journals
are restamped — do not silently rewrite later `cost_*` unless we add an
explicit “rebuild lots from date” command. Default: new posts stamp from
current history; historical rows keep the cost they were posted with until a
functional-currency change restates all `cost_*` at one rate.

## Securities later

FIFO / specific ID / weighted average for `kind=security` remains a future
module with a separate `asset_lots` table. It must not share the cash-lot
walk. Bank accounts stay on the posting `cost_*` reconstruction above.
