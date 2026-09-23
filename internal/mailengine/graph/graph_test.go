package graph

import (
	"context"
	"errors"
	"testing"

	"github.com/project-chassis/chassis/internal/mailengine"
)

func TestRefuseInternalMessage(t *testing.T) {
	provider := NewProvider(Config{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		MailboxEmail: "helpdesk@local.org",
	})

	ctx := context.Background()

	// An internal technician note must NEVER be dispatched to an external recipient
	internalMsg := mailengine.OutboundMessage{
		WorkItemCode: "HD-1001",
		Recipient:    "requester@example.com",
		Subject:      "Internal Diagnostic",
		Body:         "Staff password reset link: https://...",
		IsInternal:   true,
	}

	err := provider.SendMessage(ctx, internalMsg)
	if !errors.Is(err, ErrRefuseInternalMsg) {
		t.Fatalf("expected ErrRefuseInternalMsg, got: %v", err)
	}
}

func TestRefuseEmptyRecipient(t *testing.T) {
	provider := NewProvider(Config{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		MailboxEmail: "helpdesk@local.org",
	})

	ctx := context.Background()

	msg := mailengine.OutboundMessage{
		WorkItemCode: "HD-1001",
		Recipient:    "   ",
		Subject:      "Public Update",
		Body:         "Hello",
		IsInternal:   false,
	}

	err := provider.SendMessage(ctx, msg)
	if !errors.Is(err, mailengine.ErrEmptyRecipient) {
		t.Fatalf("expected ErrEmptyRecipient, got: %v", err)
	}
}
