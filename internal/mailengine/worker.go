package mailengine

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/db"
)

// SyncWorker manages background mail polling and transactional ingestion.
type SyncWorker struct {
	provider     MailProvider
	repo         *db.Repository
	threader     *Threader
	pollInterval time.Duration
	defaultPrefix string
	logger       *slog.Logger
	mu           sync.Mutex
	running      bool
}

// Config defines worker parameters.
type WorkerConfig struct {
	Provider      MailProvider
	Repository    *db.Repository
	PollInterval  time.Duration
	DefaultPrefix string // e.g. "HD" or "WO"
	Logger        *slog.Logger
}

// NewSyncWorker creates a new background mail sync worker.
func NewSyncWorker(cfg WorkerConfig) *SyncWorker {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.DefaultPrefix == "" {
		cfg.DefaultPrefix = "HD"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &SyncWorker{
		provider:      cfg.Provider,
		repo:          cfg.Repository,
		threader:      NewThreader(cfg.Repository.DB()),
		pollInterval:  cfg.PollInterval,
		defaultPrefix: cfg.DefaultPrefix,
		logger:        cfg.Logger,
	}
}

// Start runs the non-overlapping polling ticker until ctx is cancelled.
func (w *SyncWorker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	w.logger.Info("Starting mail sync worker",
		"provider", w.provider.Name(),
		"account", w.provider.AccountID(),
		"interval", w.pollInterval,
	)

	// Immediate first sync
	w.syncOnce(ctx)

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Stopping mail sync worker gracefully", "provider", w.provider.Name())
			return
		case <-ticker.C:
			w.syncOnce(ctx)
		}
	}
}

// SyncOnce performs a single non-overlapping sync cycle.
func (w *SyncWorker) SyncOnce(ctx context.Context) error {
	return w.syncOnce(ctx)
}

func (w *SyncWorker) syncOnce(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	providerName := w.provider.Name()
	accountID := w.provider.AccountID()

	// 1. Fetch current delta token from checkpoint table
	deltaToken, err := w.repo.GetCheckpoint(ctx, providerName, accountID)
	if err != nil {
		w.logger.Error("Failed to fetch checkpoint", "error", err)
		return err
	}

	// 2. Fetch new messages from provider
	messages, nextDeltaToken, err := w.provider.FetchNewMessages(ctx, deltaToken)
	if err != nil {
		w.logger.Error("Failed to fetch messages from provider", "provider", providerName, "error", err)
		return err
	}

	if len(messages) == 0 {
		return nil
	}

	w.logger.Info("Fetched new messages for ingestion", "count", len(messages), "provider", providerName)

	// 3. Process each message atomically
	for _, msg := range messages {
		if err := w.ingestMessage(ctx, msg); err != nil {
			w.logger.Error("Failed to ingest inbound message",
				"message_id", msg.ExternalMessageID,
				"error", err,
			)
			// Continue processing remaining messages
		}
	}

	// 4. Update checkpoint atomically
	if nextDeltaToken != "" && nextDeltaToken != deltaToken {
		if err := w.repo.SaveCheckpoint(ctx, nil, providerName, accountID, nextDeltaToken); err != nil {
			w.logger.Error("Failed to save sync checkpoint", "error", err)
			return err
		}
	}

	return nil
}

func (w *SyncWorker) ingestMessage(ctx context.Context, msg InboundMessage) error {
	// First-pass deduplication: if external_message_id was already ingested, skip entirely
	if msg.ExternalMessageID != "" {
		var exists int
		err := w.repo.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM thread_messages WHERE external_message_id = ?", msg.ExternalMessageID).Scan(&exists)
		if err == nil && exists > 0 {
			w.logger.Debug("Message already ingested, skipping", "message_id", msg.ExternalMessageID)
			return nil
		}
	}

	body := msg.BodyText
	if strings.TrimSpace(body) == "" {
		body = msg.BodyHTML
	}
	if strings.TrimSpace(body) == "" {
		body = "(No content)"
	}

	// Run correlation heuristic
	corr, err := w.threader.Correlate(ctx, msg)
	if err != nil {
		return fmt.Errorf("correlation failed: %w", err)
	}

	return w.repo.DB().WithTx(ctx, func(tx *sql.Tx) error {
		var workItemID int64

		if corr.Stage == "NEW_ITEM" {
			// Generate new item code, e.g. HD-1001
			code, err := w.generateItemCode(ctx, tx, w.defaultPrefix)
			if err != nil {
				return fmt.Errorf("failed to generate item code: %w", err)
			}

			senderName := msg.SenderName
			if senderName == "" {
				senderName = msg.SenderEmail
			}

			insertItem := `
				INSERT INTO work_items (item_code, domain_type, requester_name, requester_email, status, priority, subject, summary)
				VALUES (?, 'IT', ?, ?, 'NEW', 'NORMAL', ?, ?);
			`
			res, err := tx.ExecContext(ctx, insertItem, code, senderName, msg.SenderEmail, msg.Subject, body)
			if err != nil {
				return fmt.Errorf("failed to create work item: %w", err)
			}
			workItemID, _ = res.LastInsertId()
			corr.ItemCode = code
		} else {
			workItemID = corr.WorkItemID
		}

		// Idempotent insertion of message
		insertMsg := `
			INSERT INTO thread_messages (work_item_id, author_user_id, sender_email, body, is_internal, external_message_id)
			VALUES (?, NULL, ?, ?, 0, ?)
			ON CONFLICT(external_message_id) DO NOTHING;
		`
		res, err := tx.ExecContext(ctx, insertMsg, workItemID, msg.SenderEmail, body, msg.ExternalMessageID)
		if err != nil {
			return fmt.Errorf("failed to insert thread message: %w", err)
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			// Duplicate message already ingested
			return nil
		}

		// If existing ticket was in PENDING_USER or RESOLVED, reopen to OPEN
		if corr.Stage != "NEW_ITEM" {
			_, err = tx.ExecContext(ctx, `
				UPDATE work_items 
				SET status = 'OPEN', updated_at = CURRENT_TIMESTAMP 
				WHERE id = ? AND status IN ('PENDING_USER', 'RESOLVED');
			`, workItemID)
			if err != nil {
				return err
			}
		}

		// Record audit log with exact correlation stage for zero-guess troubleshooting
		auditDetail := core.MailCorrelationDetail{
			Stage:             corr.Stage,
			ExternalMessageID: msg.ExternalMessageID,
			MatchedHeader:     corr.HeaderValue,
			SubjectMatched:    corr.SubjectValue,
		}
		insertAudit := `
			INSERT INTO audit_logs (work_item_id, actor_id, action, details_json)
			VALUES (?, NULL, ?, ?);
		`
		_, err = tx.ExecContext(ctx, insertAudit, workItemID, core.ActionMailCorrelated, core.DetailsToJSON(auditDetail))
		return err
	})
}

func (w *SyncWorker) generateItemCode(ctx context.Context, tx *sql.Tx, prefix string) (string, error) {
	// Look up the highest existing numeric suffix for this prefix
	query := `SELECT COUNT(id) FROM work_items WHERE item_code LIKE ?;`
	var count int
	err := tx.QueryRowContext(ctx, query, prefix+"-%").Scan(&count)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d", prefix, count+1001), nil
}
