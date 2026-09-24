package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Status represents the lifecycle stage of a work item.
type Status string

const (
	StatusNew         Status = "NEW"
	StatusOpen        Status = "OPEN"
	StatusPendingUser Status = "PENDING_USER"
	StatusResolved    Status = "RESOLVED"
	StatusClosed      Status = "CLOSED"
)

// Priority represents the operational urgency of a work item.
type Priority string

const (
	PriorityLow      Priority = "LOW"
	PriorityNormal   Priority = "NORMAL"
	PriorityHigh     Priority = "HIGH"
	PriorityCritical Priority = "CRITICAL"
)

// DomainType identifies the operational domain (Implement).
type DomainType string

const (
	DomainIT    DomainType = "IT"
	DomainEdu   DomainType = "EDU"
	DomainCivic DomainType = "CIVIC"
	DomainMRO   DomainType = "MRO"
)

// WorkItem represents a single operational ticket, work order, or case.
type WorkItem struct {
	ID             int64      `json:"id"`
	ItemCode       string     `json:"item_code"`
	DomainType     DomainType `json:"domain_type"`
	RequesterName  string     `json:"requester_name"`
	RequesterEmail string     `json:"requester_email"`
	AssignedUserID *int64     `json:"assigned_user_id,omitempty"`
	ContactID      *int64     `json:"contact_id,omitempty"`
	OrganizationID *int64     `json:"organization_id,omitempty"`
	Status         Status     `json:"status"`
	Priority       Priority   `json:"priority"`
	Subject        string     `json:"subject"`
	Summary        string     `json:"summary"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

// WorkItemSummary is a lightweight projection for queue triage views.
type WorkItemSummary struct {
	ID               int64      `json:"id"`
	ItemCode         string     `json:"item_code"`
	DomainType       DomainType `json:"domain_type"`
	RequesterName    string     `json:"requester_name"`
	RequesterEmail   string     `json:"requester_email"`
	AssignedUserID   *int64     `json:"assigned_user_id,omitempty"`
	AssignedName     string     `json:"assigned_name,omitempty"`
	ContactID        *int64     `json:"contact_id,omitempty"`
	OrganizationID   *int64     `json:"organization_id,omitempty"`
	OrganizationName string     `json:"organization_name,omitempty"`
	Status           Status     `json:"status"`
	Priority         Priority   `json:"priority"`
	Subject          string     `json:"subject"`
	MessageCount     int        `json:"message_count"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// WorkItemDetail contains the full item, associated thread messages, linked assets, and audit records.
type WorkItemDetail struct {
	Item     WorkItem        `json:"item"`
	Messages []ThreadMessage `json:"messages"`
	Assets   []Asset         `json:"assets"`
	AuditLog []AuditLog      `json:"audit_log"`
}

// QueueFilter specifies search and triage filtering parameters.
type QueueFilter struct {
	DomainType     *DomainType `json:"domain_type,omitempty"`
	Status         *Status     `json:"status,omitempty"`
	AssignedUserID *int64      `json:"assigned_user_id,omitempty"`
	ContactID      *int64      `json:"contact_id,omitempty"`
	OrganizationID *int64      `json:"organization_id,omitempty"`
	Priority       *Priority   `json:"priority,omitempty"`
	SearchQuery    string      `json:"search_query,omitempty"`
	Limit          int         `json:"limit,omitempty"`
	Offset         int         `json:"offset,omitempty"`
}

var (
	ErrInvalidTransition = errors.New("invalid work item status transition")
	ErrItemNotFound      = errors.New("work item not found")
	ErrUnauthorized      = errors.New("unauthorized action on work item")
	ErrInvalidStatus     = errors.New("invalid status value")
)

// CanTransition validates whether moving from 'current' to 'next' is allowed.
// Enforces deterministic state transitions from the Chassis specification:
// NEW -> OPEN
// OPEN -> PENDING_USER, RESOLVED, CLOSED
// PENDING_USER -> OPEN, RESOLVED, CLOSED
// RESOLVED -> OPEN (reopen), CLOSED
// CLOSED -> OPEN (reopen)
func CanTransition(current, next Status) bool {
	if current == next {
		return true
	}
	switch current {
	case StatusNew:
		return next == StatusOpen || next == StatusClosed
	case StatusOpen:
		return next == StatusPendingUser || next == StatusResolved || next == StatusClosed
	case StatusPendingUser:
		return next == StatusOpen || next == StatusResolved || next == StatusClosed
	case StatusResolved:
		return next == StatusOpen || next == StatusClosed
	case StatusClosed:
		return next == StatusOpen
	default:
		return false
	}
}

// ValidateTransition returns an error if the transition is illegal.
func ValidateTransition(current, next Status) error {
	if !CanTransition(current, next) {
		return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidTransition, current, next)
	}
	return nil
}

// WorkItemService defines the primary interface contract for queue and ticket operations.
// This is the boundary between the Core Engine (Track A) and the HTTP/UI layer (Track B).
type WorkItemService interface {
	ListQueue(ctx context.Context, filter QueueFilter) ([]WorkItemSummary, error)
	GetItem(ctx context.Context, id int64) (*WorkItemDetail, error)
	GetItemByCode(ctx context.Context, code string) (*WorkItemDetail, error)
	CreateItem(ctx context.Context, item *WorkItem, initialMessage *ThreadMessage) (*WorkItem, error)
	TransitionStatus(ctx context.Context, id int64, actorID int64, newStatus Status) error
	AddThreadMessage(ctx context.Context, id int64, authorID *int64, senderEmail string, body string, isInternal bool, externalID *string) (*ThreadMessage, error)
	AssignItem(ctx context.Context, id int64, actorID int64, targetUserID *int64) error
	LinkAsset(ctx context.Context, itemID int64, assetID int64, actorID int64) error
	UnlinkAsset(ctx context.Context, itemID int64, assetID int64, actorID int64) error
}
