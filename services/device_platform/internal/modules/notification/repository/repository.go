// Package repository persists notifications and their per-recipient delivery
// state.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound reports a missing notification row.
var ErrNotFound = errors.New("notification repository: not found")

// Repository defines notification persistence operations.
type Repository interface {
	// Create stores one notification and its initial delivery rows in a single
	// transaction so a fan-out is never partially visible.
	Create(ctx context.Context, notification *domain.Notification, deliveries []*domain.Delivery) error
	Get(ctx context.Context, notificationID string) (*domain.Notification, error)
	// ListInbox returns one guardian's visible notifications newest first. The
	// cursor is a notification id to page after, and now excludes expired rows.
	ListInbox(
		ctx context.Context,
		parentAccountID string,
		now time.Time,
		limit int,
		cursor string,
		category string,
	) ([]domain.InboxItem, error)
	CountUnread(ctx context.Context, parentAccountID string, now time.Time) (int, error)
	// MarkRead flips one delivery to read. It is scoped by guardian so a
	// notification cannot be marked read across accounts.
	MarkRead(ctx context.Context, notificationID string, parentAccountID string, now time.Time) error
	MarkAllRead(ctx context.Context, parentAccountID string, now time.Time) error
	Delete(ctx context.Context, notificationID string) error
	// ListAdmin returns a filtered, paged operator projection plus the total.
	ListAdmin(ctx context.Context, filter domain.AdminFilter) ([]domain.AdminItem, int, error)
	// ActiveParentIDs returns the recipient set for a broadcast.
	ActiveParentIDs(ctx context.Context) ([]string, error)
	// ParentOwnsDevice reports whether the guardian has the device bound.
	ParentOwnsDevice(ctx context.Context, parentAccountID string, deviceID string) (bool, error)
	// RecordDeliveryCommand links a device delivery row to the device command
	// that carries the message.
	RecordDeliveryCommand(
		ctx context.Context,
		notificationID string,
		parentAccountID string,
		commandID string,
	) error
	// UpdateDeliveryStatus is used by the delivery worker and read receipts.
	UpdateDeliveryStatus(
		ctx context.Context,
		notificationID string,
		parentAccountID string,
		channel string,
		status string,
		failureCode string,
		now time.Time,
	) error
	// PendingDeliveries returns queued or failed deliveries for one channel.
	PendingDeliveries(
		ctx context.Context,
		channel string,
		limit int,
	) ([]*domain.Delivery, error)
}

