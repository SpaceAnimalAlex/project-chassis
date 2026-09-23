package core

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrAssetNotFound   = errors.New("asset not found")
	ErrDuplicateAsset  = errors.New("asset with this identifier already exists")
	ErrInvalidMetadata = errors.New("invalid asset metadata JSON")
)

// Asset represents an entity linked to operational work (Workstations, Roster Sections, Municipal Agencies, Machines).
// Polymorphic metadata is stored in MetadataJSON to allow Implements to extend properties without schema changes.
type Asset struct {
	ID           int64     `json:"id"`
	DomainType   string    `json:"domain_type"` // e.g. HARDWARE, CLASS_SECTION, AGENCY, MACHINE
	Identifier   string    `json:"identifier"`  // e.g. Hostname, Serial Number, Course Code, Parcel ID
	Name         string    `json:"name"`
	MetadataJSON string    `json:"metadata_json"`
	CreatedAt    time.Time `json:"created_at"`
}

// WorkItemAsset links an Asset to a WorkItem (Many-to-Many).
type WorkItemAsset struct {
	WorkItemID int64 `json:"work_item_id"`
	AssetID    int64 `json:"asset_id"`
}

// ParseMetadata parses the polymorphic MetadataJSON into a target structure.
func (a Asset) ParseMetadata(v any) error {
	if a.MetadataJSON == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(a.MetadataJSON), v); err != nil {
		return errors.Join(ErrInvalidMetadata, err)
	}
	return nil
}
