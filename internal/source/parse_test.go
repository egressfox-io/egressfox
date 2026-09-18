package source_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/source"
)

const vlessID = "7ae477a8-3884-4dad-a5a8-a5106778cbbb"

func TestParseURIListNormalizesAndDeduplicates(t *testing.T) {
	t.Parallel()
	input := strings.Join([]string{
		"vless://" + vlessID + "@EDGE.Example.com.:443?encryption=none&security=tls&type=ws&path=%2Fproxy&sni=edge.example.com#Alpha",
		"vless://" + strings.ToUpper(vlessID) + "@edge.example.com:443?type=ws&path=%2Fproxy&security=tls#Beta",
		"trojan://synthetic-password@[2001:0db8::1]:443#IPv6",
	}, "\n")
	snapshot, report, err := parse(t, "source-a", []byte(input), source.ParseOptions{Format: source.FormatURIList})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if snapshot.Len() != 2 || report.Accepted != 3 || report.Rejected() != 0 {
		t.Fatalf("snapshot/report = %d/%+v", snapshot.Len(), report)
	}
	records := snapshot.Records()
	var aliases []string
	for _, record := range records {
		if record.Configuration().Protocol() == endpoint.ProtocolVLESS {
			aliases = record.Provenance()[0].Aliases()
		}
	}
	if !reflect.DeepEqual(aliases, []string{"Alpha", "Beta"}) {
		t.Fatalf("aliases = %#v", aliases)
	}
}

func TestParseClassificationAndTransactionalPolicy(t *testing.T) {
	t.Parallel()
	valid := "trojan://synthetic-password@example.com:443#ok"
	tests := []struct {
		name, record string
		kind         source.DiagnosticKind
		code         string
	}{
		{"malformed", "not a uri", source.DiagnosticMalformed, "invalid_uri"},
		{"unsupported protocol", "vmess://opaque@example.com:443", source.DiagnosticUnsupported, "unsupported_protocol"},
		{"unsupported option", "trojan://secret@example.com:443?fingerprint=chrome", source.DiagnosticUnsupported, "unsupported_parameter"},
		{"duplicate option", "trojan://secret@example.com:443?sni=a.example&sni=b.example", source.DiagnosticMalformed, "duplicate_parameter"},
		{"empty option", "vless://" + vlessID + "@example.com:443?security=", source.DiagnosticMalformed, "empty_parameter"},
		{"URI path", "trojan://secret@example.com:443/native-path", source.DiagnosticUnsupported, "unsupported_uri_path"},
		{"invalid field", "vless://not-a-uuid@example.com:443", source.DiagnosticInvalid, "invalid_credential"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, report, err := parse(t, "source-a", []byte(valid+"\n"+test.record), source.ParseOptions{Format: source.FormatURIList})
			if !errors.Is(err, source.ErrRefresh) {
				t.Fatalf("error = %v", err)
			}
			if report.Accepted != 1 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Kind != test.kind || report.Diagnostics[0].Code != test.code {
				t.Fatalf("report = %+v", report)
			}
		})
	}
	snapshot, report, err := parse(t, "source-a", []byte(valid+"\nvmess://opaque@example.com:443"), source.ParseOptions{Format: source.FormatURIList, Admission: source.Admission{AllowPartial: true}})
	if err != nil || snapshot.Len() != 1 || report.Unsupported != 1 {
		t.Fatalf("partial result = %v %+v %v", snapshot, report, err)
	}
}

func TestBase64AndDetection(t *testing.T) {
	t.Parallel()
	raw := []byte("trojan://synthetic-password@example.com:443#Node\n")
	encoded := base64.StdEncoding.EncodeToString(raw)
	for _, test := range []struct {
		name   string
		data   []byte
		format source.Format
	}{
		{"explicit", []byte(encoded), source.FormatBase64URIList},
		{"auto base64", []byte(encoded[:8] + "\n" + encoded[8:]), source.FormatAuto},
		{"auto uri", raw, source.FormatAuto},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot, _, err := parse(t, "source-a", test.data, source.ParseOptions{Format: test.format})
			if err != nil || snapshot.Len() != 1 {
				t.Fatalf("Parse() = %v, %v", snapshot, err)
			}
		})
	}
	_, _, err := parse(t, "source-a", []byte("%%%"), source.ParseOptions{Format: source.FormatBase64URIList})
	if !errors.Is(err, source.ErrFormat) {
		t.Fatalf("malformed Base64 error = %v", err)
	}
}

