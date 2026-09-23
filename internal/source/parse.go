package source

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

type Format uint8

const (
	FormatAuto Format = iota
	FormatURIList
	FormatBase64URIList
	FormatJSON
)

type Admission struct {
	AllowPartial    bool
	AllowedProtocol []endpoint.Protocol
	DenyInsecureTLS bool
}

type ParseOptions struct {
	Format    Format
	Limits    Limits
	Admission Admission
}

type parseInput struct {
	uri           string
	configuration endpoint.Configuration
	alias         string
	kind          DiagnosticKind
	code          string
}

func Parse(payload Payload, options ParseOptions) (Snapshot, Report, error) {
	limits, err := options.Limits.normalized()
	if err != nil {
		return Snapshot{}, Report{}, failure(payload.source, ErrFormat, "invalid_limits")
	}
	data := payload.Bytes()
	if len(data) > limits.MaxSourceBytes {
		return Snapshot{}, Report{}, failure(payload.source, ErrFormat, "source_too_large")
	}
	format := options.Format
	if format == FormatAuto {
		format, err = detectFormat(data, limits)
		if err != nil {
			return Snapshot{}, Report{}, failure(payload.source, ErrFormat, "unrecognized_format")
		}
	}
	if format == FormatBase64URIList {
		data, err = decodeEnvelope(data, limits.MaxDecodedBytes)
		if err != nil {
			return Snapshot{}, Report{}, failure(payload.source, ErrFormat, "malformed_base64")
		}
	} else if format != FormatURIList && format != FormatJSON {
		return Snapshot{}, Report{}, failure(payload.source, ErrFormat, "unsupported_format")
	}

	var inputs []parseInput
	if format == FormatJSON {
		inputs, err = decodeJSONInputs(data, limits)
		if err != nil {
			return Snapshot{}, Report{}, failure(payload.source, ErrFormat, "invalid_json_subscription")
		}
	} else {
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			inputs = append(inputs, parseInput{uri: string(bytes.TrimSuffix(line, []byte{'\r'}))})
		}
	}
	records := make([]endpoint.Record, 0, len(inputs))
	report := Report{}
	recordNumber := 0
	for _, input := range inputs {
		line := []byte(input.uri)
		if input.code == "" && input.configuration.Protocol() == endpoint.ProtocolUnknown && (len(bytes.TrimSpace(line)) == 0 || bytes.HasPrefix(bytes.TrimSpace(line), []byte{'#'})) {
			continue
		}
		recordNumber++
		if recordNumber > limits.MaxRecords {
			return Snapshot{}, report, failure(payload.source, ErrFormat, "too_many_records")
		}
		if len(line) > limits.MaxRecordBytes {
			addDiagnostic(&report, Diagnostic{payload.source, recordNumber, DiagnosticMalformed, "record_too_large"})
			continue
		}
		configuration, alias, kind, code := input.configuration, input.alias, input.kind, input.code
		if code == "" && configuration.Protocol() == endpoint.ProtocolUnknown {
			configuration, alias, kind, code = parseURI(string(line))
		}
		if code != "" {
			addDiagnostic(&report, Diagnostic{payload.source, recordNumber, kind, code})
			continue
		}
		if filterCode := filtered(configuration, options.Admission); filterCode != "" {
			addDiagnostic(&report, Diagnostic{payload.source, recordNumber, DiagnosticFiltered, filterCode})
			continue
		}
		recordID, recordErr := endpoint.NewRecordID(configuration.ID().String())
		if recordErr != nil {
			return Snapshot{}, report, failure(payload.source, ErrRefresh, "internal_record_id")
		}
		aliases := []string(nil)
		if alias != "" {
			aliases = []string{alias}
		}
		provenance, provenanceErr := endpoint.NewProvenance(payload.source, recordID, aliases...)
		if provenanceErr != nil {
			addDiagnostic(&report, Diagnostic{payload.source, recordNumber, DiagnosticInvalid, "invalid_alias"})
			continue
		}
		record, recordErr := endpoint.NewRecord(configuration, provenance)
		if recordErr != nil {
			return Snapshot{}, report, failure(payload.source, ErrRefresh, "internal_record")
		}
		records = append(records, record)
		report.Accepted++
	}
	if report.Accepted == 0 && report.Rejected() > 0 {
		return Snapshot{}, report, failure(payload.source, ErrRefresh, "no_accepted_records")
	}
	if report.Rejected() > 0 && !options.Admission.AllowPartial {
		return Snapshot{}, report, failure(payload.source, ErrRefresh, "partial_snapshot")
	}
	inventory, err := endpoint.Deduplicate(records)
	if err != nil {
		return Snapshot{}, report, failure(payload.source, ErrRefresh, "inventory_conflict")
	}
	return newSnapshot(payload.source, inventory.Records()), report, nil
}

