package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/project-chassis/chassis/internal/core"
)

// Repository provides thread-safe, transactional SQLite persistence for Chassis.
type Repository struct {
	db *DB
}

// NewRepository creates a new database repository.
func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// DB returns the underlying *DB wrapper.
func (r *Repository) DB() *DB {
	return r.db
}

// Ensure Repository implements core.WorkItemService
var _ core.WorkItemService = (*Repository)(nil)

// ListQueue queries work items matching the given filter.
func (r *Repository) ListQueue(ctx context.Context, filter core.QueueFilter) ([]core.WorkItemSummary, error) {
	var conditions []string
	var args []any

	if filter.DomainType != nil {
		conditions = append(conditions, "w.domain_type = ?")
		args = append(args, *filter.DomainType)
	}
	if filter.Status != nil {
		conditions = append(conditions, "w.status = ?")
		args = append(args, *filter.Status)
	}
	if filter.AssignedUserID != nil {
		conditions = append(conditions, "w.assigned_user_id = ?")
		args = append(args, *filter.AssignedUserID)
	}
	if filter.Priority != nil {
		conditions = append(conditions, "w.priority = ?")
		args = append(args, *filter.Priority)
	}
	if filter.ContactID != nil {
		conditions = append(conditions, "w.contact_id = ?")
		args = append(args, *filter.ContactID)
	}
	if filter.OrganizationID != nil {
		conditions = append(conditions, "w.organization_id = ?")
		args = append(args, *filter.OrganizationID)
	}
	if filter.SearchQuery != "" {
		like := "%" + filter.SearchQuery + "%"
		conditions = append(conditions, "(w.item_code LIKE ? OR w.subject LIKE ? OR w.requester_name LIKE ? OR w.requester_email LIKE ?)")
		args = append(args, like, like, like, like)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := max(0, filter.Offset)

	query := fmt.Sprintf(`
		SELECT 
			w.id, w.item_code, w.domain_type, w.requester_name, w.requester_email,
			w.assigned_user_id, COALESCE(u.full_name, ''),
			w.contact_id, w.organization_id, COALESCE(o.name, ''),
			w.status, w.priority, w.subject,
			(SELECT COUNT(*) FROM thread_messages tm WHERE tm.work_item_id = w.id) AS msg_count,
			w.created_at, w.updated_at
		FROM work_items w
		LEFT JOIN users u ON w.assigned_user_id = u.id
		LEFT JOIN organizations o ON w.organization_id = o.id
		%s
		ORDER BY 
			CASE w.priority 
				WHEN 'CRITICAL' THEN 1 
				WHEN 'HIGH' THEN 2 
				WHEN 'NORMAL' THEN 3 
				WHEN 'LOW' THEN 4 
				ELSE 5 
			END,
			w.updated_at DESC
		LIMIT ? OFFSET ?;
	`, whereClause)

	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list work items: %w", err)
	}
	defer rows.Close()

	var results []core.WorkItemSummary
	for rows.Next() {
		var s core.WorkItemSummary
		var requesterName sql.NullString
		var assignedID, contactID, orgID sql.NullInt64
		var assignedName, orgName string
		var createdAtStr, updatedAtStr string

		err := rows.Scan(
			&s.ID, &s.ItemCode, &s.DomainType, &requesterName, &s.RequesterEmail,
			&assignedID, &assignedName,
			&contactID, &orgID, &orgName,
			&s.Status, &s.Priority, &s.Subject,
			&s.MessageCount,
			&createdAtStr, &updatedAtStr,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan work item row: %w", err)
		}

		if requesterName.Valid {
			s.RequesterName = requesterName.String
		}
		if assignedID.Valid {
			s.AssignedUserID = &assignedID.Int64
		}
		s.AssignedName = assignedName
		if contactID.Valid {
			s.ContactID = &contactID.Int64
		}
		if orgID.Valid {
			s.OrganizationID = &orgID.Int64
		}
		s.OrganizationName = orgName
		s.CreatedAt = parseTime(createdAtStr)
		s.UpdatedAt = parseTime(updatedAtStr)

		results = append(results, s)
	}

	return results, rows.Err()
}

// GetItem retrieves a work item with its messages, linked assets, and audit history.
func (r *Repository) GetItem(ctx context.Context, id int64) (*core.WorkItemDetail, error) {
	return r.getItemByQuery(ctx, "w.id = ?", id)
}

