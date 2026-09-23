package core

import (
	"encoding/json"
	"time"
)

// AuditAction defines the standard operational audit event actions.
type AuditAction string

const (
	ActionItemCreated     AuditAction = "ITEM_CREATED"
	ActionStatusChanged   AuditAction = "STATUS_CHANGED"
	ActionReassigned      AuditAction = "REASSIGNED"
	ActionNoteAdded       AuditAction = "NOTE_ADDED"
	ActionPublicReply     AuditAction = "PUBLIC_REPLY"
	ActionAssetLinked     AuditAction = "ASSET_LINKED"
	ActionAssetUnlinked   AuditAction = "ASSET_UNLINKED"
	ActionMailCorrelated  AuditAction = "MAIL_CORRELATED"
)

// AuditLog represents an immutable record of an operational action.
type AuditLog struct {
	ID          int64       `json:"id"`
	WorkItemID  *int64      `json:"work_item_id,omitempty"`
	ActorID     *int64      `json:"actor_id,omitempty"` // nil if system / incoming email
	ActorName   string      `json:"actor_name,omitempty"`
	Action      AuditAction `json:"action"`
	DetailsJSON string      `json:"details_json,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

// MailCorrelationDetail records the exact matching heuristic fired during email ingestion.
// As agreed with Claude, this enables zero-guess troubleshooting for misrouted replies.
type MailCorrelationDetail struct {
	Stage             string `json:"stage"` // "HEADER_MATCH", "SUBJECT_REGEX", "NEW_ITEM"
	ExternalMessageID string `json:"external_message_id"`
	MatchedHeader     string `json:"matched_header,omitempty"`
	SubjectMatched    string `json:"subject_matched,omitempty"`
}

// ToJSON serializes any detail struct to JSON string for storage in AuditLog.
func DetailsToJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
