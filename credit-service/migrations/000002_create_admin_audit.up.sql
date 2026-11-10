-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
-- Scope: credit-service's admin audit table (D-152): the same columns, indexes
--   and immutability trigger as the other services' (D-106 to D-115), copied
--   from order-service's migration 000004. One row per Re-verify that clears a
--   flag or is refused, immutable in the database, kept for 6 months.
-- Author review: Reviewed by Seth

CREATE TABLE IF NOT EXISTS admin_audit (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id            UUID NOT NULL,
    actor_role          TEXT NOT NULL,
    action              TEXT NOT NULL,
    target_type         TEXT NOT NULL,
    target_id           TEXT NOT NULL DEFAULT '',
    outcome             TEXT NOT NULL CHECK (outcome IN ('succeeded', 'refused', 'failed')),
    refusal_reason      TEXT NOT NULL DEFAULT '',
    reason              TEXT NOT NULL DEFAULT '',
    before_state        JSONB,
    after_state         JSONB,
    additional_comments JSONB,
    correlation_id      TEXT NOT NULL DEFAULT ''
);

-- The read endpoint lists newest first and filters by target, so these two
-- cover it; the purge job reads occurred_at too.
CREATE INDEX IF NOT EXISTS admin_audit_occurred_at_idx ON admin_audit (occurred_at DESC);
CREATE INDEX IF NOT EXISTS admin_audit_target_idx ON admin_audit (target_type, target_id);

-- D-114: an entry is immutable. An UPDATE is always refused, and a DELETE only
-- when the entry is under 6 months old, which is the one thing the scheduled
-- purge (D-115) is allowed to remove.
CREATE OR REPLACE FUNCTION admin_audit_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'admin_audit rows are immutable';
    END IF;
    IF OLD.occurred_at > now() - interval '6 months' THEN
        RAISE EXCEPTION 'admin_audit rows are kept for 6 months';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS admin_audit_guard ON admin_audit;
CREATE TRIGGER admin_audit_guard
    BEFORE UPDATE OR DELETE ON admin_audit
    FOR EACH ROW EXECUTE FUNCTION admin_audit_guard();
