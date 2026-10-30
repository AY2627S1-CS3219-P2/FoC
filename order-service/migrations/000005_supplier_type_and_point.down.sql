-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
-- Scope: Reverses 000005_supplier_type_and_point.up.sql.
-- Author review: PENDING — <reviewer to complete>

ALTER TABLE orders
    DROP CONSTRAINT IF EXISTS orders_supplier_point_whole,
    DROP COLUMN IF EXISTS supplier_lon,
    DROP COLUMN IF EXISTS supplier_lat,
    DROP COLUMN IF EXISTS supplier_type;
