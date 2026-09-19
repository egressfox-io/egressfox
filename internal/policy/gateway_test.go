package policy_test

import (
	"errors"
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/policy"
)

func TestGatewayRequiresInventoryAndLoopbackListener(t *testing.T) {
	t.Parallel()
	if _, err := policy.NewSOCKSListener("0.0.0.0", 1080); !errors.Is(err, policy.ErrInvalid) {
		t.Fatalf("non-loopback error = %v", err)
	}
	listener, err := policy.NewSOCKSListener("127.0.0.1", 1080)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.NewGateway(endpoint.Inventory{}, listener); !errors.Is(err, policy.ErrInvalid) {
		t.Fatalf("empty inventory error = %v", err)
	}
}
