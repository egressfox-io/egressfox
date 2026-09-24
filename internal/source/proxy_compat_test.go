package source_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/source"
)

func TestExplicitProxyURIsAndCredentialRevision(t *testing.T) {
	t.Parallel()
	lines := []string{
		"socks5://proxy.example:1080#anonymous",
		"socks5://u%73er:p%40ss@proxy.example:1080#authenticated",
		"socks5://user:rotated@proxy.example:1080",
		"http://user:p%40ss@proxy.example:8080",
		"https://user:p%40ss@proxy.example:8080",
		"socks5://[2001:db8::1]:1080",
	}
	snapshot, report, err := parse(t, "proxy-fixture", []byte(strings.Join(lines, "\n")), source.ParseOptions{Format: source.FormatURIList})
	if err != nil || report.Accepted != len(lines) || snapshot.Len() != len(lines) {
		t.Fatalf("parse: %v, report=%+v, len=%d", err, report, snapshot.Len())
	}
	var anonymous, authenticated, rotated endpoint.Identity
	var plain, secured endpoint.ID
	for _, record := range snapshot.Records() {
		configuration := record.Configuration()
		if configuration.Address().Host() == "proxy.example" && configuration.Address().Port() == 1080 {
			switch configuration.Credential().Reveal() {
			case "":
				anonymous = record.Identity()
			case "p@ss":
				authenticated = record.Identity()
			case "rotated":
				rotated = record.Identity()
			}
		}
		if configuration.Address().Port() == 8080 {
			if configuration.TLS().Enabled() {
				secured = record.ID()
			} else {
				plain = record.ID()
			}
		}
	}
	if !anonymous.SameEndpoint(authenticated) || anonymous.Equal(authenticated) || authenticated.Equal(rotated) || plain == secured {
		t.Fatal("proxy authentication or TLS semantics collapsed")
	}
	for _, golden := range []struct{ name, actual, want string }{
		{"SOCKS5", anonymous.ID().String(), "ef3_dnnapawlky2kowsn2z54krq2qaw5bpkfbddqgt6af7jvfsgwga3q"},
		{"HTTP", plain.String(), "ef3_4tqcatw6krend6v3vgkow4ba4ew77avuhllandmm7v2jtfobeyda"},
		{"HTTPS", secured.String(), "ef3_te5lzqze46njaomnbge6mf7ydxxtcl6m76ezmmj47voljjiaj2ra"},
	} {
		if golden.actual != golden.want {
			t.Fatalf("%s identity changed: got %s", golden.name, golden.actual)
		}
	}
	for _, record := range snapshot.Records() {
		for _, secret := range []string{"user", "p@ss", "rotated"} {
			if strings.Contains(record.ID().String(), secret) {
				t.Fatal("credential leaked into logical ID")
			}
		}
	}
}

func TestProxyURIRequiresExplicitContext(t *testing.T) {
	t.Parallel()
	for _, format := range []source.Format{source.FormatAuto, source.FormatJSON} {
		input := []byte("socks5://proxy.example:1080")
		if format == source.FormatJSON {
			input, _ = json.Marshal([]string{string(input)})
		}
		snapshot, report, err := parse(t, "proxy-fixture", input, source.ParseOptions{Format: format})
		if err != nil || snapshot.Len() != 1 || report.Accepted != 1 {
			t.Fatalf("SOCKS5 format=%v: report=%+v err=%v", format, report, err)
		}
	}
	for _, format := range []source.Format{source.FormatAuto, source.FormatJSON} {
		input := []byte("https://user:password@proxy.example:443")
		if format == source.FormatJSON {
			input, _ = json.Marshal([]string{string(input)})
		}
		_, report, err := parse(t, "proxy-fixture", input, source.ParseOptions{Format: format})
		if err == nil || report.Accepted != 0 {
			t.Fatalf("ambiguous HTTP link admitted: format=%v report=%+v err=%v", format, report, err)
		}
	}
	for _, input := range []string{
		"socks4://host.example:1080", "socks5h://host.example:1080",
		"socks5://user@host.example:1080", "socks5://user:@host.example:1080",
		"http://host.example:8080/path", "https://host.example:443?insecure=1",
		"http://u%3Aname:password@host.example:8080",
	} {
		_, report, err := parse(t, "proxy-fixture", []byte(input), source.ParseOptions{Format: source.FormatURIList})
		if err == nil || report.Accepted != 0 || report.Unsupported != 1 {
			t.Fatalf("invalid proxy URI %q: report=%+v err=%v", input, report, err)
		}
		if strings.Contains(report.Diagnostics[0].Code, "user") || strings.Contains(report.Diagnostics[0].Code, "password") {
			t.Fatal("diagnostic leaked credentials")
		}
	}
}

