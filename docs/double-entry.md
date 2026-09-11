# Double-entry

Double-entry is the oldest idea in bookkeeping, and it is the one Financy is built on. Every time money moves, you write **two sides** of the same story.

## The everyday picture

You buy groceries for €42 from a checking account.

- The checking account goes down by €42.
- The groceries expense goes up by €42.

The money left one place and arrived in another. The books record both. If you only wrote “checking −€42”, you would know something left, and you would not know where it went. If you only wrote “groceries +€42”, you would know you spent, and you would not know which pocket it came from.

That pairing is double-entry. Accountants call one side a *debit* and the other a *credit*. You do not need those words to use Financy. What matters is: **the two sides add up to zero**.

## A journal is a balanced story

A **journal** is one event: payday, a transfer, a card payment. Inside it are **postings** — the individual lines.

A salary of €3,000 looks like:

| Account | Amount |
|---|---|
| Checking (asset) | +€3,000 |
| Salary (income) | −€3,000 |

A transfer of €200 from a German account to an Indian account, arriving as ₹21,000, looks like:

| Account | Amount |
|---|---|
| Indian savings (asset) | +₹21,000 |
| German checking (asset) | −€200 |

Those two lines are in different currencies. Financy still requires the journal to balance. It does that with an explicit **price** on the journal: here, 105 rupees per euro. With that price, both sides weigh the same. See [Foreign cash and lots](foreign-cash-and-lots.md).

If a bank keeps a little of the money as a fee, that fee is its own posting — an ordinary expense in the currency the bank actually charged.

## Why this helps a household

- You can always answer “where did this money come from?” and “where did it go?”
- Account balances are the running total of their postings. The checking balance is the sum of every line on checking.
- The whole ledger stays consistent. Income, expenses, assets, liabilities, and equity fit together.

Financy will not save a journal that does not balance. That is a kindness: it is easier to fix a draft than to hunt a hole months later.

## Weights

Behind the scenes, each posting has a **weight** in a settlement currency so mixed-currency journals can still sum to zero. When both sides are already in the same currency, the weight is simply the amount. When currencies differ, the price on the journal produces an exact weight.

Amounts are stored as whole minor units (cents for euros, yen with no fraction). Prices are stored as exact fractions. The app insists that journal weights multiply with no leftover remainder, so the books stay precise.

## What you will see in the app

Day to day you record a **transaction**: payee, amount, accounts, date. Financy turns that into a balanced journal. You can also post a journal directly if you want every line visible.

Either way, the rule is the same. Two sides, same story, books in balance. [Home currency](home-currency.md) is how those stories are measured together when accounts speak different currencies.
