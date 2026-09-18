package endpoint

import (
	"fmt"
	"sort"
	"unicode"
	"unicode/utf8"
)

const (
	maxProvenanceIDBytes = 128
	maxAliasBytes        = 256
)

// SourceID is a safe application-generated source identifier. It is not a URL.
type SourceID struct {
	value string
}

// NewSourceID validates a source identifier for use in provenance and diagnostics.
func NewSourceID(value string) (SourceID, error) {
	if err := validateProvenanceID("provenance.source_id", value); err != nil {
		return SourceID{}, err
	}
	return SourceID{value: value}, nil
}

func (id SourceID) String() string {
	if id.value == "" {
		return "<invalid-source-id>"
	}
	return id.value
}

func (id SourceID) GoString() string { return fmt.Sprintf("endpoint.SourceID(%q)", id.String()) }

// RecordID is a safe source-local record identifier. It is not raw source input.
type RecordID struct {
	value string
}

// NewRecordID validates a source-local record identifier.
func NewRecordID(value string) (RecordID, error) {
	if err := validateProvenanceID("provenance.record_id", value); err != nil {
		return RecordID{}, err
	}
	return RecordID{value: value}, nil
}

func (id RecordID) String() string {
	if id.value == "" {
		return "<invalid-record-id>"
	}
	return id.value
}

func (id RecordID) GoString() string { return fmt.Sprintf("endpoint.RecordID(%q)", id.String()) }

func validateProvenanceID(field, value string) error {
	if len(value) == 0 || len(value) > maxProvenanceIDBytes {
		return invalid(field, "must contain between 1 and 128 characters")
	}
	for index := range len(value) {
		character := value[index]
		alphanumeric := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9'
		if !alphanumeric && character != '.' && character != '_' && character != '-' {
			return invalid(field, "must use only ASCII letters, digits, dots, underscores, and hyphens")
		}
	}
	return nil
}

// Provenance associates one connection with a source-local record and its display
// aliases. Aliases are untrusted metadata and are omitted from formatting.
type Provenance struct {
	source        SourceID
	record        RecordID
	aliases       *aliasSet
	nonComparable []struct{}
}

type aliasSet struct {
	values []string
}

// NewProvenance constructs a source relationship and canonicalizes its alias set.
func NewProvenance(source SourceID, record RecordID, aliases ...string) (Provenance, error) {
	if source.value == "" {
		return Provenance{}, invalid("provenance.source_id", "must be constructed with NewSourceID")
	}
	if record.value == "" {
		return Provenance{}, invalid("provenance.record_id", "must be constructed with NewRecordID")
	}
	normalized, err := normalizeAliases(aliases)
	if err != nil {
		return Provenance{}, err
	}
	return Provenance{source: source, record: record, aliases: &aliasSet{values: normalized}}, nil
}

// SourceID returns the contributing source.
func (provenance Provenance) SourceID() SourceID { return provenance.source }

// RecordID returns the stable source-local record identifier.
func (provenance Provenance) RecordID() RecordID { return provenance.record }

// Aliases returns a copy of the source-provided display aliases.
func (provenance Provenance) Aliases() []string {
	if provenance.aliases == nil {
		return nil
	}
	return append([]string(nil), provenance.aliases.values...)
}

func (provenance Provenance) valid() bool {
	return provenance.source.value != "" && provenance.record.value != ""
}

func (provenance Provenance) key() string {
	return provenance.source.value + "\x00" + provenance.record.value
}

func (provenance Provenance) clone() Provenance {
	provenance.aliases = &aliasSet{values: provenance.Aliases()}
	return provenance
}

func (provenance Provenance) String() string {
	if !provenance.valid() {
		return "provenance <invalid>"
	}
	return fmt.Sprintf("provenance source=%s record=%s aliases=%d", provenance.source, provenance.record, len(provenance.Aliases()))
}

func (provenance Provenance) GoString() string { return provenance.String() }

func (provenance Provenance) Format(state fmt.State, _ rune) {
	writeSafeFormat(state, provenance.String())
}

func (Provenance) MarshalJSON() ([]byte, error) {
	return nil, errorsForJSON("endpoint provenance")
}

func normalizeAliases(aliases []string) ([]string, error) {
	unique := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		if alias == "" {
			return nil, invalid("provenance.alias", "must not be empty")
		}
		if len(alias) > maxAliasBytes {
			return nil, invalid("provenance.alias", "must not exceed 256 bytes")
		}
		if !utf8.ValidString(alias) {
			return nil, invalid("provenance.alias", "must be valid UTF-8")
		}
		for _, character := range alias {
			if unicode.IsControl(character) || unicode.In(character, unicode.Cf) {
				return nil, invalid("provenance.alias", "must not contain control characters")
			}
		}
		unique[alias] = struct{}{}
	}

	normalized := make([]string, 0, len(unique))
	for alias := range unique {
		normalized = append(normalized, alias)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func mergeProvenance(groups ...[]Provenance) []Provenance {
	byKey := make(map[string]Provenance)
	for _, group := range groups {
		for _, provenance := range group {
			key := provenance.key()
			if existing, found := byKey[key]; found {
				aliases, _ := normalizeAliases(append(existing.Aliases(), provenance.Aliases()...))
				existing.aliases = &aliasSet{values: aliases}
				byKey[key] = existing
				continue
			}
			byKey[key] = provenance.clone()
		}
	}

	merged := make([]Provenance, 0, len(byKey))
	for _, provenance := range byKey {
		merged = append(merged, provenance)
	}
	sort.Slice(merged, func(left, right int) bool {
		if merged[left].source.value != merged[right].source.value {
			return merged[left].source.value < merged[right].source.value
		}
		return merged[left].record.value < merged[right].record.value
	})
	return merged
}
