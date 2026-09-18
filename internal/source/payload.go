package source

import (
	"context"
	"fmt"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

const (
	DefaultMaxSourceBytes  = 4 << 20
	DefaultMaxDecodedBytes = 4 << 20
	DefaultMaxRecords      = 10_000
	DefaultMaxRecordBytes  = 16 << 10
	maxConfiguredBytes     = 64 << 20
	maxConfiguredRecords   = 1_000_000
	maxConfiguredRecord    = 1 << 20
)

type Limits struct {
	MaxSourceBytes  int
	MaxDecodedBytes int
	MaxRecords      int
	MaxRecordBytes  int
}

func DefaultLimits() Limits {
	return Limits{DefaultMaxSourceBytes, DefaultMaxDecodedBytes, DefaultMaxRecords, DefaultMaxRecordBytes}
}

func (l Limits) normalized() (Limits, error) {
	if l == (Limits{}) {
		return DefaultLimits(), nil
	}
	if l.MaxSourceBytes <= 0 || l.MaxDecodedBytes <= 0 || l.MaxRecords <= 0 || l.MaxRecordBytes <= 0 {
		return Limits{}, fmt.Errorf("source limits must all be positive")
	}
	if l.MaxSourceBytes > maxConfiguredBytes || l.MaxDecodedBytes > maxConfiguredBytes || l.MaxRecords > maxConfiguredRecords || l.MaxRecordBytes > maxConfiguredRecord {
		return Limits{}, fmt.Errorf("source limits exceed implementation ceilings")
	}
	return l, nil
}

type Payload struct {
	source      endpoint.SourceID
	data        []byte
	contentType string
}

func newPayload(source endpoint.SourceID, data []byte, contentType string) Payload {
	return Payload{source: source, data: append([]byte(nil), data...), contentType: contentType}
}

func (p Payload) SourceID() endpoint.SourceID { return p.source }
func (p Payload) Bytes() []byte               { return append([]byte(nil), p.data...) }
func (p Payload) ContentType() string         { return p.contentType }
func (p Payload) String() string {
	return fmt.Sprintf("source payload source=%s bytes=%d", p.source, len(p.data))
}
func (p Payload) GoString() string               { return p.String() }
func (p Payload) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(p.String())) }

type Acquirer interface {
	Acquire(context.Context) (Payload, error)
}

type Inline struct {
	source endpoint.SourceID
	data   []byte
	limits Limits
}

func NewInline(source endpoint.SourceID, data []byte, limits Limits) (Inline, error) {
	if source.String() == "<invalid-source-id>" {
		return Inline{}, failure(source, ErrAcquire, "invalid_source_id")
	}
	limits, err := limits.normalized()
	if err != nil {
		return Inline{}, err
	}
	return Inline{source: source, data: append([]byte(nil), data...), limits: limits}, nil
}

func (s Inline) Acquire(ctx context.Context) (Payload, error) {
	if err := ctx.Err(); err != nil {
		return Payload{}, failure(s.source, ErrAcquire, "cancelled")
	}
	if len(s.data) > s.limits.MaxSourceBytes {
		return Payload{}, failure(s.source, ErrAcquire, "source_too_large")
	}
	return newPayload(s.source, s.data, ""), nil
}

func (s Inline) String() string                 { return fmt.Sprintf("inline source=%s", s.source) }
func (s Inline) GoString() string               { return s.String() }
func (s Inline) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(s.String())) }
