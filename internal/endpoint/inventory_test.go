package endpoint_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

func TestDeduplicatePreservesAndCanonicalizesProvenance(t *testing.T) {
	t.Parallel()

	configuration := mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), mustTLS(t, "", false))
	fromB := mustRecord(t, configuration, mustProvenance(t, "source-b", "node-1", "Zulu", "Beta"))
	fromA := mustRecord(t, configuration, mustProvenance(t, "source-a", "node-9", "Alpha"))
	duplicateB := mustRecord(t, configuration, mustProvenance(t, "source-b", "node-1", "Renamed", "Beta"))

	inventory, err := endpoint.Deduplicate([]endpoint.Record{fromB, fromA, duplicateB})
	if err != nil {
		t.Fatalf("Deduplicate() error = %v", err)
	}
	if inventory.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", inventory.Len())
	}

	record := inventory.Records()[0]
	if !record.Identity().Equal(configuration.Identity()) {
		t.Fatal("deduplication changed connection identity")
	}
	provenance := record.Provenance()
	if len(provenance) != 2 {
		t.Fatalf("provenance count = %d, want 2", len(provenance))
	}
	if got := provenance[0].SourceID().String(); got != "source-a" {
		t.Fatalf("first source = %q, want source-a", got)
	}
	if got := provenance[1].SourceID().String(); got != "source-b" {
		t.Fatalf("second source = %q, want source-b", got)
	}
	if got, want := provenance[1].Aliases(), []string{"Beta", "Renamed", "Zulu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source-b aliases = %#v, want %#v", got, want)
	}
	if fromA.ID() != fromB.ID() || fromA.ID() != duplicateB.ID() {
		t.Fatal("source or display-name differences changed endpoint ID")
	}
}

func TestDeduplicateIsOrderIndependentAndIdempotent(t *testing.T) {
	t.Parallel()

	first := mustVLESSConfiguration(t, "alpha.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), endpoint.DisabledTLS())
	second := mustTrojanConfiguration(t, "beta.example.com", "synthetic-password", mustWebSocket(t, "/proxy"), mustTLS(t, "", false))
	rotated := mustVLESSConfiguration(t, "alpha.example.com", "94515a2d-6681-4df5-933c-ab937e3539ca", endpoint.NewTCPTransport(), endpoint.DisabledTLS())

	input := []endpoint.Record{
		mustRecord(t, second, mustProvenance(t, "source-c", "node-3", "Third")),
		mustRecord(t, first, mustProvenance(t, "source-b", "node-2", "Second")),
		mustRecord(t, rotated, mustProvenance(t, "source-a", "node-1", "Rotated")),
		mustRecord(t, first, mustProvenance(t, "source-a", "node-4", "First")),
	}

	baseline, err := endpoint.Deduplicate(input)
	if err != nil {
		t.Fatalf("Deduplicate() error = %v", err)
	}
	baselineRecords := baseline.Records()
	if len(baselineRecords) != 3 {
		t.Fatalf("record count = %d, want 3", len(baselineRecords))
	}

	for _, permutation := range permutations(len(input)) {
		permuted := make([]endpoint.Record, len(input))
		for index, sourceIndex := range permutation {
			permuted[index] = input[sourceIndex]
		}
		got, dedupeErr := endpoint.Deduplicate(permuted)
		if dedupeErr != nil {
			t.Fatalf("Deduplicate(permutation %v) error = %v", permutation, dedupeErr)
		}
		if !reflect.DeepEqual(got.Records(), baselineRecords) {
			t.Fatalf("permutation %v changed inventory\n got: %#v\nwant: %#v", permutation, got.Records(), baselineRecords)
		}
	}

	idempotent, err := endpoint.Deduplicate(baseline.Records())
	if err != nil {
		t.Fatalf("Deduplicate(deduplicated inventory) error = %v", err)
	}
	if !reflect.DeepEqual(idempotent.Records(), baselineRecords) {
		t.Fatalf("deduplication is not idempotent\n got: %#v\nwant: %#v", idempotent.Records(), baselineRecords)
	}

	ids := make([]string, len(baselineRecords))
	for index, record := range baselineRecords {
		ids[index] = record.ID().String()
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("logical ID output order is not deterministic: %v", ids)
	}
}

func TestCredentialRevisionsRemainDistinctInventoryRecords(t *testing.T) {
	t.Parallel()

	first := mustTrojanConfiguration(t, "edge.example.com", "first-password", endpoint.NewTCPTransport(), mustTLS(t, "", false))
	second := mustTrojanConfiguration(t, "edge.example.com", "second-password", endpoint.NewTCPTransport(), mustTLS(t, "", false))
	records := []endpoint.Record{
		mustRecord(t, first, mustProvenance(t, "source-a", "node-1", "Original")),
		mustRecord(t, second, mustProvenance(t, "source-b", "node-2", "Rotated")),
	}

	inventory, err := endpoint.Deduplicate(records)
	if err != nil {
		t.Fatalf("Deduplicate() error = %v", err)
	}
	if inventory.Len() != 2 {
		t.Fatalf("Len() = %d, want 2 credential revisions", inventory.Len())
	}
	got := inventory.Records()
	if got[0].ID() != got[1].ID() {
		t.Fatal("credential rotation did not preserve logical endpoint ID")
	}
	if got[0].Identity().Equal(got[1].Identity()) {
		t.Fatal("credential revisions share a complete connection identity")
	}
}

func TestProvenanceValidationAndCopyIsolation(t *testing.T) {
	t.Parallel()

	const sourceURI = "https://user:synthetic-canary@example.com/subscription?token=synthetic"
	_, sourceURIErr := endpoint.NewSourceID(sourceURI)
	if !errors.Is(sourceURIErr, endpoint.ErrInvalid) {
		t.Fatalf("source URI error = %v, want ErrInvalid", sourceURIErr)
	}
	assertDoesNotContain(t, sourceURIErr.Error(), sourceURI)

	for _, value := range []string{"", "source/uri", "source:id", "источник"} {
		if _, err := endpoint.NewSourceID(value); !errors.Is(err, endpoint.ErrInvalid) {
			t.Fatalf("NewSourceID(%q) error = %v, want ErrInvalid", value, err)
		}
	}
	if _, err := endpoint.NewRecordID(strings.Repeat("a", 129)); !errors.Is(err, endpoint.ErrInvalid) {
		t.Fatalf("oversized NewRecordID() error = %v, want ErrInvalid", err)
	}

	source := mustSourceID(t, "source-a")
	recordID := mustRecordID(t, "node-1")
	for _, alias := range []string{"", "line\nbreak", "hidden\u200dformat", strings.Repeat("a", 257), string([]byte{0xff})} {
		if _, err := endpoint.NewProvenance(source, recordID, alias); !errors.Is(err, endpoint.ErrInvalid) {
			t.Fatalf("NewProvenance(alias %q) error = %v, want ErrInvalid", alias, err)
		}
	}

	provenance := mustProvenance(t, "source-a", "node-1", "Beta", "Alpha", "Alpha")
	aliases := provenance.Aliases()
	aliases[0] = "mutated"
	if got, want := provenance.Aliases(), []string{"Alpha", "Beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("caller mutation changed provenance aliases: %#v", got)
	}
}

func TestRecordAndInventoryRejectInvalidInput(t *testing.T) {
	t.Parallel()

	configuration := mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), endpoint.DisabledTLS())
	if _, err := endpoint.NewRecord(configuration); !errors.Is(err, endpoint.ErrInvalid) {
		t.Fatalf("NewRecord(without provenance) error = %v, want ErrInvalid", err)
	}
	if _, err := endpoint.NewRecord(endpoint.Configuration{}, mustProvenance(t, "source-a", "node-1")); !errors.Is(err, endpoint.ErrInvalid) {
		t.Fatalf("NewRecord(zero configuration) error = %v, want ErrInvalid", err)
	}
	if _, err := endpoint.Deduplicate([]endpoint.Record{{}}); !errors.Is(err, endpoint.ErrInvalid) {
		t.Fatalf("Deduplicate(zero record) error = %v, want ErrInvalid", err)
	}
}

