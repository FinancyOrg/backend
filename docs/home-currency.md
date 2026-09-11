# Home currency

Your **home currency** is the currency you think in. Financy uses it as the measurement currency of the books.

In the international standard IAS 21 this is called the **functional currency**: the currency of the primary economic environment of the person keeping the books. Reports, net worth, and income are all in this currency.

## Native currency stays native

Home currency is ledger-wide. Each *account* still has its own native currency.

- A German checking account remains euros, even if home currency is yen.
- An Indian savings account remains rupees, even if home currency is euros.
- A fee charged in dollars is recorded in dollars.

The bank statement and the books agree on the native amount. Home currency is how those native amounts are *measured together* so a dashboard can show one picture.

## What home currency is for

| Question | Answered in |
|---|---|
| How many rupees are in the Indian account? | Rupees (native) |
| What did those rupees cost me when I bought them? | Home currency (lots) |
| What are they worth today? | Home currency at today’s ECB rate |
| How much did groceries cost me, in the currency I think in? | Home currency (the lot that was spent) |
| What is my net worth? | Home currency |

## First choice

During welcome you set home currency. Financy stores that choice with the ledger and prepares the system accounts that live in it — for example a transaction-cost account for bank fees and FX margins, and the accounts used if you later record retranslation.

Until you choose, reports that need a measurement currency wait. Theme and timezone are separate; they do not choose home currency for you.

## It can change, when you ask

If you move country and your economic life really changes, you can change home currency. That is a confirmed, on-demand restatement. Remaining foreign cash and stored home-currency costs are converted at **that day’s** ECB rate. Native bank amounts stay as they were. History is not rewritten as if the new currency had always been home.

IAS 21 treats this as an infrequent event. Financy asks for confirmation and runs the restatement in the background. See [Changing home currency](changing-home-currency.md).

## Home currency is a fact of the books

The official copy of home currency lives with the ledger in CockroachDB. The settings document in MongoDB may mirror it for the welcome screen. If the two ever differ, the ledger wins.

Theme, timezone, and “hide this account” are preferences. Home currency is not a preference. It is how the books are measured.
