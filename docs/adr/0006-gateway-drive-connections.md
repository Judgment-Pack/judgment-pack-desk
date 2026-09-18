# ADR 0006: Drive connections remain in gateway companions

Status: proposed; implementation awaits gateway material review and a live Google
client registration/sign-in test.

This implements the personal OAuth/selected-file portion proposed in ADR 0004;
shared organization identity and policy remain future work.

Desk ships the gateway's `gateway-connections` and `adapter-drive` executables.
Provider credentials, consent, refresh/revocation and retrieval live in those
programs, outside the signer module. The Desk chassis relays a fixed list of
control operations over a private parent pipe after its existing session and
origin checks. It never handles provider API calls or persists provider tokens.
The Admin file input supplies operator-owned Desktop app registration JSON once;
the gateway retains it privately, outside project files and chat backups.

The local gateway binds `drive` to an HTTP-shaped source with fixed operator
arguments. File selection uses Google's native browser picker, with selected-file
scope and PKCE, so the Desk browser receives no Google access token. A selected
file carries a short-lived single-use grant; the gateway checks it and its
connection before retrieving. The resulting original, source/version and extraction
record are independently verified by Desk under the existing signer pin before
being offered as model context. Existing PDF previews, partial-text review,
citations, attachment limits and late-result cancellation are reused.

This first deployment is personal and local. The installed companion fixes the
principal to this OS-account installation; project files and requests cannot set
it. An explicitly configured external gateway takes precedence and does not
silently activate local Drive connections. A shared deployment needs a real
identity issuer, caller mapping and policy enforcement before the same control
contract is exposed remotely. No organization-management UI is enabled here.

New Drive documents currently require document processing enabled. The Drive
source itself is limited to 4 MiB originals (including Google document PDF
exports), and the existing configured Desk upload limit may be lower. The response
allowance is 16 MiB; a result exceeding it fails rather than truncates. The files
and verified proof are retained in the existing private attachment store; removing
an attachment or disconnecting Drive does not delete prior exports.

Validation covers OAuth state/PKCE, account mismatch, cancellation, grant replay,
expiry/principal/file/connection substitution, revocation, content bounds and
version changes, private custody, and independent consumer verification. A Google
Cloud registration and a user's consent are external prerequisites for the live
sign-in test; fake-provider tests do not establish successful production consent.
