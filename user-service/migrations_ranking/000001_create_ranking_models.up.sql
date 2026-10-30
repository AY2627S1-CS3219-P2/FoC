-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
-- Scope: The ranking-model database's one table (D-095, D-099). This folder's
--   migrations run against USER_RANKING_DB_URL, a database separate from
--   USER_DB_URL's.
-- Author review: PENDING — <reviewer to complete>

-- One row per user: the errand-ranking model order-service last saved for
-- them, kept as the opaque JSON object it sent.
CREATE TABLE ranking_models (
    user_id    UUID        PRIMARY KEY,
    model      JSONB       NOT NULL,
    trained_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT ranking_models_model_is_object CHECK (jsonb_typeof(model) = 'object')
);
