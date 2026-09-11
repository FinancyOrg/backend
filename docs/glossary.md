# Glossary

**Account.** A named place money lives (a bank) or a category it flows through (groceries, salary). Each account has one native currency.

**Balance.** How much is in an account, in that account’s native currency. Kept in step with postings.

**Carrying amount.** The home-currency cost still attached to foreign cash you hold, from FIFO lots (or from the last restatement).

**Closing rate.** Today’s ECB reference rate, used to value remaining assets and liabilities.

**Commodity.** A unit the ledger knows — here, a currency such as EUR or INR.

**Credit / debit.** The two sides of double-entry. In Financy you can think “this account went up or down” and trust the journal to balance.

**Double-entry.** Every transaction has two sides that sum to zero.

**ECB.** The European Central Bank. Financy’s source of daily foreign-exchange reference rates.

**Equity.** The opening position of the books, and the offset used when you record retranslation. Opening equity is not income and is not part of net worth.

**FIFO.** First in, first out. The oldest batch of foreign cash is spent first.

**Functional currency.** Accounting name for [home currency](home-currency.md). IAS 21’s measurement currency.

**Home currency.** The currency you think in. Reports are measured in it.

**IAS 21.** International Accounting Standard 21, *The Effects of Changes in Foreign Exchange Rates*. Financy follows it for measuring foreign money, using the closing rate on remaining cash, and restating prospectively when home currency changes.

**IAS 2.** International Accounting Standard 2, *Inventories*. Financy uses FIFO from this family of cost-flow ideas for foreign cash batches.

**IE.** Income minus expenses, in home currency, using lot cost when a line has one.

**Journal.** One event in the books (payday, a transfer, a card payment), made of postings.

**Ledger.** The full set of official books.

**Lot.** One batch of foreign cash, with its native units and home-currency cost.

**Minor units.** The smallest stored piece of a currency: cents for euros, whole yen for JPY.

**MongoDB.** Preferences, ECB prints, and the cache. See [How data is stored](how-data-is-stored.md).

**Native currency.** The currency of an account, matching the bank. It does not change when home currency changes.

**Net worth (NW).** Assets minus liabilities, foreign balances at the closing rate, in home currency. Equity excluded.

**Posting.** One line inside a journal.

**Restatement.** Changing home currency on demand: stored costs converted at the change-day rate. Native amounts stay.

**Retranslation.** The difference IE − NW: paper movement on foreign cash you still hold. You can view it anytime and record it when you ask.

**Transaction cost.** A bank fee or FX margin, recorded as an ordinary expense in the currency actually charged.

**UTC.** Coordinated Universal Time. How journal timestamps are stored. See [Dates and calendar](dates-and-calendar.md).

**Weight.** The amount used to prove a journal balances, including when lines are in different currencies.
