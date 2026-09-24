package mailengine

import (
	"context"
	"errors"

	"github.com/project-chassis/chassis/internal/core"
)

var (
	ErrProviderNotInitialized = errors.New("mail provider not initialized")
	ErrEmptyRecipient         = errors.New("outbound message recipient cannot be empty")
)

// InboundMessage represents a normalized message fetched from an email provider or voice webhook.
// Aliased to core.InboundMessage to maintain an abstract core ingestion seam.
type InboundMessage = core.InboundMessage

// Attachment holds binary content attached to inbound or outbound mail.
// Aliased to core.Attachment.
type Attachment = core.Attachment

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

// MailProvider defines the contract for email backends (Microsoft Graph, Google Workspace, Relay).
type MailProvider interface {
	Name() string
	AccountID() string
	Initialize(ctx context.Context, config map[string]string) error
	FetchNewMessages(ctx context.Context, deltaToken string) (messages []InboundMessage, nextDeltaToken string, err error)
	SendMessage(ctx context.Context, msg OutboundMessage) error
	HealthCheck(ctx context.Context) error
}