// PostgresRepository is the PostgreSQL-backed notification repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a notification repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create stores a notification and its delivery rows atomically.
func (r *PostgresRepository) Create(
	ctx context.Context,
	notification *domain.Notification,
	deliveries []*domain.Delivery,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin notification transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO notifications (
			id,
			category,
			severity,
			title,
			body,
			action_path,
			action_label,
			audience,
			parent_account_id,
			device_id,
			display_duration_seconds,
			channels,
			created_by,
			source,
			publish_at,
			expires_at,
			created_at,
			updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			NULLIF($9, '')::uuid, $10, $11, $12,
			NULLIF($13, '')::uuid, $14, $15, $16, $17, $18
		)
	`,
		notification.ID,
		notification.Category,
		notification.Severity,
		notification.Title,
		notification.Body,
		notification.ActionPath,
		notification.ActionLabel,
		notification.Audience,
		notification.ParentAccountID,
		notification.DeviceID,
		notification.DisplayDurationSeconds,
		notification.Channels,
		notification.CreatedBy,
		notification.Source,
		notification.PublishAt,
		notification.ExpiresAt,
		notification.CreatedAt,
		notification.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}

	for _, delivery := range deliveries {
		_, err = tx.Exec(ctx, `
			INSERT INTO notification_deliveries (
				id,
				notification_id,
				parent_account_id,
				channel,
				status,
				command_id,
				created_at,
				updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (notification_id, parent_account_id, channel)
			DO NOTHING
		`,
			delivery.ID,
			delivery.NotificationID,
			delivery.ParentAccountID,
			delivery.Channel,
			delivery.Status,
			delivery.CommandID,
			delivery.CreatedAt,
			delivery.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert notification delivery: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit notification transaction: %w", err)
	}
	return nil
}

// Get returns one notification by id.
func (r *PostgresRepository) Get(
	ctx context.Context,
	notificationID string,
) (*domain.Notification, error) {
	row := r.pool.QueryRow(ctx, notificationSelect+` WHERE id = $1`, notificationID)
	notification, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return notification, nil
}

const notificationSelect = `
	SELECT
		id,
		category,
		severity,
		title,
		body,
		action_path,
		action_label,
		audience,
		COALESCE(parent_account_id::text, ''),
		device_id,
		display_duration_seconds,
		channels,
		source,
		COALESCE(created_by::text, ''),
		publish_at,
		expires_at,
		created_at,
		updated_at
	FROM notifications
`

// ListInbox returns one guardian's visible notifications.
func (r *PostgresRepository) ListInbox(
	ctx context.Context,
	parentAccountID string,
	now time.Time,
	limit int,
	cursor string,
	category string,
) ([]domain.InboxItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			n.id,
			n.category,
			n.severity,
			n.title,
			n.body,
			n.action_path,
			n.action_label,
			n.device_id,
			n.channels,
			n.publish_at,
			n.expires_at,
			d.status,
			d.read_at
		FROM notification_deliveries AS d
		JOIN notifications AS n ON n.id = d.notification_id
		WHERE d.parent_account_id = $1
		  AND d.channel = 'in_app'
		  AND d.status <> 'expired'
		  AND n.publish_at <= $2
		  AND (n.expires_at IS NULL OR n.expires_at > $2)
		  AND ($3 = '' OR n.category = $3)
		  AND (
		    $4 = ''
		    OR (n.created_at, n.id)
		       < (
		         SELECT anchor.created_at, anchor.id
		         FROM notifications AS anchor
		         WHERE anchor.id::text = $4
		       )
		  )
		ORDER BY n.created_at DESC, n.id DESC
		LIMIT $5
	`, parentAccountID, now, category, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list notification inbox: %w", err)
	}
	defer rows.Close()

	items := make([]domain.InboxItem, 0, limit)
	for rows.Next() {
		var item domain.InboxItem
		var channels []string
		var status string
		var readAt *time.Time
		if err := rows.Scan(
			&item.ID,
			&item.Category,
			&item.Severity,
			&item.Title,
			&item.Body,
			&item.ActionPath,
			&item.ActionLabel,
			&item.DeviceID,
			&channels,
			&item.PublishAt,
			&item.ExpiresAt,
			&status,
			&readAt,
		); err != nil {
			return nil, fmt.Errorf("scan notification inbox row: %w", err)
		}
		item.Channels = channels
		if item.Channels == nil {
			item.Channels = []string{}
		}
		item.Read = status == domain.DeliveryStatusRead
		item.ReadAt = readAt
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification inbox: %w", err)
	}
	return items, nil
}

// CountUnread returns the bounded unread badge value.
func (r *PostgresRepository) CountUnread(
	ctx context.Context,
	parentAccountID string,
	now time.Time,
) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM notification_deliveries AS d
		JOIN notifications AS n ON n.id = d.notification_id
		WHERE d.parent_account_id = $1
		  AND d.channel = 'in_app'
		  AND d.status = 'delivered'
		  AND n.publish_at <= $2
		  AND (n.expires_at IS NULL OR n.expires_at > $2)
	`, parentAccountID, now).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	if count > domain.MaxUnreadCount {
		count = domain.MaxUnreadCount
	}
	return count, nil
}

// MarkRead flips one delivery to read for one guardian.
func (r *PostgresRepository) MarkRead(
	ctx context.Context,
	notificationID string,
	parentAccountID string,
	now time.Time,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notification_deliveries
		SET status = 'read',
		    read_at = COALESCE(read_at, $3),
		    updated_at = $3
		WHERE notification_id = $1
		  AND parent_account_id = $2
		  AND channel = 'in_app'
	`, notificationID, parentAccountID, now)
	if err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkAllRead marks every unread inbox row for one guardian.
