# M8.5 engine protocol and transport compatibility matrix

Status: C1 research and domain contract, 2026-09-24. This is the **single
authoritative combination matrix** for M8.5. It distinguishes an upstream source
feature from the **exact EgressFox build profile** and from an EgressFox
source-to-managed-traffic claim. The [M8 subscription matrix](subscription-compatibility.md)
remains the authority for currently admitted input formats.

## Exact profiles and evidence

| Profile | Source and build | Consequence |
| --- | --- | --- |
| Mihomo `egressfox.mihomo/v1` | 1.19.31, commit `ab405bad5beeeac8b003bb01f60f134f6df54471`, build revision 1, Go 1.27.1, `with_gvisor` | The pinned [parser](https://github.com/MetaCubeX/mihomo/blob/ab405bad5beeeac8b003bb01f60f134f6df54471/adapter/parser.go) registers VLESS, VMess, Trojan, SS, SOCKS5, HTTP and Hysteria2. Its [VLESS outbound](https://github.com/MetaCubeX/mihomo/blob/ab405bad5beeeac8b003bb01f60f134f6df54471/adapter/outbound/vless.go) includes Reality, Vision, HTTP/2, gRPC and XHTTP options. |
| sing-box `egressfox.sing-box/v1` | 1.14.1, commit `1ac1a339cb1223e9c70eae14c44411c75033c02d`, build revision 1, Go 1.27.1, **no optional build tags**, EgressFox command overlay | The pinned [outbound registry](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/include/registry.go) includes the common TCP families. Its [transport union](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/option/v2ray_transport.go) includes HTTP, WS, QUIC, gRPC and HTTPUpgrade, **not XHTTP**. The no-tag [QUIC stub](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/include/quic_stub.go) rejects Hysteria2; the [uTLS stub](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/common/tls/utls_stub.go) rejects uTLS and Reality. `with_grpc` is absent, so the [lite gRPC implementation](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/transport/v2ray/grpc_lite.go) is selected. |

The source references above were checked against the local corresponding-source
archives produced by the existing release tooling and the build tags in
[`release/manifest.json`](../../release/manifest.json). The [official sing-box
build-tag contract](https://sing-box.sagernet.org/installation/build-from-source/)
describes `with_quic`, `with_utls` and `with_grpc`. A source type or current
upstream website alone is **not** a claim that this build can execute it. No
engine version or build revision changes in C1.

## Meaning of status

- **Implemented and tested:** EgressFox already ingests, renders, natively
  validates and has controlled traffic evidence for the named subset.
- **Engine supports; EgressFox pending:** the exact built profile has the needed
  option/implementation, but C2 or C3 still needs source, renderer, probe and
  traffic qualification.
- **Engine-specific:** one pinned build has the feature while the other lacks it.
- **Unsupported by current build:** the upstream source may implement it, but
  EgressFox's pinned build cannot execute it, or the combination is invalid.
- **Unverified:** parameter-level interoperability or exact native/traffic
  behavior has not been demonstrated; ingestion must reject it meanwhile.

| Connection family and semantics | Mihomo 1.19.31 build | sing-box 1.14.1 EgressFox build | EgressFox today / slice | Required parameters and evidence |
| --- | --- | --- | --- | --- |
| VLESS UUID, TCP/WS path and Host, plain or ordinary TLS | Supported | Supported | **Implemented and tested** in M1–M8 | UUID, server/port, WS path/Host, SNI and verify mode; existing renderer goldens and native/traffic tests. |
| Trojan password, TCP/WS, ordinary TLS | Supported | Supported | **Implemented and tested** in M1–M8 | Password, server/port, required TLS/SNI, WS path/Host; existing tests. Trojan without TLS is invalid domain data. |
| VMess UUID, alter ID 0, admitted payload cipher, TCP/WS, plain or TLS | Supported | Supported | **Implemented and tested** in M8 | UUID, cipher distinct from TLS, server/port, WS path/Host, SNI; [M8 matrix](subscription-compatibility.md) and plans 0018–0019. Legacy alter IDs and other ciphers remain unsupported input. |
| Shadowsocks plugin-free admitted AEAD methods over TCP | Supported | Supported | **Implemented and tested** in M8 | Method and password are both connection-critical; plugins and 2022 methods require separate C2 qualification. |
| SOCKS5 TCP, optional username/password | Supported | Supported | C2 source, render, native and controlled probe/traffic pass; managed kind pending | Explicit SOCKS version 5, server/port, anonymous or username/password auth; probe DNS/IP behavior must be controlled. UDP extensions are unverified and excluded. |
| HTTP CONNECT TCP, optional Basic auth; HTTPS CONNECT with TLS | Supported | Supported | C2 source, render, native and controlled probe/traffic pass; managed kind pending | Server/port, auth pair, TLS/SNI/verification for HTTPS. Extra headers, HTTP version, paths and HTTP/3 are unverified for the pinned pair and rejected. [sing-box HTTP](https://sing-box.sagernet.org/configuration/outbound/http/) documents fields introduced only in 1.15. |
| VLESS/VMess HTTP/2 with TLS | Supported | HTTP transport with TLS; exact HTTP/2 negotiation needs proof | Typed path/host; **C3 pending** | HTTP/2 without TLS is rejected in C1 to prevent sing-box's HTTP/1.1 behavior. Pinned Mihomo Trojan has no HTTP/2 option, so Trojan/HTTP2 is invalid for the common C1 domain. |
| VLESS/VMess/Trojan gRPC | Supported | Lite gRPC; standard `with_grpc` build absent | Typed service; **C3 pending** | Preserve service name and mode; qualify lite implementation against target servers. |
| VLESS/VMess/Trojan HTTPUpgrade | WS upgrade mode; exact interop unverified | Dedicated HTTPUpgrade transport | Typed path/host; **C3 pending** | Early data, request headers and method require C3 types. Equivalence of Mihomo WS upgrade and sing-box HTTPUpgrade is **unverified**, so no current support claim. |
| VLESS TCP + TLS/Reality + Vision, SNI/ALPN/client fingerprint | Source supports | **Unsupported by current build**: `with_utls` absent, so Reality/uTLS stub returns an error | Typed subset; **C3 pending** after separately reviewed build revision | UUID, flow, Reality public key, short ID, SNI, ordered ALPN and fingerprint; no substitution with ordinary TLS. [Pinned sing-box Reality implementation](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/common/tls/reality_client.go) requires uTLS. |
| VLESS XHTTP, mode/path/authority and connection-critical request settings | **Engine-specific source support** in pinned VLESS outbound | **Unsupported**: absent from pinned transport union | Discriminator reserved; **C3 pending**, no C1 constructor | Mihomo modes include `auto`, `stream-one`, `stream-up`, `packet-up`; headers, padding, upload/download settings and HTTP version can change the connection. No WS/gRPC substitution. Current sing-box parity would require a separately reviewed upstream change or an explicit engine-specific M8.5 exception. |
| Hysteria2 over UDP/QUIC with TLS, password, bandwidth and optional Salamander | Source supports | **Unsupported by current build**: `with_quic` absent; stub rejects outbound | Typed basic domain; **C4 pending** after separately reviewed build revision | UDP server/port, password, TLS/SNI, ALPN, bandwidth/CC, obfuscation secret, timeout and authorized DNS. Port hopping, Gecko, realm, QUIC tuning and certificate pinning need C4 parameter-level contracts. [Pinned Mihomo options](https://github.com/MetaCubeX/mihomo/blob/ab405bad5beeeac8b003bb01f60f134f6df54471/adapter/outbound/hysteria2.go); [pinned sing-box options](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/option/hysteria2.go). |
| Hysteria2 over TCP; Reality downgraded to ordinary TLS; XHTTP replaced by WS | Invalid or incompatible | Invalid or incompatible | **Unsupported** permanently as substitutions | These do not preserve the requested wire protocol. |

## C2 qualification contract

C2 retains the four existing basic families above without changing their admitted
TCP/WS, ordinary TLS, credential or cipher subsets. The following new forms are
the only C2 additions; each needs the same subscription → identity → capability
→ renderer → native check → controlled probe → managed Gateway traffic path for
**both** pinned profiles before its status changes to implemented.

| Input and canonical form | Mihomo mapping | sing-box mapping | Exclusions |
| --- | --- | --- | --- |
| `socks5://host:port` or `socks5://user:password@host:port` in a URI list or JSON URI array; Xray `socks` outbound; sing-box `socks` outbound with version 5 or omitted. `SOCKS5`, TCP, TLS disabled, no auth or a nonempty username/password pair. | `type: socks5`, `username`/`password` when present, `udp: false` | `type: socks`, `version: "5"`, `username`/`password` when present, `network: "tcp"` | SOCKS4/4a, SOCKS-over-TLS, UDP, `socks5h`/curl DNS semantics, empty-password auth and dial options. SOCKS5 passes the destination hostname to the proxy; endpoint-server DNS remains the engine's dial concern. |
| Explicit URI-list `http://host:port` or `http://user:password@host:port`; Xray `http` outbound; sing-box `http` outbound without TLS. `HTTPProxy`, TCP, TLS disabled, no auth or nonempty Basic pair. | `type: http`, TLS false, optional `username`/`password` | `type: http`, optional `username`/`password` | Arbitrary HTTP links in auto-detected or generic JSON URI lists, extra headers, path, HTTP version and non-Basic authentication. The engine uses CONNECT for tunneled targets. |
| Explicit URI-list `https://host:port` or `https://user:password@host:port`; sing-box `http` outbound with TLS; Xray `http` outbound with ordinary TLS stream. `HTTPProxy`, TCP, TLS enabled with effective SNI and verification mode. | `type: http`, `tls: true`, `sni`, `skip-cert-verify`, optional auth | `type: http`, `tls.enabled`, `tls.server_name`, `tls.insecure`, optional auth | HTTPS describes TLS **to the proxy**, independent of target HTTPS. Certificate pinning, ALPN, client fingerprints, custom CAs and extra HTTP options remain unsupported. |

HTTP(S) proxy URIs require an explicit `FormatURIList` or
`FormatBase64URIList` source setting. Automatic format detection intentionally
does not infer an HTTP proxy inventory from an HTTP link. JSON requires the
recognized outbound schema for HTTP(S); unambiguous `socks5://` records are
accepted in URI lists and JSON URI arrays. Existing public logical IDs
never include credentials; the protected connection revision distinguishes no auth
from authenticated connections and password rotation. C2 does not reinterpret
legacy `ef1_`/`ef2_` identities.

## Subscription and identity implications

M8 already admits URI lines, Base64 envelopes, JSON URI arrays, bounded Xray
client configurations and sing-box outbound arrays. VLESS/Trojan URIs, VMess
Base64 JSON and Shadowsocks SIP002/Xray records cover only the current subset.
C2 must research and admit bounded SOCKS5, HTTP(S) and newly qualified variants;
C3 must preserve Reality, Vision, HTTPUpgrade/gRPC/HTTP2/XHTTP share parameters;
C4 must preserve Hysteria2 URI auth, obfs, QUIC and port behavior. Existing Xray
profiles' routing, DNS and inbounds never enter the control plane. The
[v3 identity decision](../decisions/0021-version-three-connection-semantics-and-capabilities.md)
keeps the original `ef1_`/`ef2_` bytes and protected observations intact.

## Gates for later slices

- **C2:** Complete bounded source decoding, auth validation, exact renderer
  mappings, native checks, isolated through-engine probe and managed Gateway
  traffic for the named protocol families. Extend this matrix per claimed cipher
  and authentication mode. Do not import native routing or implement UDP variants.
- **C3:** Specify typed HTTP/2, HTTPUpgrade, gRPC and XHTTP options from real
  shares, then renderer-specific mappings and negative capability cases. Review
  a sing-box build revision enabling `with_utls` before any sing-box Reality/uTLS
  claim. Keep XHTTP Mihomo-specific unless an exact sing-box implementation is
  separately qualified. No fallback to another transport/security method.
- **C4:** Review a sing-box build revision enabling `with_quic` before claiming
  Hysteria2. Extend the typed QUIC options and UDP destination authorization for
  every dial/port-hop/realm address. Native check, controlled QUIC through-engine
  probe and managed traffic are required. No custom QUIC client in EgressFox.

Each new combination must pass subscription → model → identity/dedup → exact
capability gate → renderer → native validation → through-engine probe → managed
Gateway traffic. Engine source support alone never changes the EgressFox status.
