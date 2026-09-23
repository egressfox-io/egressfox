package source_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/source"
)

func parseCompatibility(t *testing.T, data string, format source.Format, partial bool) (source.Snapshot, source.Report, error) {
	t.Helper()
	inline, err := source.NewInline(sourceID(t, "compat"), []byte(data), source.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := inline.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return source.Parse(payload, source.ParseOptions{Format: format, Admission: source.Admission{AllowPartial: partial}})
}

func TestJSONURIEnvelopesAndConservativeDetection(t *testing.T) {
	uri := "vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=tls"
	for _, body := range []string{
		fmt.Sprintf("[%q]", uri),
		fmt.Sprintf(`{"nodes":[%q]}`, uri),
		fmt.Sprintf(`{"proxies":[%q]}`, uri),
	} {
		snapshot, report, err := parseCompatibility(t, body, source.FormatAuto, false)
		if err != nil || report.Accepted != 1 || snapshot.Len() != 1 {
			t.Fatalf("json subscription = %d %+v %v", snapshot.Len(), report, err)
		}
	}
	for _, body := range []string{`{"error":"invalid token"}`, `{"nodes":"not an array"}`, `{"nodes":[],"proxies":[]}`, `[{"random":"vless://secret@example.com:443"}]`, `["unterminated"`, strings.Repeat("[", 34) + `"vless://x"` + strings.Repeat("]", 34)} {
		if _, _, err := parseCompatibility(t, body, source.FormatAuto, false); err == nil {
			t.Fatalf("invalid JSON admitted: %s", body)
		}
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(uri + "\n"))
	if snapshot, _, err := parseCompatibility(t, encoded, source.FormatAuto, false); err != nil || snapshot.Len() != 1 {
		t.Fatalf("base64 regression: %v", err)
	}
}

func TestVMessAndShadowsocksShareRepresentations(t *testing.T) {
	vmess := map[string]string{"v": "2", "ps": "synthetic", "add": "edge.example.com", "port": "443", "id": "11111111-1111-4111-8111-111111111111", "aid": "0", "net": "ws", "type": "none", "host": "front.example.com", "path": "/ws", "tls": "tls", "sni": "edge.example.com", "scy": "auto"}
	payload, _ := json.Marshal(vmess)
	vmessURI := "vmess://" + base64.StdEncoding.EncodeToString(payload)
	ssUser := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:synthetic-secret"))
	ssURI := "ss://" + ssUser + "@ss.example.com:8388#ss"
	legacySS := "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:synthetic-secret@ss.example.com:8388"))
	snapshot, report, err := parseCompatibility(t, strings.Join([]string{vmessURI, ssURI, legacySS}, "\n"), source.FormatURIList, false)
	if err != nil || report.Accepted != 3 || snapshot.Len() != 2 {
		t.Fatalf("share decode = %d %+v %v", snapshot.Len(), report, err)
	}
	var foundVMess, foundSS bool
	for _, record := range snapshot.Records() {
		switch record.Configuration().Protocol() {
		case endpoint.ProtocolVMess:
			foundVMess = record.Configuration().Transport().WebSocketHost() == "front.example.com" && record.Configuration().TLS().Enabled()
		case endpoint.ProtocolShadowsocks:
			foundSS = record.Configuration().Method() == "aes-256-gcm"
		}
	}
	if !foundVMess || !foundSS {
		t.Fatal("connection semantics lost")
	}
	for _, invalid := range []string{
		"ss://" + ssUser + "@ss.example.com:8388/?plugin=obfs-local",
		"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"edge.example.com","port":"443","id":"11111111-1111-4111-8111-111111111111","aid":"4"}`)),
	} {
		_, report, err := parseCompatibility(t, invalid, source.FormatURIList, false)
		if err == nil || report.Unsupported != 1 {
			t.Fatalf("unsupported variant = %+v %v", report, err)
		}
	}
}

