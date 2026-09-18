package source

import (
	"errors"
	"fmt"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

var (
	ErrAcquire = errors.New("source acquisition failed")
	ErrFormat  = errors.New("source format failed")
	ErrRefresh = errors.New("source refresh rejected")
)

type Failure struct {
	source endpoint.SourceID
	kind   error
	code   string
	status int
}

func failure(source endpoint.SourceID, kind error, code string) error {
	return &Failure{source: source, kind: kind, code: code}
}

func (e *Failure) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("source %s: %s (%s, status=%d)", e.source, e.kind, e.code, e.status)
	}
	return fmt.Sprintf("source %s: %s (%s)", e.source, e.kind, e.code)
}

func (e *Failure) Unwrap() error               { return e.kind }
func (e *Failure) SourceID() endpoint.SourceID { return e.source }
func (e *Failure) Code() string                { return e.code }
func (e *Failure) StatusCode() int             { return e.status }

type DiagnosticKind uint8

const (
	DiagnosticMalformed DiagnosticKind = iota + 1
	DiagnosticUnsupported
	DiagnosticInvalid
	DiagnosticFiltered
)

func (k DiagnosticKind) String() string {
	switch k {
	case DiagnosticMalformed:
		return "malformed"
	case DiagnosticUnsupported:
		return "unsupported"
	case DiagnosticInvalid:
		return "invalid"
	case DiagnosticFiltered:
		return "filtered"
	default:
		return "unknown"
	}
}

type Diagnostic struct {
	Source endpoint.SourceID
	Record int
	Kind   DiagnosticKind
	Code   string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("source diagnostic source=%s record=%d kind=%s code=%s", d.Source, d.Record, d.Kind, d.Code)
}

type Report struct {
	Accepted    int
	Malformed   int
	Unsupported int
	Invalid     int
	Filtered    int
	Diagnostics []Diagnostic
}

func (r Report) Rejected() int { return r.Malformed + r.Unsupported + r.Invalid + r.Filtered }
