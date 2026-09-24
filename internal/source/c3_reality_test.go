package source_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/engine"
	"github.com/egressfox-io/egressfox/internal/source"
)

func TestRealityVisionURIAndXrayJSONCanonicalize(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	uri := fmt.Sprintf("vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?encryption=none&security=reality&type=tcp&sni=front.example.com&pbk=%s&sid=a1b2&fp=qq&flow=xtls-rprx-vision&alpn=h2%%2Chttp%%2F1.1", key)
	xray := fmt.Sprintf(`{"outbounds":[{"protocol":"vless","tag":"synthetic","settings":{"vnext":[{"address":"EDGE.EXAMPLE.COM","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none","flow":"xtls-rprx-vision"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"front.example.com","publicKey":%q,"shortId":"a1b2","fingerprint":"qq","alpn":["h2","http/1.1"]}}}]}`, key)
	uriSnapshot, _, err := parseCompatibility(t, uri, source.FormatURIList, false)
	if err != nil {
		t.Fatal(err)
	}
	jsonSnapshot, _, err := parseCompatibility(t, xray, source.FormatJSON, false)
	if err != nil {
		t.Fatal(err)
	}
	left, right := uriSnapshot.Records()[0], jsonSnapshot.Records()[0]
	singbox := fmt.Sprintf(`{"outbounds":[{"type":"vless","server":"edge.example.com","server_port":443,"uuid":"11111111-1111-4111-8111-111111111111","flow":"xtls-rprx-vision","tls":{"enabled":true,"server_name":"front.example.com","alpn":["h2","http/1.1"],"utls":{"enabled":true,"fingerprint":"qq"},"reality":{"enabled":true,"public_key":%q,"short_id":"a1b2"}}}]}`, key)
	singSnapshot, _, err := parseCompatibility(t, singbox, source.FormatJSON, false)
	if err != nil || !left.Identity().Equal(singSnapshot.Records()[0].Identity()) {
		t.Fatal("sing-box Reality profile differs from URI")
	}
	if !left.Identity().Equal(right.Identity()) || !left.Configuration().Equivalent(right.Configuration()) || !strings.HasPrefix(left.ID().String(), "ef3_") {
		t.Fatal("Reality/Vision URI and Xray profile differ")
	}
	if left.Configuration().Flow() != endpoint.FlowVision || left.Configuration().SecurityOptions().Fingerprint() != "qq" {
		t.Fatal("Reality/Vision semantics lost")
	}
	rotated, _, err := parseCompatibility(t, strings.Replace(uri, "sid=a1b2", "sid=c3d4", 1), source.FormatURIList, false)
	if err != nil || left.ID() != rotated.Records()[0].ID() || left.Identity().Equal(rotated.Records()[0].Identity()) {
		t.Fatal("Reality short ID rotation reused confidential revision")
	}
}

func TestRealityURIRejectsIncompleteAndUnknownFingerprint(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	base := fmt.Sprintf("vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=reality&sni=front.example.com&pbk=%s&fp=chrome&flow=xtls-rprx-vision", key)
	for _, uri := range []string{
		strings.Replace(base, "fp=chrome", "fp=unknown", 1),
		strings.Replace(base, "pbk="+key+"&", "", 1),
		strings.Replace(base, "sni=front.example.com&", "", 1),
		strings.Replace(base, "flow=xtls-rprx-vision", "flow=other", 1),
		strings.Replace(base, "security=reality", "security=tls", 1),
	} {
		if _, report, err := parseCompatibility(t, uri, source.FormatURIList, false); err == nil || report.Accepted != 0 {
			t.Fatal("unsafe Reality URI admitted")
		}
	}
}

