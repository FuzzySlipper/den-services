package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("pinging messages store: %w", err)
	}
	return nil
}

func (s *Store) CreateMessage(ctx context.Context, message *Message) (*Message, error) {
	created, err := scanMessage(s.pool.QueryRow(ctx, createMessageSQL,
		message.ProjectID(),
		message.TaskID(),
		message.ThreadID(),
		message.Sender(),
		message.Content(),
		message.Intent(),
		jsonOrNil(message.Metadata()),
		message.CreatedAt(),
	))
	if err != nil {
		return nil, fmt.Errorf("creating message: %w", err)
	}
	return created, nil
}

func (s *Store) GetMessage(ctx context.Context, id int64) (*Message, error) {
	message, err := scanMessage(s.pool.QueryRow(ctx, getMessageSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("getting message %d: %w", id, err)
	}
	return message, nil
}

func (s *Store) ListMessages(ctx context.Context, query ListMessagesQuery) ([]*Message, error) {
	rows, err := s.pool.Query(ctx, listMessagesSQL,
		query.ProjectID,
		query.TaskID,
		query.Since,
		emptyToNil(query.UnreadFor),
		emptyToNil(query.Intent),
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Store) UnreadAfterCursor(ctx context.Context, projectID string, unreadFor string, cursor int64, limit int) ([]*Message, error) {
	rows, err := s.pool.Query(ctx, unreadAfterCursorSQL, projectID, unreadFor, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("waiting for messages: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Store) GetThread(ctx context.Context, id int64) (Thread, error) {
	root, err := s.GetMessage(ctx, id)
	if err != nil {
		return Thread{}, err
	}
	rows, err := s.pool.Query(ctx, threadRepliesSQL, id)
	if err != nil {
		return Thread{}, fmt.Errorf("listing thread replies: %w", err)
	}
	defer rows.Close()
	replies, err := scanMessages(rows)
	if err != nil {
		return Thread{}, err
	}
	return Thread{Root: root, Replies: replies}, nil
}

func (s *Store) MarkRead(ctx context.Context, agent string, ids []int64) error {
	_, err := s.pool.Exec(ctx, markReadSQL, agent, ids)
	if err != nil {
		return fmt.Errorf("marking messages read: %w", err)
	}
	return nil
}

func (s *Store) ListNotifications(ctx context.Context, query NotificationQuery) ([]NotificationItem, error) {
	rows, err := s.pool.Query(ctx, listNotificationsSQL,
		emptyToNil(query.ProjectID),
		query.TaskID,
		emptyToNil(query.Sender),
		emptyToNil(query.MetadataType),
		emptyToNil(query.Urgency),
		emptyToNil(query.ReadForAgent),
		query.HasReadFilter,
		query.IsRead,
		query.Limit,
		query.Offset,
	)
	if err != nil {
		return nil, fmt.Errorf("listing notifications: %w", err)
	}
	defer rows.Close()
	var items []NotificationItem
	for rows.Next() {
		message, urgency, isRead, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, NotificationItem{Message: message, Urgency: urgency, IsRead: isRead})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning notifications: %w", err)
	}
	return items, nil
}

func (s *Store) MarkNotificationsRead(ctx context.Context, agent string, ids []int64) error {
	_, err := s.pool.Exec(ctx, markNotificationsReadSQL, agent, ids)
	if err != nil {
		return fmt.Errorf("marking notifications read: %w", err)
	}
	return nil
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, agent string, projectID string, taskID *int64) error {
	_, err := s.pool.Exec(ctx, markAllNotificationsReadSQL, agent, projectID, taskID)
	if err != nil {
		return fmt.Errorf("marking scoped notifications read: %w", err)
	}
	return nil
}

func (s *Store) LatestTaskPacket(ctx context.Context, projectID string, taskID int64, packetType string, role string) (*Message, error) {
	message, err := scanMessage(s.pool.QueryRow(ctx, latestTaskPacketSQL, projectID, taskID, emptyToNil(packetType), emptyToNil(role)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(0)
	}
	if err != nil {
		return nil, fmt.Errorf("getting latest task packet: %w", err)
	}
	return message, nil
}

func (s *Store) LatestCompletion(ctx context.Context, projectID string, taskID *int64, role string, runID string) (*Message, error) {
	message, err := scanMessage(s.pool.QueryRow(ctx, latestCompletionSQL, projectID, taskID, emptyToNil(role), emptyToNil(runID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(0)
	}
	if err != nil {
		return nil, fmt.Errorf("getting latest completion: %w", err)
	}
	return message, nil
}

func scanMessages(rows pgx.Rows) ([]*Message, error) {
	var messages []*Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning messages: %w", err)
	}
	return messages, nil
}

type messageScanner interface {
	Scan(dest ...any) error
}

func scanMessage(row messageScanner) (*Message, error) {
	var params NewMessageParams
	var metadata []byte
	if err := row.Scan(&params.ID, &params.ProjectID, &params.TaskID, &params.ThreadID, &params.Sender, &params.Content, &params.Intent, &metadata, &params.CreatedAt); err != nil {
		return nil, err
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &params.Metadata); err != nil {
			return nil, fmt.Errorf("decoding message metadata: %w", err)
		}
	}
	message, err := NewMessage(params)
	if err != nil {
		return nil, err
	}
	return message, nil
}

func scanNotification(row pgx.Row) (*Message, string, *bool, error) {
	var params NewMessageParams
	var metadata []byte
	var urgency string
	var isRead *bool
	if err := row.Scan(&params.ID, &params.ProjectID, &params.TaskID, &params.ThreadID, &params.Sender, &params.Content, &params.Intent, &metadata, &params.CreatedAt, &urgency, &isRead); err != nil {
		return nil, "", nil, err
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &params.Metadata); err != nil {
			return nil, "", nil, fmt.Errorf("decoding notification metadata: %w", err)
		}
	}
	message, err := NewMessage(params)
	if err != nil {
		return nil, "", nil, err
	}
	return message, urgency, isRead, nil
}

func jsonOrNil(value map[string]any) any {
	if len(value) == 0 {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

const messageColumns = `id, project_id, task_id, thread_id, sender, content, intent, metadata, created_at`

const createMessageSQL = `
insert into den_messages.messages(project_id, task_id, thread_id, sender, content, intent, metadata, created_at)
values ($1, $2, $3, $4, $5, $6, $7, $8)
returning ` + messageColumns

const getMessageSQL = `select ` + messageColumns + ` from den_messages.messages where id = $1`

const listMessagesSQL = `
select ` + messageColumns + `
from den_messages.messages m
where m.project_id = $1
  and ($2::bigint is null or m.task_id = $2)
  and ($3::timestamptz is null or m.created_at > $3)
  and ($4::text is null or (m.sender <> $4 and not exists (select 1 from den_messages.message_reads r where r.message_id = m.id and r.agent = $4)))
  and ($5::text is null or m.intent = $5)
order by m.created_at desc, m.id desc
limit $6`

const unreadAfterCursorSQL = `
select ` + messageColumns + `
from den_messages.messages m
where m.project_id = $1
  and m.id > $3
  and m.sender <> $2
  and not exists (select 1 from den_messages.message_reads r where r.message_id = m.id and r.agent = $2)
order by m.created_at desc, m.id desc
limit $4`

const threadRepliesSQL = `
select ` + messageColumns + `
from den_messages.messages
where thread_id = $1
order by created_at asc, id asc`

const markReadSQL = `
insert into den_messages.message_reads(message_id, agent)
select unnest($2::bigint[]), $1
on conflict (message_id, agent) do nothing`

const listNotificationsSQL = `
select ` + messageColumns + `,
       coalesce(metadata->>'urgency', 'normal') as urgency,
       case when $6::text is null then null::boolean else exists (select 1 from den_messages.message_reads r where r.message_id = m.id and r.agent = $6) end as is_read
from den_messages.messages m
where m.intent = 'notification'
  and ($1::text is null or m.project_id = $1)
  and ($2::bigint is null or m.task_id = $2)
  and ($3::text is null or m.sender = $3)
  and ($4::text is null or m.metadata->>'type' = $4)
  and ($5::text is null or coalesce(m.metadata->>'urgency', 'normal') = $5)
  and ($7::boolean = false or (exists (select 1 from den_messages.message_reads r where r.message_id = m.id and r.agent = $6)) = $8)
order by m.created_at desc, m.id desc
limit $9 offset $10`

const markNotificationsReadSQL = `
insert into den_messages.message_reads(message_id, agent)
select id, $1
from den_messages.messages
where intent = 'notification' and id = any($2::bigint[])
on conflict (message_id, agent) do nothing`

const markAllNotificationsReadSQL = `
insert into den_messages.message_reads(message_id, agent)
select id, $1
from den_messages.messages
where intent = 'notification'
  and project_id = $2
  and ($3::bigint is null or task_id = $3)
on conflict (message_id, agent) do nothing`

const latestTaskPacketSQL = `
select ` + messageColumns + `
from den_messages.messages
where project_id = $1
  and task_id = $2
  and metadata->>'schema' = 'den_worker_packet'
  and ($3::text is null or metadata->>'type' = $3 or metadata->>'packet_kind' = $3)
  and ($4::text is null or metadata->>'role' = $4)
order by created_at desc, id desc
limit 1`

const latestCompletionSQL = `
select ` + messageColumns + `
from den_messages.messages
where project_id = $1
  and ($2::bigint is null or task_id = $2)
  and (metadata->>'schema' = 'den_worker_completion' or metadata->>'completion_packet' = 'true')
  and ($3::text is null or metadata->>'role' = $3)
  and ($4::text is null or metadata->>'run_id' = $4 or metadata->>'session_id' = $4)
order by created_at desc, id desc
limit 1`