// GetItemByCode retrieves a work item by its item code (e.g. "HD-1001").
func (r *Repository) GetItemByCode(ctx context.Context, code string) (*core.WorkItemDetail, error) {
	return r.getItemByQuery(ctx, "w.item_code = ?", code)
}

func (r *Repository) getItemByQuery(ctx context.Context, where string, arg any) (*core.WorkItemDetail, error) {
	query := fmt.Sprintf(`
		SELECT 
			w.id, w.item_code, w.domain_type, w.requester_name, w.requester_email,
			w.assigned_user_id, w.contact_id, w.organization_id,
			w.status, w.priority, w.subject, w.summary,
			w.created_at, w.updated_at, w.resolved_at
		FROM work_items w
		WHERE %s;
	`, where)

	var item core.WorkItem
	var reqName, summary, resolvedAtStr sql.NullString
	var assignedID, contactID, orgID sql.NullInt64
	var createdAtStr, updatedAtStr string

	err := r.db.QueryRowContext(ctx, query, arg).Scan(
		&item.ID, &item.ItemCode, &item.DomainType, &reqName, &item.RequesterEmail,
		&assignedID, &contactID, &orgID,
		&item.Status, &item.Priority, &item.Subject, &summary,
		&createdAtStr, &updatedAtStr, &resolvedAtStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, core.ErrItemNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query work item: %w", err)
	}

	if reqName.Valid {
		item.RequesterName = reqName.String
	}
	if summary.Valid {
		item.Summary = summary.String
	}
	if assignedID.Valid {
		item.AssignedUserID = &assignedID.Int64
	}
	if contactID.Valid {
		item.ContactID = &contactID.Int64
	}
	if orgID.Valid {
		item.OrganizationID = &orgID.Int64
	}
	item.CreatedAt = parseTime(createdAtStr)
	item.UpdatedAt = parseTime(updatedAtStr)
	if resolvedAtStr.Valid {
		t := parseTime(resolvedAtStr.String)
		item.ResolvedAt = &t
	}

	// Fetch thread messages
	messages, err := r.getThreadMessages(ctx, item.ID)
	if err != nil {
		return nil, err
	}

	// Fetch linked assets
	assets, err := r.getLinkedAssets(ctx, item.ID)
	if err != nil {
		return nil, err
	}

	// Fetch audit history
	auditLogs, err := r.getAuditLogs(ctx, item.ID)
	if err != nil {
		return nil, err
	}

	return &core.WorkItemDetail{
		Item:     item,
		Messages: messages,
		Assets:   assets,
		AuditLog: auditLogs,
	}, nil
}

