# Security

EgressFox is preparing its first alpha runtime release. Alpha releases receive
security fixes on the latest published prerelease only; older prereleases and source
snapshots are unsupported. There are no production security guarantees or response-
time SLA.
The [threat model](docs/security/threat-model.md) records requirements and open risks.

## Reporting

Do not put subscription URLs, credentials, private keys, generated configurations,
or sensitive exploit details in public issues, discussions, or pull requests.

Use **Report a vulnerability** on the repository's
[Security page](https://github.com/egressfox-io/egressfox/security/advisories/new)
when GitHub private vulnerability reporting is enabled. Enabling that repository
setting and maintainer notifications is a required manual action before the first
public runtime release; source code cannot prove it is enabled. If the button is not
available, open only a minimal public issue requesting a confidential channel. Do
not include exploit details or sensitive data. No email address or unconfigured
service is implied.

Include the affected version/image digest, component, impact, sanitized reproduction,
conditions needed to exploit it, and any suggested mitigation. Maintainers will
coordinate through the private advisory and disclose after a remedy is available
when practical, but promise no fixed acknowledgment or remediation time. For
non-sensitive hardening proposals, a normal issue or design pull request is appropriate.

The [release guide](docs/operations/releasing.md) describes artifact verification,
SBOMs, vulnerability review and the exact external setup maintainers must complete.
