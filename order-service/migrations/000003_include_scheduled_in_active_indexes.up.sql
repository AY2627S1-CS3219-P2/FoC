-- AI Assistance Disclosure:
-- Tool: Codex (model: GPT-6), date: 2026-10-28
-- Scope: Applies D-085 to the existing active-errand partial indexes.
-- Author review: ZI YANG - reviewed and verified

DROP INDEX orders_supplier_active;
CREATE INDEX orders_supplier_active ON orders (supplier_id)
    WHERE state IN ('SCHEDULED', 'OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED');

DROP INDEX orders_active_oldest;
CREATE INDEX orders_active_oldest ON orders (state_changed_at)
    WHERE state IN ('SCHEDULED', 'OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED');
