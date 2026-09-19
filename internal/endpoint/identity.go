package endpoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	endpointIdentityDomain   = "egressfox.endpoint/v1"
	connectionRevisionDomain = "egressfox.connection/v1"
)

// ID is a versioned, non-secret logical endpoint identifier.
type ID struct {
	value string
}

// ParseID restores a validated safe logical endpoint identifier.
func ParseID(value string) (ID, error) {
	if !strings.HasPrefix(value, "ef1_") {
		return ID{}, invalid("endpoint.id", "has an unsupported identity version")
	}
	encoded := strings.TrimPrefix(value, "ef1_")
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(encoded))
	if err != nil || len(decoded) != sha256.Size || strings.ToLower(encoded) != encoded {
		return ID{}, invalid("endpoint.id", "is not a canonical endpoint identifier")
	}
	return ID{value: value}, nil
}

func (id ID) String() string {
	if id.value == "" {
		return "<invalid-endpoint-id>"
	}
	return id.value
}

func (id ID) GoString() string { return fmt.Sprintf("endpoint.ID(%q)", id.String()) }

// Identity is the complete connection identity. Its private revision changes
// with credentials and has no textual or serialization representation.
type Identity struct {
	id            ID
	revision      *[sha256.Size]byte
	nonComparable []struct{}
}

// ID returns the safe logical endpoint ID.
func (identity Identity) ID() ID { return identity.id }

// Revision returns the confidential connection revision.
func (identity Identity) Revision() Revision {
	if identity.revision == nil {
		return Revision{}
	}
	value := *identity.revision
	return Revision{value: &value}
}

// Equal reports whether two values describe the same complete connection revision.
func (identity Identity) Equal(other Identity) bool {
	if identity.id != other.id || identity.revision == nil || other.revision == nil {
		return identity.id == other.id && identity.revision == nil && other.revision == nil
	}
	return *identity.revision == *other.revision
}

// SameEndpoint reports whether two revisions belong to the same logical endpoint.
func (identity Identity) SameEndpoint(other Identity) bool { return identity.id == other.id }

func (identity Identity) String() string {
	return identity.id.String() + "@<private-revision>"
}

func (identity Identity) GoString() string { return "endpoint.Identity(" + identity.String() + ")" }

func (identity Identity) Format(state fmt.State, _ rune) {
	writeSafeFormat(state, identity.String())
}

func (Identity) MarshalJSON() ([]byte, error) {
	return nil, errorsForJSON("connection identity")
}

// Revision is a confidential deterministic connection revision. Its protected
// bytes may only cross an explicitly protected persistence boundary.
type Revision struct {
	value         *[sha256.Size]byte
	nonComparable []struct{}
}

// RestoreRevision restores a revision read from protected persistence.
func RestoreRevision(encoded []byte) (Revision, error) {
	if len(encoded) != sha256.Size {
		return Revision{}, errors.New("connection revision must contain 32 protected bytes")
	}
	var value [sha256.Size]byte
	copy(value[:], encoded)
	return Revision{value: &value}, nil
}

// Equal reports whether two confidential revisions are equal.
func (revision Revision) Equal(other Revision) bool {
	return revision.value != nil && other.value != nil && *revision.value == *other.value
}

// RevealForPersistence returns a copy for a protected persistence boundary.
func (revision Revision) RevealForPersistence() ([]byte, error) {
	if revision.value == nil {
		return nil, errors.New("invalid connection revision")
	}
	return append([]byte(nil), revision.value[:]...), nil
}

func (Revision) String() string   { return "<private-connection-revision>" }
func (Revision) GoString() string { return "endpoint.Revision(<private>)" }
func (revision Revision) Format(state fmt.State, _ rune) {
	writeSafeFormat(state, revision.String())
}
func (Revision) MarshalJSON() ([]byte, error) {
	return nil, errorsForJSON("connection revision")
}

var _ json.Marshaler = Revision{}

// ID returns the safe logical endpoint ID.
func (c Configuration) ID() ID { return c.Identity().ID() }

// Identity returns the complete deterministic connection identity.
func (c Configuration) Identity() Identity {
	if !c.valid() {
		return Identity{}
	}
	logical := canonicalConfiguration(c, false)
	logicalDigest := sha256.Sum256(logical)
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(logicalDigest[:])
	id := ID{value: "ef1_" + strings.ToLower(encoded)}

	private := canonicalConfiguration(c, true)
	revision := sha256.Sum256(private)
	return Identity{id: id, revision: &revision}
}

func canonicalConfiguration(c Configuration, includeCredential bool) []byte {
	domain := endpointIdentityDomain
	if includeCredential {
		domain = connectionRevisionDomain
	}

	encoder := canonicalEncoder{}
	encoder.writeString(domain)
	encoder.writeUint8(uint8(c.protocol))
	encoder.writeUint8(uint8(c.address.kind))
	encoder.writeString(c.address.host)
	encoder.writeUint16(c.address.port)
	encoder.writeUint8(uint8(c.transport.kind))
	encoder.writeString(c.transport.WebSocketPath())
	encoder.writeBool(c.tls.enabled)
	encoder.writeString(c.tls.serverName)
	encoder.writeBool(c.tls.insecureSkipVerify)
	if includeCredential {
		encoder.writeUint8(uint8(c.credential.protocol))
		encoder.writeString(c.credential.secret.value)
	}
	return encoder.Bytes()
}

type canonicalEncoder struct {
	bytes.Buffer
}

func (encoder *canonicalEncoder) writeString(value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	encoder.Write(length[:])
	encoder.WriteString(value)
}

func (encoder *canonicalEncoder) writeUint8(value uint8) { encoder.WriteByte(value) }

func (encoder *canonicalEncoder) writeUint16(value uint16) {
	var encoded [2]byte
	binary.BigEndian.PutUint16(encoded[:], value)
	encoder.Write(encoded[:])
}

func (encoder *canonicalEncoder) writeBool(value bool) {
	if value {
		encoder.WriteByte(1)
		return
	}
	encoder.WriteByte(0)
}
