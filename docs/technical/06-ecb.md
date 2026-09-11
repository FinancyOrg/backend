# ECB

Using ECB is **not optional**. Currency valuation, lot stamping, exchange-rate
benchmarking, closing net worth, retranslation, and change-of-\(F\) restamps
all go through the ECB SDMX CSV API.

## Series

- Base: `https://data-api.ecb.europa.eu/service/data/EXR`
- Daily spot: `D.{CCY}.EUR.SP00.A?startPeriod={date}&endPeriod={date}&format=csvdata`
- Quote: **units of CCY per 1 EUR** (`domain.EcbQuoteCurrency`). Copy `FetchEcbRatePerEur`.
- EUR: identity `1/1`, never HTTP, never Mongo `ecb:*` doc. This is the
  ECB series convention. EUR does **not** have to exist as a `commodities`
  row. Create EUR only if the user uses euros (account, posting, or \(F\)).
- Timeout 8s. Empty CSV / non-200 / network → `MissingEcbRate`.

## Supported currency allowlist

The backend embeds a static allowlist of currencies for which the ECB EXR
series is supported. The list is reviewed and updated deliberately when the
ECB reference-rate universe changes; it is not discovered from user input at
runtime.

As of this rebuild specification (2026-09-10), the allowlist is:

```go
var SupportedCurrencyCodes = map[string]struct{}{
	"EUR": {}, // identity; no ECB request
	"AUD": {}, "BRL": {}, "CAD": {}, "CHF": {}, "CNY": {},
	"CZK": {}, "DKK": {}, "GBP": {}, "HKD": {}, "HUF": {},
	"IDR": {}, "ILS": {}, "INR": {}, "ISK": {}, "JPY": {},
	"KRW": {}, "MXN": {}, "MYR": {}, "NOK": {}, "NZD": {},
	"PHP": {}, "PLN": {}, "RON": {}, "SEK": {}, "SGD": {},
	"THB": {}, "TRY": {}, "USD": {}, "ZAR": {},
}
```

Do not add a currency commodity merely because it has an ISO 4217 code. A
currency commodity creation request with a code outside this list returns
`UnsupportedCurrency`. Account creation performs the same check against the
account's native currency, so an unsupported currency cannot enter the
ledger through an existing commodity either.

Copy `ecbSpotAvailable`: do not fetch **today** before 16:00 Europe/Berlin; never fetch future dates.

## Lookback

Copy `FetchEcbRatePerEurWithLookback` (10 calendar days).

- Requested civil date has no print (weekend, TARGET) → try D-1, D-2, … 
- First **successful** observation wins.
- Journal `effective_date` stays the user's date; audit and rate metadata store
  the successful `ecbObservedDate`.
- For a **cross**, both legs use the **same observed date** as the first leg, not independent lookbacks that might land on different publication days (copy `docs/exchange-rate.md`).

Persist every successful observation in Mongo. Lookback must not hammer ECB: check Mongo → HTTP.

## Crosses

No USDJPY series. Always:

1. Observe `INR` per EUR and `JPY` per EUR on one day.
2. `rate_JPY_per_INR = CrossRateBPerA(inrPerEur, jpyPerEur)`.

Functional EUR→JPY is `CrossRateBPerA(eurPerEur=1/1, jpyPerEur)`.

If ECB does not publish a currency (or the code is outside the embedded
allowlist), valuation that needs it fails `UnsupportedCurrency` or
`MissingEcbRate`. Do not invent a valuation rate. Actual bank-statement
amounts may imply an `actual` transaction price, but a user-typed unlabelled
FX rate is not an ECB valuation. Manual prices for future securities are
out of scope; currency valuation is ECB only.

## Who consumes ECB

| Moment | Date used | Result |
|---|---|---|
| Stamp a foreign-cash lot or expense cost | Transaction civil date + lookback | Carrying amount in \(F\) |
| Benchmark an exchange or bank margin | Transaction civil date + lookback | ECB reference price |
| Net worth / dashboard | **Today** ledger-TZ civil date + lookback | Closing rate into **\(F\)** |
| Change of \(F\) restamp | Change-date civil day + lookback | One rate old \(F\) → new \(F\) applied to all `cost_*` |

## Caching

- In-process memo per `(ccy, date)` for the life of the instance (copy `rateMemo`).
- Mongo `ecb:{CCY}:{YYYY-MM-DD}` is the durable cache after first fetch.
- Nil Mongo: HTTP on miss; `rateMemo` still applies.

Do not store crosses in Mongo or CRDB. Recompute in Go (cheap, exact rationals).
`GET /api/prices` returns in-memory crosses against \(F\). `PUT /api/prices` is rejected (`InvalidPrice`): currency quotes are not stored.

## Tests to copy / extend

- Weekend lookback uses Friday’s print.
- Same observed date on both legs of a cross.
- Missing 10-day window → error.
- Today before 16:00 Berlin → missing.
- JPY `minor_units=0` conversion still rounds via `ConvertMinorAtRate`.
- A change of \(F\) restamps `cost_*` and may post no journal besides audit; it does not rewrite native fees.
