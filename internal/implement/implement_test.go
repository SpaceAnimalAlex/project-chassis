package implement_test

import (
	"testing"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/implement"
	_ "github.com/project-chassis/chassis/internal/implement/civic"
	_ "github.com/project-chassis/chassis/internal/implement/edu"
	_ "github.com/project-chassis/chassis/internal/implement/it"
	_ "github.com/project-chassis/chassis/internal/implement/mro"
)

func TestImplementRegistry(t *testing.T) {
	domains := []core.DomainType{core.DomainIT, core.DomainEdu, core.DomainCivic, core.DomainMRO}
	expectedPrefixes := map[core.DomainType]string{
		core.DomainIT:    "HD",
		core.DomainEdu:   "EDU",
		core.DomainCivic: "CW",
		core.DomainMRO:   "WO",
	}

	for _, d := range domains {
		impl, err := implement.Get(d)
		if err != nil {
			t.Fatalf("expected implement for domain %s, got error: %v", d, err)
		}
		if impl.ItemCodePrefix() != expectedPrefixes[d] {
			t.Fatalf("for domain %s, expected prefix %s, got %s", d, expectedPrefixes[d], impl.ItemCodePrefix())
		}
	}
}

func TestAssetValidation(t *testing.T) {
	itImpl, err := implement.Get(core.DomainIT)
	if err != nil {
		t.Fatalf("Get(IT) failed: %v", err)
	}

	validJSON := []byte(`{"os":"Debian 12","serial_number":"SN-998822","mac_address":"00:1A:2B:3C:4D:5E"}`)
	if err := itImpl.ValidateAssetMetadata("WORKSTATION", validJSON); err != nil {
		t.Fatalf("expected valid workstation metadata, got error: %v", err)
	}

	invalidJSON := []byte(`{invalid-json`)
	if err := itImpl.ValidateAssetMetadata("WORKSTATION", invalidJSON); err == nil {
		t.Fatal("expected error on invalid JSON, got nil")
	}

	if err := itImpl.ValidateAssetMetadata("UNSUPPORTED_TYPE", validJSON); err == nil {
		t.Fatal("expected error on unsupported asset type, got nil")
	}
}
