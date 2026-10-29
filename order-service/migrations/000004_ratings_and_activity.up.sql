-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
-- Scope: Tables for ratings (D-093) and Find errands' shown and search
--   records (D-088, D-092), kept in order-service (D-089). Numbered 000004 to
--   follow open PR #67's 000002 and 000003.
-- Author review: PENDING — <reviewer to complete>

-- One row per side per errand: the requester rates the courier, the courier
-- the requester. The primary key is what makes a rating impossible to give
-- twice; nothing updates a row.
CREATE TABLE errand_ratings (
    order_id   UUID        NOT NULL REFERENCES orders (id),
    rater_id   UUID        NOT NULL,
    ratee_id   UUID        NOT NULL,
    rater_role TEXT        NOT NULL,
    stars      SMALLINT    NOT NULL,
    rated_at   TIMESTAMPTZ NOT NULL,
    CONSTRAINT errand_ratings_pkey PRIMARY KEY (order_id, rater_id),
    CONSTRAINT errand_ratings_role_valid CHECK (rater_role IN ('requester', 'courier')),
    CONSTRAINT errand_ratings_stars_range CHECK (stars BETWEEN 1 AND 5),
    CONSTRAINT errand_ratings_not_self CHECK (rater_id <> ratee_id)
);

-- A user's rating is read by who received it.
CREATE INDEX errand_ratings_ratee ON errand_ratings (ratee_id);

-- Finding a user's completed errands still to rate reads COMPLETED rows by
-- either party; the existing requester and courier indexes cover both.

-- One row per errand a courier saw on Find errands, with its place on the
-- list and the filters set at the time.
CREATE TABLE errand_views (
    id         BIGSERIAL   PRIMARY KEY,
    courier_id UUID        NOT NULL,
    order_id   UUID        NOT NULL REFERENCES orders (id),
    position   INTEGER     NOT NULL,
    filters    JSONB       NOT NULL,
    shown_at   TIMESTAMPTZ NOT NULL,
    CONSTRAINT errand_views_position_valid CHECK (position >= 0)
);

CREATE INDEX errand_views_courier ON errand_views (courier_id, shown_at);

-- One row per change a courier made to Find errands' filters.
CREATE TABLE errand_searches (
    id          BIGSERIAL   PRIMARY KEY,
    courier_id  UUID        NOT NULL,
    filters     JSONB       NOT NULL,
    searched_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX errand_searches_courier ON errand_searches (courier_id, searched_at);
