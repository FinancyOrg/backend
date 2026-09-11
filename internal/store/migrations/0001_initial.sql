-- Ledger of record (currency-neutral). Timestamps TEXT ISO-8601 UTC.
-- Money INT minor units. Rationals are TEXT decimal integer strings.

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
  -- Optional exact relation from native units to the journal's settlement/weight commodity.
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

-- Current functional currency. Changeable. See docs/rebuild/backend/08-functional-currency.md.
CREATE TABLE app_config (
  key TEXT PRIMARY KEY NOT NULL,
  value TEXT NOT NULL
);

CREATE TABLE account_balances (
  account_id TEXT NOT NULL REFERENCES accounts(id),
  commodity_id TEXT NOT NULL REFERENCES commodities(id),
  units_minor INT NOT NULL,
  PRIMARY KEY (account_id, commodity_id)
);

-- Keep account_balances in sync with postings. CockroachDB requires
-- (NEW)/(OLD) parentheses when accessing trigger row columns.

CREATE FUNCTION postings_account_balance_ai() RETURNS TRIGGER AS $$
BEGIN
  INSERT INTO account_balances (account_id, commodity_id, units_minor)
  VALUES ((NEW).account_id, (NEW).commodity_id, (NEW).units_minor)
  ON CONFLICT (account_id, commodity_id) DO UPDATE SET
    units_minor = account_balances.units_minor + excluded.units_minor;
  DELETE FROM account_balances
  WHERE account_id = (NEW).account_id
    AND commodity_id = (NEW).commodity_id
    AND units_minor = 0;
  RETURN NEW;
END
$$ LANGUAGE PLpgSQL;

CREATE TRIGGER postings_ai_account_balance
AFTER INSERT ON postings
FOR EACH ROW EXECUTE FUNCTION postings_account_balance_ai();

CREATE FUNCTION postings_account_balance_ad() RETURNS TRIGGER AS $$
BEGIN
  INSERT INTO account_balances (account_id, commodity_id, units_minor)
  VALUES ((OLD).account_id, (OLD).commodity_id, -((OLD).units_minor))
  ON CONFLICT (account_id, commodity_id) DO UPDATE SET
    units_minor = account_balances.units_minor + excluded.units_minor;
  DELETE FROM account_balances
  WHERE account_id = (OLD).account_id
    AND commodity_id = (OLD).commodity_id
    AND units_minor = 0;
  RETURN OLD;
END
$$ LANGUAGE PLpgSQL;

CREATE TRIGGER postings_ad_account_balance
AFTER DELETE ON postings
FOR EACH ROW EXECUTE FUNCTION postings_account_balance_ad();

CREATE FUNCTION postings_account_balance_au() RETURNS TRIGGER AS $$
BEGIN
  INSERT INTO account_balances (account_id, commodity_id, units_minor)
  VALUES ((OLD).account_id, (OLD).commodity_id, -((OLD).units_minor))
  ON CONFLICT (account_id, commodity_id) DO UPDATE SET
    units_minor = account_balances.units_minor + excluded.units_minor;
  DELETE FROM account_balances
  WHERE account_id = (OLD).account_id
    AND commodity_id = (OLD).commodity_id
    AND units_minor = 0;
  INSERT INTO account_balances (account_id, commodity_id, units_minor)
  VALUES ((NEW).account_id, (NEW).commodity_id, (NEW).units_minor)
  ON CONFLICT (account_id, commodity_id) DO UPDATE SET
    units_minor = account_balances.units_minor + excluded.units_minor;
  DELETE FROM account_balances
  WHERE account_id = (NEW).account_id
    AND commodity_id = (NEW).commodity_id
    AND units_minor = 0;
  RETURN NEW;
END
$$ LANGUAGE PLpgSQL;

CREATE TRIGGER postings_au_account_balance
AFTER UPDATE ON postings
FOR EACH ROW EXECUTE FUNCTION postings_account_balance_au();
