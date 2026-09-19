package observation

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultMaxResponseBytes = int64(4 << 10)
	MaxResponseBytes        = int64(1 << 20)
)

type TargetID struct{ value string }
type VantageID struct{ value string }

func NewTargetID(value string) (TargetID, error) {
	if !safeID(value) {
		return TargetID{}, errors.New("invalid probe target identifier")
	}
	return TargetID{value: value}, nil
}

func NewVantageID(value string) (VantageID, error) {
	if !safeID(value) {
		return VantageID{}, errors.New("invalid probe vantage identifier")
	}
	return VantageID{value: value}, nil
}

func (id TargetID) String() string  { return id.value }
func (id VantageID) String() string { return id.value }

func safeID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '.' || character == '_' ||
			character == '/' || character == '-') {
			return false
		}
	}
	return true
}

type TargetRevision struct {
	value         *[sha256.Size]byte
	nonComparable []struct{}
}

func restoreTargetRevision(encoded []byte) (TargetRevision, error) {
	if len(encoded) != sha256.Size {
		return TargetRevision{}, errors.New("target revision must contain 32 protected bytes")
	}
	var value [sha256.Size]byte
	copy(value[:], encoded)
	return TargetRevision{value: &value}, nil
}

func (revision TargetRevision) Equal(other TargetRevision) bool {
	return revision.value != nil && other.value != nil && *revision.value == *other.value
}

func (revision TargetRevision) RevealForPersistence() ([]byte, error) {
	if revision.value == nil {
		return nil, errors.New("invalid target revision")
	}
	return append([]byte(nil), revision.value[:]...), nil
}

func (TargetRevision) String() string   { return "<private-target-revision>" }
func (TargetRevision) GoString() string { return "observation.TargetRevision(<private>)" }
func (revision TargetRevision) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(revision.String()))
}

type HTTPOptions struct {
	AllowHTTP        bool
	AllowPrivate     bool
	MaxResponseBytes int64
}

type HTTPTarget struct {
	id               TargetID
	revision         TargetRevision
	requestURL       *url.URL
	expectedStatus   int
	timeout          time.Duration
	maxResponseBytes int64
	allowPrivate     bool
}

func NewHTTPTarget(id TargetID, rawURL string, expectedStatus int, timeout time.Duration, options HTTPOptions) (HTTPTarget, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.IsAbs() == false || parsed.Opaque != "" || parsed.Host == "" || parsed.Fragment != "" {
		return HTTPTarget{}, errors.New("invalid HTTP probe target URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && options.AllowHTTP) {
		return HTTPTarget{}, errors.New("HTTP probe target scheme is not authorized")
	}
	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if !validHostname(hostname) {
		return HTTPTarget{}, errors.New("invalid HTTP probe target host")
	}
	port := parsed.Port()
	if port != "" {
		value, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || value == 0 {
			return HTTPTarget{}, errors.New("invalid HTTP probe target port")
		}
	}
	if port == "" || parsed.Scheme == "https" && port == "443" || parsed.Scheme == "http" && port == "80" {
		parsed.Host = hostname
		if strings.Contains(hostname, ":") {
			parsed.Host = "[" + hostname + "]"
		}
	} else {
		parsed.Host = net.JoinHostPort(hostname, port)
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	if expectedStatus < 200 || expectedStatus > 599 {
		return HTTPTarget{}, errors.New("invalid HTTP probe expected status")
	}
	if timeout < 100*time.Millisecond || timeout > 2*time.Minute {
		return HTTPTarget{}, errors.New("invalid HTTP probe timeout")
	}
	maxBytes := options.MaxResponseBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxResponseBytes
	}
	if maxBytes < 1 || maxBytes > MaxResponseBytes {
		return HTTPTarget{}, errors.New("invalid HTTP probe response bound")
	}
	revisionValue := targetDigest(parsed.String(), expectedStatus, timeout, maxBytes)
	return HTTPTarget{
		id: id, revision: TargetRevision{value: &revisionValue}, requestURL: parsed,
		expectedStatus: expectedStatus, timeout: timeout, maxResponseBytes: maxBytes,
		allowPrivate: options.AllowPrivate,
	}, nil
}

func (target HTTPTarget) ID() TargetID                    { return target.id }
func (target HTTPTarget) Revision() TargetRevision        { return target.revision }
func (target HTTPTarget) ExpectedStatus() int             { return target.expectedStatus }
func (target HTTPTarget) Timeout() time.Duration          { return target.timeout }
func (target HTTPTarget) MaxResponseBytes() int64         { return target.maxResponseBytes }
func (target HTTPTarget) AllowsPrivateDestinations() bool { return target.allowPrivate }
func (target HTTPTarget) String() string {
	return fmt.Sprintf("HTTP probe target id=%s url=<redacted>", target.id)
}
func (target HTTPTarget) GoString() string               { return target.String() }
func (target HTTPTarget) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(target.String())) }
func (target HTTPTarget) ExecutionURL() *url.URL         { clone := *target.requestURL; return &clone }
func (HTTPTarget) MarshalJSON() ([]byte, error) {
	return nil, errors.New("HTTP probe target JSON serialization is disabled")
}

func targetDigest(canonicalURL string, expectedStatus int, timeout time.Duration, maxBytes int64) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("egressfox.target/http-get/v1"))
	writeTargetString(hash, canonicalURL)
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], uint64(expectedStatus))
	_, _ = hash.Write(value[:])
	binary.BigEndian.PutUint64(value[:], uint64(timeout))
	_, _ = hash.Write(value[:])
	binary.BigEndian.PutUint64(value[:], uint64(maxBytes))
	_, _ = hash.Write(value[:])
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func writeTargetString(hash interface{ Write([]byte) (int, error) }, value string) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = hash.Write(size[:])
	_, _ = hash.Write([]byte(value))
}

func validHostname(host string) bool {
	if address, err := netip.ParseAddr(host); err == nil {
		return address.Zone() == ""
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}

var _ json.Marshaler = HTTPTarget{}
