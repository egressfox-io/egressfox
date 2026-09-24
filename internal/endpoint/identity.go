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
	if !strings.HasPrefix(value, "ef1_") && !strings.HasPrefix(value, "ef2_") && !strings.HasPrefix(value, "ef3_") {
		return ID{}, invalid("endpoint.id", "has an unsupported identity version")
	}
	encoded := value[4:]
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

// Compare orders complete connection identities without exposing confidential
// revision bytes. Invalid identities sort before valid identities.
func (identity Identity) Compare(other Identity) int {
	if compared := strings.Compare(identity.id.value, other.id.value); compared != 0 {
		return compared
	}
	if identity.revision == nil {
		if other.revision == nil {
			return 0
		}
		return -1
	}
	if other.revision == nil {
		return 1
	}
	return bytes.Compare(identity.revision[:], other.revision[:])
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
	version := "ef1_"
	if c.protocol == ProtocolVMess || c.protocol == ProtocolShadowsocks || c.transport.webSocketHost != "" {
		version = "ef2_"
	}
	if c.advanced != nil || c.transport.advanced != nil {
		version = "ef3_"
	}
	logical := canonicalConfiguration(c, false)
	logicalDigest := sha256.Sum256(logical)
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(logicalDigest[:])
	id := ID{value: version + strings.ToLower(encoded)}

	private := canonicalConfiguration(c, true)
	revision := sha256.Sum256(private)
	return Identity{id: id, revision: &revision}
}

func canonicalConfiguration(c Configuration, includeCredential bool) []byte {
	domain := endpointIdentityDomain
	if includeCredential {
		domain = connectionRevisionDomain
	}
	version3 := c.advanced != nil || c.transport.advanced != nil
	version2 := version3 || c.protocol == ProtocolVMess || c.protocol == ProtocolShadowsocks || c.transport.webSocketHost != ""
	if version2 {
		domain = "egressfox.endpoint/v2"
		if includeCredential {
			domain = "egressfox.connection/v2"
		}
	}
	if version3 {
		domain = "egressfox.endpoint/v3"
		if includeCredential {
			domain = "egressfox.connection/v3"
		}
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
	if version2 {
		encoder.writeString(c.transport.webSocketHost)
		encoder.writeString(c.method)
	}
	if version3 {
		encoder.writeString(c.transport.Path())
		encoder.writeString(c.transport.Host())
		encoder.writeString(c.transport.identityService())
		encoder.writeUint8(uint8(c.advanced.flow))
		encoder.writeUint8(uint8(len(c.advanced.security.alpn)))
		for _, value := range c.advanced.security.alpn {
			encoder.writeString(value)
		}
		encoder.writeString(c.advanced.security.fingerprint)
		if c.advanced.security.reality == nil {
			encoder.writeBool(false)
		} else {
			encoder.writeBool(true)
			encoder.writeString(c.advanced.security.reality.publicKey)
			if includeCredential {
				encoder.writeString(c.advanced.security.reality.shortID)
			}
		}
		if c.advanced.hysteria == nil {
			encoder.writeBool(false)
		} else {
			encoder.writeBool(true)
			encoder.writeUint32(uint32(c.advanced.hysteria.upMbps))
			encoder.writeUint32(uint32(c.advanced.hysteria.downMbps))
			encoder.writeBool(c.advanced.hysteria.obfsPassword != nil)
			if includeCredential && c.advanced.hysteria.obfsPassword != nil {
				encoder.writeString(c.advanced.hysteria.obfsPassword.value)
			}
		}
	}
	if includeCredential {
		encoder.writeUint8(uint8(c.credential.protocol))
		encoder.writeString(c.credential.secret.value)
		if version3 {
			encoder.writeString(c.credential.username)
		}
	}
	// A domain-separated tail keeps every pre-C4 ef3 encoding byte-for-byte
	// stable and prevents hop metadata from being parsed as credential fields.
	if version3 && c.advanced.hysteria != nil && len(c.advanced.hysteria.portRanges) != 0 {
		encoder.writeString("hysteria2-ports/v1")
		encoder.writeUint8(uint8(len(c.advanced.hysteria.portRanges)))
		for _, entry := range c.advanced.hysteria.portRanges {
			encoder.writeString(entry)
		}
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

func (encoder *canonicalEncoder) writeUint32(value uint32) {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	encoder.Write(encoded[:])
}

func (encoder *canonicalEncoder) writeBool(value bool) {
	if value {
		encoder.WriteByte(1)
		return
	}
	encoder.WriteByte(0)
}