func (r *Repository) getThreadMessages(ctx context.Context, itemID int64) ([]core.ThreadMessage, error) {
	query := `
		SELECT 
			tm.id, tm.work_item_id, tm.author_user_id, COALESCE(u.full_name, ''),
			tm.sender_email, tm.body, tm.is_internal, tm.external_message_id, tm.created_at
		FROM thread_messages tm
		LEFT JOIN users u ON tm.author_user_id = u.id
		WHERE tm.work_item_id = ?
		ORDER BY tm.created_at ASC;
	`
	rows, err := r.db.QueryContext(ctx, query, itemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query thread messages: %w", err)
	}
	defer rows.Close()

	var messages []core.ThreadMessage
	for rows.Next() {
		var m core.ThreadMessage
		var authorID sql.NullInt64
		var extID sql.NullString
		var createdAtStr string

		if err := rows.Scan(&m.ID, &m.WorkItemID, &authorID, &m.AuthorName, &m.SenderEmail, &m.Body, &m.IsInternal, &extID, &createdAtStr); err != nil {
			return nil, fmt.Errorf("failed to scan thread message: %w", err)
		}
		if authorID.Valid {
			m.AuthorUserID = &authorID.Int64
		}
		if extID.Valid {
			m.ExternalMessageID = &extID.String
		}
		m.CreatedAt = parseTime(createdAtStr)
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (r *Repository) getLinkedAssets(ctx context.Context, itemID int64) ([]core.Asset, error) {
	query := `
		SELECT a.id, a.domain_type, a.identifier, a.name, COALESCE(a.metadata_json, ''), a.created_at
		FROM assets a
		INNER JOIN work_item_assets wia ON a.id = wia.asset_id
		WHERE wia.work_item_id = ?
		ORDER BY a.identifier ASC;
	`
	rows, err := r.db.QueryContext(ctx, query, itemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query linked assets: %w", err)
	}
	defer rows.Close()

	var assets []core.Asset
	for rows.Next() {
		var a core.Asset
		var createdAtStr string
		if err := rows.Scan(&a.ID, &a.DomainType, &a.Identifier, &a.Name, &a.MetadataJSON, &createdAtStr); err != nil {
			return nil, fmt.Errorf("failed to scan asset: %w", err)
		}
		a.CreatedAt = parseTime(createdAtStr)
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

func (r *Repository) getAuditLogs(ctx context.Context, itemID int64) ([]core.AuditLog, error) {
	query := `
		SELECT al.id, al.work_item_id, al.actor_id, COALESCE(u.full_name, 'System'), al.action, COALESCE(al.details_json, ''), al.created_at
		FROM audit_logs al
		LEFT JOIN users u ON al.actor_id = u.id
		WHERE al.work_item_id = ?
		ORDER BY al.created_at ASC;
	`
	rows, err := r.db.QueryContext(ctx, query, itemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []core.AuditLog
	for rows.Next() {
		var l core.AuditLog
		var itemIDVal, actorIDVal sql.NullInt64
		var createdAtStr string
		if err := rows.Scan(&l.ID, &itemIDVal, &actorIDVal, &l.ActorName, &l.Action, &l.DetailsJSON, &createdAtStr); err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}
		if itemIDVal.Valid {
			l.WorkItemID = &itemIDVal.Int64
		}
		if actorIDVal.Valid {
			l.ActorID = &actorIDVal.Int64
		}
		l.CreatedAt = parseTime(createdAtStr)
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// CreateItem atomically creates a work item, initial message, and audit entry in one IMMEDIATE transaction.
func (r *Repository) CreateItem(ctx context.Context, item *core.WorkItem, initialMessage *core.ThreadMessage) (*core.WorkItem, error) {
	if item.ItemCode == "" {
		return nil, errors.New("item_code is required")
	}
	if item.Subject == "" {
		return nil, errors.New("subject is required")
	}
	if item.RequesterEmail == "" {
		return nil, errors.New("requester_email is required")
	}
	if item.Status == "" {
		item.Status = core.StatusNew
	}
	if item.Priority == "" {
		item.Priority = core.PriorityNormal
	}

	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		insertItem := `
			INSERT INTO work_items (item_code, domain_type, requester_name, requester_email, assigned_user_id, contact_id, organization_id, status, priority, subject, summary)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`
		res, err := tx.ExecContext(ctx, insertItem,
			item.ItemCode, item.DomainType, item.RequesterName, item.RequesterEmail,
			item.AssignedUserID, item.ContactID, item.OrganizationID,
			item.Status, item.Priority, item.Subject, item.Summary,
		)
		if err != nil {
			return fmt.Errorf("failed to insert work item: %w", err)
		}

		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		item.ID = id

		// Insert initial message if provided
		if initialMessage != nil {
			insertMsg := `
				INSERT INTO thread_messages (work_item_id, author_user_id, sender_email, body, is_internal, external_message_id)
				VALUES (?, ?, ?, ?, ?, ?);
			`
			isInternalInt := 0
			if initialMessage.IsInternal {
				isInternalInt = 1
			}
			msgRes, err := tx.ExecContext(ctx, insertMsg,
				item.ID, initialMessage.AuthorUserID, initialMessage.SenderEmail,
				initialMessage.Body, isInternalInt, initialMessage.ExternalMessageID,
			)
			if err != nil {
				return fmt.Errorf("failed to insert initial message: %w", err)
			}
			msgID, _ := msgRes.LastInsertId()
			initialMessage.ID = msgID
			initialMessage.WorkItemID = item.ID
		}

		// Record audit entry
		insertAudit := `
			INSERT INTO audit_logs (work_item_id, actor_id, action, details_json)
			VALUES (?, ?, ?, ?);
		`
		var actorID *int64
		if initialMessage != nil {
			actorID = initialMessage.AuthorUserID
		}
		details := fmt.Sprintf(`{"item_code":"%s","status":"%s"}`, item.ItemCode, item.Status)
		_, err = tx.ExecContext(ctx, insertAudit, item.ID, actorID, core.ActionItemCreated, details)
		return err
	})

	if err != nil {
		return nil, err
	}
	return item, nil
}

// TransitionStatus validates and performs a deterministic state transition within an IMMEDIATE transaction.
func (r *Repository) TransitionStatus(ctx context.Context, id int64, actorID int64, newStatus core.Status) error {
	return r.db.WithTx(ctx, func(tx *sql.Tx) error {
		var currentStatus core.Status
		var itemCode string
		err := tx.QueryRowContext(ctx, "SELECT status, item_code FROM work_items WHERE id = ?", id).Scan(&currentStatus, &itemCode)
		if errors.Is(err, sql.ErrNoRows) {
			return core.ErrItemNotFound
		}
		if err != nil {
			return err
		}

		if err := core.ValidateTransition(currentStatus, newStatus); err != nil {
			return err
		}

		var updateQuery string
		var args []any
		if newStatus == core.StatusResolved {
			updateQuery = "UPDATE work_items SET status = ?, updated_at = CURRENT_TIMESTAMP, resolved_at = CURRENT_TIMESTAMP WHERE id = ?"
			args = []any{newStatus, id}
		} else {
			updateQuery = "UPDATE work_items SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?"
			args = []any{newStatus, id}
		}

		if _, err := tx.ExecContext(ctx, updateQuery, args...); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}

		// Record immutable audit record
		auditQuery := `INSERT INTO audit_logs (work_item_id, actor_id, action, details_json) VALUES (?, ?, ?, ?)`
		details := fmt.Sprintf(`{"from":"%s","to":"%s"}`, currentStatus, newStatus)
		_, err = tx.ExecContext(ctx, auditQuery, id, actorID, core.ActionStatusChanged, details)
		return err
	})
}

// AddThreadMessage appends a note or public update.
// If an external requester sends a message, automatic status reversal to OPEN is triggered.
func (r *Repository) AddThreadMessage(ctx context.Context, id int64, authorID *int64, senderEmail string, body string, isInternal bool, externalID *string) (*core.ThreadMessage, error) {
	if strings.TrimSpace(body) == "" {
		return nil, core.ErrEmptyMessageBody
	}
	if senderEmail == "" {
		return nil, core.ErrSenderRequired
	}

	var createdMsg core.ThreadMessage

	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		var currentStatus core.Status
		err := tx.QueryRowContext(ctx, "SELECT status FROM work_items WHERE id = ?", id).Scan(&currentStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return core.ErrItemNotFound
		}
		if err != nil {
			return err
		}

		isInternalInt := 0
		if isInternal {
			isInternalInt = 1
		}

		insertQuery := `
			INSERT INTO thread_messages (work_item_id, author_user_id, sender_email, body, is_internal, external_message_id)
			VALUES (?, ?, ?, ?, ?, ?);
		`
		res, err := tx.ExecContext(ctx, insertQuery, id, authorID, senderEmail, body, isInternalInt, externalID)
		if err != nil {
			return fmt.Errorf("failed to insert thread message: %w", err)
		}

		msgID, err := res.LastInsertId()
		if err != nil {
			return err
		}

		createdMsg = core.ThreadMessage{
			ID:                msgID,
			WorkItemID:        id,
			AuthorUserID:      authorID,
			SenderEmail:       senderEmail,
			Body:              body,
			IsInternal:        isInternal,
			ExternalMessageID: externalID,
			CreatedAt:         time.Now().UTC(),
		}

		// State machine side-effects:
		// 1. If requester replies (authorID == nil) and status is PENDING_USER or RESOLVED -> transition to OPEN
		if authorID == nil && (currentStatus == core.StatusPendingUser || currentStatus == core.StatusResolved) {
			_, err = tx.ExecContext(ctx, "UPDATE work_items SET status = 'OPEN', updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
			if err != nil {
				return fmt.Errorf("failed to reopen work item: %w", err)
			}
			details := fmt.Sprintf(`{"from":"%s","to":"OPEN","reason":"requester_reply"}`, currentStatus)
			_, _ = tx.ExecContext(ctx, "INSERT INTO audit_logs (work_item_id, actor_id, action, details_json) VALUES (?, NULL, ?, ?)", id, core.ActionStatusChanged, details)
		} else if authorID != nil && !isInternal && currentStatus == core.StatusOpen {
			// 2. Staff replies publicly to requester -> transition to PENDING_USER
			_, err = tx.ExecContext(ctx, "UPDATE work_items SET status = 'PENDING_USER', updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
			if err != nil {
				return fmt.Errorf("failed to set pending user status: %w", err)
			}
			details := fmt.Sprintf(`{"from":"OPEN","to":"PENDING_USER","reason":"staff_public_reply"}`)
			_, _ = tx.ExecContext(ctx, "INSERT INTO audit_logs (work_item_id, actor_id, action, details_json) VALUES (?, ?, ?, ?)", id, authorID, core.ActionStatusChanged, details)
		} else {
			// Just touch updated_at
			_, _ = tx.ExecContext(ctx, "UPDATE work_items SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
		}

		// Audit log for the message
		action := core.ActionPublicReply
		if isInternal {
			action = core.ActionNoteAdded
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO audit_logs (work_item_id, actor_id, action, details_json) VALUES (?, ?, ?, ?)", id, authorID, action, fmt.Sprintf(`{"message_id":%d}`, msgID))
		return err
	})

	if err != nil {
		return nil, err
	}
	return &createdMsg, nil
}

// AssignItem assigns a work item to an operator or unassigns it.
func (r *Repository) AssignItem(ctx context.Context, id int64, actorID int64, targetUserID *int64) error {
	return r.db.WithTx(ctx, func(tx *sql.Tx) error {
		var oldAssignedID sql.NullInt64
		err := tx.QueryRowContext(ctx, "SELECT assigned_user_id FROM work_items WHERE id = ?", id).Scan(&oldAssignedID)
		if errors.Is(err, sql.ErrNoRows) {
			return core.ErrItemNotFound
		}
		if err != nil {
			return err
		}

		// If target user provided, verify they exist
		if targetUserID != nil {
			var exists int
			err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE id = ? AND is_active = 1", *targetUserID).Scan(&exists)
			if err != nil || exists == 0 {
				return core.ErrUserNotFound
			}
		}

		query := "UPDATE work_items SET assigned_user_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?"
		if _, err := tx.ExecContext(ctx, query, targetUserID, id); err != nil {
			return fmt.Errorf("failed to assign work item: %w", err)
		}

		details := fmt.Sprintf(`{"from_user":%v,"to_user":%v}`, oldAssignedID.Int64, targetUserID)
		_, err = tx.ExecContext(ctx, "INSERT INTO audit_logs (work_item_id, actor_id, action, details_json) VALUES (?, ?, ?, ?)", id, actorID, core.ActionReassigned, details)
		return err
	})
}

// LinkAsset links an asset to a work item.
func (r *Repository) LinkAsset(ctx context.Context, itemID int64, assetID int64, actorID int64) error {
	return r.db.WithTx(ctx, func(tx *sql.Tx) error {
		query := `INSERT OR IGNORE INTO work_item_assets (work_item_id, asset_id) VALUES (?, ?)`
		if _, err := tx.ExecContext(ctx, query, itemID, assetID); err != nil {
			return fmt.Errorf("failed to link asset: %w", err)
		}
		details := fmt.Sprintf(`{"asset_id":%d}`, assetID)
		_, err := tx.ExecContext(ctx, "INSERT INTO audit_logs (work_item_id, actor_id, action, details_json) VALUES (?, ?, ?, ?)", itemID, actorID, core.ActionAssetLinked, details)
		return err
	})
}

// UnlinkAsset unlinks an asset from a work item.
func (r *Repository) UnlinkAsset(ctx context.Context, itemID int64, assetID int64, actorID int64) error {
	return r.db.WithTx(ctx, func(tx *sql.Tx) error {
		query := `DELETE FROM work_item_assets WHERE work_item_id = ? AND asset_id = ?`
		if _, err := tx.ExecContext(ctx, query, itemID, assetID); err != nil {
			return fmt.Errorf("failed to unlink asset: %w", err)
		}
		details := fmt.Sprintf(`{"asset_id":%d}`, assetID)
		_, err := tx.ExecContext(ctx, "INSERT INTO audit_logs (work_item_id, actor_id, action, details_json) VALUES (?, ?, ?, ?)", itemID, actorID, core.ActionAssetUnlinked, details)
		return err
	})
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Now().UTC()
}
