package core

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
)

// ChannelType denotes whether a communication channel is email or phone.
type ChannelType string

const (
	ChannelEmail ChannelType = "EMAIL"
	ChannelPhone ChannelType = "PHONE"
)

var (
	ErrContactNotFound      = errors.New("contact not found")
	ErrOrganizationNotFound = errors.New("organization not found")
	ErrChannelConflict      = errors.New("communication channel already in use")
	ErrInvalidChannel       = errors.New("invalid communication channel")
)

// Contact represents an individual human (constituent, customer, student, vendor lead, technician).
type Contact struct {
	ID           int64                  `json:"id"`
	FullName     string                 `json:"full_name"`
	Notes        string                 `json:"notes,omitempty"`
	MetadataJSON string                 `json:"metadata_json,omitempty"`
	Channels     []CommunicationChannel `json:"channels,omitempty"`
	Affiliations []AffiliationDetail    `json:"affiliations,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// Organization represents a collective entity (company, agency, school, vendor, household).
type Organization struct {
	ID               int64                  `json:"id"`
	Name             string                 `json:"name"`
	OrganizationType string                 `json:"organization_type"` // VENDOR, AGENCY, SCHOOL, CLIENT, HOUSEHOLD, DEFAULT
	Notes            string                 `json:"notes,omitempty"`
	MetadataJSON     string                 `json:"metadata_json,omitempty"`
	Channels         []CommunicationChannel `json:"channels,omitempty"`
	Contacts         []AffiliatedContact    `json:"contacts,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// Affiliation represents the M:N link between a Contact and an Organization.
type Affiliation struct {
	ID             int64     `json:"id"`
	ContactID      int64     `json:"contact_id"`
	OrganizationID int64     `json:"organization_id"`
	RoleTitle      string    `json:"role_title,omitempty"` // e.g. "Plant Superintendent", "Principal", "Lead Tech"
	IsPrimary      bool      `json:"is_primary"`
	CreatedAt      time.Time `json:"created_at"`
}

// AffiliationDetail is a Contact's affiliation enriched with Organization details.
type AffiliationDetail struct {
	AffiliationID    int64  `json:"affiliation_id"`
	OrganizationID   int64  `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
	OrganizationType string `json:"organization_type"`
	RoleTitle        string `json:"role_title,omitempty"`
	IsPrimary        bool   `json:"is_primary"`
}

// AffiliatedContact is an Organization's affiliated member enriched with Contact details.
type AffiliatedContact struct {
	AffiliationID int64  `json:"affiliation_id"`
	ContactID     int64  `json:"contact_id"`
	FullName      string `json:"full_name"`
	RoleTitle     string `json:"role_title,omitempty"`
	IsPrimary     bool   `json:"is_primary"`
}

// CommunicationChannel represents an email address or phone number attached to a Contact OR an Organization.
type CommunicationChannel struct {
	ID             int64       `json:"id"`
	ContactID      *int64      `json:"contact_id,omitempty"`
	OrganizationID *int64      `json:"organization_id,omitempty"`
	ChannelType    ChannelType `json:"channel_type"` // EMAIL, PHONE
	Value          string      `json:"value"`        // Normalized lowercase email or E.164 phone
	Label          string      `json:"label,omitempty"` // main, switchboard, mobile, direct, billing
	IsPrimary      bool        `json:"is_primary"`
	CreatedAt      time.Time   `json:"created_at"`
}

// CallerIDResolution is the rich context surfaced when resolving an incoming phone call or email off the wire.
type CallerIDResolution struct {
	Channel            CommunicationChannel `json:"channel"`
	Contact            *Contact             `json:"contact,omitempty"`
	Organization       *Organization        `json:"organization,omitempty"`
	PrimaryAffiliation *AffiliationDetail   `json:"primary_affiliation,omitempty"`
	AffiliatedContacts []AffiliatedContact  `json:"affiliated_contacts,omitempty"`
	ActiveWorkItems    []WorkItemSummary    `json:"active_work_items,omitempty"`
}

// NormalizeEmail returns a trimmed, lowercase representation of an email address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormalizePhone cleans and normalizes a phone number to standard E.164 (+1XXXXXXXXXX for North America).
// If the number cannot be formatted to E.164, it strips non-digit characters and prepends '+' if digits exist.
func NormalizePhone(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var digits strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}

	d := digits.String()
	if d == "" {
		return ""
	}

	// North American 10-digit number -> prepend +1
	if len(d) == 10 {
		return "+1" + d
	}
	// 11 digits starting with 1 -> prepend +
	if len(d) == 11 && strings.HasPrefix(d, "1") {
		return "+" + d
	}

	// If already starts with '+', keep '+' with digits
	if strings.HasPrefix(raw, "+") {
		return "+" + d
	}

	return "+" + d
}

// ContactService defines the domain boundary for Contact and Organization management.
type ContactService interface {
	// Contact operations
	GetContact(ctx context.Context, id int64) (*Contact, error)
	ListContacts(ctx context.Context, query string, limit, offset int) ([]Contact, error)
	CreateContact(ctx context.Context, contact *Contact) (*Contact, error)
	UpdateContact(ctx context.Context, contact *Contact) (*Contact, error)
	DeleteContact(ctx context.Context, id int64) error

	// Organization operations
	GetOrganization(ctx context.Context, id int64) (*Organization, error)
	ListOrganizations(ctx context.Context, query string, limit, offset int) ([]Organization, error)
	CreateOrganization(ctx context.Context, org *Organization) (*Organization, error)
	UpdateOrganization(ctx context.Context, org *Organization) (*Organization, error)
	DeleteOrganization(ctx context.Context, id int64) error

	// Affiliation operations
	AddAffiliation(ctx context.Context, contactID, orgID int64, roleTitle string, isPrimary bool) error
	RemoveAffiliation(ctx context.Context, contactID, orgID int64) error

	// Channel operations
	AddChannel(ctx context.Context, channel *CommunicationChannel) (*CommunicationChannel, error)
	RemoveChannel(ctx context.Context, channelID int64) error

	// Identity resolution & screen-pop lookup
	ResolveCaller(ctx context.Context, channelType ChannelType, value string) (*CallerIDResolution, error)

	// Promote ticket requester to a persistent contact with optional organization affiliation
	PromoteRequesterToContact(ctx context.Context, workItemID int64, organizationName *string, roleTitle *string) (*Contact, error)
}
