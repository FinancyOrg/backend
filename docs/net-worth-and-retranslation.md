# Net worth and retranslation

Two pictures of the same household, both in home currency:

- **Net worth (NW)** — what you have minus what you owe, valued **today**.
- **Income minus expenses (IE)** — what came in minus what went out, measured at the **cost of the cash that moved**.

When every account is already in home currency, those two pictures meet. When you still hold foreign cash, they differ by the paper movement on that leftover cash. Financy shows that difference as **retranslation**, and you can record it when you ask.

## Net worth

Net worth adds assets and liabilities (equity is left out, so openings do not look like profit). Each foreign balance is converted at the **closing** ECB rate into home currency. A euro checking account in a euro home contributes its euro balance as-is. An Indian account contributes rupees × today’s rupee-to-home rate.

That is IAS 21 for monetary items: remaining foreign money is shown at the closing rate.

## Income and expenses

Income and expenses use **lot cost** when a line has one. The grocery paid from leftover rupees is the euro cost of the rupees that left, not “₹10,500 at today’s rate.” Same-currency salary in home currency is already home currency.

This is how Financy keeps spending tied to the cash that paid for it. See [Foreign cash and lots](foreign-cash-and-lots.md).

## The identity

```
retranslation  =  income − expenses  −  net worth
```

In the handbook we write this as **IE − NW**.

- If IE is higher than NW, leftover foreign cash is worth less today than it cost. That is an unrealised loss.
- If NW is higher than IE, leftover foreign cash is worth more today than it cost. That is an unrealised gain.

A compact way to see the same gap:

```
IE − NW  =  remaining foreign lot cost  −  remaining foreign cash at today’s rate
```

When remaining foreign cash is zero, remaining lot cost is zero, and the preview is zero. Same-currency life in the home currency, on the transaction date, also gives a preview of zero.

Opening balances are equity, so they sit outside net worth. A brand-new ledger with only openings shows IE − NW equal to the negative of that net worth until income appears. That is the books reconciling, and it settles as you record earnings.

## Why the two pictures differ

P&L keeps historical cost (the lot, or the last restatement). The balance sheet uses today’s close on what is still in the bank. Spending *realises* a slice of that history into the grocery. What is still in the bank keeps its paper gain or loss until you record it — or until rates move again.

Transfers that empty a foreign account into a home-currency asset realise the difference on **that journal**, as transaction cost. Covering an overdraft does the same. Those are part of the original transaction. Retranslation is reserved for leftover cash that is still sitting there.

## Looking and recording, on demand

You can **look** at any time. The retranslation screen (and the matching API) returns the pending amount in home currency, together with IE and NW, as of today.

You **record** it with an explicit action. Financy posts:

- `Expenses:Forex Retranslation` for the pending amount in home currency
- `Equity:Retranslation` on the other side

Equity is the offset so the amount enters the P&L without washing itself out of IE. This is an exchange difference (IAS 21), not a capital gain.

You may book the full pending amount, or part of it, and you may backdate within today. A zero preview posts nothing. After a full booking, the preview stays at zero until rates move.

Changing home currency does not post this journal for you. After a restatement, look again in the new currency and record when you are ready. See [Changing home currency](changing-home-currency.md).

## The grocery, continued

Home currency euros. €200 bought ₹21,000. You spent ₹10,500 (cost €100). ₹10,500 remains.

If today’s close makes the remainder worth €87.50, then:

- IE includes a €100 grocery (and the €200 that left checking, and so on)
- NW values the remaining rupees at €87.50
- Retranslation preview is the €12.50 paper movement on what is still in India

The grocery itself is left at €100. Opening the dashboard does not reprice last month’s food. Only remaining cash is remeasured at the close.
