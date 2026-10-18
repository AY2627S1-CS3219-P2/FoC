-- AI Assistance Disclosure:
-- Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
-- Scope: order-service's three tables (D-053): orders (F3.1.1, D-045, D-046,
--   D-047, D-049, D-052, D-053), order_history (F3.13.1, F3.7.7, F3.5.3) and
--   the outbox (D-047, D-051).
-- Author review: James Teo — reviewed and verified.

-- "order" is a reserved word in SQL, hence "orders".
CREATE TABLE orders (
    id                UUID        PRIMARY KEY,
    requester_id      UUID        NOT NULL,
    courier_id        UUID,
    -- The supplier as it was at create or the last amend (D-052).
    supplier_id       UUID        NOT NULL,
    supplier_name     TEXT        NOT NULL,
    supplier_location TEXT        NOT NULL,
    item_description  TEXT        NOT NULL,
    delivery_location TEXT        NOT NULL,
    delivery_note     TEXT        NOT NULL DEFAULT '',
    reward            INTEGER     NOT NULL,
    state             TEXT        NOT NULL,
    version           INTEGER     NOT NULL DEFAULT 1,
    -- The X-Request-Id the errand was created with (D-047).
    request_key       TEXT,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    state_changed_at  TIMESTAMPTZ NOT NULL,
    -- Set only while OPEN (D-049, D-050).
    expires_at        TIMESTAMPTZ,
    -- Set only while DELIVERED (D-049, D-050).
    confirm_by        TIMESTAMPTZ,

    CONSTRAINT orders_state_valid CHECK (state IN
        ('OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED', 'COMPLETED', 'CANCELLED', 'EXPIRED')),
    CONSTRAINT orders_reward_positive CHECK (reward > 0),
    CONSTRAINT orders_item_length CHECK (char_length(item_description) BETWEEN 1 AND 280),
    CONSTRAINT orders_note_length CHECK (char_length(delivery_note) <= 140),
    CONSTRAINT orders_courier_not_requester CHECK (courier_id IS NULL OR courier_id <> requester_id),
    CONSTRAINT orders_open_has_no_courier CHECK (state <> 'OPEN' OR courier_id IS NULL),
    CONSTRAINT orders_held_has_courier CHECK
        (state NOT IN ('ACCEPTED', 'PICKED_UP', 'DELIVERED') OR courier_id IS NOT NULL),
    CONSTRAINT orders_open_has_deadline CHECK (state <> 'OPEN' OR expires_at IS NOT NULL),
    CONSTRAINT orders_delivered_has_window CHECK (state <> 'DELIVERED' OR confirm_by IS NOT NULL)
);

CREATE UNIQUE INDEX orders_requester_request_key
    ON orders (requester_id, request_key) WHERE request_key IS NOT NULL;
CREATE INDEX orders_open_newest ON orders (created_at DESC) WHERE state = 'OPEN';
CREATE INDEX orders_requester_state ON orders (requester_id, state, state_changed_at DESC);
CREATE INDEX orders_courier_state ON orders (courier_id, state, state_changed_at DESC)
    WHERE courier_id IS NOT NULL;
CREATE INDEX orders_supplier_active ON orders (supplier_id)
    WHERE state IN ('OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED');
CREATE INDEX orders_active_oldest ON orders (state_changed_at)
    WHERE state IN ('OPEN', 'ACCEPTED', 'PICKED_UP', 'DELIVERED');
CREATE INDEX orders_expiry_due ON orders (expires_at) WHERE state = 'OPEN';
CREATE INDEX orders_confirm_due ON orders (confirm_by) WHERE state = 'DELIVERED';

-- Every transition and amendment (F3.13.1, F3.7.7).
CREATE TABLE order_history (
    id             BIGSERIAL   PRIMARY KEY,
    order_id       UUID        NOT NULL REFERENCES orders (id),
    action         TEXT        NOT NULL,
    from_state     TEXT,
    to_state       TEXT        NOT NULL,
    actor_id       UUID,
    actor_kind     TEXT        NOT NULL,
    -- The courier assigned when the row was written, so a withdrawn courier's
    -- history still finds the errand (F3.5.3).
    courier_id     UUID,
    changes        JSONB,
    occurred_at    TIMESTAMPTZ NOT NULL,
    correlation_id TEXT        NOT NULL DEFAULT '',

    CONSTRAINT order_history_actor_kind_valid CHECK
        (actor_kind IN ('requester', 'courier', 'admin', 'system'))
);

CREATE INDEX order_history_order ON order_history (order_id, id);
CREATE INDEX order_history_courier ON order_history (courier_id, occurred_at DESC)
    WHERE courier_id IS NOT NULL;

-- Lifecycle events waiting for the relay (D-051). available_at delays D-047's
-- queued refund; nothing references orders, because a refund may outlive a
-- create that never saved its errand.
CREATE TABLE outbox (
    event_id       UUID        PRIMARY KEY,
    routing_key    TEXT        NOT NULL,
    payload        JSONB       NOT NULL,
    correlation_id TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    available_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    attempts       INTEGER     NOT NULL DEFAULT 0,
    last_error     TEXT
);

CREATE INDEX outbox_due ON outbox (available_at) WHERE published_at IS NULL;
