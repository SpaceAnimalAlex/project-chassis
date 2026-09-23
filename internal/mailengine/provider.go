package mailengine

import (
	"context"
	"errors"
)

var (
	ErrProviderNotInitialized = errors.New("mail provider not initialized")
	ErrEmptyRecipient         = errors.New("outbound message recipient cannot be empty")
)

// InboundMessage represents a normalized message fetched from an email provider.
type InboundMessage struct {
	ExternalMessageID string       `json:"external_message_id"`
	ThreadID          string       `json:"thread_id,omitempty"`
	InReplyTo         string       `json:"in_reply_to,omitempty"`
	References        []string     `json:"references,omitempty"`
	SenderEmail       string       `json:"sender_email"`
	SenderName        string       `json:"sender_name"`
	Subject           string       `json:"subject"`
	BodyText          string       `json:"body_text"`
	BodyHTML          string       `json:"body_html,omitempty"`
	Attachments       []Attachment `json:"attachments,omitempty"`
}

// OutboundMessage represents an email being dispatched to a requester.
type OutboundMessage struct {
	WorkItemCode string       `json:"work_item_code"`
	Recipient    string       `json:"recipient"`
	Subject      string       `json:"subject"`
	Body         string       `json:"body"`
	InReplyTo    string       `json:"in_reply_to,omitempty"`
	References   string       `json:"references,omitempty"`
	IsInternal   bool         `json:"is_internal"` // Must ALWAYS be false when sent externally
	Attachments  []Attachment `json:"attachments,omitempty"`
}

// Attachment holds binary content attached to inbound or outbound mail.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
}

// MailProvider defines the contract for email backends (Microsoft Graph, Google Workspace, Relay).
type MailProvider interface {
	Name() string
	AccountID() string
	Initialize(ctx context.Context, config map[string]string) error
	FetchNewMessages(ctx context.Context, deltaToken string) (messages []InboundMessage, nextDeltaToken string, err error)
	SendMessage(ctx context.Context, msg OutboundMessage) error
	HealthCheck(ctx context.Context) error
}
