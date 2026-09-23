package edu

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/implement"
)

func init() {
	_ = implement.Register(&EduImplement{})
}

type CourseMetadata struct {
	Term         string `json:"term,omitempty"`
	Instructor   string `json:"instructor,omitempty"`
	RoomNumber   string `json:"room_number,omitempty"`
	EnrollmentMax int   `json:"enrollment_max,omitempty"`
}

type EduImplement struct{}

var _ implement.Implement = (*EduImplement)(nil)

func (e *EduImplement) Domain() core.DomainType {
	return core.DomainEdu
}

func (e *EduImplement) DisplayName() string {
	return "Education & Academic Casework"
}

func (e *EduImplement) ItemCodePrefix() string {
	return "EDU"
}

func (e *EduImplement) SupportedAssetTypes() []string {
	return []string{"CLASS_SECTION", "STUDENT_RECORD", "COURSE"}
}

func (e *EduImplement) ValidateAssetMetadata(assetType string, rawJSON []byte) error {
	if len(rawJSON) == 0 {
		return nil
	}
	switch assetType {
	case "CLASS_SECTION", "COURSE":
		var meta CourseMetadata
		if err := json.Unmarshal(rawJSON, &meta); err != nil {
			return fmt.Errorf("invalid education asset metadata: %w", err)
		}
		return nil
	default:
		return errors.New("unsupported asset type for EDU domain")
	}
}
