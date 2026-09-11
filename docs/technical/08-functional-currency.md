# Functional currency

The ledger has one **functional currency** \(F\): the IAS 21 measurement
currency. The UI reports in \(F\). There is no separate presentation overlay
and no “ten days in Japan, rewrite nothing” view switch.

Native accounts stay native (Axis is always INR). Foreign-currency cash is a
monetary item. Its **carrying amount in \(F\)** is kept on FIFO lots
([07-lots-fifo.md](07-lots-fifo.md)).

## Where \(F\) lives

CRDB `app_config` key `functional_commodity_id` is the authority. It is
**changeable**, not write-once.

Mongo `store.config.commodityId` (onboarding blob) may mirror it for the UI.
A change of \(F\) is a **ledger mutation**: it restamps functional cost on
postings and writes CRDB audit `functional.set`. Theme and timezone stay
Mongo-only ([14-time.md](14-time.md)).

Nil Mongo: in-memory copy of \(F\) for the process; CRDB still holds the
row. Tests may set \(F\) without Mongo.

First onboarding `PUT /api/config` with no prior \(F\) only stores the
choice and provisions `Expenses:Transaction Cost` (and related system
accounts) in that commodity. It does not restamp lots.

## API

`PUT /api/config/default-currency` `{ "commodityId": "JPY", "confirm": true }`

- `commodityId` **is** \(F\) (existing field name, so clients do not churn).
- First set: write CRDB, provision fee accounts, audit `functional.set`,
  200 with `{ "commodityId": "JPY" }`.
- Change when \(F\) already exists: **require** `confirm: true`. Without it,
  return 409 `FunctionalCurrencyChangeNotConfirmed`. With it, **start** the
  change procedure below on a background job and return immediately.
  `commodityId` on that response is still the old \(F\). Settings polls
  `GET /api/config` every `functionalChangePollMs` (10 000) until
  `functionalChange.status` is `done` or `error`. A second confirmed PUT
  while a job is running returns 409 `FunctionalCurrencyChangeInProgress`.
- The Settings UI opens a confirmation dialog before that PUT. The copy
  states that lots are restated into the new \(F\) at today’s ECB rate, that
  the restamp can take several minutes, and that IAS 21 treats a change of
  functional currency as infrequent — not a travel or report-view toggle.
- `GET /api/config/default-currency` / `GET /api/config`: `commodityId` is
  \(F\), or null until chosen (`FunctionalCurrencyNotSet` / 409 on the
  older default-currency GET).
- `GET`/`PUT /api/config` also return `postedJournalCount` (posted
  `journal_entries`), `functionalChangeEstimatedMs` (\(800 + 40 \times N\)
  ms), `functionalChangePollMs` (10 000), and `functionalChange`
  (`running` / `done` / `error`, or null). Settings fills the restamp bar
  to 95% over the estimate while polling; it holds if the job is still
  running, then snaps to 100% when status is `done`. Theme/timezone-only
  saves do not show the bar or start a job.
- Job status is kept in process memory and mirrored on Mongo
  `store.config.functionalChangeJob`. If the process dies mid-restamp, the
  CRDB transaction rolls back and the next GET marks the job `error`
  (`interrupted`).

Welcome screen copy: **functional currency**, not presentation.

## Change procedure (IAS 21.35–37)

Applied **prospectively from the change date**. The change date is ledger-TZ
today (or an explicit `effectiveDate` no later than today).

**Do**

1. Load remaining foreign-cash lots in **old** \(F\) (same FIFO walk as
   `stampReportingLots` / `loadMonetaryLots`).
2. Convert each remaining lot’s carrying amount old \(F\) → new \(F\) at the
   **change-date** ECB cross (lookback as usual).
3. Restate stored `cost_*` on postings from old \(F\) to new \(F\) at that
   **same single rate**. Native `units_*`, `price_*`, and `weight_*` do not
   change. FIFO order is not replayed. Historical expenses keep the same
   native grocery; their functional carrying becomes
   \(\text{old }F\text{ cost} \times \text{rate}_{\text{new}/\text{old}}(\text{change date})\).
   Old-\(F\)-native asset, liability, income, and expense postings that
   legitimately had no cost while \(F\) was old \(F\) are seeded with a
   new-\(F\) cost by converting their native units at that same change-date
   rate. This includes old-\(F\)-native historical P&L and fee lines; equity
   and other non-monetary lines remain costless.
