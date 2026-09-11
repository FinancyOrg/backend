# Changing home currency

Most households pick a home currency and keep it. If your economic life moves — you relocate, you start thinking and earning in a new currency — Financy can restate the books **when you ask**.

This follows IAS 21: a change of functional currency is applied **from the change date forward**. The rate on that day becomes the new historical cost. The app does not replay every old purchase as if the new currency had always been home.

## What you confirm

In settings you choose the new currency and confirm. The confirmation is there because this is a ledger event, not a display toggle.

The restatement can take a little while on a long history. Financy starts it in the background and lets settings wait until it is done. A second change cannot start while one is running.

The change date is today in your ledger timezone (or an earlier day you name, still not in the future).

## What happens

1. Remaining foreign-cash lots are still expressed in the *old* home currency.
2. Each of those carrying amounts is converted to the *new* home currency at **one** ECB rate: the change-day rate (with the usual weekend lookback).
3. Stored home-currency costs on past journals are restated at that same single rate. A 2021 grocery keeps its original rupee amount. Its home-currency cost becomes “old cost × that day’s rate,” not “rupees × the 2021 rupee–yen rate.”
4. Lines that were already in the old home currency (a euro salary while home was euros) receive a cost in the new currency at that same rate, so reports can keep using stored costs.
5. The ledger’s home currency pointer moves. Fee and retranslation accounts are prepared in the new currency.
6. An audit record notes from, to, date, and rate.
7. Dashboard snapshots are refreshed on the next read. Official ECB prints are kept.

Native bank amounts, journal counts, and FIFO order stay as they were. The Indian account still holds rupees. Monday’s batch is still spent before Friday’s. Only the home-currency measuring stick is updated.

## What stays for you to do

Retranslation is **not** posted automatically. After the restatement, you can open the preview in the new home currency and record it when you want. See [Net worth and retranslation](net-worth-and-retranslation.md).

Old fee lines stay in the currency they were charged. New residuals use the new home-currency fee account.

## After the change

| Line | In the new home currency |
|---|---|
| Foreign cash on the balance sheet | Native balance at the **closing** ECB rate |
| Past income and expenses that have a stored cost | That stored cost, already restated |
| Net worth | Assets minus liabilities at the closing rate |
| Retranslation preview | Income − net worth |

On the change day itself, remaining foreign lots sit at today’s rate, so paper movement on leftover foreign cash starts near zero. From then on, rates move as they always do.

## How often

The API allows more than one change, each with confirmation. IAS 21 treats the event as infrequent. Use it when home really changes, and the books will follow from that day.
