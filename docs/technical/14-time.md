# Time: UTC storage, ledger timezone, months and years

This file records the decisions. Implementation lives in `internal/domain/time.go`,
`internal/ledger/settings.go`, and `internal/ledger/views.go` (`ViewPeriods`).

## Decision 1 — Journals store UTC instants only

`journal_entries.effective_date`, `posted_at`, and `created_at` are `TEXT`
values in `domain.UTCLayout`:

```
2006-01-02T15:04:05.000000000Z
```

`PostJournalEntry` / datetime patches run `domain.CanonicalUTC`. The ledger
never stores a civil date, an offset like `+02:00`, or an IANA zone on a
journal row. A posting that happened at 20:00 UTC on 30 June is always
`2026-06-30T20:00:00.000000000Z`, whether the owner is in Berlin or Kolkata.

CRDB indexes on `effective_date` are therefore lexicographic UTC instants,
not calendar months.

## Decision 2 — Ledger timezone is Mongo `store.config.timezone`

The IANA zone chosen at onboarding (and later via `PUT /api/config/timezone`)
is stored on the single Mongo document `store.config`, field `timezone`.
Same document as `theme`. Functional currency \(F\) is **not** this
document; it is CRDB `app_config`.

It is **not** stored in CRDB. Tests fail if a timezone write
lands on the ledger (`TestLedgerTimezonePersistsInSettingsNotCRDB`,
`TestSetAppConfigWritesMongoNotCRDB`).

Default when the field is missing: `Europe/Berlin` (`domain.DefaultLedgerTimezone`).
Onboarding writes the user's zone so later views do not rely on that default.

### Why Mongo, not CRDB

The preference is Mongo. CRDB would not be more efficient for months/years:

| Claim | Reality |
|---|---|
| SQL `GROUP BY` month is faster if TZ is in CRDB | Months/years are **not** grouped in SQL. `ViewPeriods` loads posted journals and buckets in Go with `viewEntryDate(effectiveUTC, loc)`. IANA civil days (`time.Location`) are a Go job, not a CRDB `AT TIME ZONE` on `TEXT` timestamps. |
| One less Mongo round-trip | `store.config` is already read for theme/timezone on the view path. |

Timezone is a view/calendar setting. Changing it must rebucket history
**without rewriting journals**. That is the Mongo settings pattern, not a
ledger mutation. Changing **\(F\)** is the opposite: it is a ledger mutation
([08-functional-currency.md](08-functional-currency.md)).

Nil `SettingsStore` (tests, `make backend` without `MONGO_URI`) uses code
defaults for timezone/theme. \(F\) still comes from CRDB `app_config` (or a
test override).
Production compose always has Mongo.

## Decision 3 — Months and years use the onboarding timezone

`ViewPeriods` (HTTP `/api/v1/view/months` and `/years`):

1. `loc := LedgerLocation(ctx)` → Mongo `timezone`, else `Europe/Berlin`.
2. For each posted journal, `viewEntryDate(entry.EffectiveDate, loc)` converts
   the stored UTC instant into a civil date in that zone.
3. Month key is `YYYY-MM` of that civil date; year key is `YYYY`.
4. P&L in each bucket uses lot / functional cost in \(F\), not today's close.

Example (already a test): `2026-06-30T20:00:00.000000000Z`

| Ledger timezone | Civil date | Month |
|---|---|---|
| `Europe/Berlin` (CEST, UTC+2) | 2026-06-30 22:00 | 2026-06 |
| `Asia/Kolkata` (UTC+5:30) | 2026-07-01 01:30 | 2026-07 |

`PUT` timezone invalidates the periods cache (`CacheKindPeriods`). The next
months/years read recomputes buckets from the same UTC journals. Cache key
includes the timezone (`periods:{F}:{tz}:{ledgerRevision}`).

Future-journal rejection also uses this zone (`RejectFutureIn`), not UTC
midnight.

## Decision 4 — What a timezone change must not do

- Must not `UPDATE journal_entries.effective_date`.
- Must not convert stored instants into local wall time.
- Must not post a retranslation or FX journal.
- Must not persist timezone on the ledger.

It may: write Mongo `store.config.timezone`, drop derived period snapshots,
and change which month/year a historical UTC instant appears in.
A timezone change must not restamp `cost_*` or change \(F\).

## API

| Call | Role |
|---|---|
| `GET/PUT /api/config` | Onboarding; timezone is required on PUT |
| `GET/PUT /api/config/timezone` | Later change; same Mongo field |
| View payloads `timezone` | Echo of the zone used to bucket that response |

## Tests that lock this

- `TestViewPeriodsBucketsCivilDateInLedgerTimezone`
- `TestViewPeriodsHonorsStoredTimezone`
- `TestChangingTimezoneRebucketsExistingTransactions`
- `TestLedgerTimezonePersistsInSettingsNotCRDB`
- `TestSetAppConfigWritesMongoNotCRDB`
- `TestPostJournalEntry` canonical `…Z` effective dates
