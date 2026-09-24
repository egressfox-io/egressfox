package engine

import (
	"errors"
	"fmt"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/policy"
)

var ErrUnsupported = errors.New("unsupported engine capability")

type Renderer interface {
	Profile() artifact.Profile
	Render(policy.Gateway) (artifact.Candidate, error)
}

type CapabilityError struct {
	Profile artifact.Profile
	Field   string
	Feature string
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("engine %s does not support %s at %s", e.Profile, e.Feature, e.Field)
}

func (e *CapabilityError) Unwrap() error { return ErrUnsupported }

// BuildSupport describes the pinned executable, independently of EgressFox's
// end-to-end implementation status.
type BuildSupport uint8

const (
	BuildUnverified BuildSupport = iota
	BuildAvailable
	BuildUnavailable
)

type Assessment struct {
	Build       BuildSupport
	Implemented bool
	Field       string
	Feature     string
}

// AssessEndpoint distinguishes exact-build capability from EgressFox support.
// The latter remains false until a slice qualifies the full traffic path.
func AssessEndpoint(profile artifact.Profile, configuration endpoint.Configuration) (Assessment, error) {
	if err := configuration.Validate(); err != nil {
		return Assessment{}, err
	}
	if profile != artifact.Mihomo11931 && profile != artifact.SingBox1141 {
		return Assessment{Build: BuildUnverified, Field: "engine.profile", Feature: "unqualified_profile"}, nil
	}
	result := Assessment{Build: BuildAvailable, Implemented: true}
	switch configuration.Protocol() {
	case endpoint.ProtocolVLESS, endpoint.ProtocolTrojan, endpoint.ProtocolVMess, endpoint.ProtocolShadowsocks:
	case endpoint.ProtocolSOCKS5, endpoint.ProtocolHTTPProxy:
		result.Implemented, result.Field, result.Feature = false, "endpoint.protocol", configuration.Protocol().String()
	case endpoint.ProtocolHysteria2:
		result.Implemented, result.Field, result.Feature = false, "endpoint.protocol", "hysteria2"
		if profile == artifact.SingBox1141 {
			result.Build = BuildUnavailable // no with_quic tag
		}
		return result, nil
	}
	if configuration.Flow() != endpoint.FlowNone {
		result.Implemented, result.Field, result.Feature = false, "endpoint.flow", "vision"
	}
	security := configuration.SecurityOptions()
	if _, reality := security.Reality(); reality {
		result.Implemented, result.Field, result.Feature = false, "endpoint.security", "reality"
		if profile == artifact.SingBox1141 {
			result.Build = BuildUnavailable // no with_utls tag
		}
		return result, nil
	}
	if security.Fingerprint() != "" {
		result.Implemented, result.Field, result.Feature = false, "endpoint.security", "client_fingerprint"
		if profile == artifact.SingBox1141 {
			result.Build = BuildUnavailable // no with_utls tag
		}
		return result, nil
	}
	if len(security.ALPN()) != 0 {
		result.Implemented, result.Field, result.Feature = false, "endpoint.security", "alpn"
		return result, nil
	}
	switch configuration.Transport().Kind() {
	case endpoint.TransportTCP, endpoint.TransportWebSocket:
	case endpoint.TransportHTTP2, endpoint.TransportGRPC:
		result.Implemented, result.Field, result.Feature = false, "endpoint.transport", configuration.Transport().Kind().String()
	case endpoint.TransportHTTPUpgrade:
		result.Implemented, result.Field, result.Feature = false, "endpoint.transport", "httpupgrade"
		if profile == artifact.Mihomo11931 {
			result.Build = BuildUnverified // WS upgrade needs wire-compatibility proof
		}
	default:
		result.Implemented, result.Build, result.Field, result.Feature = false, BuildUnverified, "endpoint.transport", configuration.Transport().Kind().String()
	}
	return result, nil
}

// CheckEndpoint is the single EgressFox renderer/probe capability gate for the
// exact shipped profiles. Upstream engine capability alone does not make a form
// supported: each admitted form must survive native validation and traffic tests.
func CheckEndpoint(profile artifact.Profile, configuration endpoint.Configuration) error {
	assessment, err := AssessEndpoint(profile, configuration)
	if err != nil {
		return err
	}
	if assessment.Implemented && assessment.Build == BuildAvailable {
		return nil
	}
	return &CapabilityError{Profile: profile, Field: assessment.Field, Feature: assessment.Feature}
}

type NamedRecord struct {
	Name   string
	Record endpoint.Record
}

func AssignNames(inventory endpoint.Inventory) []NamedRecord {
	counts := make(map[string]int)
	records := inventory.Records()
	result := make([]NamedRecord, len(records))
	for index, record := range records {
		id := record.ID().String()
		counts[id]++
		result[index] = NamedRecord{
			Name:   fmt.Sprintf("egressfox-%s-%03d", id, counts[id]),
			Record: record,
		}
	}
	return result
}
