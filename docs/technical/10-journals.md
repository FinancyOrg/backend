# Journals, exchanges, transactions

Copy `PostJournalEntry`, `DeleteJournalEntry`, `PostTransaction`,
`BuildExchangePostings`, and **`stampReportingLots`** from production
`financy`. Restore cash lots ([07-lots-fifo.md](07-lots-fifo.md)).

## Post

1. Validate that every posting commodity matches its account's native commodity.
2. Canonical UTC effective instant (`domain.CanonicalUTC`). Reject a civil
   date after ledger-TZ today. Do not store the timezone on the journal.
3. For cross-currency journals, require explicit actual or ECB-reference
   prices and derive exact journal weights.
4. Record separately known bank fees or FX margins in the currency actually
   charged, or the currency explicitly chosen at posting time.
5. `stampReportingLots` — FIFO consume/add in current \(F\); attach `cost_*`.
6. `ResolvePostings` + `ValidateBalance`.
7. Insert `journal_entries` and `postings` in one CRDB transaction. Triggers
   update `account_balances`.
8. Audit `journal_entry.posted` / `transaction.posted`.
9. Cache: write-through balances and `journal:{id}`; delete
   derived snapshots (`networth:*`, `retranslate:*`, `periods:*`, `view:*`).

Same-currency two-sided: debit amount may differ from credit only when an
explicit fee posting explains the difference. That fee stays in the native
currency of the fee account.

Cross-currency: `BuildExchangePostings` records actual amounts and a price
relationship. ECB supplies the reference cross and observed date. If the
actual amount differs, create an explicit FX-margin/transaction-cost posting.
Valuation quotes are computed from ECB in Go; do not persist a pair table.

## Delete

Delete postings then journal. Later lot walks omit the deleted rows
(`loadMonetaryLots`). Do not rewrite remaining historical `cost_*` unless the
user changes \(F\). Write-through balances; delete `journal:{id}`; flush
derived snapshots.

## Patch

Copy current: metadata (description, datetime) only if implemented; amounts
and accounts stay as posted. Changing datetime can change which ECB date a
*new* stamp would have used; do not silently restamp `cost_*` on patch.
Invalidate derived reports.

## Opening balance

Copy: debit asset, credit `Equity:Opening:{native}`. Equity excluded from NW
→ large IE−NW until income appears. That is a reconciliation consequence, not
an FX bug. Opening foreign cash still gets an \(F\) lot at the opening-date
ECB rate.

## Commodities and accounts

`CreateCommodity` for new ISO codes is allowed only when the code is in
`SupportedCurrencyCodes` ([06-ecb.md](06-ecb.md)).

Accounts: one `native_commodity_id`. Posting commodity must match.
Hide/liquid are Mongo `account_flags`.

## Idempotency / concurrency

Single user. Still use CRDB transactions. Posting inserts must commit
atomically.

## Change of \(F\)

Not a silent view switch. Confirmed `PUT` runs the restamp in
[08-functional-currency.md](08-functional-currency.md). Native journals stay.
No exchange “to yen” of Axis rupees. No FIFO replay.
