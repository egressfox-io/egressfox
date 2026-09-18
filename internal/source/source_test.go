package source_test

import (
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

func sourceID(t testing.TB, value string) endpoint.SourceID {
	t.Helper()
	id, err := endpoint.NewSourceID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