func addDiagnostic(report *Report, diagnostic Diagnostic) {
	report.Diagnostics = append(report.Diagnostics, diagnostic)
	switch diagnostic.Kind {
	case DiagnosticMalformed:
		report.Malformed++
	case DiagnosticUnsupported:
		report.Unsupported++
	case DiagnosticInvalid:
		report.Invalid++
	case DiagnosticFiltered:
		report.Filtered++
	}
}

func detectFormat(data []byte, limits Limits) (Format, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
		return FormatJSON, nil
	}
	if len(trimmed) == 0 || recognizableURIList(trimmed) {
		return FormatURIList, nil
	}
	decoded, err := decodeEnvelope(data, limits.MaxDecodedBytes)
	if err == nil && recognizableURIList(bytes.TrimSpace(decoded)) {
		return FormatBase64URIList, nil
	}
	return FormatAuto, errors.New("unrecognized")
}

// JSON is recognized structurally, never by Content-Type or a recursive search.
func decodeJSONInputs(data []byte, limits Limits) ([]parseInput, error) {
	if !boundedJSONDepth(data, 32) {
		return nil, errors.New("json depth")
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("empty json")
	}
	var items []json.RawMessage
	switch trimmed[0] {
	case '[':
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
		if len(items) > 0 && len(bytes.TrimSpace(items[0])) > 0 && bytes.TrimSpace(items[0])[0] == '{' {
			var first map[string]json.RawMessage
			if json.Unmarshal(items[0], &first) == nil && first["type"] != nil && first["outbounds"] == nil {
				return decodeSingBoxRecords(items, limits)
			}
			return decodeXrayProfiles(items, limits)
		}
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return nil, err
		}
		if _, exists := object["outbounds"]; exists {
			return decodeXrayProfiles([]json.RawMessage{trimmed}, limits)
		}
		var selected json.RawMessage
		for _, key := range []string{"proxies", "nodes"} {
			if value, exists := object[key]; exists {
				if selected != nil {
					return nil, errors.New("ambiguous json subscription")
				}
				selected = value
			}
		}
		if selected == nil || json.Unmarshal(selected, &items) != nil {
			return nil, errors.New("unknown json subscription")
		}
	default:
		return nil, errors.New("invalid json root")
	}
	if len(items) > limits.MaxRecords {
		return nil, errors.New("too many records")
	}
	result := make([]parseInput, 0, len(items))
	for _, item := range items {
		var uri string
		if err := json.Unmarshal(item, &uri); err != nil || len(uri) > limits.MaxRecordBytes {
			return nil, errors.New("invalid json uri record")
		}
		result = append(result, parseInput{uri: uri})
	}
	return result, nil
}

func boundedJSONDepth(data []byte, max int) bool {
	depth, quoted, escaped := 0, false, false
	for _, ch := range data {
		if quoted {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				quoted = false
			}
			continue
		}
		switch ch {
		case '"':
			quoted = true
		case '[', '{':
			depth++
			if depth > max {
				return false
			}
		case ']', '}':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0 && !quoted
}

func recognizableURIList(data []byte) bool {
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || bytes.HasPrefix(line, []byte{'#'}) {
			continue
		}
		text := strings.ToLower(string(line))
		for _, scheme := range []string{"vless://", "trojan://", "vmess://", "ss://", "hysteria://", "hysteria2://", "tuic://"} {
			if strings.HasPrefix(text, scheme) {
				return true
			}
		}
		return false
	}
	return true
}

func decodeEnvelope(data []byte, max int) ([]byte, error) {
	compact := make([]byte, 0, len(data))
	for _, value := range data {
		if value == ' ' || value == '\n' || value == '\r' || value == '\t' {
			continue
		}
		compact = append(compact, value)
	}
	if base64.StdEncoding.DecodedLen(len(compact)) > max {
		return nil, errors.New("decoded input too large")
	}
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(compact)))
	n, err := base64.StdEncoding.Decode(decoded, compact)
	if err != nil {
		decoded = make([]byte, base64.RawStdEncoding.DecodedLen(len(compact)))
		n, err = base64.RawStdEncoding.Decode(decoded, compact)
	}
	if err != nil || n > max {
		return nil, errors.New("invalid base64")
	}
	return decoded[:n], nil
}

