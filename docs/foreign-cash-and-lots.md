# Foreign cash and lots

You can hold accounts in several currencies at once. Each account stays in the currency of the bank. **Lots** are how Financy remembers what foreign cash cost in your home currency.

## Multi-currency in practice

Suppose home currency is euros, and you also have an Indian savings account in rupees.

- Salary lands in euros. That is already home currency. No lot is needed.
- You send €200 to India and receive ₹21,000. The Indian account goes up by ₹21,000. Financy also records that this batch of rupees **cost €200** (minus any bank residual you booked as a fee).
- Later you spend ₹10,500 on groceries from that Indian account. The grocery expense is measured as the home-currency cost of the rupees that left — here, half the batch, so €100 — not as “whatever ₹10,500 is worth today.”

Native facts stay native. Axis (or whichever Indian bank) still shows rupees. The euro cost lives alongside, so income and net worth can be expressed in one currency.

## FIFO: oldest batch first

FIFO means **first in, first out**. It is the costing method IAS 2 describes for units that arrived at different costs.

If you buy rupees twice:

1. Monday: €200 → ₹21,000 (105 rupees per euro)
2. Friday: €100 → ₹12,000 (120 rupees per euro)

you now have two batches. Spend ₹10,500 and Financy takes them from Monday’s batch first. The grocery inherits Monday’s euro cost. Friday’s batch is untouched until Monday’s rupees are used up.

That is fair and predictable. Groceries are measured from the cash that actually paid for them.

## What a lot holds

A lot is a small record Financy reconstructs from the journal lines:

- how many native units (rupees, dollars, yen) are still in that batch
- the home-currency carrying amount of that batch
- when it was acquired

There is no separate “lots table” for bank cash. Lots are walked from the postings themselves, in date order. Same-currency accounts (already in home currency) do not need lots.

## Spending, overdrafts, and transfers

- **Spending foreign cash** consumes lots FIFO. The expense receives that carrying amount.
- **Spending more than you hold** records a short (a negative lot) at the day’s rate. A later inflow covers the short, oldest first, so that when the native balance is back to zero the home-currency carrying is zero too.
- **Moving foreign cash into a home-currency asset** (for example paying a euro bill from a dollar balance) realizes the difference between the lot cost and the euro amount on that same journal, as a transaction-cost line. Once the foreign account is empty, nothing is left hanging.
- **Exchanging two foreign currencies** (rupees to dollars, both foreign to a euro home) realizes the difference between the source lot and the destination lot in home currency, on that journal.

Bank fees and FX margins are ordinary expenses in the currency actually charged. They are separate lines, so you can see what the bank kept.

## Remaining cash versus spent cash

Spent cash has already become an expense, at the cost of the lots consumed. Remaining cash still has a historical cost (the lots) and a **today’s value** (native balance at the closing ECB rate). The gap between those two is the paper movement on money you still hold. That gap is the retranslation preview. See [Net worth and retranslation](net-worth-and-retranslation.md).

## A short example

Home currency: euros.

1. Salary €1,000 into a euro checking account.
2. Convert €200 to ₹21,000 at 105 rupees per euro.
3. Spend ₹10,500 on groceries.

The grocery is €100 of home-currency cost (half of the €200 batch). ₹10,500 remains in India, still carrying €100. If the rupee has moved by the time you open the dashboard, net worth values that remainder at today’s rate, while income still holds the €100 grocery. The difference is unrealised exchange movement, waiting for you if you want to record it.
