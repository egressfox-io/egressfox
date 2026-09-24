# M8.5 engine protocol and transport compatibility matrix

Status: C1–C2 complete; C3 local traffic qualified, expanded managed qualification pending, 2026-09-25. This is the **single
authoritative combination matrix** for M8.5. It distinguishes an upstream source
feature from the **exact EgressFox build profile** and from an EgressFox
source-to-managed-traffic claim. The [M8 subscription matrix](subscription-compatibility.md)
remains the authority for currently admitted input formats.

## Exact profiles and evidence

| Profile | Source and build | Consequence |
| --- | --- | --- |
| Mihomo `egressfox.mihomo/v1` | 1.19.31, commit `ab405bad5beeeac8b003bb01f60f134f6df54471`, build revision 1, Go 1.27.1, `with_gvisor` | The pinned [parser](https://github.com/MetaCubeX/mihomo/blob/ab405bad5beeeac8b003bb01f60f134f6df54471/adapter/parser.go) registers VLESS, VMess, Trojan, SS, SOCKS5, HTTP and Hysteria2. Its [VLESS outbound](https://github.com/MetaCubeX/mihomo/blob/ab405bad5beeeac8b003bb01f60f134f6df54471/adapter/outbound/vless.go) includes Reality, Vision, HTTP/2, gRPC and XHTTP options. |
| sing-box `egressfox.sing-box/v2` | 1.14.1, commit `1ac1a339cb1223e9c70eae14c44411c75033c02d`, build revision 2, Go 1.27.1, `with_utls`, EgressFox command overlay | The pinned [outbound registry](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/include/registry.go) includes the common TCP families. Its [transport union](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/option/v2ray_transport.go) includes HTTP, WS, QUIC, gRPC and HTTPUpgrade, **not XHTTP**. `with_utls` compiles Reality/uTLS; the no-tag [QUIC stub](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/include/quic_stub.go) still rejects Hysteria2. `with_grpc` is absent, so the [lite gRPC implementation](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/transport/v2ray/grpc_lite.go) is selected. |

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
| SOCKS5 TCP, optional username/password | Supported | Supported | **Implemented and tested in C2**, including both managed engines | Explicit SOCKS version 5, server/port, anonymous or username/password auth; probe DNS/IP behavior is controlled. UDP extensions are unverified and excluded. |
| HTTP CONNECT TCP, optional Basic auth; HTTPS CONNECT with TLS | Supported | Supported | **Implemented and tested in C2**, including both managed engines | Server/port, auth pair, TLS/SNI/verification for HTTPS. Extra headers, HTTP version, paths and HTTP/3 are unverified for the pinned pair and rejected. [sing-box HTTP](https://sing-box.sagernet.org/configuration/outbound/http/) documents fields introduced only in 1.15. |
| VLESS HTTP/2 with TLS | Supported | HTTP transport negotiates HTTP/2 under TLS | Basic path/one Host **local and managed traffic passed** both engines; full kind rerun pending | HTTP/2 without TLS is invalid. VMess remains unqualified; Trojan/HTTP2 is invalid in the common domain. |
| VLESS gRPC over TLS | Supported | Lite gRPC with no `with_grpc` tag | Basic service name **local and managed traffic passed** both engines; full kind rerun pending | Additional gRPC authority, mode and timers are rejected by ingestion. VMess/Trojan remain unqualified. |
| VLESS HTTPUpgrade over TLS | WS upgrade mode | Dedicated HTTPUpgrade transport | Basic path/Host **local and managed traffic passed** both engines; full kind rerun pending | Mihomo `ws-opts.v2ray-http-upgrade` interoperated with the pinned sing-box server. Early data, headers and method are unqualified and rejected. VMess/Trojan remain unqualified. |
| VLESS TCP + Reality + Vision, SNI/ALPN/client fingerprint | Supported | `with_utls` build revision 2 supports Reality/Vision | URI/Xray/sing-box JSON, native validation and **local and managed traffic passed** both engines; full kind rerun pending | UUID, flow, public key, private short ID, SNI, ordered ALPN and fingerprint are preserved. No TLS substitution. Ordinary TLS Vision and Reality with advanced transports remain unqualified. [Pinned sing-box Reality implementation](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/common/tls/reality_client.go) requires uTLS. |
| VLESS XHTTP with ordinary TLS, path/Host and explicit `stream-one`, `stream-up` or `packet-up` mode | **Local traffic qualified in all three modes** | **Unsupported**: absent from pinned transport union | Mihomo-only URI/Xray input, native validation and local traffic; managed path pending | `auto` and advanced headers/padding/upload/download options are rejected until their connection semantics can be bounded. No WS/gRPC substitution. |
| Hysteria2 over UDP/QUIC with TLS, password, bandwidth and optional Salamander | Source supports | **Unsupported by current build**: `with_quic` absent; stub rejects outbound | Typed basic domain; **C4 pending** after separately reviewed build revision | UDP server/port, password, TLS/SNI, ALPN, bandwidth/CC, obfuscation secret, timeout and authorized DNS. Port hopping, Gecko, realm, QUIC tuning and certificate pinning need C4 parameter-level contracts. [Pinned Mihomo options](https://github.com/MetaCubeX/mihomo/blob/ab405bad5beeeac8b003bb01f60f134f6df54471/adapter/outbound/hysteria2.go); [pinned sing-box options](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/option/hysteria2.go). |
| Hysteria2 over TCP; Reality downgraded to ordinary TLS; XHTTP replaced by WS | Invalid or incompatible | Invalid or incompatible | **Unsupported** permanently as substitutions | These do not preserve the requested wire protocol. |

## C2 qualification contract

C2 retains the four existing basic families above without changing their admitted
TCP/WS, ordinary TLS, credential or cipher subsets. The following new forms are
the only C2 additions. Their subscription → identity → capability → renderer →
native check → controlled probe → managed Gateway traffic path passed for
**both** pinned profiles; exact evidence is in [plan 0020](../plans/0020-m85-protocol-transport-compatibility.md).

| Input and canonical form | Mihomo mapping | sing-box mapping | Exclusions |
| --- | --- | --- | --- |
| `socks5://host:port` or `socks5://user:password@host:port` in a URI list or JSON URI array; Xray `socks` outbound; sing-box `socks` outbound with version 5 or omitted. `SOCKS5`, TCP, TLS disabled, no auth or a nonempty username/password pair. | `type: socks5`, `username`/`password` when present, `udp: false` | `type: socks`, `version: "5"`, `username`/`password` when present, `network: "tcp"` | Ambiguous `socks://`, SOCKS4/4a, SOCKS-over-TLS, UDP, `socks5h`/curl DNS semantics, empty-password auth and dial options. SOCKS5 passes the destination hostname to the proxy; endpoint-server DNS remains the engine's dial concern. |
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

## C3 bounded profile and evidence stages

The admitted C3 subset is VLESS only. TLS transport tests use a synthetic local
certificate with explicit test-only verification opt-out. Production verification
continues to default to strict. Reality public keys are canonical 32-byte
base64url; short IDs are zero to eight bytes of even-length hex and enter only
the protected connection revision. `xtls-rprx-vision` is the only admitted flow.
Ordinary TLS Vision without Reality remains behind the capability gate.
Reality without Vision and plain gRPC, HTTPUpgrade or XHTTP remain outside the
selected traffic-qualified subset and fail the exact-profile capability gate.
Fingerprints admitted from the common pinned uTLS names are `chrome`, `firefox`,
`safari`, `ios`, `android`, `edge`, `360`, and `qq`. `qq` is a pinned-engine name,
not an alias for Chrome. Unknown values fail domain validation. Dynamic `random`
and `randomized`, deprecated browser variants and engine-specific names remain
outside the reproducible common subset.

| Protocol / transport / security | Auth and connection fields | Accepted input | Mihomo 1.19.31 | sing-box 1.14.1 v2 | Evidence stage |
| --- | --- | --- | --- | --- | --- |
| VLESS / TCP / Reality + Vision | UUID, Vision flow, SNI, key, short ID, fingerprint, ordered ALPN | URI `security=reality`, `pbk`, `sid`, `fp`, `flow`; Xray `realitySettings`; sing-box `tls.reality` + `utls` | Reality opts, client fingerprint, flow, ALPN | TLS Reality, uTLS, flow, ALPN; requires `with_utls` | Domain ✓; source ✓; renderer ✓; native ✓; controlled traffic ✓ both; managed traffic ✓ both; full kind rerun pending |
| VLESS / TCP / ordinary TLS with uTLS/ALPN | UUID, SNI, verify mode, fingerprint, ordered ALPN | URI `fp`, `alpn`; Xray `tlsSettings`; sing-box `tls.utls`/`alpn` | Client fingerprint, ALPN | TLS uTLS, ALPN; requires `with_utls` | Domain ✓; source ✓; renderer ✓; native ✓; local `qq` + `http/1.1` traffic ✓ both; managed traffic ✓ both; full kind rerun pending |
| VLESS / HTTP/2 / ordinary TLS | UUID, path, one Host, SNI, verify mode | URI `type=h2`; Xray `httpSettings`; sing-box `transport.type=http` | `network: h2`, `h2-opts` | `transport.type=http`, TLS | Domain ✓; source ✓; renderer ✓; native ✓; controlled traffic ✓ both; managed traffic ✓ both; full kind rerun pending |
| VLESS / HTTPUpgrade / ordinary TLS | UUID, path, Host, SNI, verify mode | URI `type=httpupgrade`; Xray `httpupgradeSettings`; sing-box `transport.type=httpupgrade` | WS option with explicit v2ray HTTP upgrade | Dedicated `httpupgrade` transport | Domain ✓; source ✓; renderer ✓; native ✓; controlled traffic ✓ both; managed traffic ✓ both; full kind rerun pending |
| VLESS / gRPC / ordinary TLS | UUID, case-preserved service name, SNI, verify mode | URI `type=grpc`; Xray `grpcSettings`; sing-box `transport.type=grpc` | `grpc-opts.grpc-service-name` | Lite gRPC transport | Domain ✓; source ✓; renderer ✓; native ✓; controlled traffic ✓ both; managed traffic ✓ both; full kind rerun pending |
| VLESS / WebSocket / plain or ordinary TLS | UUID, path, Host, SNI, verify mode | Existing URI/Xray/sing-box WS forms | Existing `ws-opts` | Existing WS transport | Previously qualified C1/C2; C3 Host override managed traffic ✓ both; full kind rerun pending |
| VLESS / XHTTP / ordinary TLS | UUID, path, Host, explicit mode, SNI, verify mode | URI `type=xhttp`; Xray `xhttpSettings` | `network: xhttp`, `xhttp-opts` | Build unavailable: no XHTTP transport union | Domain ✓; source ✓; renderer ✓; native ✓; controlled traffic ✓ Mihomo three modes; managed pending |

The table does not equate source admission with health evidence. The shared
capability gate rejects unsupported engine/profile pairs before probing or
publication, so they never become negative endpoint-health observations.
Execution-only address pinning retains the original SNI, HTTP Host, gRPC service
and Reality name. The existing ef3 encoding carries mode in the unused transport
service slot for XHTTP; prior ef1/ef2/ef3 bytes remain unchanged. Short-ID
changes rotate the private revision, and mode/Host/path/flow/key/fingerprint
changes change the logical or private connection identity.

## Subscription and identity implications

M8 already admits URI lines, Base64 envelopes, JSON URI arrays, bounded Xray
client configurations and sing-box outbound arrays. VLESS/Trojan URIs, VMess
Base64 JSON and Shadowsocks SIP002/Xray records cover only the current subset.
C2 admits bounded SOCKS5 and HTTP(S) variants. C3 preserves Reality, Vision,
HTTPUpgrade, gRPC, HTTP/2 and XHTTP share parameters for the selected VLESS
subset; C4 must preserve Hysteria2 URI auth, obfs, QUIC and port behavior. Existing Xray
profiles' routing, DNS and inbounds never enter the control plane. The
[v3 identity decision](../decisions/0021-version-three-connection-semantics-and-capabilities.md)
keeps the original `ef1_`/`ef2_` bytes and protected observations intact.

## Gates for later slices

- **C2:** Complete for the bounded SOCKS5 and HTTP(S) proxy subset above.
- **C3:** The bounded VLESS options, sing-box `with_utls` build revision and
  engine-specific negative capability cases are recorded above. Managed traffic
  remains the final qualification gate until the kind scenario passes. XHTTP
  remains Mihomo-specific. No fallback to another transport/security method.
- **C4:** Review a sing-box build revision enabling `with_quic` before claiming
  Hysteria2. Extend the typed QUIC options and UDP destination authorization for
  every dial/port-hop/realm address. Native check, controlled QUIC through-engine
  probe and managed traffic are required. No custom QUIC client in EgressFox.

Each new combination must pass subscription → model → identity/dedup → exact
capability gate → renderer → native validation → through-engine probe → managed
Gateway traffic. Engine source support alone never changes the EgressFox status.
