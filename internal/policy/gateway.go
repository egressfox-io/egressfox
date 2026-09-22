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
	mode    listenerMode
	user    string
	pass    string
}

type listenerMode uint8

const (
	listenerLoopback listenerMode = iota + 1
	listenerManaged
)

func NewSOCKSListener(address string, port int) (Listener, error) {
	parsed, err := netip.ParseAddr(address)
	if err != nil || !parsed.IsLoopback() || parsed.Zone() != "" {
		return Listener{}, invalid("listener.address", "must be a loopback IP address")
	}
	if port < 1 || port > 65535 {
		return Listener{}, invalid("listener.port", "must be between 1 and 65535")
	}
	return Listener{address: parsed, port: uint16(port), mode: listenerLoopback}, nil
}

// NewManagedSOCKSListener constructs the common authenticated Pod-network
// listener supported by both exact M7 engine profiles. Credentials are deliberately
// available only through explicit accessors and never through formatting.
func NewManagedSOCKSListener(port int, username, password string) (Listener, error) {
	if port < 1 || port > 65535 {
		return Listener{}, invalid("listener.port", "must be between 1 and 65535")
	}
	if !safeCredential(username, 1, 64) {
		return Listener{}, invalid("listener.authentication.username", "must be 1 to 64 safe ASCII characters")
	}
	if !safeCredential(password, 16, 128) {
		return Listener{}, invalid("listener.authentication.password", "must be 16 to 128 safe ASCII characters")
	}
	return Listener{address: netip.IPv4Unspecified(), port: uint16(port), mode: listenerManaged, user: username, pass: password}, nil
}

func safeCredential(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' || character == '~') {
			return false
		}
	}
	return true
}

func (l Listener) Address() string { return l.address.String() }
func (l Listener) Port() uint16    { return l.port }
func (l Listener) Managed() bool   { return l.mode == listenerManaged }
func (l Listener) Username() string {
	return l.user
}
func (l Listener) Password() string {
	return l.pass
}

type Gateway struct {
	inventory endpoint.Inventory
	listener  Listener
}

func NewGateway(inventory endpoint.Inventory, listener Listener) (Gateway, error) {
	if inventory.Len() == 0 {
		return Gateway{}, invalid("inventory", "must contain at least one admitted endpoint")
	}
	if !listener.address.IsValid() || listener.port == 0 || listener.mode == 0 {
		return Gateway{}, invalid("listener", "must be constructed with a SOCKS listener constructor")
	}
	if listener.mode == listenerLoopback && !listener.address.IsLoopback() {
		return Gateway{}, invalid("listener", "BYO listener must be loopback")
	}
	if listener.mode == listenerManaged && (!listener.address.IsUnspecified() || listener.user == "" || listener.pass == "") {
		return Gateway{}, invalid("listener", "managed listener must be authenticated on the Pod network")
	}
	return Gateway{inventory: inventory, listener: listener}, nil
}

func (g Gateway) Inventory() endpoint.Inventory { return g.inventory }
func (g Gateway) Listener() Listener            { return g.listener }
func (g Gateway) String() string {
	mode := "loopback"
	if g.listener.Managed() {
		mode = "managed-authenticated"
	}
	return fmt.Sprintf("gateway policy endpoints=%d listener=%s:%d credentials=<redacted>", g.inventory.Len(), mode, g.listener.port)
}

type ValidationError struct{ field, problem string }

func invalid(field, problem string) error { return &ValidationError{field: field, problem: problem} }
func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid gateway field %q: %s", e.field, e.problem)
}
func (e *ValidationError) Unwrap() error { return ErrInvalid }
func (e *ValidationError) Field() string { return e.field }