func parseURI(raw string) (endpoint.Configuration, string, DiagnosticKind, string) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_uri"
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "vmess" {
		return parseVMessURI(raw)
	}
	if scheme == "ss" {
		return parseShadowsocksURI(raw)
	}
	if scheme != "vless" && scheme != "trojan" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_protocol"
	}
	if u.User == nil || u.User.Username() == "" || u.Hostname() == "" || u.Port() == "" {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "missing_authority_field"
	}
	if u.Path != "" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_uri_path"
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "userinfo_password_form"
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_port"
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_query_encoding"
	}
	for key, values := range query {
		if len(values) != 1 {
			return endpoint.Configuration{}, "", DiagnosticMalformed, "duplicate_parameter"
		}
		if !knownParameter(key) {
			return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_parameter"
		}
	}
	for _, key := range []string{"type", "security", "sni", "allowInsecure"} {
		if values, present := query[key]; present && values[0] == "" {
			return endpoint.Configuration{}, "", DiagnosticMalformed, "empty_parameter"
		}
	}
	if values, present := query["encryption"]; present && values[0] == "" {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "empty_parameter"
	}
	_, hasALPN := query["alpn"]
	if hasALPN || query.Get("flow") != "" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_parameter"
	}
	if scheme == "vless" && valueOr(query.Get("encryption"), "none") != "none" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_encryption"
	}
	if scheme == "trojan" {
		if _, present := query["encryption"]; present {
			return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_parameter"
		}
		if _, present := query["flow"]; present {
			return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_parameter"
		}
	}
	security := valueOr(query.Get("security"), map[bool]string{true: "tls", false: "none"}[scheme == "trojan"])
	if security != "none" && security != "tls" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_security"
	}
	if scheme == "trojan" && security != "tls" {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "trojan_requires_tls"
	}
	transportName := valueOr(query.Get("type"), "tcp")
	var transport endpoint.Transport
	switch transportName {
	case "tcp":
		if query.Get("path") != "" || query.Get("host") != "" {
			return endpoint.Configuration{}, "", DiagnosticUnsupported, "path_without_websocket"
		}
		transport = endpoint.NewTCPTransport()
	case "ws":
		if query.Get("path") == "" {
			return endpoint.Configuration{}, "", DiagnosticInvalid, "websocket_path_required"
		}
		transport, err = endpoint.NewWebSocketTransportWithHost(query.Get("path"), query.Get("host"))
	default:
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "unsupported_transport"
	}
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_transport"
	}
	insecure, ok := parseBool(query.Get("allowInsecure"))
	if !ok {
		return endpoint.Configuration{}, "", DiagnosticMalformed, "invalid_boolean"
	}
	address, err := endpoint.NewAddress(u.Hostname(), port)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_address"
	}
	tls := endpoint.DisabledTLS()
	if security == "tls" {
		tls, err = endpoint.NewTLS(query.Get("sni"), insecure)
		if err != nil {
			return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_tls"
		}
	} else if query.Get("sni") != "" || query.Get("allowInsecure") != "" {
		return endpoint.Configuration{}, "", DiagnosticUnsupported, "tls_option_without_tls"
	}
	protocol := endpoint.ProtocolVLESS
	credential, err := endpoint.NewVLESSCredential(u.User.Username())
	if scheme == "trojan" {
		protocol = endpoint.ProtocolTrojan
		credential, err = endpoint.NewTrojanCredential(u.User.Username())
	}
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_credential"
	}
	configuration, err := endpoint.NewConfiguration(protocol, address, credential, transport, tls)
	if err != nil {
		return endpoint.Configuration{}, "", DiagnosticInvalid, "invalid_endpoint"
	}
	return configuration, u.Fragment, 0, ""
}

func knownParameter(key string) bool {
	switch key {
	case "encryption", "type", "security", "sni", "allowInsecure", "path", "flow", "host", "alpn":
		return true
	default:
		return false
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func parseBool(value string) (bool, bool) {
	switch value {
	case "", "0", "false":
		return false, true
	case "1", "true":
		return true, true
	default:
		return false, false
	}
}

func filtered(configuration endpoint.Configuration, admission Admission) string {
	if len(admission.AllowedProtocol) > 0 {
		allowed := false
		for _, protocol := range admission.AllowedProtocol {
			allowed = allowed || configuration.Protocol() == protocol
		}
		if !allowed {
			return "protocol_filtered"
		}
	}
	if admission.DenyInsecureTLS && configuration.TLS().InsecureSkipVerify() {
		return "insecure_tls_filtered"
	}
	return ""
}
