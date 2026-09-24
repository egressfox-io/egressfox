package source_test

import (
	"strings"
	"testing"

	"github.com/egressfox-io/egressfox/internal/source"
)

func TestHysteria2URIAndSingBoxJSONIdentity(t *testing.T) {
	uri := "hy2://auth-canary@EDGE.example.com:443?sni=front.example.com&alpn=h3&obfs=salamander&obfs-password=obfs-canary&upmbps=20&downmbps=40&ports=443-444%2C8443"
	json := `{"outbounds":[{"type":"hysteria2","server":"edge.example.com","password":"auth-canary","up_mbps":20,"down_mbps":40,"obfs":{"type":"salamander","password":"obfs-canary"},"tls":{"enabled":true,"server_name":"front.example.com","alpn":["h3"]},"server_ports":["443:444","8443"]}]}`
	left, _, err := parseCompatibility(t, uri, source.FormatURIList, false)
	if err != nil || left.Len() != 1 {
		t.Fatalf("URI: %v", err)
	}
	right, _, err := parseCompatibility(t, json, source.FormatJSON, false)
	if err != nil || right.Len() != 1 {
		t.Fatalf("JSON: %v", err)
	}
	if !left.Records()[0].Identity().Equal(right.Records()[0].Identity()) {
		t.Fatal("canonical identity differs")
	}
	official, _, err := parseCompatibility(t, "hysteria2://auth-canary@edge.example.com:443-444,8443/?sni=front.example.com&alpn=h3&obfs=salamander&obfs-password=obfs-canary&upmbps=20&downmbps=40", source.FormatURIList, false)
	if err != nil || !left.Records()[0].Identity().Equal(official.Records()[0].Identity()) {
		t.Fatalf("official multi-port URI differs: %v", err)
	}
	defaultPort, _, err := parseCompatibility(t, "hy2://user:password@edge.example.com/?sni=edge.example.com", source.FormatURIList, false)
	if err != nil || defaultPort.Records()[0].Configuration().Address().Port() != 443 {
		t.Fatalf("default port or userpass: %v", err)
	}
	if !strings.HasPrefix(left.Records()[0].ID().String(), "ef3_") {
		t.Fatal("Hysteria2 must use ef3")
	}
	rotated, _, err := parseCompatibility(t, strings.Replace(uri, "obfs-canary", "rotated-canary", 1), source.FormatURIList, false)
	if err != nil || rotated.Records()[0].ID() != left.Records()[0].ID() || rotated.Records()[0].Identity().Equal(left.Records()[0].Identity()) {
		t.Fatal("secret rotation reused revision or changed logical ID")
	}
}

func TestHysteria2URIRejectsUnsupportedOptions(t *testing.T) {
	base := "hysteria2://auth-canary@edge.example.com:443?"
	for _, query := range []string{"realm=x", "obfs=gecko&obfs-password=x", "obfs=salamander", "ports=1-500", "upmbps=5", "insecure=maybe", "sni=x&sni=y"} {
		_, report, err := parseCompatibility(t, base+query, source.FormatURIList, false)
		if err == nil || report.Accepted != 0 {
			t.Fatalf("unsafe query %q admitted: %+v %v", query, report, err)
		}
	}
}