func TestXrayMultiProfileExtractionExcludesServicesAndReality(t *testing.T) {
	const uuid = "11111111-1111-4111-8111-111111111111"
	config := func(tag string) map[string]any {
		return map[string]any{
			"dns":         map[string]any{"servers": []string{"8.8.8.8"}},
			"routing":     map[string]any{"domainStrategy": "AsIs", "rules": []any{map[string]any{"outboundTag": "direct"}}},
			"inbounds":    []any{map[string]any{"protocol": "socks", "port": 1080}},
			"observatory": map[string]any{"subjectSelector": []string{"proxy"}},
			"remarks":     "ignored profile label",
			"outbounds": []any{
				map[string]any{"protocol": "freedom", "tag": "direct"},
				map[string]any{"protocol": "blackhole", "tag": "block"},
				map[string]any{"protocol": "vless", "tag": tag, "settings": map[string]any{"vnext": []any{map[string]any{"address": "edge.example.com", "port": 443, "users": []any{map[string]any{"id": uuid, "encryption": "none"}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "tls", "tlsSettings": map[string]any{"serverName": "edge.example.com"}}},
				map[string]any{"protocol": "vless", "tag": "reality", "settings": map[string]any{"vnext": []any{map[string]any{"address": "reality.example.com", "port": 443, "users": []any{map[string]any{"id": uuid, "flow": "xtls-rprx-vision"}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{"publicKey": "synthetic", "shortId": "abcd"}}},
			},
		}
	}
	body, err := json.Marshal([]any{config("first"), config("second")})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, report, err := parseCompatibility(t, string(body), source.FormatJSON, true)
	if err != nil || report.Accepted != 2 || report.Unsupported != 2 || snapshot.Len() != 1 {
		t.Fatalf("xray extraction = %d %+v %v", snapshot.Len(), report, err)
	}
	if len(snapshot.Records()[0].Provenance()[0].Aliases()) != 2 {
		t.Fatal("profile alias provenance was not merged")
	}
	_, strictReport, strictErr := parseCompatibility(t, string(body), source.FormatAuto, false)
	if strictErr == nil || strictReport.Unsupported != 2 {
		t.Fatal("default admission accepted partial Reality profile")
	}
	if strings.Contains(fmt.Sprint(strictErr, strictReport), uuid) || strings.Contains(fmt.Sprint(strictErr, strictReport), "synthetic") {
		t.Fatal("diagnostic leaked subscription material")
	}
}

func TestXrayNestedServerAndUserExpansion(t *testing.T) {
	body := `{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"one.example.com","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"},{"id":"22222222-2222-4222-8222-222222222222","encryption":"none"}]},{"address":"two.example.com","port":443,"users":[{"id":"33333333-3333-4333-8333-333333333333","encryption":"none"}]}]},"streamSettings":{"security":"tls","tlsSettings":{"serverName":"front.example.com"}}},{"protocol":"trojan","settings":{"servers":[{"address":"trojan-a.example.com","port":443,"password":"synthetic-a"},{"address":"trojan-b.example.com","port":443,"password":"synthetic-b"}]},"streamSettings":{"security":"tls","tlsSettings":{"serverName":"front.example.com"}}}]}`
	snapshot, report, err := parseCompatibility(t, body, source.FormatJSON, false)
	if err != nil || report.Accepted != 5 || snapshot.Len() != 5 {
		t.Fatalf("nested extraction = %d %+v %v", snapshot.Len(), report, err)
	}
	if strings.Contains(fmt.Sprint(report), "synthetic-a") {
		t.Fatal("diagnostic leaked credential")
	}
}

func TestURIAndXrayJSONShareCanonicalIdentity(t *testing.T) {
	uri := "vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=tls&sni=front.example.com&type=ws&path=%2Fws&host=ws.example.com"
	jsonConfig := `{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"EDGE.EXAMPLE.COM","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"security":"tls","network":"ws","tlsSettings":{"serverName":"FRONT.EXAMPLE.COM"},"wsSettings":{"path":"/ws","headers":{"Host":"WS.EXAMPLE.COM"}}}}]}`
	uriSnapshot, _, err := parseCompatibility(t, uri, source.FormatURIList, false)
	if err != nil {
		t.Fatal(err)
	}
	jsonSnapshot, _, err := parseCompatibility(t, jsonConfig, source.FormatJSON, false)
	if err != nil {
		t.Fatal(err)
	}
	left, right := uriSnapshot.Records()[0], jsonSnapshot.Records()[0]
	if !left.Identity().Equal(right.Identity()) || !left.Configuration().Equivalent(right.Configuration()) {
		t.Fatal("equivalent URI and JSON endpoints have different identity")
	}
	if !strings.HasPrefix(left.ID().String(), "ef2_") {
		t.Fatal("WebSocket Host did not use identity v2")
	}
	noHost, _, err := parseCompatibility(t, "vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=tls&sni=front.example.com&type=ws&path=%2Fws", source.FormatURIList, false)
	if err != nil || left.ID() == noHost.Records()[0].ID() {
		t.Fatal("different WebSocket Host reused identity")
	}
}

func TestSingBoxJSONOutboundRecords(t *testing.T) {
	content := `{"outbounds":[{"type":"direct","tag":"direct"},{"type":"vmess","tag":"vm","server":"vm.example.com","server_port":443,"uuid":"11111111-1111-4111-8111-111111111111","security":"auto","alter_id":0,"network":"tcp","tls":{"enabled":true,"server_name":"front.example.com"}},{"type":"shadowsocks","tag":"ss","server":"ss.example.com","server_port":8388,"method":"aes-256-gcm","password":"synthetic-secret"}]}`
	snapshot, report, err := parseCompatibility(t, content, source.FormatAuto, false)
	if err != nil || report.Accepted != 2 || snapshot.Len() != 2 {
		t.Fatalf("sing-box object = %d %+v %v", snapshot.Len(), report, err)
	}
	array := `[ {"type":"shadowsocks","server":"ss.example.com","server_port":8388,"method":"aes-256-gcm","password":"synthetic-secret"} ]`
	arraySnapshot, _, err := parseCompatibility(t, array, source.FormatJSON, false)
	if err != nil || arraySnapshot.Len() != 1 {
		t.Fatalf("sing-box record array = %v", err)
	}
	for _, bad := range []string{
		`{"outbounds":[{"type":"vmess","server":"vm.example.com","server_port":443,"uuid":"11111111-1111-4111-8111-111111111111","alter_id":1}]}`,
		`{"outbounds":[{"type":"vless","server":"vm.example.com","server_port":443,"uuid":"11111111-1111-4111-8111-111111111111","tls":{"enabled":true,"reality":{"public_key":"synthetic"}}}]}`,
	} {
		_, badReport, badErr := parseCompatibility(t, bad, source.FormatJSON, false)
		if badErr == nil || badReport.Unsupported != 1 {
			t.Fatalf("unsupported sing-box option = %+v %v", badReport, badErr)
		}
	}
}
