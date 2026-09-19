package engine

import (
	"errors"
	"fmt"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/policy"
)

var ErrUnsupported = errors.New("unsupported engine capability")

type Renderer interface {
	Profile() artifact.Profile
	Render(policy.Gateway) (artifact.Candidate, error)
}

type CapabilityError struct {
	Profile artifact.Profile
	Field   string
	Feature string
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("engine %s does not support %s at %s", e.Profile, e.Feature, e.Field)
}

func (e *CapabilityError) Unwrap() error { return ErrUnsupported }

type NamedRecord struct {
	Name   string
	Record endpoint.Record
}

func AssignNames(inventory endpoint.Inventory) []NamedRecord {
	counts := make(map[string]int)
	records := inventory.Records()
	result := make([]NamedRecord, len(records))
	for index, record := range records {
		id := record.ID().String()
		counts[id]++
		result[index] = NamedRecord{
			Name:   fmt.Sprintf("egressfox-%s-%03d", id, counts[id]),
			Record: record,
		}
	}
	return result
}
