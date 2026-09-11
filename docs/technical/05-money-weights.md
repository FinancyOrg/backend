# Money, rationals, journal weights

Copy `apps/backend/internal/domain/money.go` and `posting.go` unless a rule below says otherwise.

## Units

Every stored amount is **minor units** of a commodity (`JPY` 0 decimals, `EUR`/`USD`/`INR` 2). JSON APIs expose them as **strings** (`"1746"` not floats).

Signs: debit-positive for assets/expenses as in current posting convention (copy existing tests). Income credits are negative units on the income account.

## Rationals

`Rational { numerator, denominator }` both `*big.Int`, normalised (positive denominator, gcd 1). Stored as TEXT strings.

`NormalizeRational`, `RationalFromPair`, `InvertRational`, `CrossRateBPerA` — copy.

Cross given ECB quotes **per 1 EUR**:

\[
\text{rate}_{B/A} = \frac{B/\text{EUR}}{A/\text{EUR}}
= \frac{\mathrm{num}_B/\mathrm{den}_B}{\mathrm{num}_A/\mathrm{den}_A}
\]

`domain.CrossRateBPerA(rateAPerEur, rateBPerEur)` already does this. EUR leg is `1/1`.

## Two multiply paths

| Function | Remainder | Used for |
|---|---|---|
| `MultiplyUnitsByRational` | **Must be 0** or `InvalidAmount` | Exact journal prices and weights |
| `ConvertMinorAtRate` | `RoundHalfAwayFromZero` | ECB valuation, closing NW, and \(F\) restamp |

`ConvertMinorAtRate` formula (copy):

`toMinor = fromMinor × (num/den) × 10^(toMinorUnits − fromMinorUnits)` then round.

To convert native amount `N` into functional currency \(F\), use:

`ConvertMinorAtRate(N, rateFperN, N.minorUnits, F.minorUnits)`

where `rateFperN = CrossRateBPerA(rateNperEur, rateFperEur)` for the chosen
ECB observation date (transaction date for a new lot, closing date for NW,
change date for an \(F\) restamp).

For summaries, aggregate native minor units (or exact rational totals) before
the final conversion and rounding. Do not round every journal line and then
sum the rounded values; that creates avoidable functional-currency gaps. Detail
screens may show line-level rounding, but their totals need an explicit
rounding adjustment or an exact accumulator.

## Weights (`ComputeWeight`)

Copy order:

1. If `Price` present → weight in `price.commodity` = units × price (exact).
2. Else weight = units in the posting commodity.

When a cross-currency journal needs to balance against a settlement
commodity, attach an explicit `Price` to the relevant line. An identity price
`1/1` is used when cost in \(F\) sits on a native foreign line
(`EnsureNativeWeight`). Do not use cost as a substitute for a missing weight.

`ValidateBalance`: sum of `weight_minor` **per `weight_commodity_id`** is
zero. A mixed-currency journal is valid when every posting has an explicit
price/weight relation that makes the journal balance in its settlement
commodity. A fee can be a separate posting in EUR, INR, USD, or another
actual fee currency.

Minimum two postings.

## Functional cost (`cost_*`)

Foreign-currency monetary postings store `cost_*` in \(F\): the FIFO
carrying amount. Copy production `CostBasis` / `AttachReportingCost`.
`MultiplyUnitsByRational` on `cost_per_unit` must have zero remainder when
the cost is attached at post time.

Change of \(F\) restates those columns at one rate with
`ConvertMinorAtRate` ([08-functional-currency.md](08-functional-currency.md)).
Native units and `price_*` stay put.

Same-currency \(F\) postings omit cost.
