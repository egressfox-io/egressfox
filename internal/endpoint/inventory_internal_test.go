package endpoint

import (
	"errors"
	"strings"
	"testing"
)

func TestMergeMatchingRecordsRejectsConfigurationConflictSafely(t *testing.T) {
	t.Parallel()

	address, err := NewAddress("edge.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	tls, err := NewTLS("", false)
	if err != nil {
		t.Fatal(err)
	}
	firstCredential, err := NewTrojanCredential("first-conflict-canary")
	if err != nil {
		t.Fatal(err)
	}
	secondCredential, err := NewTrojanCredential("second-conflict-canary")
	if err != nil {
		t.Fatal(err)
	}
	firstConfig, err := NewConfiguration(ProtocolTrojan, address, firstCredential, NewTCPTransport(), tls)
	if err != nil {
		t.Fatal(err)
	}
	secondConfig, err := NewConfiguration(ProtocolTrojan, address, secondCredential, NewTCPTransport(), tls)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := NewSourceID("source-a")
	recordID, _ := NewRecordID("node-1")
	provenance, _ := NewProvenance(source, recordID, "conflict-alias-canary")
	first, _ := NewRecord(firstConfig, provenance)
	second, _ := NewRecord(secondConfig, provenance)

	// Deduplicate calls this guard only after a complete identity match. Supplying
	// two configurations directly exercises the collision/inconsistent-key path.
	_, err = mergeMatchingRecords(first, second)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("mergeMatchingRecords() error = %v, want ErrConflict", err)
	}
	for _, secret := range []string{"first-conflict-canary", "second-conflict-canary", "conflict-alias-canary"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("conflict diagnostic contained a redaction canary")
		}
	}
}
