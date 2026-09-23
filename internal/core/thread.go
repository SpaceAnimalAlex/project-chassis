package core

import (
	"errors"
	"time"
)

var (
	ErrEmptyMessageBody = errors.New("message body cannot be empty")
	ErrSenderRequired   = errors.New("sender email is required")
)

// ThreadMessage represents a single message in a work item conversation thread.
// Dual-channel architecture enforces strict separation between internal notes and public replies.
type ThreadMessage struct {
	ID                int64     `json:"id"`
	WorkItemID        int64     `json:"work_item_id"`
	AuthorUserID      *int64    `json:"author_user_id,omitempty"` // nil if external requester
	AuthorName        string    `json:"author_name,omitempty"`    // joined from users table if available
	SenderEmail       string    `json:"sender_email"`
	Body              string    `json:"body"`
	IsInternal        bool      `json:"is_internal"`                   // true = private note, false = public requester update
	ExternalMessageID *string   `json:"external_message_id,omitempty"` // M365 Graph / Gmail Message-ID
	CreatedAt         time.Time `json:"created_at"`
}

// IsPublic returns true if the message is visible to the external requester.
func (m ThreadMessage) IsPublic() bool {
	return !m.IsInternal
}

// SanitizeForRequester filters out all internal messages before any thread data is presented externally.
// This is a strict safety guarantee from the Ancestral Mandate: internal notes must NEVER leak.
func SanitizeForRequester(messages []ThreadMessage) []ThreadMessage {
	publicMsgs := make([]ThreadMessage, 0, len(messages))
	for _, msg := range messages {
		if !msg.IsInternal {
			publicMsgs = append(publicMsgs, msg)
		}
	}
	return publicMsgs
}
