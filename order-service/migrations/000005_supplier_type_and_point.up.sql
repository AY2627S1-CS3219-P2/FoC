-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
-- Scope: The supplier copy D-052 takes also keeps the supplier's type and
--   position, for errand ranking (D-089).
-- Author review: PENDING — <reviewer to complete>

-- Errands posted before this migration have no type ('') and no position.
ALTER TABLE orders
    ADD COLUMN supplier_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN supplier_lat  DOUBLE PRECISION,
    ADD COLUMN supplier_lon  DOUBLE PRECISION,
    ADD CONSTRAINT orders_supplier_point_whole
        CHECK ((supplier_lat IS NULL) = (supplier_lon IS NULL));
