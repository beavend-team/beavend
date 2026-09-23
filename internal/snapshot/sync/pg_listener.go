package sync

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// PostgresListener listens for PostgreSQL NOTIFY events on channel cortexbase_revisions.
type PostgresListener struct {
	databaseURL string
	handler     func(ctx context.Context, event RevisionEvent) error
	logger      *slog.Logger
}

// NewPostgresListener creates a new PostgreSQL LISTEN listener.
func NewPostgresListener(
	databaseURL string,
	handler func(ctx context.Context, event RevisionEvent) error,
	logger *slog.Logger,
) *PostgresListener {
	if logger == nil {
		logger = slog.Default()
	}
	return &PostgresListener{
		databaseURL: databaseURL,
		handler:     handler,
		logger:      logger,
	}
}

// ListenAndServe connects to PostgreSQL, executes LISTEN, and handles incoming notifications.
// It automatically reconnects with backoff if disconnected until ctx is canceled.
func (l *PostgresListener) ListenAndServe(ctx context.Context) error {
	backoff := 500 * time.Millisecond
	maxBackoff := 10 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err := pgx.Connect(ctx, l.databaseURL)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			l.logger.Warn("postgres listener failed to connect, retrying", "error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// Connected successfully, reset backoff
		backoff = 500 * time.Millisecond

		err = l.listenLoop(ctx, conn)
		_ = conn.Close(context.Background())

		if errors.Is(err, context.Canceled) {
			return err
		}
		l.logger.Warn("postgres listener connection lost, reconnecting", "error", err)
	}
}

func (l *PostgresListener) listenLoop(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, "LISTEN "+PostgresNotificationChannel)
	if err != nil {
		return err
	}

	l.logger.Info("listening on postgres notifications", "channel", PostgresNotificationChannel)

	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}

		var event RevisionEvent
		if err := json.Unmarshal([]byte(notification.Payload), &event); err != nil {
			l.logger.Error("failed to unmarshal postgres notification payload",
				"channel", notification.Channel,
				"payload", notification.Payload,
				"error", err,
			)
			continue
		}

		if err := l.handler(ctx, event); err != nil {
			// Compilation/swap error logged in handler, continue listening
			continue
		}
	}
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
