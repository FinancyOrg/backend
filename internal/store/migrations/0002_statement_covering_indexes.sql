-- Additive covering indexes for the hottest statement fingerprints.
-- Fat STORING clauses trade disk for index-only scans. Does not drop the
-- skinny 0001 indexes. Unlock schema_locked around DDL (CRDB v26.1+).

ALTER TABLE postings SET (schema_locked = false);
ALTER TABLE journal_entries SET (schema_locked = false);
ALTER TABLE accounts SET (schema_locked = false);

-- loadPostingsFromDB:
--   SELECT … FROM postings WHERE journal_entry_id = ANY (_)
--   ORDER BY journal_entry_id, line_order
CREATE INDEX IF NOT EXISTS idx_postings_journal_line_cover
ON postings (journal_entry_id, line_order)
STORING (
  id,
  account_id,
  units_minor,
  commodity_id,
  cost_per_unit_numerator,
  cost_per_unit_denominator,
  cost_commodity_id,
  cost_date,
  cost_label,
  price_numerator,
  price_denominator,
  price_commodity_id,
  price_source,
  price_observed_date,
  weight_minor,
  weight_commodity_id,
  memo
);

-- loadMonetaryLots / ListPnLPostings / accountHasPostings / list-by-account:
--   WHERE account_id = ANY (_)  (join journals for time order)
--   SELECT DISTINCT account_id / COUNT(*) WHERE account_id = _
CREATE INDEX IF NOT EXISTS idx_postings_account_line_cover
ON postings (account_id, journal_entry_id, line_order)
STORING (
  id,
  units_minor,
  commodity_id,
  cost_per_unit_numerator,
  cost_per_unit_denominator,
  cost_commodity_id,
  cost_date,
  cost_label,
  price_numerator,
  price_denominator,
  price_commodity_id,
  price_source,
  price_observed_date,
  weight_minor,
  weight_commodity_id,
  memo
);

-- restampFunctionalCosts:
--   SELECT id, units_minor, cost_* FROM postings WHERE cost_commodity_id = _
CREATE INDEX IF NOT EXISTS idx_postings_cost_commodity_cover
ON postings (cost_commodity_id)
STORING (
  units_minor,
  cost_per_unit_numerator,
  cost_per_unit_denominator,
  cost_date,
  cost_label
)
WHERE cost_commodity_id IS NOT NULL;

-- Journal list (any status):
--   SELECT j.id FROM journal_entries WHERE status = _
--   ORDER BY effective_date DESC, posted_at DESC, id DESC
-- STORING covers the follow-up SELECT of description/created_at by id prefix.
CREATE INDEX IF NOT EXISTS idx_journal_entries_status_list
ON journal_entries (status, effective_date DESC, posted_at DESC, id DESC)
STORING (description, created_at);

-- FIFO / P&L chronological join (posted only, ASC):
--   JOIN journal_entries e … WHERE e.status = 'posted'
--   ORDER BY e.effective_date, e.posted_at, e.created_at
CREATE INDEX IF NOT EXISTS idx_journal_entries_posted_fifo
ON journal_entries (effective_date, posted_at, created_at, id)
STORING (description)
WHERE status = 'posted';

-- ListPnLPostings / period views: filter accounts by type, then join postings.
CREATE INDEX IF NOT EXISTS idx_accounts_type_cover
ON accounts (account_type)
STORING (code, name, parent_id, native_commodity_id, created_at);

ALTER TABLE postings SET (schema_locked = true);
ALTER TABLE journal_entries SET (schema_locked = true);
ALTER TABLE accounts SET (schema_locked = true);
