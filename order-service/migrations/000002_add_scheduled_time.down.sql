-- AI Assistance Disclosure:
-- Tool: Codex (model: GPT-6), date: 2026-10-28
-- Scope: Reverses 000002_add_scheduled_time.up.sql.
-- Author review: ZI YANG - reviewed and verified

ALTER TABLE orders
    DROP CONSTRAINT orders_scheduled_time_after_creation,
    DROP COLUMN scheduled_time,
    DROP CONSTRAINT orders_state_valid,
    ADD CONSTRAINT orders_state_valid CHECK (state IN
        ('OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED', 'COMPLETED', 'CANCELLED', 'EXPIRED'));
