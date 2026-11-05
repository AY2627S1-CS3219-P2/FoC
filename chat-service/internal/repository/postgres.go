package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"foc/chat-service/internal/chat"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func ConnectPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect db pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

const selectConversationCols = `
	id, errand_id, requester_id, courier_id, status,
	errand_completed_at, requester_last_read_at, courier_last_read_at,
	last_event_occurred_at, created_at, updated_at
`

func scanConversation(row pgx.Row) (*chat.Conversation, error) {
	var c chat.Conversation
	err := row.Scan(
		&c.ID,
		&c.ErrandID,
		&c.RequesterID,
		&c.CourierID,
		&c.Status,
		&c.ErrandCompletedAt,
		&c.RequesterLastReadAt,
		&c.CourierLastReadAt,
		&c.LastEventOccurredAt,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, chat.ErrConversationNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *PostgresRepository) GetConversation(ctx context.Context, id uuid.UUID) (*chat.Conversation, error) {
	query := fmt.Sprintf(`SELECT %s FROM conversations WHERE id = $1`, selectConversationCols)
	return scanConversation(r.pool.QueryRow(ctx, query, id))
}

func (r *PostgresRepository) GetConversationByErrandCourier(ctx context.Context, errandID, courierID uuid.UUID) (*chat.Conversation, error) {
	query := fmt.Sprintf(`SELECT %s FROM conversations WHERE errand_id = $1 AND courier_id = $2`, selectConversationCols)
	return scanConversation(r.pool.QueryRow(ctx, query, errandID, courierID))
}

func (r *PostgresRepository) ListConversationsByUser(ctx context.Context, userID uuid.UUID, errandIDFilter *uuid.UUID) ([]*chat.Conversation, error) {
	var query string
	var args []any

	if errandIDFilter != nil {
		query = fmt.Sprintf(`SELECT %s FROM conversations WHERE (requester_id = $1 OR courier_id = $1) AND errand_id = $2 ORDER BY updated_at DESC`, selectConversationCols)
		args = []any{userID, *errandIDFilter}
	} else {
		query = fmt.Sprintf(`SELECT %s FROM conversations WHERE (requester_id = $1 OR courier_id = $1) ORDER BY updated_at DESC`, selectConversationCols)
		args = []any{userID}
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversations by user: %w", err)
	}
	defer rows.Close()

	var convs []*chat.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		convs = append(convs, c)
	}
	return convs, rows.Err()
}

func (r *PostgresRepository) ListAllConversationsAdmin(ctx context.Context, errandIDFilter *uuid.UUID, limit int) ([]*chat.Conversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var query string
	var args []any

	if errandIDFilter != nil {
		query = fmt.Sprintf(`SELECT %s FROM conversations WHERE errand_id = $1 ORDER BY updated_at DESC LIMIT $2`, selectConversationCols)
		args = []any{*errandIDFilter, limit}
	} else {
		query = fmt.Sprintf(`SELECT %s FROM conversations ORDER BY updated_at DESC LIMIT $1`, selectConversationCols)
		args = []any{limit}
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversations admin: %w", err)
	}
	defer rows.Close()

	var convs []*chat.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		convs = append(convs, c)
	}
	return convs, rows.Err()
}

func (r *PostgresRepository) UpsertConversationOnAccepted(ctx context.Context, errandID, requesterID, courierID uuid.UUID, occurredAt time.Time) (*chat.Conversation, error) {
	query := fmt.Sprintf(`
		INSERT INTO conversations (
			errand_id, requester_id, courier_id, status, last_event_occurred_at, created_at, updated_at
		) VALUES ($1, $2, $3, 'ACTIVE', $4, NOW(), NOW())
		ON CONFLICT (errand_id, courier_id) DO UPDATE
		SET status = 'ACTIVE',
			last_event_occurred_at = EXCLUDED.last_event_occurred_at,
			updated_at = NOW()
		WHERE conversations.last_event_occurred_at <= EXCLUDED.last_event_occurred_at
		RETURNING %s
	`, selectConversationCols)

	conv, err := scanConversation(r.pool.QueryRow(ctx, query, errandID, requesterID, courierID, occurredAt))
	if err == nil {
		return conv, nil
	}
	if errors.Is(err, chat.ErrConversationNotFound) {
		// Event occurred before conversations.last_event_occurred_at (out-of-order event); fetch current state
		return r.GetConversationByErrandCourier(ctx, errandID, courierID)
	}
	return nil, fmt.Errorf("upsert conversation: %w", err)
}