func TestProxyJSONOutboundsExpandAndDeduplicate(t *testing.T) {
	t.Parallel()
	input := `[{"dns":{"servers":["metadata.example"]},"routing":{"rules":[{"outboundTag":"direct"}]},"inbounds":[{"protocol":"socks"}],"outbounds":[{"protocol":"socks","tag":"socks","settings":{"servers":[{"address":"proxy.example","port":1080,"users":[{"user":"alice","pass":"one"},{"user":"bob","pass":"two"}]}]}},{"protocol":"http","settings":{"address":"proxy.example","port":8443,"user":"alice","pass":"one"},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"proxy.example","allowInsecure":true}}},{"protocol":"freedom"}]},{"outbounds":[{"protocol":"socks","settings":{"servers":[{"address":"proxy.example","port":1080,"users":[{"user":"alice","pass":"one"}]}]}},{"type":"http","server":"proxy.example","server_port":8443,"username":"alice","password":"one","tls":{"enabled":true,"server_name":"proxy.example","insecure":true}},{"type":"direct"}]}]`
	snapshot, report, err := parse(t, "proxy-fixture", []byte(input), source.ParseOptions{Format: source.FormatJSON})
	if err != nil || report.Accepted != 5 || snapshot.Len() != 3 {
		t.Fatalf("JSON extraction: %v report=%+v len=%d", err, report, snapshot.Len())
	}
	for _, record := range snapshot.Records() {
		if record.Configuration().Protocol() == endpoint.ProtocolHTTPProxy && (!record.Configuration().TLS().Enabled() || !record.Configuration().TLS().InsecureSkipVerify()) {
			t.Fatal("HTTPS proxy TLS semantics lost")
		}
	}
	for _, unsupported := range []string{
		`[{"type":"socks","server":"proxy.example","server_port":1080,"version":"4"}]`,
		`[{"type":"socks","server":"proxy.example","server_port":1080,"tls":{"enabled":false}}]`,
		`[{"type":"http","server":"proxy.example","server_port":8080,"headers":{"X-Secret":"hidden"}}]`,
		`[{"type":"http","server":"proxy.example","server_port":8080,"version":""}]`,
		`[{"outbounds":[{"protocol":"http","settings":{"address":"proxy.example","port":8080,"headers":{"X-Secret":"hidden"}}}]}]`,
	} {
		_, rejected, err := parse(t, "proxy-fixture", []byte(unsupported), source.ParseOptions{Format: source.FormatJSON})
		if err == nil || rejected.Unsupported != 1 {
			t.Fatalf("unsupported JSON admitted: report=%+v err=%v", rejected, err)
		}
		if strings.Contains(rejected.Diagnostics[0].Code, "hidden") {
			t.Fatal("diagnostic leaked credential-like option")
		}
	}
}

func TestProxyURIAndJSONCanonicalizeIdentically(t *testing.T) {
	t.Parallel()
	for _, pair := range []struct{ uri, json string }{
		{"socks5://user:p%40ss@proxy.example:1080", `[{"type":"socks","server":"proxy.example","server_port":1080,"version":"5","username":"user","password":"p@ss","network":"tcp"}]`},
		{"http://user:p%40ss@proxy.example:8080", `[{"type":"http","server":"proxy.example","server_port":8080,"username":"user","password":"p@ss"}]`},
		{"https://user:p%40ss@proxy.example:8443", `[{"type":"http","server":"proxy.example","server_port":8443,"username":"user","password":"p@ss","tls":{"enabled":true,"server_name":"proxy.example","insecure":false}}]`},
	} {
		uriSnapshot, _, err := parse(t, "proxy-uri", []byte(pair.uri), source.ParseOptions{Format: source.FormatURIList})
		if err != nil {
			t.Fatal(err)
		}
		jsonSnapshot, _, err := parse(t, "proxy-json", []byte(pair.json), source.ParseOptions{Format: source.FormatJSON})
		if err != nil {
			t.Fatal(err)
		}
		if !uriSnapshot.Records()[0].Identity().Equal(jsonSnapshot.Records()[0].Identity()) {
			t.Fatalf("URI/JSON connection identity differs for %s", uriSnapshot.Records()[0].Configuration().Protocol())
		}
	}
}
