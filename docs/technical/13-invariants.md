# Invariants and tests

Port production tests under `internal/domain` and `internal/ledger`. Add the
cases below. A rebuild that fails these is wrong even if the UI looks fine.

## Always

1. Posted journal: weights sum to 0 per weight commodity.
2. `account_balances` equals sum of posting units (trigger).
3. No journal with civil date after ledger-TZ today. Effective timestamps
   on the row are always UTC instants.
4. Currency cross-posts require an ECB reference or an explicitly recorded
   actual price; never use an unlabelled typed FX rate for valuation.
5. `MultiplyUnitsByRational` on stored prices, weights, and attached costs
   has zero remainder.
6. Mongo is unused for arithmetic (assert in review, not runtime).
7. A currency outside `SupportedCurrencyCodes` cannot be created as a
   commodity or used as an account's native currency.

## Currency accounting

8. An INR account remains INR when \(F\) changes to EUR, USD, or JPY.
9. N26→Axis at €1,000 / ₹90,000 preserves both native amounts and the
   explicit transaction price; Axis receives an \(F\) lot equal to the \(F\)
   given minus any ECB residual booked as transaction cost
   (`functionalNetGiven`).
10. Spending Axis to Ma consumes FIFO lots; the expense `cost_*` is the
    carrying amount of the consumed lots. Spend-all remaining foreign cash
    (and draining a same-ccy transfer pocket) → retranslation preview 0
    when no other foreign A/L remains.
10a. Spend of foreign cash onto an \(F\) expense copies FIFO onto that
     line’s `cost_*`; IE uses the lot, not the billed \(F\) amount.
10b. Overdraft (spend with empty lots) stores a negative lot; a later
     covering inflow clears carrying when native returns to 0.
10c. `ReportingMinor` never converts foreign P&L at today’s close.
10d. Foreign cash leaving for an \(F\)-native asset/liability recognizes the
     lot-cost difference in a realized transaction-cost posting and leaves no
     foreign carrying when native balance is 0.
10e. Covering an overdraft recognizes `CoverInflow`'s realized difference in
     the same journal; the original exchange-cost line remains distinct.
10f. A foreign inflow funded by \(F\)-native income/refund uses functional
     P&L less transaction cost as its lot cost.
10g. An exchange between different foreign cash currencies recognizes the
     source-lot versus destination-lot difference in \(F\).
10h. After every journal, \(\mathrm{IE}_F\) equals \(F\)-native A/L plus
     remaining foreign lot carrying; consequently retranslation is remaining
     carrying minus remaining foreign balance at close.
11. A EUR bank fee remains a EUR fee after \(F\) changes. Only `cost_*` may
    be restated at the change-date rate.
12. Deleting/reinserting a journal changes later lot walks; it does not by
    itself restamp unrelated historical `cost_*`.

## Functional currency

13. First `PUT` \(F\) writes CRDB, provisions the fee account, and does not
    update existing posting `cost_*`.
14. `PUT` \(F\) without `confirm` while \(F\) is already set → no mutation.
15. Confirmed EUR→JPY restates every `cost_*` by the **change-date** EURJPY
    ECB cross. Native units, `price_*`, and FIFO consumption order are
    unchanged. A 2021 INR expense is **not** converted at 2021 INRJPY.
15a. Confirmed \(F\) changes seed current-\(F\) `cost_*` for every old-\(F\)-native
     asset, liability, income, and expense posting that was costless while it
     was same-\(F\). Equity and other non-monetary postings remain costless.
     The existing ledger is restamped in place; restore replay is not required.
15b. `GET /api/config` `functionalChangeEstimatedMs` is \(800 + 40 \times\)
     posted journal count. Settings fills the restamp bar to 95% over that
     duration while polling every `functionalChangePollMs` (10 000). A
     confirmed `PUT` returns before restamp commits; `commodityId` stays
     the old \(F\) until the job is `done`. A second job is 409
     `FunctionalCurrencyChangeInProgress`.
16. Net worth translates current native A/L at the closing rate into current
    \(F\).
17. Warm cache: first set of \(F\) does not miss `commodities`, `balances`,
    or `ecb:*`. A confirmed change drops derived snapshots and cached journal
    postings, because the change rewrites existing `cost_*`.

## Retranslation

18. Same-currency salary only, in that same \(F\), on the transaction date →
    pending 0.
19. Confirmed change of \(F\) does not post a retranslation journal.
20. After restamp, preview is computed in the **new** \(F\) only.
21. Booked FX retranslation is immutable after posting; later \(F\) change
    restates its `cost_*` like any other posting if it is foreign to the new
    \(F\) (same-F lines have no cost).

## ECB

22. Weekend uses lookback; cross legs share observed date.
23. Mongo `ecb:{CCY}:{date}` Put is idempotent (same rate). EUR identity is never stored.
24. Cache clear leaves `kind=ecb`.

## Cache

25. Journal post does not `DeleteMany` commodities.
26. Derived snapshots are keyed by current \(F\). After EUR→JPY, `networth:EUR:day` is not a valid JPY hit.
27. Nil Mongo: all tests pass on CRDB only (\(F\) in `app_config` or test override).
28. Timezone lives only in Mongo `store.config`. Changing it rebuckets
    months/years in Go and never updates `journal_entries.effective_date`.
    See [14-time.md](14-time.md).

## Copy these production tests by name

- `TestRetranslation` identity tests (lot cost, not spot on spend;
  spend-all remaining foreign → pending 0)
- `TestRetranslation_ForeignCashToFunctionalAssetRealizesLotDifference`
- `TestRetranslation_ForeignIncomeUsesNetFunctionalGiven`
- `TestRetranslation_ForeignCurrencyTransferRealizesLotDifference`
- `TestRetranslation_OverdraftCoverRealizesCoverDifference`
- `TestExchangeCurrency_TransactionCost`
- `TestExchangeCurrency_WeekendLookback`
- `TestPostTransaction_*`
- `TestLedger_PresentationCurrencyCanChange` → replace with first-set vs
  confirmed-change tests (`TestLedger_FunctionalCurrencyCanChange`)
- Auth session cookie `__session` tests
- Production `domain/inventory` FIFO tests (`TestConsumeLots_FIFO`,
  `TestIncomeExpenseFromPostings_UsesLotCostNotSpot`)

## Implementation order for an agent

1. Domain money/posting/valuation + **inventory lots**.
2. CRDB migrations (`cost_*`, `app_config`).
3. ECB fetch + persist.
4. Journal post/delete + `stampReportingLots` + explicit prices.
5. Net worth + IE from lot cost + retranslation preview/book in \(F\).
6. Confirmed change of \(F\) restamp.
7. Mongo cache write-through.
8. HTTP; port tests; add 13–17, 19–21, 25–28.
