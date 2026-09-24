package artifact

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

type Engine uint8

const (
	EngineMihomo Engine = iota + 1
	EngineSingBox
)

func (e Engine) String() string {
	switch e {
	case EngineMihomo:
		return "mihomo"
	case EngineSingBox:
		return "sing-box"
	default:
		return "unknown"
	}
}

type Profile struct {
	Engine         Engine
	Version        string
	RendererSchema string
	MediaType      string
}

var (
	Mihomo11931 = Profile{EngineMihomo, "1.19.31", "egressfox.mihomo/v1", "application/yaml"}
	SingBox1141 = Profile{EngineSingBox, "1.14.1", "egressfox.sing-box/v2", "application/json"}
)

func (p Profile) String() string {
	return fmt.Sprintf("%s/%s schema=%s", p.Engine, p.Version, p.RendererSchema)
}

type Candidate struct {
	profile Profile
	content []byte
}

func NewCandidate(profile Profile, content []byte) (Candidate, error) {
	if profile.Engine.String() == "unknown" || profile.Version == "" || profile.RendererSchema == "" || profile.MediaType == "" || len(content) == 0 {
		return Candidate{}, errors.New("invalid artifact candidate metadata")
	}
	return Candidate{profile: profile, content: append([]byte(nil), content...)}, nil
}

func (a Candidate) Profile() Profile { return a.profile }
func (a Candidate) Reveal() []byte   { return append([]byte(nil), a.content...) }
func (a Candidate) String() string {
	return fmt.Sprintf("candidate artifact profile=%s bytes=%d content=<redacted>", a.profile, len(a.content))
}
func (a Candidate) GoString() string               { return a.String() }
func (a Candidate) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(a.String())) }
func (Candidate) MarshalJSON() ([]byte, error) {
	return nil, errors.New("artifact candidate JSON serialization is disabled")
}

type Evidence struct{ ValidatorID string }

type Checker interface {
	Check(context.Context, Candidate) (Evidence, error)
}

type Validated struct {
	candidate  Candidate
	generation [sha256.Size]byte
	evidence   Evidence
}

// Publication is safe metadata about a durable publisher operation.
type Publication struct {
	Profile Profile
	Changed bool
}

func (p Publication) String() string {
	return fmt.Sprintf("publication result profile=%s changed=%t", p.Profile, p.Changed)
}

func Validate(ctx context.Context, candidate Candidate, checker Checker) (Validated, error) {
	if checker == nil {
		return Validated{}, errors.New("artifact validation requires a checker")
	}
	evidence, err := checker.Check(ctx, candidate)
	if err != nil {
		return Validated{}, err
	}
	if !safeValidatorID(evidence.ValidatorID) {
		return Validated{}, errors.New("artifact checker returned invalid evidence")
	}
	return Validated{candidate: candidate, generation: generation(candidate), evidence: evidence}, nil
}

func (a Validated) Profile() Profile   { return a.candidate.profile }
func (a Validated) Reveal() []byte     { return a.candidate.Reveal() }
func (a Validated) Evidence() Evidence { return a.evidence }
func (a Validated) String() string {
	return fmt.Sprintf("validated artifact profile=%s generation=<redacted> bytes=%d", a.Profile(), len(a.candidate.content))
}
func (a Validated) GoString() string               { return a.String() }
func (a Validated) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(a.String())) }
func (Validated) MarshalJSON() ([]byte, error) {
	return nil, errors.New("validated artifact JSON serialization is disabled")
}

func (a Validated) Equal(other Validated) bool {
	return a.candidate.profile == other.candidate.profile && a.generation == other.generation
}

func generation(candidate Candidate) [sha256.Size]byte {
	h := sha256.New()
	h.Write([]byte("egressfox.artifact/v1"))
	h.Write([]byte{byte(candidate.profile.Engine)})
	writeString(h, candidate.profile.Version)
	writeString(h, candidate.profile.RendererSchema)
	writeString(h, candidate.profile.MediaType)
	h.Write(candidate.content)
	var result [sha256.Size]byte
	copy(result[:], h.Sum(nil))
	return result
}

type writer interface{ Write([]byte) (int, error) }

func writeString(w writer, value string) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = w.Write(size[:])
	_, _ = w.Write([]byte(value))
}

func safeValidatorID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '/' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

type receiptData struct {
	Engine         Engine `json:"engine"`
	Version        string `json:"version"`
	RendererSchema string `json:"renderer_schema"`
	MediaType      string `json:"media_type"`
	Generation     []byte `json:"generation"`
	ValidatorID    string `json:"validator_id"`
}

// Receipt is protected publication evidence for one exact validated artifact.
// Its encoded value only crosses explicit protected persistence boundaries.
type Receipt struct {
	encoded       []byte
	nonComparable []struct{}
}

func RestoreReceipt(encoded []byte) (Receipt, error) {
	var data receiptData
	if json.Unmarshal(encoded, &data) != nil || len(data.Generation) != sha256.Size || !safeValidatorID(data.ValidatorID) {
		return Receipt{}, errors.New("invalid protected artifact receipt")
	}
	profile := Profile{data.Engine, data.Version, data.RendererSchema, data.MediaType}
	if _, err := NewCandidate(profile, []byte{1}); err != nil {
		return Receipt{}, errors.New("invalid protected artifact receipt")
	}
	canonical, err := json.Marshal(data)
	if err != nil {
		return Receipt{}, errors.New("invalid protected artifact receipt")
	}
	return Receipt{encoded: canonical}, nil
}

func (receipt Receipt) RevealForPersistence() ([]byte, error) {
	if len(receipt.encoded) == 0 {
		return nil, errors.New("invalid protected artifact receipt")
	}
	return append([]byte(nil), receipt.encoded...), nil
}

func (receipt Receipt) Equal(other Receipt) bool {
	return len(receipt.encoded) > 0 && len(receipt.encoded) == len(other.encoded) && subtle.ConstantTimeCompare(receipt.encoded, other.encoded) == 1
}

func (Receipt) String() string                         { return "artifact receipt <protected>" }
func (Receipt) GoString() string                       { return "artifact.Receipt(<protected>)" }
func (receipt Receipt) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(receipt.String())) }
func (Receipt) MarshalJSON() ([]byte, error) {
	return nil, errors.New("artifact receipt JSON serialization is disabled")
}

func (a Validated) Receipt() (Receipt, error) {
	encoded, err := a.ProtectedReceipt()
	if err != nil {
		return Receipt{}, err
	}
	return RestoreReceipt(encoded)
}

func (a Validated) ProtectedReceipt() ([]byte, error) {
	if len(a.candidate.content) == 0 || !safeValidatorID(a.evidence.ValidatorID) {
		return nil, errors.New("invalid validated artifact")
	}
	return json.Marshal(receiptData{a.Profile().Engine, a.Profile().Version, a.Profile().RendererSchema, a.Profile().MediaType, a.generation[:], a.evidence.ValidatorID})
}

func MatchesProtectedReceipt(content, encoded []byte) (Profile, bool) {
	var data receiptData
	if json.Unmarshal(encoded, &data) != nil || len(data.Generation) != sha256.Size || !safeValidatorID(data.ValidatorID) {
		return Profile{}, false
	}
	profile := Profile{data.Engine, data.Version, data.RendererSchema, data.MediaType}
	candidate, err := NewCandidate(profile, content)
	if err != nil {
		return Profile{}, false
	}
	want := generation(candidate)
	return profile, subtle.ConstantTimeCompare(want[:], data.Generation) == 1
}