func (r *PostgresRepository) FreezeConversation(ctx context.Context, errandID, courierID uuid.UUID, occurredAt time.Time) error {
	query := `
		UPDATE conversations
		SET status = 'FROZEN',
			last_event_occurred_at = $3,
			updated_at = NOW()
		WHERE errand_id = $1 AND courier_id = $2
		  AND last_event_occurred_at <= $3
	`
	_, err := r.pool.Exec(ctx, query, errandID, courierID, occurredAt)
	if err != nil {
		return fmt.Errorf("freeze conversation: %w", err)
	}
	return nil
}

func (r *PostgresRepository) MarkErrandCompleted(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error {
	query := `
		UPDATE conversations
		SET errand_completed_at = COALESCE(errand_completed_at, $2),
			last_event_occurred_at = GREATEST(last_event_occurred_at, $2),
			updated_at = NOW()
		WHERE errand_id = $1
	`
	_, err := r.pool.Exec(ctx, query, errandID, occurredAt)
	if err != nil {
		return fmt.Errorf("mark errand completed: %w", err)
	}
	return nil
}

func (r *PostgresRepository) FreezeErrandCancelledOrExpired(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error {
	query := `
		UPDATE conversations
		SET status = 'FROZEN',
			last_event_occurred_at = GREATEST(last_event_occurred_at, $2),
			updated_at = NOW()
		WHERE errand_id = $1 AND last_event_occurred_at <= $2
	`
	_, err := r.pool.Exec(ctx, query, errandID, occurredAt)
	if err != nil {
		return fmt.Errorf("freeze errand cancelled/expired: %w", err)
	}
	return nil
}

func (r *PostgresRepository) FreezeConversationsPast24Hours(ctx context.Context, cutoff time.Time) (int64, error) {
	query := `
		UPDATE conversations
		SET status = 'FROZEN',
			updated_at = NOW()
		WHERE status = 'ACTIVE'
		  AND errand_completed_at IS NOT NULL
		  AND errand_completed_at <= $1
	`
	tag, err := r.pool.Exec(ctx, query, cutoff)
	if err != nil {
		return 0, fmt.Errorf("freeze past 24h: %w", err)
	}
	return tag.RowsAffected(), nil
}

const selectMessageCols = `
	id, conversation_id, sender_id, message_type, text_content,
	media_id, media_url, client_message_id, created_at
`

func scanMessage(row pgx.Row) (*chat.Message, error) {
	var m chat.Message
	err := row.Scan(
		&m.ID,
		&m.ConversationID,
		&m.SenderID,
		&m.Type,
		&m.TextContent,
		&m.MediaID,
		&m.MediaURL,
		&m.ClientMessageID,
		&m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *PostgresRepository) SaveMessage(ctx context.Context, msg *chat.Message) (*chat.Message, bool, error) {
	if msg.ClientMessageID != nil {
		insertQuery := fmt.Sprintf(`
			INSERT INTO chat_messages (
				id, conversation_id, sender_id, message_type, text_content,
				media_id, media_url, client_message_id, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (conversation_id, client_message_id) WHERE client_message_id IS NOT NULL
			DO NOTHING
			RETURNING %s
		`, selectMessageCols)

		saved, err := scanMessage(r.pool.QueryRow(ctx, insertQuery,
			msg.ID, msg.ConversationID, msg.SenderID, msg.Type, msg.TextContent,
			msg.MediaID, msg.MediaURL, msg.ClientMessageID, msg.CreatedAt,
		))
		if err == nil {
			_, _ = r.pool.Exec(ctx, `UPDATE conversations SET updated_at = NOW() WHERE id = $1`, msg.ConversationID)
			return saved, false, nil
		}
		if errors.Is(err, pgx.ErrNoRows) {
			// Duplicate client_message_id: retrieve existing message
			existingQuery := fmt.Sprintf(`
				SELECT %s FROM chat_messages
				WHERE conversation_id = $1 AND client_message_id = $2
			`, selectMessageCols)
			existing, err := scanMessage(r.pool.QueryRow(ctx, existingQuery, msg.ConversationID, msg.ClientMessageID))
			if err != nil {
				return nil, false, fmt.Errorf("get existing message on dup: %w", err)
			}
			return existing, true, nil
		}
		return nil, false, fmt.Errorf("insert message: %w", err)
	}

	// Message without client_message_id
	insertQuery := fmt.Sprintf(`
		INSERT INTO chat_messages (
			id, conversation_id, sender_id, message_type, text_content,
			media_id, media_url, client_message_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING %s
	`, selectMessageCols)

	saved, err := scanMessage(r.pool.QueryRow(ctx, insertQuery,
		msg.ID, msg.ConversationID, msg.SenderID, msg.Type, msg.TextContent,
		msg.MediaID, msg.MediaURL, msg.ClientMessageID, msg.CreatedAt,
	))
	if err != nil {
		return nil, false, fmt.Errorf("insert message: %w", err)
	}
	_, _ = r.pool.Exec(ctx, `UPDATE conversations SET updated_at = NOW() WHERE id = $1`, msg.ConversationID)
	return saved, false, nil
}

func (r *PostgresRepository) GetMessage(ctx context.Context, messageID uuid.UUID) (*chat.Message, error) {
	query := fmt.Sprintf(`SELECT %s FROM chat_messages WHERE id = $1`, selectMessageCols)
	m, err := scanMessage(r.pool.QueryRow(ctx, query, messageID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("message not found")
		}
		return nil, err
	}
	return m, nil
}

func (r *PostgresRepository) ListMessages(ctx context.Context, conversationID uuid.UUID, cursor *chat.MessageCursor, since *time.Time, limit int) ([]*chat.Message, *chat.MessageCursor, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	if since != nil {
		// Gapless reconnect catch-up: order chronologically ASC
		query := fmt.Sprintf(`
			SELECT %s FROM chat_messages
			WHERE conversation_id = $1 AND created_at > $2
			ORDER BY created_at ASC, id ASC
			LIMIT $3
		`, selectMessageCols)

		rows, err := r.pool.Query(ctx, query, conversationID, *since, limit)
		if err != nil {
			return nil, nil, false, fmt.Errorf("list messages since: %w", err)
		}
		defer rows.Close()

		var msgs []*chat.Message
		for rows.Next() {
			m, err := scanMessage(rows)
			if err != nil {
				return nil, nil, false, err
			}
			msgs = append(msgs, m)
		}
		return msgs, nil, false, rows.Err()
	}

	// Standard cursor pagination: order DESC
	var query string
	var args []any

	if cursor != nil {
		query = fmt.Sprintf(`
			SELECT %s FROM chat_messages
			WHERE conversation_id = $1 AND (created_at, id) < ($2, $3)
			ORDER BY created_at DESC, id DESC
			LIMIT $4
		`, selectMessageCols)
		args = []any{conversationID, cursor.CreatedAt, cursor.ID, limit + 1}
	} else {
		query = fmt.Sprintf(`
			SELECT %s FROM chat_messages
			WHERE conversation_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		`, selectMessageCols)
		args = []any{conversationID, limit + 1}
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, false, fmt.Errorf("list messages cursor: %w", err)
	}
	defer rows.Close()

	var msgs []*chat.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, nil, false, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, err
	}

	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}

	var nextCursor *chat.MessageCursor
	if hasMore && len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		nextCursor = &chat.MessageCursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		}
	}

	return msgs, nextCursor, hasMore, nil
}

