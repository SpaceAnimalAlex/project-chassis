package mro

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/implement"
)

func init() {
	_ = implement.Register(&MROImplement{})
}

type MachineMetadata struct {
	Make                    string `json:"make,omitempty"`
	Model                   string `json:"model,omitempty"`
	SerialNumber            string `json:"serial_number,omitempty"`
	YearManufactured        int    `json:"year_manufactured,omitempty"`
	MaintenanceIntervalDays int    `json:"maintenance_interval_days,omitempty"`
	Location                string `json:"location,omitempty"`
}

type MROImplement struct{}

var _ implement.Implement = (*MROImplement)(nil)

func (m *MROImplement) Domain() core.DomainType {
	return core.DomainMRO
}

func (m *MROImplement) DisplayName() string {
	return "MRO, Trades & Field Work Orders"
}

func (m *MROImplement) ItemCodePrefix() string {
	return "WO"
}

func (m *MROImplement) SupportedAssetTypes() []string {
	return []string{"MACHINE", "VEHICLE", "FACILITY", "BOM_ITEM"}
}

func (m *MROImplement) ValidateAssetMetadata(assetType string, rawJSON []byte) error {
	if len(rawJSON) == 0 {
		return nil
	}
	switch assetType {
	case "MACHINE", "VEHICLE":
		var meta MachineMetadata
		if err := json.Unmarshal(rawJSON, &meta); err != nil {
			return fmt.Errorf("invalid MRO asset metadata: %w", err)
		}
		return nil
	case "FACILITY", "BOM_ITEM":
		return nil
	default:
		return errors.New("unsupported asset type for MRO domain")
	}
}
