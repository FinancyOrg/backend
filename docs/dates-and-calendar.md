# Dates and calendar

Financy stores **when something happened** as an exact moment in UTC. It shows **which day, month, and year** that was in the timezone you chose at welcome.

## Journals remember the moment

Every journal timestamp is stored like:

```
2026-06-30T20:00:00.000000000Z
```

That is 20:00 UTC on 30 June, everywhere. The row does not store “evening in Berlin” or “+02:00”. The same grocery is the same instant if you later read the books from another country.

You cannot post a journal dated after **today** in your ledger timezone. “Today” follows the calendar you live on, not UTC midnight.

## Timezone is a calendar setting

The IANA timezone from onboarding (`Europe/Berlin`, `Asia/Kolkata`, …) lives in MongoDB with theme. It is not a column on the journal.

Default if unset: `Europe/Berlin`. Onboarding writes your zone so months do not depend on that default.

Home currency is separate and lives with the ledger. Changing timezone does not change home currency, restamp costs, or post any FX journal.

## Months and years

When you open months or years, Financy:

1. Reads your timezone.
2. Turns each stored UTC instant into a civil date in that zone.
3. Groups by `YYYY-MM` or `YYYY`.
4. Totals income and expense in each bucket using lot cost in home currency.

A journal at `2026-06-30T20:00:00.000000000Z` is 30 June evening in Berlin (still June) and 1 July after midnight in Kolkata (July). Change timezone and the same journals land in different months, with no rewrite of the cupboard.

The periods cache includes the timezone in its key, so a change of zone rebuilds the picture.

## Exchange-rate “today”

Closing net worth and “is today’s ECB print out yet?” also use the ledger timezone for the civil day. The ECB publication clock itself is 16:00 Europe/Berlin, because that is when the ECB series appears. See [Exchange rates](exchange-rates.md).

## Changing timezone

You may:

- write the new zone to settings
- drop cached month/year snapshots
- see historical UTC instants appear under different calendar headings

Journals keep their UTC timestamps. Costs and home currency stay as they are.