func (r *PostgresRepository) UpdateLastRead(ctx context.Context, conversationID, userID uuid.UUID, readAt time.Time) error {
	query := `
		UPDATE conversations
		SET requester_last_read_at = CASE WHEN requester_id = $2 THEN $3 ELSE requester_last_read_at END,
			courier_last_read_at   = CASE WHEN courier_id = $2 THEN $3 ELSE courier_last_read_at END,
			updated_at             = NOW()
		WHERE id = $1 AND (requester_id = $2 OR courier_id = $2)
	`
	_, err := r.pool.Exec(ctx, query, conversationID, userID, readAt)
	if err != nil {
		return fmt.Errorf("update last read: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetUnreadSummary(ctx context.Context, userID uuid.UUID) (bool, []uuid.UUID, error) {
	query := `
		SELECT DISTINCT c.errand_id
		FROM conversations c
		JOIN chat_messages m ON m.conversation_id = c.id
		WHERE (
			(c.requester_id = $1 AND m.sender_id != $1 AND m.created_at > c.requester_last_read_at)
			OR
			(c.courier_id = $1 AND m.sender_id != $1 AND m.created_at > c.courier_last_read_at)
		)
	`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return false, nil, fmt.Errorf("get unread summary: %w", err)
	}
	defer rows.Close()

	var unreadErrandIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return false, nil, err
		}
		unreadErrandIDs = append(unreadErrandIDs, id)
	}
	if err := rows.Err(); err != nil {
		return false, nil, err
	}

	return len(unreadErrandIDs) > 0, unreadErrandIDs, nil
}

func (r *PostgresRepository) RecordAudit(ctx context.Context, entry *chat.AuditEntry) error {
	query := `
		INSERT INTO chat_audit_log (id, admin_id, action, conversation_id, errand_id, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.pool.Exec(ctx, query, entry.ID, entry.AdminID, entry.Action, entry.ConversationID, entry.ErrandID, entry.OccurredAt)
	if err != nil {
		return fmt.Errorf("record audit: %w", err)
	}
	return nil
}
