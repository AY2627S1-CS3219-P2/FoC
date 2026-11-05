-- Conversations table
CREATE TABLE IF NOT EXISTS conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id UUID NOT NULL,
    requester_id UUID NOT NULL,
    courier_id UUID NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'FROZEN')),
    errand_completed_at TIMESTAMPTZ,
    requester_last_read_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    courier_last_read_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_event_occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversations_errand_courier ON conversations (errand_id, courier_id);
CREATE INDEX IF NOT EXISTS idx_conversations_requester ON conversations (requester_id);
CREATE INDEX IF NOT EXISTS idx_conversations_courier ON conversations (courier_id);
CREATE INDEX IF NOT EXISTS idx_conversations_errand ON conversations (errand_id);
CREATE INDEX IF NOT EXISTS idx_conversations_active_completed ON conversations (status, errand_completed_at)
    WHERE status = 'ACTIVE' AND errand_completed_at IS NOT NULL;

-- Chat messages table
CREATE TABLE IF NOT EXISTS chat_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_id UUID NOT NULL,
    message_type VARCHAR(10) NOT NULL CHECK (message_type IN ('TEXT', 'IMAGE')),
    text_content TEXT,
    media_id UUID,
    media_url TEXT,
    client_message_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_messages_client_msg_id ON chat_messages (conversation_id, client_message_id)
    WHERE client_message_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_chat_messages_cursor ON chat_messages (conversation_id, created_at DESC, id DESC);

-- Chat audit log table (admin inspect)
CREATE TABLE IF NOT EXISTS chat_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id UUID NOT NULL,
    action VARCHAR(50) NOT NULL DEFAULT 'chat.inspect',
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    errand_id UUID NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chat_audit_log_conversation ON chat_audit_log (conversation_id, occurred_at DESC);
