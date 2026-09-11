# Financy handbook

Welcome. This handbook is for people who want to keep personal accounts and are doing it for the first time. You do not need accounting training. Each page starts with the everyday idea, then shows how Financy uses it.

The rules Financy follows come from international accounting standards, especially **IAS 21** (how to measure foreign money) and **IAS 2** (how to cost batches of things you hold). The app applies those measurement rules to a household ledger.

## Start here

1. [Getting started](getting-started.md) — choose a home currency, a timezone, and your first accounts.
2. [Double-entry](double-entry.md) — every movement of money has two sides, so the books always balance.
3. [Home currency](home-currency.md) — the currency you think in. Reports are in this currency.

## Money in more than one currency

4. [Foreign cash and lots](foreign-cash-and-lots.md) — bank accounts stay in their own currency; FIFO remembers what each batch cost at home.
5. [Exchange rates](exchange-rates.md) — daily rates from the European Central Bank, used everywhere valuation is needed.
6. [Changing home currency](changing-home-currency.md) — restatement on demand if your life moves to a new home currency.
7. [Net worth and retranslation](net-worth-and-retranslation.md) — income and net worth compared, and the optional journal that records the difference.

## How the backend is put together

8. [How data is stored](how-data-is-stored.md) — CockroachDB for the official books, MongoDB for preferences and a cache that keeps database load low.
9. [Dates and calendar](dates-and-calendar.md) — journals stored in UTC, months and years in the timezone you pick.
10. [Glossary](glossary.md) — short definitions of the words used in these pages.

## A picture of the whole

```
You record a transaction
        │
        ▼
  The journal balances (double-entry)
        │
        ▼
  Foreign cash is stamped with a home-currency cost (FIFO lots)
        │
        ▼
  Official books are written in CockroachDB
        │
        ▼
  Copies and dashboard numbers are refreshed in MongoDB
        │
        ▼
  Screens show net worth, income, and (when you ask) retranslation
```

A small worked example runs through several pages: a euro salary, some rupees bought for a trip, groceries paid from the Indian account, and later a look at net worth. You can read the pages in order, or jump to the topic you need.
