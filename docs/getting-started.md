# Getting started

Financy is a ledger for one household. You sign in, choose how you want the books measured, and then record money as it moves.

## Sign in

Access is limited to the Google accounts listed for this installation. After you sign in, a session cookie keeps you signed in while you work.

On a local machine the development login can issue that same session without Google, so you can explore the books on your own computer.

## Choose a home currency

The first real choice is your **home currency** — the currency you think in. If you live in Germany and get paid in euros, euros are a natural home currency. If you live in Japan and think in yen, choose yen.

This is the measurement currency of the books (in accounting language, the *functional currency* under IAS 21). Dashboards, net worth, and income reports are all in this currency.

You pick it once during welcome. You can change it later, with confirmation, if your life actually moves. That change is a real accounting event; see [Changing home currency](changing-home-currency.md).

Currencies Financy accepts are the ones the European Central Bank publishes daily, plus the euro itself. That list is built into the app. See [Exchange rates](exchange-rates.md).

## Choose a timezone

Pick the timezone of your daily life (for example `Europe/Berlin` or `Asia/Kolkata`). Journals are stored as exact moments in UTC. Months, years, and “today” use this timezone so a late-evening purchase lands on the calendar day you experienced.

You can change timezone later. Existing journals stay as they were; only the calendar buckets move. See [Dates and calendar](dates-and-calendar.md).

## Add currencies and accounts

A **currency** in Financy is a named unit of money (EUR, INR, JPY, USD, …). An **account** is a place money lives or a category it flows through.

Every account has one native currency that does not change:

| Kind of account | Everyday meaning | Example |
|---|---|---|
| Asset | Something you have | Checking account, cash, wallet |
| Liability | Something you owe | Credit card, loan |
| Equity | The opening position of the books | Opening balances |
| Income | Money that came in | Salary, refund |
| Expense | Money that went out | Groceries, rent, fees |

A German checking account is native euros. An Indian savings account is native rupees. When you later change home currency, those accounts stay in euros and rupees. Only the *home-currency measurement* of foreign cash is restated.

## Opening balances

When you first connect a bank, you record what is already there: debit the asset, credit an opening-equity account in that same currency (`Equity:Opening:EUR`, `Equity:Opening:INR`, and so on).

Opening equity is the starting point of the books. It is not income, and it is not counted in net worth. After you record openings, the accounts match the banks; as you add income and spending, net worth and income grow together. See [Net worth and retranslation](net-worth-and-retranslation.md).

## What you do day to day

1. Record salary, transfers, card payments, and cash spending as they happen.
2. For a transfer between two currencies, record both bank amounts. The app stores the actual rate you got and compares it with the ECB reference.
3. Open the dashboard when you want a picture in your home currency: balances, net worth, income, and months.
4. When foreign cash you still hold has moved in value, look at the retranslation preview. Record it when you want it in the books.

The next page, [Double-entry](double-entry.md), is the one idea everything else rests on.
