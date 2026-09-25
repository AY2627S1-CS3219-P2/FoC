CREATE TABLE IF NOT EXISTS suppliers (
    id                   UUID PRIMARY KEY,
    name                 TEXT NOT NULL,
    type                 TEXT NOT NULL,
    building             TEXT NOT NULL,
    floor                TEXT NOT NULL DEFAULT '',
    location_description TEXT NOT NULL,
    latitude             DOUBLE PRECISION NOT NULL,
    longitude            DOUBLE PRECISION NOT NULL,
    opening_time         TEXT NOT NULL DEFAULT '',
    closing_time         TEXT NOT NULL DEFAULT '',
    image_url            TEXT NOT NULL DEFAULT '',
    description          TEXT NOT NULL DEFAULT '',
    is_available         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at           TIMESTAMPTZ
);

-- Duplicate check (FR F2.2.2) only applies to active (non soft-deleted)
-- suppliers, so a name+location combination can be reused after deletion.
CREATE UNIQUE INDEX IF NOT EXISTS suppliers_name_building_location_active_idx
    ON suppliers (name, building, location_description)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS suppliers_type_idx ON suppliers (type) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS suppliers_name_idx ON suppliers (lower(name)) WHERE deleted_at IS NULL;
