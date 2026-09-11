# HTTP API

JSON, session cookie `__session`. Copy auth, rate limit, security headers, public vs protected table from `httpapi/server.go`. Amounts as strings.

Reports are in **functional currency \(F\)**. `reportingCommodityId` /
`commodityId` on payloads are \(F\), so the client never guesses.

## Public

| Method | Path |
|---|---|
| GET | `/api/health` |
| GET | `/api/db/firestore` (ping Mongo; name may become `/api/db/mongo`) |
| POST | `/api/auth/google` |
| POST | `/api/auth/dev` (local only) |
| POST | `/api/auth/logout` |

## Config

| Method | Path | Notes |
|---|---|---|
| GET/PUT | `/api/config` | Onboarding: `commodityId` (\(F\), null until chosen), `theme`, `timezone`. Also `postedJournalCount`, `functionalChangeEstimatedMs` (\(800 + 40 \times N\) ms), `functionalChangePollMs` (10 000), and `functionalChange` (async restamp job). PUT writes timezone/theme to Mongo. First \(F\) writes CRDB synchronously. A confirmed change starts the restamp job and returns while \(F\) is still the old commodity. |
| GET/PUT | `/api/config/default-currency` | \(F\). Change requires `{ "commodityId", "confirm": true }` and returns 202 while the restamp job runs ([08-functional-currency.md](08-functional-currency.md)). |
| GET/PUT | `/api/config/theme` | Mongo |
| GET/PUT | `/api/config/timezone` | Mongo. Civil calendar only ([14-time.md](14-time.md)). |

`GET /api/config` returns 200 when \(F\) is unset (`commodityId: null`).
The welcome screen uses that. `GET /api/config/default-currency` returns 409
`FunctionalCurrencyNotSet` (keep mapping the old `PresentationCurrencyNotSet`
/ `DefaultCurrencyNotSet` codes if clients still send them).

## Catalog and ledger (copy)

`/api/commodities`, `/api/accounts`, `/api/accounts/{id}`, balances,
journal-entries, `/api/prices`, `/api/net-worth`, `/api/v1/transaction`,
`/api/v1/fx/rate`, `GET`/`POST /api/v1/retranslate`, `DELETE /api/v1/cache`.

`GET /api/prices` computes ECB crosses for catalog currencies against \(F\).
`PUT /api/prices/{commodity}/{quote}` returns `InvalidPrice`.

`POST /api/v1/retranslate` books pending unrealised FX in **current \(F\)**
into `Expenses:Forex Retranslation` / `Equity:Retranslation`. A change of
\(F\) never calls it automatically.

Net-worth and retranslation minors are in **\(F\)**.

Keep existing JSON names:

- `commodityId` — functional currency in config;
- `reportingCommodityId` — \(F\) on view responses;
- `retranslationMinor` — pending IE−NW in \(F\).

`GET /api/v1/fx/rate?from&to&date` — ECB cross, copy.

Transaction write requests may include an explicit fee/margin currency. If
omitted, use \(F\) for the residual account. Native fee units do not change
when \(F\) later changes; only `cost_*` restates.

`POST /api/commodities` and `POST /api/accounts` return
`UnsupportedCurrency` when a code is outside the ECB allowlist.
`GET /api/commodities` is empty until a currency is created. EUR is not seeded.

## Views (`/api/v1/view/*`)

dashboard, accounts, transactions, months, years, retranslation — all in
\(F\). P&L uses lot cost; A/L native balances also shown as
`nativeMinor` + `nativeCommodityId`.

## Errors (copy domain codes)

`Unauthorized`, `FunctionalCurrencyNotSet`, `FunctionalCurrencyChangeNotConfirmed`,
`FunctionalCurrencyChangeInProgress`,
`UnsupportedCurrency`, `MissingEcbRate`, `InvalidPrice`,
`MissingValuationPrice`, `UnbalancedJournalEntry`, `InvalidAmount`,
`CurrencyMismatch`, `InvalidTransaction`, `AccountNotFound`, `RateLimited`.

Map legacy `PresentationCurrencyNotSet` / `DefaultCurrencyNotSet` to the
same HTTP status as `FunctionalCurrencyNotSet`.

## Compatibility

`/api/config/default-currency` is retained. Confirmed change of \(F\)
starts the async restamp (202) and does not wait for `cost_*` to finish.
