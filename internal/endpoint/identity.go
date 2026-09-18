package endpoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
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
	id       ID
	revision [sha256.Size]byte
}

// ID returns the safe logical endpoint ID.
func (identity Identity) ID() ID { return identity.id }

// Equal reports whether two values describe the same complete connection revision.
func (identity Identity) Equal(other Identity) bool { return identity == other }

// SameEndpoint reports whether two revisions belong to the same logical endpoint.
func (identity Identity) SameEndpoint(other Identity) bool { return identity.id == other.id }

func (identity Identity) String() string {
	return identity.id.String() + "@<private-revision>"
}

func (identity Identity) GoString() string { return "endpoint.Identity(" + identity.String() + ")" }

func (Identity) MarshalJSON() ([]byte, error) {
	return nil, errorsForJSON("connection identity")
}

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
	return Identity{id: id, revision: sha256.Sum256(private)}
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
	encoder.writeString(c.transport.webSocketPath)
	encoder.writeBool(c.tls.enabled)
	encoder.writeString(c.tls.serverName)
	encoder.writeBool(c.tls.insecureSkipVerify)
	if includeCredential {
		encoder.writeUint8(uint8(c.credential.protocol))
		encoder.writeString(c.credential.value)
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
