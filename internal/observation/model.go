package observation

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
)

type Kind uint8

const (
	KindHTTPGet Kind = iota + 1
)

func (kind Kind) String() string {
	if kind == KindHTTPGet {
		return "http_get"
	}
	return "unknown"
}

type Outcome uint8

const (
	OutcomeSuccess Outcome = iota + 1
	OutcomeTimeout
	OutcomeConnectionFailure
	OutcomeTLSFailure
	OutcomeUnexpectedResponse
	OutcomeResponseTooLarge
)

func (outcome Outcome) String() string {
	switch outcome {
	case OutcomeSuccess:
		return "success"
	case OutcomeTimeout:
		return "timeout"
	case OutcomeConnectionFailure:
		return "connection_failure"
	case OutcomeTLSFailure:
		return "tls_failure"
	case OutcomeUnexpectedResponse:
		return "unexpected_response"
	case OutcomeResponseTooLarge:
		return "response_too_large"
	default:
		return "unknown"
	}
}

type ConnectionRef struct {
	id       endpoint.ID
	revision endpoint.Revision
}

func NewConnectionRef(identity endpoint.Identity) (ConnectionRef, error) {
	if _, err := identity.Revision().RevealForPersistence(); err != nil || identity.ID().String() == "<invalid-endpoint-id>" {
		return ConnectionRef{}, errors.New("invalid observation connection reference")
	}
	return ConnectionRef{id: identity.ID(), revision: identity.Revision()}, nil
}

func RestoreConnectionRef(id endpoint.ID, revision endpoint.Revision) (ConnectionRef, error) {
	if _, err := revision.RevealForPersistence(); err != nil || id.String() == "<invalid-endpoint-id>" {
		return ConnectionRef{}, errors.New("invalid persisted connection reference")
	}
	return ConnectionRef{id: id, revision: revision}, nil
}

func (reference ConnectionRef) ID() endpoint.ID             { return reference.id }
func (reference ConnectionRef) Revision() endpoint.Revision { return reference.revision }
func (reference ConnectionRef) Equal(other ConnectionRef) bool {
	return reference.id == other.id && reference.revision.Equal(other.revision)
}

type TargetRef struct {
	id       TargetID
	revision TargetRevision
}

func (target HTTPTarget) Ref() TargetRef { return TargetRef{id: target.id, revision: target.revision} }
func RestoreTargetRef(id TargetID, encodedRevision []byte) (TargetRef, error) {
	revision, err := restoreTargetRevision(encodedRevision)
	if err != nil || id.value == "" {
		return TargetRef{}, errors.New("invalid persisted target reference")
	}
	return TargetRef{id: id, revision: revision}, nil
}
func (reference TargetRef) ID() TargetID             { return reference.id }
func (reference TargetRef) Revision() TargetRevision { return reference.revision }
func (reference TargetRef) Equal(other TargetRef) bool {
	return reference.id == other.id && reference.revision.Equal(other.revision)
}

type Key struct {
	connection ConnectionRef
	target     TargetRef
	vantage    VantageID
	kind       Kind
	profile    artifact.Profile
}

func NewKey(connection ConnectionRef, target TargetRef, vantage VantageID, kind Kind, profile artifact.Profile) (Key, error) {
	if connection.id.String() == "<invalid-endpoint-id>" || target.id.value == "" || vantage.value == "" ||
		kind != KindHTTPGet || !supportedProfile(profile) {
		return Key{}, errors.New("invalid observation key")
	}
	return Key{connection: connection, target: target, vantage: vantage, kind: kind, profile: profile}, nil
}

func (key Key) Connection() ConnectionRef { return key.connection }
func (key Key) Target() TargetRef         { return key.target }
func (key Key) Vantage() VantageID        { return key.vantage }
func (key Key) Kind() Kind                { return key.kind }
func (key Key) Profile() artifact.Profile { return key.profile }

func supportedProfile(profile artifact.Profile) bool {
	return profile == artifact.Mihomo11931 || profile == artifact.SingBox1141
}

type Params struct {
	Key         Key
	StartedAt   time.Time
	CompletedAt time.Time
	Duration    time.Duration
	Outcome     Outcome
	StatusCode  int
}