func (r *PostgresRepository) MarkAllRead(
	ctx context.Context,
	parentAccountID string,
	now time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE notification_deliveries
		SET status = 'read',
		    read_at = COALESCE(read_at, $2),
		    updated_at = $2
		WHERE parent_account_id = $1
		  AND channel = 'in_app'
		  AND status = 'delivered'
	`, parentAccountID, now)
	if err != nil {
		return fmt.Errorf("mark all notifications read: %w", err)
	}
	return nil
}

// Delete removes one notification; cascading removes its deliveries.
func (r *PostgresRepository) Delete(ctx context.Context, notificationID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM notifications WHERE id = $1`, notificationID)
	if err != nil {
		return fmt.Errorf("delete notification: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAdmin returns a filtered, paged operator projection.
func (r *PostgresRepository) ListAdmin(
	ctx context.Context,
	filter domain.AdminFilter,
) ([]domain.AdminItem, int, error) {
	page, pageSize := domain.ClampPage(filter.Page, filter.PageSize)
	offset := (page - 1) * pageSize
	pattern := "%" + filter.Query + "%"

	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM notifications
		WHERE ($1 = '' OR category = $1)
		  AND ($2 = '' OR audience = $2)
		  AND ($3 = '' OR source = $3)
		  AND ($4 = '' OR title ILIKE $5 OR body ILIKE $5)
	`, filter.Category, filter.Audience, filter.Source, filter.Query, pattern).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count notifications: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT
			n.id,
			n.category,
			n.severity,
			n.title,
			n.body,
			n.action_path,
			n.action_label,
			n.audience,
			COALESCE(n.parent_account_id::text, ''),
			n.device_id,
			n.display_duration_seconds,
			n.channels,
			n.source,
			COALESCE(n.created_by::text, ''),
			n.publish_at,
			n.expires_at,
			n.created_at,
			n.updated_at,
			COUNT(d.id)::int AS delivery_total,
			COUNT(d.id) FILTER (WHERE d.status = 'pending')::int AS delivery_pending,
			COUNT(d.id) FILTER (WHERE d.status = 'delivered')::int AS delivery_delivered,
			COUNT(d.id) FILTER (WHERE d.status = 'read')::int AS delivery_read,
			COUNT(d.id) FILTER (WHERE d.status = 'failed')::int AS delivery_failed
		FROM notifications AS n
		LEFT JOIN notification_deliveries AS d ON d.notification_id = n.id
		WHERE ($1 = '' OR n.category = $1)
		  AND ($2 = '' OR n.audience = $2)
		  AND ($3 = '' OR n.source = $3)
		  AND ($4 = '' OR n.title ILIKE $5 OR n.body ILIKE $5)
		GROUP BY n.id
		ORDER BY n.created_at DESC, n.id DESC
		LIMIT $6 OFFSET $7
	`, filter.Category, filter.Audience, filter.Source, filter.Query, pattern, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()

	items := make([]domain.AdminItem, 0, pageSize)
	for rows.Next() {
		item, err := scanAdminItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate notifications: %w", err)
	}
	return items, total, nil
}

// ActiveParentIDs returns the broadcast recipient set.
func (r *PostgresRepository) ActiveParentIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text
		FROM parent_accounts
		WHERE status = 'active'
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list active parents: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan active parent: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active parents: %w", err)
	}
	return ids, nil
}

