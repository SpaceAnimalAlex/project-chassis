package it

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/implement"
)

func init() {
	_ = implement.Register(&ITImplement{})
}

// WorkstationMetadata defines the schema stored in assets.metadata_json for IT devices.
type WorkstationMetadata struct {
	OS           string `json:"os,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	MACAddress   string `json:"mac_address,omitempty"`
	IPAddress    string `json:"ip_address,omitempty"`
	Location     string `json:"location,omitempty"`
}

type ITImplement struct{}

var _ implement.Implement = (*ITImplement)(nil)

func (i *ITImplement) Domain() core.DomainType {
	return core.DomainIT
}

func (i *ITImplement) DisplayName() string {
	return "IT Operations & Workstation Registry"
}

func (i *ITImplement) ItemCodePrefix() string {
	return "HD"
}

func (i *ITImplement) SupportedAssetTypes() []string {
	return []string{"WORKSTATION", "SERVER", "NETWORK_DEVICE", "PRINTER"}
}

func (i *ITImplement) ValidateAssetMetadata(assetType string, rawJSON []byte) error {
	if len(rawJSON) == 0 {
		return nil
	}
	switch assetType {
	case "WORKSTATION", "SERVER", "NETWORK_DEVICE", "PRINTER":
		var meta WorkstationMetadata
		if err := json.Unmarshal(rawJSON, &meta); err != nil {
			return fmt.Errorf("invalid IT asset metadata: %w", err)
		}
		return nil
	default:
		return errors.New("unsupported asset type for IT domain")
	}
}
