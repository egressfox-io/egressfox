package source_test

import (
	"reflect"
	"testing"

	"github.com/egressfox-io/egressfox/internal/source"
)

func TestSnapshotReplacementRemovalAndCrossSourceProvenance(t *testing.T) {
	t.Parallel()
	a1, _, _ := parse(t, "source-a", []byte("trojan://shared@example.com:443#A\ntrojan://gone@gone.example.com:443"), source.ParseOptions{Format: source.FormatURIList})
	b1, _, _ := parse(t, "source-b", []byte("trojan://shared@example.com:443#B"), source.ParseOptions{Format: source.FormatURIList})
	set, err := (source.Set{}).Replace(a1, false)
	if err != nil {
		t.Fatal(err)
	}
	set, err = set.Replace(b1, false)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := set.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Len() != 2 {
		t.Fatalf("initial inventory = %d", inventory.Len())
	}
	a2, _, _ := parse(t, "source-a", []byte("trojan://new@new.example.com:443"), source.ParseOptions{Format: source.FormatURIList})
	set, err = set.Replace(a2, false)
	if err != nil {
		t.Fatal(err)
	}
	inventory, _ = set.Inventory()
	if inventory.Len() != 2 {
		t.Fatalf("replaced inventory = %d", inventory.Len())
	}
	var sources []string
	for _, record := range inventory.Records() {
		if record.Configuration().Address().Host() == "example.com" {
			for _, p := range record.Provenance() {
				sources = append(sources, p.SourceID().String())
			}
		}
	}
	if !reflect.DeepEqual(sources, []string{"source-b"}) {
		t.Fatalf("shared provenance = %v", sources)
	}
	set = set.Remove(sourceID(t, "source-b"))
	inventory, _ = set.Inventory()
	if inventory.Len() != 1 {
		t.Fatalf("removed inventory = %d", inventory.Len())
	}
}

func TestEmptyReplacementRequiresPolicy(t *testing.T) {
	t.Parallel()
	full, _, _ := parse(t, "source-a", []byte("trojan://secret@example.com:443"), source.ParseOptions{Format: source.FormatURIList})
	empty, _, _ := parse(t, "source-a", nil, source.ParseOptions{Format: source.FormatURIList})
	set, _ := (source.Set{}).Replace(full, false)
	if _, err := set.Replace(empty, false); err == nil {
		t.Fatal("empty replacement unexpectedly accepted")
	}
	set, err := set.Replace(empty, true)
	if err != nil {
		t.Fatal(err)
	}
	inventory, _ := set.Inventory()
	if inventory.Len() != 0 {
		t.Fatalf("empty inventory = %d", inventory.Len())
	}
}