func TestLimitsFiltersAndEmpty(t *testing.T) {
	t.Parallel()
	limits := source.DefaultLimits()
	limits.MaxRecordBytes = 20
	_, report, err := parse(t, "source-a", []byte("trojan://secret@example.com:443"), source.ParseOptions{Format: source.FormatURIList, Limits: limits})
	if !errors.Is(err, source.ErrRefresh) || report.Malformed != 1 {
		t.Fatalf("oversize result = %+v, %v", report, err)
	}
	_, report, err = parse(t, "source-a", []byte("trojan://secret@example.com:443?allowInsecure=1"), source.ParseOptions{Format: source.FormatURIList, Admission: source.Admission{DenyInsecureTLS: true}})
	if !errors.Is(err, source.ErrRefresh) || report.Filtered != 1 {
		t.Fatalf("filter result = %+v, %v", report, err)
	}
	snapshot, report, err := parse(t, "source-a", []byte("\n# comment\n"), source.ParseOptions{Format: source.FormatAuto})
	if err != nil || snapshot.Len() != 0 || report.Accepted != 0 {
		t.Fatalf("empty result = %v %+v %v", snapshot, report, err)
	}
}

func TestRecordOrderingDoesNotChangeInventoryOrRecordIDs(t *testing.T) {
	t.Parallel()
	first := "vless://" + vlessID + "@a.example.com:443?security=tls#A"
	second := "trojan://synthetic-password@b.example.com:443#B"
	left, _, err := parse(t, "source-a", []byte(first+"\n"+second), source.ParseOptions{Format: source.FormatURIList})
	if err != nil {
		t.Fatal(err)
	}
	right, _, err := parse(t, "source-a", []byte(second+"\n"+first), source.ParseOptions{Format: source.FormatURIList})
	if err != nil {
		t.Fatal(err)
	}
	lr, rr := left.Records(), right.Records()
	if len(lr) != len(rr) {
		t.Fatal("ordering changed record count")
	}
	for i := range lr {
		if !lr[i].Configuration().Equivalent(rr[i].Configuration()) || lr[i].Provenance()[0].RecordID() != rr[i].Provenance()[0].RecordID() {
			t.Fatal("ordering changed inventory semantics")
		}
	}
}

func TestCredentialRotationKeepsLogicalRecordKeyAndSeparateRevision(t *testing.T) {
	t.Parallel()
	input := "trojan://first-password@example.com:443\ntrojan://second-password@example.com:443"
	snapshot, _, err := parse(t, "source-a", []byte(input), source.ParseOptions{Format: source.FormatURIList})
	if err != nil {
		t.Fatal(err)
	}
	records := snapshot.Records()
	if len(records) != 2 || records[0].ID() != records[1].ID() || records[0].Identity().Equal(records[1].Identity()) {
		t.Fatal("credential revisions were not represented distinctly")
	}
	if records[0].Provenance()[0].RecordID() != records[1].Provenance()[0].RecordID() {
		t.Fatal("credential rotation changed source-local logical record key")
	}
}

func TestDiagnosticsAndFormattingDoNotLeak(t *testing.T) {
	t.Parallel()
	const secret = "LEAK-CANARY-trojan://password@example.com:443?token=private"
	id := sourceID(t, "source-safe")
	inline, err := source.NewInline(id, []byte(secret), source.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := inline.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, report, parseErr := source.Parse(payload, source.ParseOptions{Format: source.FormatURIList})
	values := []string{fmt.Sprintf("%v %+v %#v", inline, inline, inline), fmt.Sprintf("%v %+v %#v", payload, payload, payload), fmt.Sprint(report), fmt.Sprint(parseErr)}
	for _, value := range values {
		if strings.Contains(value, secret) || strings.Contains(value, "password") || strings.Contains(value, "private") {
			t.Fatal("diagnostic leaked canary")
		}
	}
}

func parse(t testing.TB, sourceName string, data []byte, options source.ParseOptions) (source.Snapshot, source.Report, error) {
	t.Helper()
	inline, err := source.NewInline(sourceID(t, sourceName), data, options.Limits)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := inline.Acquire(context.Background())
	if err != nil {
		return source.Snapshot{}, source.Report{}, err
	}
	return source.Parse(payload, options)
}

func FuzzParseURIList(f *testing.F) {
	f.Add("trojan://synthetic@example.com:443")
	f.Add("vless://" + vlessID + "@example.com:443?security=tls")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 64<<10 {
			t.Skip()
		}
		_, report, err := parse(t, "fuzz-source", []byte(input), source.ParseOptions{Format: source.FormatURIList, Admission: source.Admission{AllowPartial: true}})
		_ = report
		_ = err
	})
}
