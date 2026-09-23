package mailengine

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

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
	Stage        string // "HEADER_MATCH", "SUBJECT_REGEX", "NEW_ITEM"
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

// Correlate evaluates an inbound message across the 3-stage heuristic chain.
func (t *Threader) Correlate(ctx context.Context, msg InboundMessage) (CorrelationResult, error) {
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
