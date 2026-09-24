# Subscription compatibility matrix

Status: M8 compatibility extension complete. This is the bounded input and
engine contract for the pinned Mihomo v1.19.31 and sing-box v1.14.1 profiles.
An accepted record still requires native validation and successful Gateway
activation; parsing alone never proves reachability.

## HTTP request profiles

| Mode | Sent client attributes | Customization |
| --- | --- | --- |
| Default (omitted profile) | `User-Agent: Happ/1.0`, a stable source-specific `x-hwid`, `x-device-os: iOS`, `x-ver-os: 18.3`, `x-device-model: iPhone 14 Pro Max` | Each value may be overridden; up to 16 additional static non-credential headers |
| Clean | No client-profile headers, including Go's implicit User-Agent | Overrides and additional headers are rejected |

The default is one documented compatibility preset, not a promise that a provider
accepts it. The HWID is a deterministic UUID-shaped value from a durable logical
subscription identity, never from URL, credentials, headers, format, validators or
cache content. Existing sources use `legacy:<Pool UID>/<source ID>` and keep their
previous exact HWID. Set `http.clientIdentity` to that legacy reference before a
source rename or Pool move, or use a unique `stable:<opaque>` reference for new
portable subscriptions. Changing the reference is an explicit automatic-HWID
reset. An explicit profile `hwid` takes precedence; removing it restores the
unchanged automatic value. Clean mode sends no HWID without resetting its
assignment. Deleting and recreating a source without a retained reference may
create a new identity; cache deletion/expiry never supplies identity recovery.
Static Authorization, Cookie, proxy credential and transport-controlled headers
are rejected; `authorizationSecretRef` is the credential path. Header names and values
are bounded and control characters are rejected. The effective headers, URL,
Authorization, format and admission settings form the private cache identity.
Changing them invalidates cached body and ETag/Last-Modified validators.
Explicit `secretHeaders` can carry other credential-bearing headers in either
mode; Clean suppresses only the client-identification profile. Secret header
rotation also invalidates the cache and validators.
HWID identity and HTTP cache identity are separate: a URL, credential, profile or
format change keeps HWID stable while invalidating incompatible cached response
bytes and conditional validators. A failed new refresh leaves the existing
published Gateway generation in place.

```yaml
# In a ProxyPool spec.sources entry; the URL itself is a Secret key.
- id: mobile
  format: Auto
  http:
    urlSecretRef: {name: subscription-url, key: url}
    profile:
      userAgent: "ProviderClient/2.0"
      hwid: "A1B2C3D4-E5F6-4789-ABCD-1234567890AB"
      deviceOS: "Android"
      osVersion: "15"
      deviceModel: "Pixel"
      headers: {X-Provider-Variant: mobile}
- id: anonymous
  format: JSON
  http:
    urlSecretRef: {name: other-subscription-url, key: url}
    profile: {mode: Clean}
```

## Payload and endpoint support

| Layer | Accepted subset | Explicitly unsupported |
| --- | --- | --- |
| Envelope | Plain URI lines; one standard/Base64-unpadded URI-list envelope; JSON array of URI strings; JSON object with exactly one `nodes` or `proxies` URI array; complete Xray/V2Ray or sing-box configuration object with `outbounds`; array of complete Xray/V2Ray profiles; array of supported sing-box outbound records | Recursive Base64, arbitrary JSON search, Mihomo YAML, generic native engine config import |
| Xray config | Extract proxy `outbounds` only; expand bounded `settings.vnext[].users[]`, `settings.servers[]`, and shorthand single settings; deduplicate across profiles | Imported routing, DNS, inbounds, observatory, metadata; service outbounds (freedom/direct, blackhole/block, DNS, loopback, balancer) are skipped; unknown connection fields are unsupported |
| sing-box JSON | Extract bounded typed `outbounds` from a client config or typed outbound array; map only supported external proxies | Service outbounds (direct/block/selector/urltest/DNS) are skipped; native routing, listeners, unknown or advanced outbound fields are never imported |
| VLESS | UUID, no flow, TCP or WS path/Host, none or ordinary TLS with SNI and verification flag | Reality, Vision, custom encryption, ALPN, fingerprint, gRPC, XHTTP and other transports |
| Trojan | Password, TCP or WS path/Host, ordinary TLS with SNI and verification flag | TLS-less, Reality, ALPN, fingerprint, other transports |
| VMess | Base64 JSON share or supported Xray outbound, UUID, alter ID zero, `auto`/`aes-128-gcm`/`chacha20-poly1305`/`none`, TCP or WS path/Host, optional ordinary TLS | Legacy alter IDs, non-UUID IDs, fingerprint, ALPN, other transports |
| Shadowsocks | SIP002 plain/percent-encoded or Base64 userinfo, legacy Base64 whole URI, and supported Xray outbound; `aes-128-gcm`, `aes-256-gcm`, `chacha20-ietf-poly1305`, TCP | Plugins, 2022 methods, stream ciphers, extra transport/security |
| Engines | Both pinned Mihomo and sing-box render the supported subset | Other engine versions and variants without exact-profile validation |

Explicit `URIList`, `Base64URIList`, `JSON` and `Auto` formats are available for
existing Secret and HTTP sources. Auto uses JSON when the body starts with `{` or
`[`; otherwise a recognizable URI list, then one Base64 URI-list envelope.
Content-Type is advisory. JSON errors and malformed recognized JSON fail without
falling back to URI fragment parsing. Format fallback acts only on already acquired
bytes; it never multiplies HTTP requests. Existing `allowPartial: false` remains
the default: any unsupported record rejects the entire new snapshot and preserves
the previous compatible HTTP cache and artifact LKG.

```text
vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=tls
trojan://synthetic-password@edge.example.com:443?security=tls
```

Those lines are a `URIList`; Base64 encoding the entire text produces a
`Base64URIList`. A JSON URI subscription can be
`["vless://11111111-1111-4111-8111-111111111111@edge.example.com:443?security=tls"]`.
All examples are synthetic. Subscription URLs and Authorization values belong in
Secrets, never in this document or status.

## Evidence and deferred work

The selected share formats follow upstream
[v2rayN subscription guidance](https://github.com/2dust/v2rayN/wiki/Description-of-subscription),
[Shadowsocks SIP002](https://github.com/shadowsocks/shadowsocks-org/wiki/SIP002-URI-Scheme),
[Mihomo provider examples](https://wiki.metacubex.one/en/config/proxy-providers/content/),
and [Xray outbound](https://xtls.github.io/en/config/outbound.html) and
[transport](https://xtls.github.io/en/config/transport.html) documentation.
The protocol output fields are checked against [Mihomo transport](https://wiki.metacubex.one/en/config/proxies/transport/)
and [sing-box outbound](https://sing-box.sagernet.org/configuration/outbound/)
documentation for the pinned profiles. The local native/traffic qualification
results belong in [execution plan 0018](../plans/0018-subscription-compatibility.md).

Reality remains mandatory before v1.0. Its exact public key, short ID, server
name, fingerprint, flow and transport combination must enter a typed endpoint
model, both renderers, probe validation and controlled traffic tests together.
Mihomo YAML import and richer provider metadata remain separate future work.
