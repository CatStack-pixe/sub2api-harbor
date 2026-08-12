package securityaudit

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultIPNoticeTTLHours = 24
	MaxIPNoticeRunes        = 1000
)

var ErrEventIPUnavailable = errors.New("prompt audit event has no usable client IP")

type IPNotice struct {
	ID                 int64      `json:"id"`
	SourceEventID      *int64     `json:"source_event_id,omitempty"`
	ClientIP           string     `json:"client_ip"`
	Message            string     `json:"message"`
	CreatedBy          int64      `json:"created_by"`
	Status             string     `json:"status"`
	DeliveredRequestID string     `json:"delivered_request_id,omitempty"`
	ExpiresAt          time.Time  `json:"expires_at"`
	DeliveredAt        *time.Time `json:"delivered_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type QueueIPNoticeRequest struct {
	Message string `json:"message" binding:"required"`
}

func normalizeNoticeIP(value string) string {
	parsed := net.ParseIP(strings.TrimSpace(value))
	if parsed == nil {
		return ""
	}
	return parsed.String()
}

func normalizeNoticeMessage(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsRune(value, '\x00') || !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxIPNoticeRunes {
		return "", errors.New("IP notice message must contain 1-1000 valid Unicode characters")
	}
	return value, nil
}

func (r *PostgreSQLRepository) QueueIPNotice(ctx context.Context, eventID, adminID int64, message string) (*IPNotice, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("prompt audit database unavailable")
	}
	message, err := normalizeNoticeMessage(message)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var storedIP string
	if err := tx.QueryRowContext(ctx, `SELECT client_ip FROM prompt_audit_events WHERE id=$1`, eventID).Scan(&storedIP); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}
	clientIP := normalizeNoticeIP(storedIP)
	if clientIP == "" {
		return nil, ErrEventIPUnavailable
	}
	now := r.clock.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM prompt_audit_ip_notices
		WHERE status <> 'pending' AND updated_at < $1`, now.Add(-30*24*time.Hour)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE prompt_audit_ip_notices SET status='expired', updated_at=$2
		WHERE client_ip=$1 AND status='pending' AND expires_at <= $2`, clientIP, now); err != nil {
		return nil, err
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO prompt_audit_ip_notices (
			source_event_id,client_ip,message,created_by,status,expires_at,created_at,updated_at
		) VALUES ($1,$2,$3,$4,'pending',$5,$6,$6)
		ON CONFLICT (client_ip) WHERE status='pending' DO UPDATE SET
			source_event_id=EXCLUDED.source_event_id,
			message=EXCLUDED.message,
			created_by=EXCLUDED.created_by,
			expires_at=EXCLUDED.expires_at,
			created_at=EXCLUDED.created_at,
			updated_at=EXCLUDED.updated_at
		RETURNING id,source_event_id,client_ip,message,created_by,status,delivered_request_id,
			expires_at,delivered_at,created_at,updated_at`,
		eventID, clientIP, message, nullableID(adminID), now.Add(DefaultIPNoticeTTLHours*time.Hour), now)
	notice, err := scanIPNotice(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return notice, nil
}

// ConsumeIPNotice atomically claims at most one pending notice. Database
// failures are returned to the coordinator, which deliberately fails open.
func (r *PostgreSQLRepository) ConsumeIPNotice(ctx context.Context, clientIP, requestID string) (*IPNotice, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("prompt audit database unavailable")
	}
	clientIP = normalizeNoticeIP(clientIP)
	if clientIP == "" {
		return nil, nil
	}
	now := r.clock.Now().UTC()
	row := r.db.QueryRowContext(ctx, `
		WITH expired AS (
			UPDATE prompt_audit_ip_notices SET status='expired',updated_at=$3
			WHERE client_ip=$1 AND status='pending' AND expires_at <= $3
			RETURNING id
		), candidate AS (
			SELECT id FROM prompt_audit_ip_notices
			WHERE client_ip=$1 AND status='pending' AND expires_at > $3
			ORDER BY created_at,id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE prompt_audit_ip_notices AS notice
		SET status='delivered',delivered_request_id=$2,delivered_at=$3,updated_at=$3
		FROM candidate WHERE notice.id=candidate.id
		RETURNING notice.id,notice.source_event_id,notice.client_ip,notice.message,notice.created_by,
			notice.status,notice.delivered_request_id,notice.expires_at,notice.delivered_at,
			notice.created_at,notice.updated_at`, clientIP, TrimRunes(strings.TrimSpace(requestID), 128), now)
	notice, err := scanIPNotice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return notice, nil
}

func scanIPNotice(row rowScanner) (*IPNotice, error) {
	notice := &IPNotice{}
	var sourceEventID, createdBy sql.NullInt64
	var deliveredAt sql.NullTime
	if err := row.Scan(&notice.ID, &sourceEventID, &notice.ClientIP, &notice.Message, &createdBy,
		&notice.Status, &notice.DeliveredRequestID, &notice.ExpiresAt, &deliveredAt,
		&notice.CreatedAt, &notice.UpdatedAt); err != nil {
		return nil, err
	}
	if sourceEventID.Valid {
		value := sourceEventID.Int64
		notice.SourceEventID = &value
	}
	if createdBy.Valid {
		notice.CreatedBy = createdBy.Int64
	}
	if deliveredAt.Valid {
		value := deliveredAt.Time
		notice.DeliveredAt = &value
	}
	return notice, nil
}
