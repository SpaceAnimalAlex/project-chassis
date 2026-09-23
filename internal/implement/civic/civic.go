package civic

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/implement"
)

func init() {
	_ = implement.Register(&CivicImplement{})
}

type AgencyMetadata struct {
	AgencyName   string `json:"agency_name,omitempty"` // e.g. PennDOT, Public Works, Water Authority
	ContactEmail string `json:"contact_email,omitempty"`
	Phone        string `json:"phone,omitempty"`
	Jurisdiction string `json:"jurisdiction,omitempty"`
}

type CivicImplement struct{}

var _ implement.Implement = (*CivicImplement)(nil)

func (c *CivicImplement) Domain() core.DomainType {
	return core.DomainCivic
}

func (c *CivicImplement) DisplayName() string {
	return "Constituent Services & Municipal Casework"
}

func (c *CivicImplement) ItemCodePrefix() string {
	return "CW"
}

func (c *CivicImplement) SupportedAssetTypes() []string {
	return []string{"MUNICIPAL_AGENCY", "PARCEL", "PRECINCT", "DISTRICT"}
}

func (c *CivicImplement) ValidateAssetMetadata(assetType string, rawJSON []byte) error {
	if len(rawJSON) == 0 {
		return nil
	}
	switch assetType {
	case "MUNICIPAL_AGENCY":
		var meta AgencyMetadata
		if err := json.Unmarshal(rawJSON, &meta); err != nil {
			return fmt.Errorf("invalid civic asset metadata: %w", err)
		}
		return nil
	case "PARCEL", "PRECINCT", "DISTRICT":
		return nil
	default:
		return errors.New("unsupported asset type for CIVIC domain")
	}
}
