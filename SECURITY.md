# Security policy

EgressFox is preparing its first development runtime release. It has no production
certification, security guarantee, or response-time SLA.

## Supported versions

No public release exists yet. After publication, only the latest development
prerelease (`dev`, `alpha` or `beta`) will receive security fixes; older prereleases
and arbitrary source snapshots are
unsupported. [GitHub Releases](https://github.com/egressfox-io/egressfox/releases)
will be the authority for the currently supported version.

| Version | Security support |
| --- | --- |
| Latest published development prerelease | Supported once a release exists |
| Earlier prereleases | Unsupported |
| Untagged source snapshots | Unsupported |

## Reporting a vulnerability

Do not include subscription URLs, proxy URIs, credentials, private keys, Secret
contents, generated configurations, confidential revisions, or sensitive exploit
details in a public issue, discussion, or pull request.

Use **Report a vulnerability** on the repository's
[Security page](https://github.com/egressfox-io/egressfox/security/advisories/new)
when GitHub private vulnerability reporting is enabled. Include:

- the affected version, commit, or image digest;
- the affected component and security impact;
- sanitized reproduction steps and required exploit conditions;
- a suggested mitigation, if known.

Enabling private vulnerability reporting and maintainer notifications is a required
manual repository setting before the first public runtime release. If the private
reporting button is unavailable, open only a minimal public issue requesting a
confidential channel. Do not include the vulnerability details. No email address or
unconfigured service is implied.

Maintainers will coordinate disclosure through the private advisory when practical,
but do not promise a fixed acknowledgment or remediation time.

## Bugs and hardening proposals

Use the public bug-report form for non-sensitive defects and a normal proposal for
non-sensitive hardening work. If disclosure could help an attacker or expose private
data, use the private process above instead. See [SUPPORT.md](SUPPORT.md) for routing.

The [threat model](docs/security/threat-model.md) documents implemented controls and
residual risks. The [release guide](docs/operations/releasing.md) covers SBOMs,
vulnerability review, signatures, provenance, and maintainer setup.
