package endpoint_test

import (
	"crypto/subtle"
	"encoding/hex"
	"testing"

	"github.com/egressfox-io/egressfox/internal/endpoint"
)

// Values were captured from the unmodified M8 implementation at b8d5b50.
// The revisions here derive only from deliberately synthetic test credentials.
func TestM8IdentityBytesRemainStable(t *testing.T) {
	t.Parallel()
	address, _ := endpoint.NewAddress("edge.example.com", 443)
	tls, _ := endpoint.NewTLS("", false)
	ws, _ := endpoint.NewWebSocketTransport("/ws")
	wsHost, _ := endpoint.NewWebSocketTransportWithHost("/ws", "front.example.com")
	vless, _ := endpoint.NewVLESSCredential(syntheticVLESSUserID)
	trojan, _ := endpoint.NewTrojanCredential("synthetic-password")
	vmess, _ := endpoint.NewVMessCredential(syntheticVLESSUserID)
	ss, _ := endpoint.NewShadowsocksCredential("synthetic-password")
	type golden struct {
		name, id, revision string
		configuration      endpoint.Configuration
	}
	makeBase := func(protocol endpoint.Protocol, credential endpoint.Credential, transport endpoint.Transport) endpoint.Configuration {
		c, err := endpoint.NewConfiguration(protocol, address, credential, transport, tls)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	vm, err := endpoint.NewVMessConfiguration(address, vmess, wsHost, tls, "auto")
	if err != nil {
		t.Fatal(err)
	}
	shadow, err := endpoint.NewShadowsocksConfiguration(address, ss, "aes-128-gcm")
	if err != nil {
		t.Fatal(err)
	}
	cases := []golden{
		{"vless-tcp", "ef1_fq56uvsashikgrfhfvnqckuxoctbrgbknbuef5bliqcuigsks33a", "ef80c09688cacda5b2a3c1fe250bfcef38e2521c2259be8ce43b5746b6236756", makeBase(endpoint.ProtocolVLESS, vless, endpoint.NewTCPTransport())},
		{"vless-ws", "ef1_izh3mcwvu5nccxxv6dizulz45ym7v4nbvjuxaunbdekanm5sif2a", "35a3591811db90e676a3302a4ae48b154b2866dc7d84764e2078e79f791a7f63", makeBase(endpoint.ProtocolVLESS, vless, ws)},
		{"trojan-tcp", "ef1_sj5x347s2su7k7ykvccfpzh4q5fgqaiswlg335oigessqniujyeq", "e8ee9662cabb90e9d8ef6ceb479ca0757e392b74394d1fc90745a8e0f0c50264", makeBase(endpoint.ProtocolTrojan, trojan, endpoint.NewTCPTransport())},
		{"trojan-ws", "ef1_t7dzyhpcw4hgo7gw3e5r3m2hx6brjaymff2264tdaunooi2wcqpa", "701bda7602ad57411e3e20e2e0674a0c3e69f69027443d0171411c2e56c004a6", makeBase(endpoint.ProtocolTrojan, trojan, ws)},
		{"vmess-ws-host", "ef2_i23vf7mxcr33cvtnwsc72jk7vulxuc455ngrvfo7teda6roj74wa", "fce3a191853f7eb20c8b1cb1e1569ddbe689e8531c616b0867b2aa3bc8e49460", vm},
		{"ss-tcp", "ef2_7zsdskn5kfrtndagv6q6lny27gdfibp6nbaqcszgqbkbfarfeska", "88184e82ef8a48c66b166d4e8130a62d6ba89b96700152b10920fb2d075c93c2", shadow},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if test.configuration.ID().String() != test.id {
				t.Fatal("M8 logical ID changed")
			}
			want, err := hex.DecodeString(test.revision)
			if err != nil {
				t.Fatal(err)
			}
			got, err := test.configuration.Identity().Revision().RevealForPersistence()
			if err != nil || subtle.ConstantTimeCompare(got, want) != 1 {
				t.Fatal("M8 protected revision changed")
			}
		})
	}
}
