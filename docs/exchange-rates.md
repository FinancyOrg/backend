# Exchange rates

Financy uses the **European Central Bank** (ECB) as its source of foreign-exchange rates. Those daily reference rates are baked into the backend. Valuation, lot stamping, net worth, retranslation, and a change of home currency all go through them.

## Why the ECB

The ECB publishes a daily spot for each currency in its reference-rate universe, quoted as **units of that currency per one euro**. The series is public, dated, and stable after publication. Using one official source means two people looking at the same books on the same day see the same conversion.

Financy embeds the list of currencies the ECB supports (Australian dollar, sterling, rupee, yen, US dollar, and the rest of that universe, plus the euro itself). You add a currency to your catalog when you need it. The euro is the ECB’s quote unit; a rate of “one euro per euro” is simply 1, with no network request.

## What a rate is used for

| Moment | Which day’s rate | What it measures |
|---|---|---|
| You acquire foreign cash | The transaction day | Home-currency cost of that lot |
| You exchange currencies | The transaction day | ECB reference next to the actual bank amounts |
| Dashboard and net worth | Today | Remaining assets and liabilities at the closing rate |
| You change home currency | The change day | One rate to restate stored costs |

“Today” follows your ledger timezone. The ECB itself publishes on European business days. Financy waits until 16:00 Europe/Berlin before treating *today’s* print as available, which matches when the ECB series is out.

## Weekends and holidays

The ECB does not print a rate on weekends or on TARGET holidays. Financy looks backward up to ten calendar days and uses the most recent published print. A Saturday grocery uses Friday’s rate. Both legs of a cross (for example rupees to yen) share that same observed date, so the two sides of a conversion sit on one photograph of the market.

The journal still shows *your* date. The rate metadata records which ECB day was used.

## Crosses

The ECB quotes everything against the euro. To go from rupees to yen, Financy reads INR per euro and JPY per euro on the same day, then computes yen per rupee from those two fractions. Crosses are calculated in the app. They are not stored as a third rate. That keeps them exact and always in line with the official prints.

## Rates are remembered

The first time Financy needs a given currency on a given day, it asks the ECB and stores the print in MongoDB. Historical prints do not change, so that document can live for the life of the books. The next dashboard, the next lot, and the next restatement reuse it.

The running process also keeps a short memory of rates it has already seen, so a single request that needs several conversions does not repeat work.

If MongoDB is not configured (some tests run that way), Financy still computes correctly; it simply asks the ECB again after a restart. See [How data is stored](how-data-is-stored.md).

## Actual bank amounts

The rate you *got* from the bank is recorded on the journal as the actual price, next to the ECB reference. If the bank’s deal differs from the reference, the difference is an ordinary transaction-cost expense — visible, in the currency that was charged. Valuation of remaining cash still uses the ECB. The two sit comfortably together: the journal tells the story of the deal; the dashboard values what is left at the official close.
