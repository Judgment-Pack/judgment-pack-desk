# Security policy

## Support status

Desk is pre-1.0 local software. It provides no security, compatibility, or support
service-level guarantee.

## Reporting a vulnerability

Use [private vulnerability reporting](https://github.com/Judgment-Pack/judgment-pack-desk/security/advisories/new)
(Security → Report a vulnerability). If it is unavailable, open a minimal,
non-sensitive issue asking a maintainer to establish a private channel.

Do not report publicly a vulnerability that could:

- bypass the launch-secret exchange, session bearer, or browser-origin checks;
- disclose launch secrets, session bearers, model credentials, or the gateway's
  stored account tokens and connection custody;
- escape the Codex subprocess restrictions or enable tools excluded by its
  profile;
- expose storage write operations such as `files-commit` to an assistant without
  the manual review and confirmation required by Desk; or
- bypass the connection relay's operation allowlist, request limits, or configured
  gateway boundary, or read and write outside the file API's permitted locations.

Include a minimal synthetic reproduction, the affected component and commit,
expected and actual behaviour, likely impact, and any suggested mitigation.
Do not include real credentials, account tokens, customer evidence, or private
operational records. Use synthetic fixtures only.
