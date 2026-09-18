package source

import (
	"fmt"
	"sort"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

type Snapshot struct {
	source  endpoint.SourceID
	records []endpoint.Record
}

func newSnapshot(source endpoint.SourceID, records []endpoint.Record) Snapshot {
	return Snapshot{source: source, records: append([]endpoint.Record(nil), records...)}
}

func (s Snapshot) SourceID() endpoint.SourceID { return s.source }
func (s Snapshot) Records() []endpoint.Record  { return append([]endpoint.Record(nil), s.records...) }
func (s Snapshot) Len() int                    { return len(s.records) }
func (s Snapshot) String() string {
	return fmt.Sprintf("source snapshot source=%s records=%d", s.source, len(s.records))
}

type Set struct{ snapshots map[string]Snapshot }

func (set Set) Replace(snapshot Snapshot, allowEmpty bool) (Set, error) {
	if snapshot.source.String() == "<invalid-source-id>" {
		return set, failure(snapshot.source, ErrRefresh, "invalid_snapshot")
	}
	if snapshot.Len() == 0 && !allowEmpty {
		return set, failure(snapshot.source, ErrRefresh, "empty_snapshot")
	}
	result := set.clone()
	result.snapshots[snapshot.source.String()] = newSnapshot(snapshot.source, snapshot.records)
	return result, nil
}

func (set Set) Remove(source endpoint.SourceID) Set {
	result := set.clone()
	delete(result.snapshots, source.String())
	return result
}

func (set Set) Snapshot(source endpoint.SourceID) (Snapshot, bool) {
	snapshot, ok := set.snapshots[source.String()]
	return newSnapshot(snapshot.source, snapshot.records), ok
}

func (set Set) Inventory() (endpoint.Inventory, error) {
	keys := make([]string, 0, len(set.snapshots))
	for key := range set.snapshots {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var records []endpoint.Record
	for _, key := range keys {
		records = append(records, set.snapshots[key].records...)
	}
	return endpoint.Deduplicate(records)
}

func (set Set) clone() Set {
	result := Set{snapshots: make(map[string]Snapshot, len(set.snapshots)+1)}
	for key, snapshot := range set.snapshots {
		result.snapshots[key] = newSnapshot(snapshot.source, snapshot.records)
	}
	return result
}