4. Point `functional_commodity_id` at the new commodity. Provision
   `Expenses:Transaction Cost` (and retranslation accounts if used) in the
   new \(F\).
5. Audit `functional.set` `{ "from", "to", "effectiveDate", "rate" }`.
6. Invalidate cached journal snapshots as well as derived cache (`networth`,
   `retranslate`, `periods`, `view`), because restamping changes `cost_*` on
   existing postings. Catalog and ECB replicas stay.

**Do not**

- Replay FIFO from inception in the new \(F\).
- Revalue historical native amounts at their original-date new-\(F\) crosses
  (a 2021 INR grocery is **not** shown as INR × INRJPY\(_{2021}\)).
- Auto-post `POST /api/v1/retranslate`. The user may book the remaining
  \(\mathrm{IE}_F-\mathrm{NW}_F\) plug afterwards ([09-retranslation.md](09-retranslation.md)).
- Change native account currencies, fees already posted, or journal weights.

After a same-day change, remaining lots sit at today’s rate, so unrealised FX
on leftover foreign cash is ~0. History in the UI is old-\(F\) measurements
converted at the switch-day rate, which is what 21.37 means by “those
amounts become the new historical cost.”
The existing ledger is restamped in place; a restore replay is not required
for a normal functional-currency change.

## Reporting after the change

Let \(F\) be the **current** functional currency.

| Line | In \(F\) |
|---|---|
| Foreign cash on the BS | Native balance at **closing** ECB into \(F\), or equivalently remaining lot carrying after period-end remeasurement |
| Historical P&L with stored cost | Stored `cost_*` (already in current \(F\) after restamp) |
| Historical P&L with no cost (same-currency \(F\) lines) | Native minors |
| Net worth | A/L at closing ECB into \(F\) |
| Retranslation preview | \(\mathrm{IE}_F - \mathrm{NW}_F\) |

`IncomeExpenseFromPostings` uses lot / functional cost when present, else a
native amount only when the posting itself is already in \(F\). A foreign
P&L line without saved `cost_*` returns `MissingFunctionalCost`; it is never
converted at today’s close or at its posting date into **current** \(F\).
After a restamp, old-\(F\)-native monetary and P&L postings have cost in
current \(F\), so no second translation and no replay are needed.

## Switching frequency

Changing \(F\) is a real accounting event. The API allows more than one
change but requires confirmation each time. It is not a theme toggle and
must not run on a traveler whim. IAS 21.35–37 apply the change
prospectively; frequent restatements revalue the same lots at a new
change-date rate and are not recommended.

## System accounts

Fee and retranslation accounts are native to \(F\) (or to the actual fee
currency). Changing \(F\) does not rewrite old `Expenses:Transaction Cost:EUR`
lines; new residuals use the new \(F\) fee account.

| Code | Type | Native commodity |
|---|---|---|
| `Expenses:Transaction Cost` or `…:{F}` | expense | \(F\) (copy production single account, or keep the per-CCY suffix already in this repo) |
| `Expenses:Forex Retranslation` | expense | \(F\) |
| `Equity:Retranslation` | equity | \(F\) |
| `Equity:Opening:{CCY}` | equity | that native CCY |

## Tests

- `GET /api/config` `postedJournalCount` matches posted journals;
  `functionalChangeEstimatedMs` is \(800 + 40 \times N\);
  `functionalChangePollMs` is 10 000.
- Confirmed `PUT` while \(F\) is set starts an async restamp and returns
  with `functionalChange.status=running` and the **old** \(F\). A second
  confirmed PUT returns 409 `FunctionalCurrencyChangeInProgress`. After the
  job finishes, `commodityId` is the new \(F\) and status is `done`.
- First `PUT` \(F\) writes CRDB, posts no restamp, provisions fee account.
- `PUT` without `confirm` while \(F\) is set → 409, no posting updates.
- EUR→JPY on a ledger with Axis lots: remaining lot carrying is old EUR
  carrying × EURJPY(change date); native Axis balance unchanged; a 2021 Ma
  expense’s `cost_*` is scaled by the same rate, not by 2021 INRJPY.
- FIFO sequence (which rupees were consumed) is identical before and after.
- Second change JPY→USD applies one USD/JPY rate to the then-current costs.
- Audit `functional.set` is written. Derived cache dropped. `ecb:*` kept.
