-- AI Assistance Disclosure:
-- Tool: Codex (model: GPT-6), date: 2026-10-28
-- Scope: Adds nullable scheduled_time storage and permits the SCHEDULED state,
--   implementing the owner's recorded schema decision in order-service/AGENTS.md.
-- Author review: ZI YANG - reviewed and verified

ALTER TABLE orders
    ADD COLUMN scheduled_time TIMESTAMPTZ,
    DROP CONSTRAINT orders_state_valid,
    ADD CONSTRAINT orders_state_valid CHECK (state IN
        ('SCHEDULED', 'OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED', 'COMPLETED', 'CANCELLED', 'EXPIRED')),
    ADD CONSTRAINT orders_scheduled_time_after_creation CHECK
        (scheduled_time IS NULL OR scheduled_time >= created_at);
