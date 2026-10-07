# Security policy

## Supported versions

| Version | Supported |
| --- | --- |
| Latest minor | yes |
| Older | no |

## Reporting

- Use GitHub private vulnerability reporting: <https://github.com/alexmchughdev/terraform-provider-terradash/security/advisories/new>.
- Do not open a public issue, PR or discussion for a vulnerability.

Include:

- Affected version and component (provider or CLI).
- Impact and reproduction steps.
- Suggested fix, if any.

## Response

- Acknowledgement: best effort, target within 7 days.
- Fix and advisory timing depends on severity; reporters are kept updated.
- Coordinated disclosure; reporters are credited unless they decline.

## Scope

In scope:

- Credential handling: `auth` and `http_headers` leaking into logs, errors, state or files, or being sent to another host.
- TLS: verification, minimum version, CA bundle handling.
- Redirect handling and response size limits in the Grafana client.
- CLI file writes (overwrite, path traversal).
- Denial of service from crafted dashboard JSON or HCL.

Out of scope:

- Vulnerabilities in Grafana, Terraform, OpenTofu or other dependencies (report upstream).
- Behaviour requiring `insecure_skip_verify` or plain HTTP, both opt-in.
- Misconfiguration, e.g. overly broad service account permissions.

## Public issues

Do not include:

- Tokens, passwords, API keys, `GRAFANA_AUTH` values.
- Terraform state, plan output or dashboards containing secrets.
- Internal hostnames or URLs.

Redact before posting logs or HCL.
