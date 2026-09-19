package policy

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

var ErrInvalid = errors.New("invalid gateway policy")

type Listener struct {
	address netip.Addr
	port    uint16
}

func NewSOCKSListener(address string, port int) (Listener, error) {
	parsed, err := netip.ParseAddr(address)
	if err != nil || !parsed.IsLoopback() || parsed.Zone() != "" {
		return Listener{}, invalid("listener.address", "must be a loopback IP address")
	}
	if port < 1 || port > 65535 {
		return Listener{}, invalid("listener.port", "must be between 1 and 65535")
	}
	return Listener{address: parsed, port: uint16(port)}, nil
}

func (l Listener) Address() string { return l.address.String() }
func (l Listener) Port() uint16    { return l.port }

type Gateway struct {
	inventory endpoint.Inventory
	listener  Listener
}

func NewGateway(inventory endpoint.Inventory, listener Listener) (Gateway, error) {
	if inventory.Len() == 0 {
		return Gateway{}, invalid("inventory", "must contain at least one admitted endpoint")
	}
	if !listener.address.IsValid() || !listener.address.IsLoopback() || listener.port == 0 {
		return Gateway{}, invalid("listener", "must be constructed with NewSOCKSListener")
	}
	return Gateway{inventory: inventory, listener: listener}, nil
}

func (g Gateway) Inventory() endpoint.Inventory { return g.inventory }
func (g Gateway) Listener() Listener            { return g.listener }
func (g Gateway) String() string {
	return fmt.Sprintf("gateway policy endpoints=%d listener=loopback:%d", g.inventory.Len(), g.listener.port)
}

type ValidationError struct{ field, problem string }

func invalid(field, problem string) error { return &ValidationError{field: field, problem: problem} }
func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid gateway field %q: %s", e.field, e.problem)
}
func (e *ValidationError) Unwrap() error { return ErrInvalid }
func (e *ValidationError) Field() string { return e.field }
