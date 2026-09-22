package policy_test

import (
	"errors"
	"strings"
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

func TestManagedListenerRequiresBoundedSafeAuthentication(t *testing.T) {
	t.Parallel()
	if _, err := policy.NewManagedSOCKSListener(1080, "egressfox", "short"); !errors.Is(err, policy.ErrInvalid) {
		t.Fatalf("short password error = %v", err)
	}
	if _, err := policy.NewManagedSOCKSListener(1080, "bad:user", "synthetic-password-0123456789"); !errors.Is(err, policy.ErrInvalid) {
		t.Fatalf("unsafe username error = %v", err)
	}
	listener, err := policy.NewManagedSOCKSListener(1080, "egressfox", "synthetic-password-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	if !listener.Managed() || listener.Address() != "0.0.0.0" || listener.Username() != "egressfox" {
		t.Fatalf("unexpected managed listener: address=%s managed=%t", listener.Address(), listener.Managed())
	}
	if strings.Contains(listener.Password(), ":") {
		t.Fatal("managed password contains Mihomo credential separator")
	}
}
