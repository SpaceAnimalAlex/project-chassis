package core

import "context"

// Attachment holds binary content attached to inbound or outbound communications
// (e.g. voicemail audio recordings, email attachments).
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
}

// InboundMessage represents a normalized message entering Chassis from an external source
// (email provider polling, telephony/voicemail webhooks, or messaging relays).
type InboundMessage struct {
	ExternalMessageID string       `json:"external_message_id"`
	ThreadID          string       `json:"thread_id,omitempty"`
	InReplyTo         string       `json:"in_reply_to,omitempty"`
	References        []string     `json:"references,omitempty"`
	SenderEmail       string       `json:"sender_email"`
	SenderName        string       `json:"sender_name"`
	SenderPhone       string       `json:"sender_phone,omitempty"`
	Subject           string       `json:"subject"`
	BodyText          string       `json:"body_text"`
	BodyHTML          string       `json:"body_html,omitempty"`
	Attachments       []Attachment `json:"attachments,omitempty"`
}

// InboundIngestor defines the transactional ingestion seam for inbound communications.
// Implemented by internal/mailengine to ingest, thread-correlate, and notify without
// requiring the HTTP layer (internal/web) to depend on storage or concrete mail engines.
type InboundIngestor interface {
	IngestInbound(ctx context.Context, msg InboundMessage) error
}
