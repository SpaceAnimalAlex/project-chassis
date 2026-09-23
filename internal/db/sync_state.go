package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SyncCheckpoint represents a stored mail ingestion delta token or high-water mark.
type SyncCheckpoint struct {
	ID         int64     `json:"id"`
	Provider   string    `json:"provider"`
	AccountID  string    `json:"account_id"`
	DeltaToken string    `json:"delta_token"`
	LastSyncAt time.Time `json:"last_sync_at"`
}

// GetCheckpoint retrieves the latest sync delta token for a provider and account.
func (r *Repository) GetCheckpoint(ctx context.Context, provider, accountID string) (string, error) {
	query := `
		SELECT delta_token
		FROM sync_checkpoints
		WHERE provider = ? AND account_id = ?;
	`
	var token string
	err := r.db.QueryRowContext(ctx, query, provider, accountID).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil // No checkpoint yet; full sync needed
	}
	if err != nil {
		return "", err
	}
	return token, nil
}

// SaveCheckpoint saves or updates the sync checkpoint within a transaction.
// This guarantees that delta token persistence is atomic with ingested messages.
func (r *Repository) SaveCheckpoint(ctx context.Context, tx *sql.Tx, provider, accountID, deltaToken string) error {
	query := `
		INSERT INTO sync_checkpoints (provider, account_id, delta_token, last_sync_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(provider, account_id) DO UPDATE SET
			delta_token = excluded.delta_token,
			last_sync_at = excluded.last_sync_at;
	`
	var err error
	if tx != nil {
		_, err = tx.ExecContext(ctx, query, provider, accountID, deltaToken)
	} else {
		_, err = r.db.ExecContext(ctx, query, provider, accountID, deltaToken)
	}
	return err
}