type Observation struct {
	key         Key
	startedAt   time.Time
	completedAt time.Time
	duration    time.Duration
	outcome     Outcome
	statusCode  int
	sampleID    [sha256.Size]byte
}

func New(params Params) (Observation, error) {
	if _, err := NewKey(params.Key.connection, params.Key.target, params.Key.vantage, params.Key.kind, params.Key.profile); err != nil {
		return Observation{}, err
	}
	started := params.StartedAt.UTC()
	completed := params.CompletedAt.UTC()
	if started.IsZero() || completed.IsZero() || completed.Before(started) || params.Duration < 0 || params.Duration > 2*time.Minute {
		return Observation{}, errors.New("invalid observation timing")
	}
	if params.Outcome < OutcomeSuccess || params.Outcome > OutcomeResponseTooLarge {
		return Observation{}, errors.New("invalid observation outcome")
	}
	if params.Outcome == OutcomeSuccess || params.Outcome == OutcomeUnexpectedResponse {
		if params.StatusCode < 100 || params.StatusCode > 599 {
			return Observation{}, errors.New("invalid observation HTTP status")
		}
	} else if params.StatusCode != 0 {
		return Observation{}, errors.New("HTTP status is not valid for this observation outcome")
	}
	value := Observation{
		key: params.Key, startedAt: started, completedAt: completed, duration: params.Duration,
		outcome: params.Outcome, statusCode: params.StatusCode,
	}
	value.sampleID = observationDigest(value)
	return value, nil
}

func (value Observation) Key() Key                { return value.key }
func (value Observation) StartedAt() time.Time    { return value.startedAt }
func (value Observation) CompletedAt() time.Time  { return value.completedAt }
func (value Observation) Duration() time.Duration { return value.duration }
func (value Observation) Outcome() Outcome        { return value.outcome }
func (value Observation) StatusCode() int         { return value.statusCode }
func (value Observation) Successful() bool        { return value.outcome == OutcomeSuccess }
func (value Observation) SampleIDForPersistence() []byte {
	return append([]byte(nil), value.sampleID[:]...)
}
func (value Observation) String() string {
	return fmt.Sprintf("observation endpoint=%s target=%s vantage=%s kind=%s outcome=%s revisions=<private>",
		value.key.connection.id, value.key.target.id, value.key.vantage, value.key.kind, value.outcome)
}
func (value Observation) GoString() string               { return value.String() }
func (value Observation) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(value.String())) }
func (Observation) MarshalJSON() ([]byte, error) {
	return nil, errors.New("observation JSON serialization is disabled because it contains confidential revisions")
}

func observationDigest(value Observation) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("egressfox.observation/v1"))
	writeDigestString(hash, value.key.connection.id.String())
	connectionRevision, _ := value.key.connection.revision.RevealForPersistence()
	_, _ = hash.Write(connectionRevision)
	writeDigestString(hash, value.key.target.id.String())
	targetRevision, _ := value.key.target.revision.RevealForPersistence()
	_, _ = hash.Write(targetRevision)
	writeDigestString(hash, value.key.vantage.String())
	_, _ = hash.Write([]byte{byte(value.key.kind), byte(value.key.profile.Engine), byte(value.outcome)})
	writeDigestString(hash, value.key.profile.Version)
	writeDigestString(hash, value.key.profile.RendererSchema)
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value.startedAt.UnixNano()))
	_, _ = hash.Write(encoded[:])
	binary.BigEndian.PutUint64(encoded[:], uint64(value.completedAt.UnixNano()))
	_, _ = hash.Write(encoded[:])
	binary.BigEndian.PutUint64(encoded[:], uint64(value.duration))
	_, _ = hash.Write(encoded[:])
	binary.BigEndian.PutUint64(encoded[:], uint64(value.statusCode))
	_, _ = hash.Write(encoded[:])
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func writeDigestString(hash interface{ Write([]byte) (int, error) }, value string) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = hash.Write(size[:])
	_, _ = hash.Write([]byte(value))
}

var _ json.Marshaler = Observation{}
