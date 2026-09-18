package endpoint

import (
	"bytes"
	"fmt"
	"sort"
)

// Record is one normalized connection and all source relationships that currently
// contribute it.
type Record struct {
	configuration Configuration
	provenance    []Provenance
}

// NewRecord constructs an inventory input record.
func NewRecord(configuration Configuration, provenance ...Provenance) (Record, error) {
	if !configuration.valid() {
		return Record{}, invalid("record.configuration", "must be constructed with NewConfiguration")
	}
	if len(provenance) == 0 {
		return Record{}, invalid("record.provenance", "must contain at least one source relationship")
	}
	for _, association := range provenance {
		if !association.valid() {
			return Record{}, invalid("record.provenance", "contains an invalid source relationship")
		}
	}
	return Record{configuration: configuration, provenance: mergeProvenance(provenance)}, nil
}

// Configuration returns the normalized connection configuration.
func (record Record) Configuration() Configuration { return record.configuration }

// ID returns the safe logical endpoint ID.
func (record Record) ID() ID { return record.configuration.ID() }

// Identity returns the full revision-sensitive connection identity.
func (record Record) Identity() Identity { return record.configuration.Identity() }

// Provenance returns a deep copy of the source relationships.
func (record Record) Provenance() []Provenance {
	result := make([]Provenance, len(record.provenance))
	for index, provenance := range record.provenance {
		result[index] = provenance.clone()
	}
	return result
}

func (record Record) valid() bool {
	if !record.configuration.valid() || len(record.provenance) == 0 {
		return false
	}
	for _, provenance := range record.provenance {
		if !provenance.valid() {
			return false
		}
	}
	return true
}

func (record Record) clone() Record {
	record.provenance = record.Provenance()
	return record
}

func (record Record) String() string {
	if !record.valid() {
		return "endpoint record <invalid>"
	}
	return fmt.Sprintf("endpoint record %s provenance=%d", record.Identity(), len(record.provenance))
}

func (record Record) GoString() string { return record.String() }

func (record Record) Format(state fmt.State, _ rune) { writeSafeFormat(state, record.String()) }

func (Record) MarshalJSON() ([]byte, error) {
	return nil, errorsForJSON("endpoint record")
}

// Inventory is a deterministic, provenance-preserving set of connection records.
type Inventory struct {
	records []Record
}

// Records returns a deep copy in deterministic identity order.
func (inventory Inventory) Records() []Record {
	result := make([]Record, len(inventory.records))
	for index, record := range inventory.records {
		result[index] = record.clone()
	}
	return result
}

// Len returns the number of distinct complete connection identities.
func (inventory Inventory) Len() int { return len(inventory.records) }

func (inventory Inventory) String() string {
	return fmt.Sprintf("endpoint inventory records=%d", len(inventory.records))
}

func (inventory Inventory) GoString() string { return inventory.String() }

func (inventory Inventory) Format(state fmt.State, _ rune) {
	writeSafeFormat(state, inventory.String())
}

func (Inventory) MarshalJSON() ([]byte, error) {
	return nil, errorsForJSON("endpoint inventory")
}

// Deduplicate merges records with equal complete connection identities, unions
// their provenance, and returns deterministic output. Different credential
// revisions remain distinct records even when their logical endpoint ID is equal.
func Deduplicate(records []Record) (Inventory, error) {
	ordered := make([]Record, len(records))
	for index, record := range records {
		if !record.valid() {
			return Inventory{}, invalid("inventory.record", "must be constructed with NewRecord")
		}
		ordered[index] = record.clone()
	}
	sortRecords(ordered)

	result := make([]Record, 0, len(ordered))
	for _, record := range ordered {
		if len(result) == 0 || !result[len(result)-1].Identity().Equal(record.Identity()) {
			result = append(result, record)
			continue
		}

		merged, err := mergeMatchingRecords(result[len(result)-1], record)
		if err != nil {
			return Inventory{}, err
		}
		result[len(result)-1] = merged
	}

	return Inventory{records: result}, nil
}

func mergeMatchingRecords(left, right Record) (Record, error) {
	if !left.configuration.Equivalent(right.configuration) {
		return Record{}, &ConflictError{id: left.ID()}
	}
	left.provenance = mergeProvenance(left.provenance, right.provenance)
	return left, nil
}

func sortRecords(records []Record) {
	sort.Slice(records, func(left, right int) bool {
		leftIdentity := records[left].Identity()
		rightIdentity := records[right].Identity()
		if leftIdentity.id.value != rightIdentity.id.value {
			return leftIdentity.id.value < rightIdentity.id.value
		}
		return bytes.Compare(leftIdentity.revision[:], rightIdentity.revision[:]) < 0
	})
}
