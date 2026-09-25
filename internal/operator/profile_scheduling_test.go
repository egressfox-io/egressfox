package operator

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/observation"
)

func TestProfileProbeBudgetAndCursorFairness(t *testing.T) {
	credential, _ := endpoint.NewVLESSCredential("11111111-1111-4111-8111-111111111111")
	tls, _ := endpoint.NewTLS("", false)
	sourceID, _ := endpoint.NewSourceID("shared")
	records := make([]endpoint.Record, 0, 100)
	for i := 0; i < 100; i++ {
		address, _ := endpoint.NewAddress("edge.example.com", 1000+i)
		configuration, err := endpoint.NewConfiguration(endpoint.ProtocolVLESS, address, credential, endpoint.NewTCPTransport(), tls)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := endpoint.NewRecordID(fmt.Sprintf("record-%d", i))
		provenance, _ := endpoint.NewProvenance(sourceID, id)
		record, err := endpoint.NewRecord(configuration, provenance)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	id, _ := observation.NewTargetID("target")
	target, _ := observation.NewHTTPTarget(id, "https://example.com", 204, time.Second, observation.HTTPOptions{})
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	pipeline := &Pipeline{cursors: map[types.UID]int{}, probeBudget: map[probeBudgetKey]probeBudgetState{}}
	revisionBytes, _ := target.Revision().RevealForPersistence()
	var revision [32]byte
	copy(revision[:], revisionBytes)
	alphaKey := profileCursorKey{gateway: "a", name: "alpha", engine: artifact.Mihomo11931, target: target.ID().String(), revision: revision}
	jobs := pipeline.budgetedJobs("pool", "a", "alpha", artifact.Mihomo11931, records, target, time.Minute, now)
	if len(jobs) != namedProbeBatch || pipeline.namedCursors[alphaKey] != namedProbeBatch {
		t.Fatalf("alpha first batch=%d cursor=%d", len(jobs), pipeline.namedCursors[alphaKey])
	}
	if again := pipeline.budgetedJobs("pool", "another-gateway", "alpha", artifact.Mihomo11931, records, target, time.Minute, now); len(again) != 0 {
		t.Fatalf("duplicate profile consumed %d extra jobs", len(again))
	}
	if small := pipeline.budgetedJobs("pool", "b", "beta", artifact.Mihomo11931, records[:1], target, time.Minute, now); len(small) != 1 {
		t.Fatalf("small profile starved: %d", len(small))
	}
	if alternate := pipeline.budgetedJobs("pool", "c", "alpha", artifact.SingBox1141, records, target, time.Minute, now); len(alternate) != namedProbeBatch {
		t.Fatalf("other engine starved: %d", len(alternate))
	}
	if next := pipeline.budgetedJobs("pool", "a", "alpha", artifact.Mihomo11931, records, target, time.Minute, now.Add(time.Minute)); len(next) != namedProbeBatch || pipeline.namedCursors[alphaKey] != 2*namedProbeBatch {
		t.Fatalf("next batch=%d cursor=%d", len(next), pipeline.namedCursors[alphaKey])
	}
	if legacy := pipeline.budgetedJobs("pool", "legacy", "default", artifact.Mihomo11931, records, target, time.Minute, now); len(legacy) != maxProbeBatch {
		t.Fatalf("legacy batch changed: %d", len(legacy))
	}
}
