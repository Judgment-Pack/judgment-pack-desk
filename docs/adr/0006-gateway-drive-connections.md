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
The advanced personal connection dialog accepts Desktop app registration JSON once;
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
silently activate local Drive connections. Pending browser operations are canceled
when their gateway or document settings change. Drive requests also carry a
relay-only local-document constraint; the server checks its resolved configuration
before dispatch and never forwards that marker upstream. Cancellation of an
already-owned local flow remains available after switching gateways, without
starting a new local companion. A shared deployment needs a real
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

## Personal connection entry points (superseded)

The historical interaction and distribution decisions in this section are
superseded by the 2026-09-21 update below.

The composer **+ → Google Drive / Gmail** owns first-use authorization. Configured
users review access, continue to Google, and return to content selection in the
same chat. Existing Drive connections open the Google picker directly; Gmail
opens its email selector. Canceling, switching chats, or changing document/gateway
configuration abandons pending UI operations and prevents late imports.

The account menu's **My connections** shares the consent and disconnect dialog.
Admin no longer has a Connections section: local PDF/gateway processing belongs
under Storage & data. Legacy Admin connection/document fragments resolve there.
No enterprise connection-policy controls are exposed.

The default publisher-owned Desktop OAuth registration remains a release
prerequisite, not a value the UI can create through user consent. An unregistered
build says so and offers an advanced **Use your own Google app** import; it does
not offer a nonfunctional Continue button. Registration import stays in the
current dialog. Starting OAuth requires a fresh user gesture so popup blockers do
not interfere. Real Google sign-in still needs external registration and consent;
synthetic-provider UI tests cannot establish it.

This follows the documented first-use authentication pattern in
[ChatGPT plugins](https://learn.chatgpt.com/docs/plugins), while keeping
[workspace controls](https://learn.chatgpt.com/docs/enterprise/apps-and-connectors)
separate from personal authorization. Exact ChatGPT menu labels and placement
vary by surface; the composer entry point is based on the supplied reference UI.

## 2026-09-21: personal setup and a shared connection pane

GitHub releases and source builds include no publisher Google registration.
Self-hosted users configure their own Google Desktop app in Admin → Connections
or inline when selecting Google Drive/Gmail in the chat connection catalog.
Saving app registration never starts account consent. A future hosted service
would need an organization-owned registration and a separate deployment decision;
no such registration is bundled into this personal Desk release.

Composer **+ → More connections** and the user menu's **My connections** share a
searchable catalog and contextual right pane. Connected providers are shortcuts
in the composer menu. The same pane handles setup instructions, registration,
explicit browser consent, source selection and connection management. It does
not stack provider dialogs. The current Google account and native picker remain
provider-owned; file/email retrieval and credentials remain gateway-owned.

Opening a connection preserves the chat. Closing restores the prior Assistant
presentation; attaching returns focus to the composer without sending. PDF/local
gateway processing remains under Storage & data. Admin → Connections remains
available for application registration. The detailed interaction and remaining
provider scope are in [the shared connection design](../design/connections.md).
