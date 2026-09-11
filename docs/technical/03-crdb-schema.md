# CockroachDB schema

Ledger of record. All timestamps `TEXT` ISO-8601 UTC as in production. Integers that are money are **minor units** (`INT`). Rationals are `TEXT` decimal integer strings (arbitrary size).

Unlock/relock `schema_locked` around DDL.

## Migration `0001_initial.sql`

```sql
CREATE TABLE commodities (
  id TEXT PRIMARY KEY NOT NULL,
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  minor_units INT NOT NULL CHECK (minor_units >= 0),
  kind TEXT NOT NULL CHECK (kind IN ('currency', 'security', 'other')),
  created_at TEXT NOT NULL DEFAULT (now()::STRING)
);

CREATE TABLE accounts (
  id TEXT PRIMARY KEY NOT NULL,
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  account_type TEXT NOT NULL CHECK (account_type IN ('asset', 'liability', 'equity', 'income', 'expense')),
  parent_id TEXT REFERENCES accounts(id),
  native_commodity_id TEXT NOT NULL REFERENCES commodities(id),
  created_at TEXT NOT NULL DEFAULT (now()::STRING)
);

CREATE INDEX idx_accounts_parent ON accounts(parent_id);
CREATE INDEX idx_accounts_type ON accounts(account_type);

CREATE TABLE journal_entries (
  id TEXT PRIMARY KEY NOT NULL,
  -- UTC instant TEXT (domain.UTCLayout), never a civil date or offset.
  effective_date TEXT NOT NULL,
  description TEXT,
  status TEXT NOT NULL CHECK (status IN ('draft', 'posted')),
  created_at TEXT NOT NULL DEFAULT (now()::STRING),
  posted_at TEXT,
  CHECK (
    (status = 'posted' AND posted_at IS NOT NULL)
    OR (status != 'posted')
  )
);

CREATE INDEX idx_journal_entries_list
ON journal_entries(effective_date DESC, posted_at DESC, id DESC)
WHERE status = 'posted';

CREATE TABLE postings (
  id TEXT PRIMARY KEY NOT NULL,
  journal_entry_id TEXT NOT NULL REFERENCES journal_entries(id),
  line_order INT NOT NULL,
  account_id TEXT NOT NULL REFERENCES accounts(id),
  units_minor INT NOT NULL,
  commodity_id TEXT NOT NULL REFERENCES commodities(id),
  -- Journal weight/settlement relation (transaction fact).
  price_numerator TEXT,
  price_denominator TEXT,
  price_commodity_id TEXT REFERENCES commodities(id),
  price_source TEXT CHECK (price_source IS NULL OR price_source IN ('actual', 'ecb', 'identity')),
  price_observed_date TEXT,
  -- Functional carrying in F (FIFO lots). Restated at one rate on change of F.
  cost_per_unit_numerator TEXT,
  cost_per_unit_denominator TEXT,
  cost_commodity_id TEXT REFERENCES commodities(id),
  cost_date TEXT,
  cost_label TEXT,
  weight_minor INT NOT NULL,
  weight_commodity_id TEXT NOT NULL REFERENCES commodities(id),
  memo TEXT,
  UNIQUE (journal_entry_id, line_order)
);

CREATE INDEX idx_postings_account_journal ON postings(account_id, journal_entry_id);
CREATE INDEX idx_postings_journal ON postings(journal_entry_id);

CREATE TABLE audit_events (
  id TEXT PRIMARY KEY NOT NULL,
  event_type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (now()::STRING)
);

CREATE INDEX idx_audit_events_type ON audit_events(event_type);
CREATE INDEX idx_audit_events_created ON audit_events(created_at);

CREATE TABLE account_balances (
  account_id TEXT NOT NULL REFERENCES accounts(id),
  commodity_id TEXT NOT NULL REFERENCES commodities(id),
  units_minor INT NOT NULL,
  PRIMARY KEY (account_id, commodity_id)
);

-- Current functional currency. Changeable; see 08-functional-currency.md.
CREATE TABLE app_config (
  key TEXT PRIMARY KEY NOT NULL,
  value TEXT NOT NULL
);
```

The commodity catalog starts empty. There is no seed of EUR, USD, INR, or
JPY. A currency becomes a `commodities` row when the user creates it
(`POST /api/commodities`, including from onboarding). EUR is not required
for migrate, accounts, or journals. It is special only as the
ECB quote unit: `FetchEcbRatePerEur` returns `1/1` and never writes Mongo
`ecb:*`. \(F\) may be EUR, JPY, or any allowlisted code. The
`functional_commodity_id` row in `app_config` is the current \(F\).

Copy the three `postings_account_balance_a{i,d,u}` functions and triggers from `apps/backend/internal/store/migrations/0001_initial.sql` verbatim.

JPY `minor_units = 0` (yen has no fractional subunit in ISO; ECB quotes
integer yen per euro). Conversion code already has `minor_units` on the
commodity — do not hard-code 2.

For `kind='currency'`, Go validates `code` against the embedded
`SupportedCurrencyCodes` allowlist before inserting. The database stores the
catalog, but it does not make unsupported ISO codes valid. Account creation
repeats this validation against `native_commodity_id`.

## Migration `0002_statement_covering_indexes.sql`

Fat covering indexes (disk for index-only scans). Copy from
`apps/backend/internal/store/migrations/0002_statement_covering_indexes.sql`:

- `idx_postings_journal_line_cover` on `postings (journal_entry_id, line_order) STORING (… price_*, cost_*, weight_*, memo)`
- `idx_postings_account_line_cover` on `postings (account_id, journal_entry_id, line_order) STORING (…)` — lots, P&L, account filters
- `idx_postings_cost_commodity_cover` on `postings (cost_commodity_id) STORING (units_minor, cost_*) WHERE cost_commodity_id IS NOT NULL` — restamp of \(F\)
- `idx_journal_entries_status_list` on `journal_entries (status, effective_date DESC, posted_at DESC, id DESC) STORING (description, created_at)`
- `idx_journal_entries_posted_fifo` on `journal_entries (effective_date, posted_at, created_at, id) WHERE status = 'posted'`
- `idx_accounts_type_cover` on `accounts (account_type) STORING (code, name, parent_id, native_commodity_id, created_at)`

## `postings.price_*`

`price_*` records the actual or reference relationship needed to balance a
cross-currency journal. Examples:

- `-€1,000` in N26 and `+₹90,000` in Axis: the INR line may carry an actual
  price of `90 INR/EUR`, with weight `€1,000`.
- An ECB-reference exchange may carry `price_source='ecb'` and
  `price_observed_date` from the ECB publication.
- An identity price is `1/1` when two lines already use the same commodity.

Fees and FX margins are separate native postings. A change of \(F\) restates
`cost_*` only; it never rewrites native fee units, `price_*`, or weights.

## What does not belong in CRDB

- `is_hidden`, `is_liquid`
- `theme`, `timezone`, `ledger_revision` (Mongo)
- A presentation overlay — there is none; \(F\) **does** belong in CRDB `app_config`
- Derived net-worth / translation adjustment / month rollups
- Derived daily P&L projections
- ECB daily spots and valuation crosses (Mongo `ecb:*` + Go)
- Session tokens
