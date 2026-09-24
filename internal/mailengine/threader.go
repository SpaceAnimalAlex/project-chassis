package mailengine

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/db"
)

var (
	// Regex matching Chassis item codes in subjects: [HD-1001], [WO-204], [EDU-55], [CW-12]
	itemCodeRegex = regexp.MustCompile(`(?i)\[([A-Z]{2,6}-\d+)\]`)
)

// CorrelationResult represents the matched work item and which heuristic stage fired.
type CorrelationResult struct {
	WorkItemID   int64
	ItemCode     string
	Stage        string // "CONTACT_PHONE_MATCH", "HEADER_MATCH", "SUBJECT_REGEX", "NEW_ITEM"
	HeaderValue  string
	SubjectValue string
}

// Threader correlates inbound emails to existing work items or determines a new item must be created.
type Threader struct {
	database *db.DB
}

// NewThreader creates a new email thread correlator.
func NewThreader(database *db.DB) *Threader {
	return &Threader{database: database}
}

// Correlate evaluates an inbound message across the 4-stage heuristic chain (Stage 0 phone -> Stage 1 headers -> Stage 2 subject regex -> Stage 3 new item).
func (t *Threader) Correlate(ctx context.Context, msg InboundMessage) (CorrelationResult, error) {
	// Stage 0: Non-Email Phone / Contact Correlation for Voice/Voicemail
	// If the inbound message has a phone number (or SenderEmail formatted as a phone),
	// correlate to the contact or organization's active open ticket if one exists.
	phoneToMatch := msg.SenderPhone
	if phoneToMatch == "" && strings.HasPrefix(strings.TrimSpace(msg.SenderEmail), "+") {
		phoneToMatch = msg.SenderEmail
	}
	if phoneToMatch != "" {
		normPhone := core.NormalizePhone(phoneToMatch)
		if normPhone != "" {
			query := `
				SELECT w.id, w.item_code
				FROM work_items w
				WHERE (
					w.contact_id IN (
						SELECT contact_id FROM communication_channels WHERE channel_type = 'PHONE' AND value = ? AND contact_id IS NOT NULL
					)
					OR w.organization_id IN (
						SELECT organization_id FROM communication_channels WHERE channel_type = 'PHONE' AND value = ? AND organization_id IS NOT NULL
					)
				)
				AND w.status IN ('NEW', 'OPEN', 'PENDING_USER')
				ORDER BY w.updated_at DESC
				LIMIT 1;
			`
			var itemID int64
			var itemCode string
			err := t.database.QueryRowContext(ctx, query, normPhone, normPhone).Scan(&itemID, &itemCode)
			if err == nil {
				return CorrelationResult{
					WorkItemID:   itemID,
					ItemCode:     itemCode,
					Stage:        "CONTACT_PHONE_MATCH",
					HeaderValue:  normPhone,
					SubjectValue: "Active Open Ticket Match",
				}, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return CorrelationResult{}, err
			}
		}
	}

	// Stage 1: Direct In-Reply-To & References Header Matching
	var headerCandidates []string
	if msg.InReplyTo != "" {
		headerCandidates = append(headerCandidates, strings.Trim(msg.InReplyTo, "<> \t\r\n"))
	}
	for _, ref := range msg.References {
		cleanRef := strings.Trim(ref, "<> \t\r\n")
		if cleanRef != "" {
			headerCandidates = append(headerCandidates, cleanRef)
		}
	}

	for _, cand := range headerCandidates {
		query := `
			SELECT tm.work_item_id, w.item_code
			FROM thread_messages tm
			JOIN work_items w ON tm.work_item_id = w.id
			WHERE tm.external_message_id = ?
			LIMIT 1;
		`
		var itemID int64
		var itemCode string
		err := t.database.QueryRowContext(ctx, query, cand).Scan(&itemID, &itemCode)
		if err == nil {
			return CorrelationResult{
				WorkItemID:  itemID,
				ItemCode:    itemCode,
				Stage:       "HEADER_MATCH",
				HeaderValue: cand,
			}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return CorrelationResult{}, err
		}
	}

	// Stage 2: Subject Line Item Code Regex Extraction
	match := itemCodeRegex.FindStringSubmatch(msg.Subject)
	if len(match) > 1 {
		matchedCode := strings.ToUpper(match[1])
		query := `SELECT id, item_code FROM work_items WHERE item_code = ? LIMIT 1;`
		var itemID int64
		var itemCode string
		err := t.database.QueryRowContext(ctx, query, matchedCode).Scan(&itemID, &itemCode)
		if err == nil {
			return CorrelationResult{
				WorkItemID:   itemID,
				ItemCode:     itemCode,
				Stage:        "SUBJECT_REGEX",
				SubjectValue: matchedCode,
			}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return CorrelationResult{}, err
		}
	}

	// Stage 3: New Work Item Required
	return CorrelationResult{
		WorkItemID: 0,
		ItemCode:   "",
		Stage:      "NEW_ITEM",
	}, nil
}