func TestAdvancedTransportRepresentations(t *testing.T) {
	variants := []struct{ name, uri, xrayNetwork, xraySettings, singboxType, singboxOptions string }{
		{"http2", "type=h2&path=%2Fh2&host=front.example.com", "h2", `"httpSettings":{"path":"/h2","host":["front.example.com"]}`, "http", `"path":"/h2","host":["front.example.com"]`},
		{"httpupgrade", "type=httpupgrade&path=%2Fup&host=front.example.com", "httpupgrade", `"httpupgradeSettings":{"path":"/up","host":"front.example.com"}`, "httpupgrade", `"path":"/up","host":"front.example.com"`},
		{"grpc", "type=grpc&serviceName=Service", "grpc", `"grpcSettings":{"serviceName":"Service"}`, "grpc", `"service_name":"Service"`},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			uri := "vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=tls&sni=edge.example.com&" + variant.uri
			xray := fmt.Sprintf(`{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"edge.example.com","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":%q,"security":"tls","tlsSettings":{"serverName":"edge.example.com"},%s}}]}`, variant.xrayNetwork, variant.xraySettings)
			singbox := fmt.Sprintf(`{"outbounds":[{"type":"vless","server":"edge.example.com","server_port":443,"uuid":"11111111-1111-4111-8111-111111111111","tls":{"enabled":true,"server_name":"edge.example.com"},"transport":{"type":%q,%s}}]}`, variant.singboxType, variant.singboxOptions)
			var records []endpoint.Record
			for _, input := range []struct {
				text   string
				format source.Format
			}{{uri, source.FormatURIList}, {xray, source.FormatJSON}, {singbox, source.FormatJSON}} {
				snapshot, _, err := parseCompatibility(t, input.text, input.format, false)
				if err != nil || snapshot.Len() != 1 {
					t.Fatalf("advanced transport rejected: %v", err)
				}
				records = append(records, snapshot.Records()[0])
			}
			if !records[0].Identity().Equal(records[1].Identity()) || !records[0].Identity().Equal(records[2].Identity()) {
				t.Fatal("subscription representations differ in canonical identity")
			}
		})
	}
}

func TestXHTTPMihomoOnlyIdentityAndAdmission(t *testing.T) {
	base := "vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=tls&type=xhttp&path=%2Fxhttp&host=front.example.com&mode="
	var ids []endpoint.ID
	for _, mode := range []string{"stream-one", "stream-up", "packet-up"} {
		snapshot, _, err := parseCompatibility(t, base+mode, source.FormatURIList, false)
		if err != nil || snapshot.Len() != 1 {
			t.Fatalf("XHTTP %s: %v", mode, err)
		}
		ids = append(ids, snapshot.Records()[0].ID())
	}
	if ids[0] == ids[1] || ids[1] == ids[2] || ids[0] == ids[2] {
		t.Fatal("XHTTP mode collapsed in ef3 identity")
	}
	xray := `{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"edge.example.com","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":"xhttp","security":"tls","xhttpSettings":{"path":"/xhttp","host":"front.example.com","mode":"stream-one"}}}]}`
	snapshot, report, err := parseCompatibility(t, xray, source.FormatJSON, false)
	if err != nil || snapshot.Len() != 1 {
		t.Fatalf("Xray XHTTP rejected: %v %+v", err, report)
	}
	if snapshot.Records()[0].ID() != ids[0] {
		t.Fatal("Xray XHTTP differs from URI identity")
	}
	for _, invalid := range []string{base + "auto", base + "unknown", base + "stream-one&extraOption=1"} {
		if _, report, err := parseCompatibility(t, invalid, source.FormatURIList, false); err == nil || report.Accepted != 0 {
			t.Fatal("unsupported XHTTP option admitted")
		}
	}
}

func TestSanitizedXrayRealityProfiles(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "c3_xray_profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, report, err := parseCompatibility(t, string(content), source.FormatJSON, true)
	if err != nil || report.Accepted != 3 || report.Unsupported != 1 || snapshot.Len() != 2 {
		t.Fatalf("profile admission: accepted=%d unsupported=%d unique=%d err=%v", report.Accepted, report.Unsupported, snapshot.Len(), err)
	}
	for _, record := range snapshot.Records() {
		if record.Configuration().Flow() != endpoint.FlowVision || len(record.Provenance()) != 1 {
			t.Fatal("profile semantics or provenance lost")
		}
		for _, profile := range []artifact.Profile{artifact.Mihomo11931, artifact.SingBox1141} {
			if err := engine.CheckEndpoint(profile, record.Configuration()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, strictReport, strictErr := parseCompatibility(t, string(content), source.FormatJSON, false); strictErr == nil || strictReport.Unsupported != 1 {
		t.Fatal("strict source replaced transactional admission")
	}
}
