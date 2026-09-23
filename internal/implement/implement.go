package implement

import (
	"errors"
	"fmt"
	"sync"

	"github.com/project-chassis/chassis/internal/core"
)

var (
	ErrDuplicateImplement = errors.New("implement already registered for domain")
	ErrImplementNotFound  = errors.New("implement not found for domain")
)

// Implement defines the contract for domain-specific micro-applications in Chassis.
// (IT, Edu, Civic Casework, MRO / Trades).
type Implement interface {
	Domain() core.DomainType
	DisplayName() string
	ItemCodePrefix() string
	ValidateAssetMetadata(assetType string, rawJSON []byte) error
	SupportedAssetTypes() []string
}

// Registry manages active domain implements.
type Registry struct {
	mu         sync.RWMutex
	implements map[core.DomainType]Implement
}

// NewRegistry creates an empty implement registry.
func NewRegistry() *Registry {
	return &Registry{
		implements: make(map[core.DomainType]Implement),
	}
}

// Register adds an implement to the registry.
func (r *Registry) Register(impl Implement) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	domain := impl.Domain()
	if _, exists := r.implements[domain]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateImplement, domain)
	}
	r.implements[domain] = impl
	return nil
}

// Get retrieves an implement by domain type.
func (r *Registry) Get(domain core.DomainType) (Implement, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	impl, exists := r.implements[domain]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrImplementNotFound, domain)
	}
	return impl, nil
}

// List returns all registered implements.
func (r *Registry) List() []Implement {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]Implement, 0, len(r.implements))
	for _, impl := range r.implements {
		list = append(list, impl)
	}
	return list
}

// Global default registry
var DefaultRegistry = NewRegistry()

// Register registers an implement in the default registry.
func Register(impl Implement) error {
	return DefaultRegistry.Register(impl)
}

// Get retrieves an implement from the default registry.
func Get(domain core.DomainType) (Implement, error) {
	return DefaultRegistry.Get(domain)
}
