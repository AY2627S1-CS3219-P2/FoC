-- AI Assistance Disclosure:
-- Tool: Codex (model: GPT-6), date: 2026-10-28
-- Scope: Restores the pre-D-085 active-errand partial-index predicates.
-- Author review: ZI YANG - reviewed and verified

DROP INDEX orders_supplier_active;
CREATE INDEX orders_supplier_active ON orders (supplier_id)
    WHERE state IN ('OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED');

DROP INDEX orders_active_oldest;
CREATE INDEX orders_active_oldest ON orders (state_changed_at)
    WHERE state IN ('OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED');