// ParentOwnsDevice reports whether a guardian has the device bound.
func (r *PostgresRepository) ParentOwnsDevice(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM device_bindings
			WHERE parent_account_id = $1
			  AND device_id = $2
		)
	`, parentAccountID, deviceID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check device ownership: %w", err)
	}
	return exists, nil
}

// RecordDeliveryCommand links a device delivery to its command id.
func (r *PostgresRepository) RecordDeliveryCommand(
	ctx context.Context,
	notificationID string,
	parentAccountID string,
	commandID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notification_deliveries
		SET command_id = $3,
		    updated_at = NOW()
		WHERE notification_id = $1
		  AND parent_account_id = $2
		  AND channel = 'device'
	`, notificationID, parentAccountID, commandID)
	if err != nil {
		return fmt.Errorf("record delivery command: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateDeliveryStatus records one delivery transition.
func (r *PostgresRepository) UpdateDeliveryStatus(
	ctx context.Context,
	notificationID string,
	parentAccountID string,
	channel string,
	status string,
	failureCode string,
	now time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE notification_deliveries
		SET status = $4,
		    delivered_at = CASE
		      WHEN $4 IN ('delivered', 'read') THEN COALESCE(delivered_at, $6)
		      ELSE delivered_at
		    END,
		    read_at = CASE
		      WHEN $4 = 'read' THEN COALESCE(read_at, $6)
		      ELSE read_at
		    END,
		    failure_code = $5,
		    retry_count = CASE
		      WHEN $4 = 'failed' THEN LEAST(retry_count + 1, 20)
		      ELSE retry_count
		    END,
		    updated_at = $6
		WHERE notification_id = $1
		  AND parent_account_id = $2
		  AND channel = $3
	`, notificationID, parentAccountID, channel, status, failureCode, now)
	if err != nil {
		return fmt.Errorf("update delivery status: %w", err)
	}
	return nil
}

// PendingDeliveries returns queued or failed deliveries for one channel.
func (r *PostgresRepository) PendingDeliveries(
	ctx context.Context,
	channel string,
	limit int,
) ([]*domain.Delivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			notification_id,
			parent_account_id,
			channel,
			status,
			read_at,
			delivered_at,
			failure_code,
			retry_count,
			command_id,
			created_at,
			updated_at
		FROM notification_deliveries
		WHERE channel = $1
		  AND status IN ('pending', 'failed')
		  AND retry_count < 20
		ORDER BY created_at ASC
		LIMIT $2
	`, channel, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]*domain.Delivery, 0, limit)
	for rows.Next() {
		delivery, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending deliveries: %w", err)
	}
	return deliveries, nil
}

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanNotification(row rowScanner) (*domain.Notification, error) {
	var notification domain.Notification
	if err := row.Scan(
		&notification.ID,
		&notification.Category,
		&notification.Severity,
		&notification.Title,
		&notification.Body,
		&notification.ActionPath,
		&notification.ActionLabel,
		&notification.Audience,
		&notification.ParentAccountID,
		&notification.DeviceID,
		&notification.DisplayDurationSeconds,
		&notification.Channels,
		&notification.Source,
		&notification.CreatedBy,
		&notification.PublishAt,
		&notification.ExpiresAt,
		&notification.CreatedAt,
		&notification.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if notification.Channels == nil {
		notification.Channels = []string{}
	}
	return &notification, nil
}

func scanAdminItem(row rowScanner) (*domain.AdminItem, error) {
	var item domain.AdminItem
	if err := row.Scan(
		&item.ID,
		&item.Category,
		&item.Severity,
		&item.Title,
		&item.Body,
		&item.ActionPath,
		&item.ActionLabel,
		&item.Audience,
		&item.ParentAccountID,
		&item.DeviceID,
		&item.DisplayDurationSeconds,
		&item.Channels,
		&item.Source,
		&item.CreatedBy,
		&item.PublishAt,
		&item.ExpiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.DeliveryTotal,
		&item.DeliveryPending,
		&item.DeliveryDelivered,
		&item.DeliveryRead,
		&item.DeliveryFailed,
	); err != nil {
		return nil, fmt.Errorf("scan notification admin row: %w", err)
	}
	if item.Channels == nil {
		item.Channels = []string{}
	}
	return &item, nil
}

func scanDelivery(row rowScanner) (*domain.Delivery, error) {
	var delivery domain.Delivery
	if err := row.Scan(
		&delivery.ID,
		&delivery.NotificationID,
		&delivery.ParentAccountID,
		&delivery.Channel,
		&delivery.Status,
		&delivery.ReadAt,
		&delivery.DeliveredAt,
		&delivery.FailureCode,
		&delivery.RetryCount,
		&delivery.CommandID,
		&delivery.CreatedAt,
		&delivery.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan notification delivery: %w", err)
	}
	return &delivery, nil
}

var _ Repository = (*PostgresRepository)(nil)