func TestProvenanceDiagnosticsDoNotExposeAliases(t *testing.T) {
	t.Parallel()

	const alias = "vless://synthetic-user@example.com:443?credential=synthetic-canary"
	configuration := mustVLESSConfiguration(t, "edge.example.com", syntheticVLESSUserID, endpoint.NewTCPTransport(), endpoint.DisabledTLS())
	provenance := mustProvenance(t, "source-a", "node-1", alias)
	record := mustRecord(t, configuration, provenance)
	inventory, err := endpoint.Deduplicate([]endpoint.Record{record})
	if err != nil {
		t.Fatalf("Deduplicate() error = %v", err)
	}

	var values []string
	formats := []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%p"}
	for _, value := range []any{provenance, record, inventory} {
		for _, format := range formats {
			values = append(values, fmt.Sprintf(format, value))
		}
	}
	values = append(values,
		fmt.Sprintf("%#v", []endpoint.Provenance{provenance}),
		fmt.Sprintf("%d", []endpoint.Provenance{provenance}),
		fmt.Sprintf("%d", []endpoint.Record{record}),
	)
	for _, value := range values {
		assertDoesNotContain(t, value, alias)
	}

	for _, value := range []any{provenance, record, inventory} {
		encoded, marshalErr := json.Marshal(value)
		assertDoesNotContain(t, string(encoded), alias)
		if marshalErr == nil {
			t.Fatalf("json.Marshal(%T) unexpectedly succeeded", value)
		}
		assertDoesNotContain(t, marshalErr.Error(), alias)
	}
}

func permutations(size int) [][]int {
	values := make([]int, size)
	for index := range values {
		values[index] = index
	}
	var result [][]int
	var generate func(int)
	generate = func(index int) {
		if index == size {
			result = append(result, append([]int(nil), values...))
			return
		}
		for swap := index; swap < size; swap++ {
			values[index], values[swap] = values[swap], values[index]
			generate(index + 1)
			values[index], values[swap] = values[swap], values[index]
		}
	}
	generate(0)
	return result
}

func mustSourceID(t testing.TB, value string) endpoint.SourceID {
	t.Helper()
	id, err := endpoint.NewSourceID(value)
	if err != nil {
		t.Fatalf("NewSourceID() error = %v", err)
	}
	return id
}

func mustRecordID(t testing.TB, value string) endpoint.RecordID {
	t.Helper()
	id, err := endpoint.NewRecordID(value)
	if err != nil {
		t.Fatalf("NewRecordID() error = %v", err)
	}
	return id
}

func mustProvenance(t testing.TB, source, record string, aliases ...string) endpoint.Provenance {
	t.Helper()
	provenance, err := endpoint.NewProvenance(mustSourceID(t, source), mustRecordID(t, record), aliases...)
	if err != nil {
		t.Fatalf("NewProvenance() error = %v", err)
	}
	return provenance
}

func mustRecord(t testing.TB, configuration endpoint.Configuration, provenance ...endpoint.Provenance) endpoint.Record {
	t.Helper()
	record, err := endpoint.NewRecord(configuration, provenance...)
	if err != nil {
		t.Fatalf("NewRecord() error = %v", err)
	}
	return record
}
