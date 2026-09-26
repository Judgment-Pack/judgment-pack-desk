# ADR 0007: Desk consumes identity; identity infrastructure stays external

Status: local single-owner OIDC consumer implemented in the working tree.
Hosted deployment and multi-user authorization remain proposed and out of scope.

Date: 2026-09-23.

## Context

Local Desk binds loopback. By default, initial setup requires a code printed by
its process. `JPACK_DESK_LOCAL_ACCESS=1` opts a personal installation into
automatic local sessions until OIDC is activated; it never overrides an existing
or unreadable sign-in policy.
Admin tests an OIDC registration and explicitly activates a verified account as
the installation owner. Thereafter every protected request requires that owner's
session and launch-secret access is disabled. The legacy `identity.provider`
configuration remains display-only; the enforced policy is a separate protected
installation record. Sign-out, idle/absolute expiry and policy changes cancel
session work. No provider tokens are retained.

The gateway separately verifies caller JWTs against an issuer, audience and a
local key file. It loads keys at startup; it does not discover or rotate them on
requests. Desk's current research relay carries no caller token. Local connection
companions fix their principal to the OS-account installation. None of those
features establish multi-user connection isolation or a hosted sign-in flow.

The existing [Protoss ownership boundary][boundary] places identity consumption
in the open Desk and operated identity infrastructure outside it. Its remote
deployment description is a target, not current functionality. The
[registration ownership decision][registrations] excludes publisher Google
registrations from public source and releases.

## Decision and remaining hosted design

### One identity interface, explicit deployment boundaries

Keep a nullable configured provider, not vendor-specific authentication engines
or a commercial/OSS code fork. A customer issuer and a Protoss-operated issuer
use the same public contract. Provider presets are configuration and instructions.

- **Local, no provider:** default to terminal-code owner setup. The operator may
  opt into `JPACK_DESK_LOCAL_ACCESS=1` for an OS-trusted personal installation.
  The backend then creates a bounded local session for a loopback request with
  an exact permitted Origin and same-origin Fetch Metadata. Private endpoints
  still require the bearer. The UI opens directly, without a fake verified
  identity or a mandatory cloud registration. Legacy tooling remains available
  until activation. Enabling OIDC or an unreadable policy disables this mode.
- **Provider configured:** configuration alone does not authorize anyone. After
  an explicit, tested activation, server-side identity and access checks apply
  to every protected operation; local launch must not bypass those checks.
- **Network deployment:** refuse startup without enforced identity, validated
  HTTPS/trusted-proxy configuration and the required authorization policy.
  Disable development origin exceptions and launch-secret API authorization.
  Provider outages never fall back to local or anonymous access.

Deployment mode determines access requirements, not whether code is open or
commercial. The initial delivery remains local; a remote bind is a separate
implementation milestone after backend enforcement and isolation pass review.

### Protocol and session responsibilities

Start with OIDC authorization code + S256 PKCE using a maintained protocol
library, with state, nonce, issuer, signature, audience, expiry and redirect
validation. No implicit grant, password grant, custom password database, or
embedded provider login. Pin trusted issuers through operator configuration;
user input must not become arbitrary discovery or token endpoint URLs.

Use one issuer per configured security context. A hosted identity broker can
offer Google, Microsoft and company SSO upstream. It handles SAML where needed
and presents OIDC to Desk. Selecting a cloud host does not select an identity
provider. AWS Cognito application login and IAM Identity Center workforce
federation are distinct integrations.

Local/installed and hosted/web clients have different registrations and callback
requirements. A public installed client has no embedded confidential secret.
Hosted confidential credentials are deployment-owned, supplied through protected
server custody. Never add secrets to project files, normal Desk config, release
artifacts, browser storage, chat backups or logs. Local desktop registration secrets, when required by the provider, live in an
owner-only private sign-in file. They do not authenticate a public client's
identity and never ship in releases. The ordinary identity schema still refuses
credentials. Hosted confidential-client custody remains a separate design.

Preserve the local bearer transport pending its own threat-model review: cookies
are not isolated by localhost port. For hosted HTTPS, prefer an opaque server
session with a host-only Secure/HttpOnly cookie, appropriate SameSite behavior,
CSRF protection and WebSocket origin checks. OAuth callback correlation must
work across the provider redirect; do not blindly copy local Strict cookies.
Keep provider tokens out of browser JavaScript. Bind all callback attempts to
the initiating session and expected issuer, expire them, and reject replay.

Provider-backed and automatic local sessions have 30-minute idle and eight-hour
absolute expiry and explicit revocation. Provider logins mint fresh credentials;
local reloads reuse a live bearer to avoid consuming session capacity. Sign-out
revokes the Desk session and closes its live
sockets; provider-wide logout is a separate supported operation. Disconnecting
Drive or Gmail is not app sign-out. Return destinations are validated internal
paths, re-authorized on return, never arbitrary URLs. Session recovery must not
render the previous user's data to a newly authenticated identity.

### Ownership, authorization and migration

Verified issuer + subject identify an external principal; email/name are display
attributes. Do not auto-link two provider accounts by matching email. Stable
local ownership must exist independently of the mocked email folder label.
Signing in does not upload, merge or rename local packs and chats. Activation
explicitly assigns access to all installation data to one verified issuer/subject.
Changing the owner requires another successful test and explicit activation by
the current owner, or a local OS-owner reset. There is no automatic email linking
or account-specific data partition in this milestone.

Identity providers establish identity; the configured authority supplies resource
permissions and workspace membership. Desk enforces those decisions server-side
without becoming an account directory, role-authoring service or identity issuer.
The policy/claim contract and administrator bootstrap need a separate reviewed
design before remote access. A user-provided workspace ID, email suffix or UI
visibility is never sufficient authorization. There is no public "first user
becomes administrator" path. Local setup requires the terminal owner code unless
the OS operator explicitly enabled automatic local access. Remote bootstrap is
controlled by deployment credentials outside the public UI.

Apply resource authorization to packs, drafts, chats, attachments, folder
assignments, configuration writes, exports, runtime tools, streams, background
jobs and connection controls. Folder hierarchy does not imply permission
inheritance unless an explicit policy defines it.

### Gateway boundary

Source integrations, registration custody and provider tokens remain in gateway
companions. App sign-in UI, callback/session handling and API guards belong to
Desk. Signer identity, authenticated caller identity and a source account are
three separate concepts.

Hosted delegation requires an explicit contract: gateway-audience access tokens,
verified caller mapping, permitted connections, key rotation and revocation
behavior. Never forward a Desk ID token or session cookie as a gateway credential.
Google sign-in does not automatically produce a gateway-audience access token;
token issuance/exchange belongs to the configured authority, not an ad hoc Desk
issuer. The existing local relay remains local until this contract is implemented.

### Repository placement and reuse

| Owner | Responsibility |
| --- | --- |
| `judgment-pack-desk` | Reusable auth views, OIDC client, local handoff, sessions, identity consumption and server authorization checks. Initially modules in this repository. |
| `judgment-pack-gateway` | Source OAuth/credentials, caller verification, connection isolation and the gateway delegation boundary. |
| Runtime / public spec | Deterministic evaluation; only public identity/provenance wire changes belong in a spec change. No account service in the evaluator. |
| `protossai/strategy` | Ownership, deployment, identity supplier and commercial operations decisions. No application code or secrets. |
| Future Protoss service repository | Operated issuer/broker, organization directory, membership/role management and provisioning when that service is approved for delivery. |

No new identity repository is needed for the mocks or Desk's OIDC consumer.
Extract shared code only when a second real consumer establishes its API. A
separate deployed service gets a separate repository when its operational owner,
data model, lifecycle and implementation scope are concrete. Prefer an established
identity product over implementing password/MFA/SAML infrastructure ourselves.

## User experience

See [authentication design and mock guide](../design/authentication.md).
Authentication entry uses a standalone page. Administrative setup uses the
existing contextual right-pane pattern, with instructions expanded below the
form. It is not a login modal and does not stack overlays.

## Sequencing and acceptance

1. Local entry and Admin setup UI are implemented. No provider registration is
   enabled on the user's installation until its owner tests and activates one.
2. Local single-owner policy, session expiry/revocation, cancellation of legacy
   work and local CLI recovery are implemented and tested.
3. OIDC reuse is exercised with independent fixture issuers and a full browser
   flow. Live-provider acceptance with operator-owned registrations remains:
   fixtures cannot establish successful real consent or organizational access.
4. Implement remote policy, isolated storage access and gateway delegation before
   supporting network deployment. Ship only capabilities actually enforced.
5. Add operated identity, enterprise federation and provisioning as separately
   scoped work. No SaaS deployment is authorized by this design.

The broader hosted security acceptance still requires code/state/nonce replay, wrong issuer and
audience, disallowed tenant, key rotation failure, expiry/revocation, open redirect,
cross-origin/CSRF/WS requests, development-launch bypass, account switching,
cross-user/workspace reads and mutations, background work after revocation,
connection/attachment isolation and controlled administrator recovery.

UX acceptance covers keyboard/focus order, visible recovery, return-to-page,
no private-data flash, mobile, dark/light themes, localization, provider cancellation
and preservation of authorized drafts across interrupted sign-in.

## Alternatives

- **Login pages only:** rejected because the existing provider is display-only.
- **Require a hosted account for local Desk:** rejected; local offline use remains.
- **Put browser login in the gateway:** rejected; source credential custody and
  app session ownership have different boundaries and lifecycles.
- **One custom engine for every provider:** rejected; OIDC and federation provide
  reuse without vendor-specific session models.
- **Create an identity microservice now:** deferred; operating accounts is a
  separate product capability, not required to implement an OIDC consumer.

## References

Reviewed 2026-09-23; upstream behavior may change. Linear observations are public
references, not access to its private design system.

- [Linear login methods](https://linear.app/docs/login-methods)
- [Linear SAML setup](https://linear.app/docs/saml-and-access-control)
- [Linear design refresh](https://linear.app/now/behind-the-latest-design-refresh)
- [OAuth security BCP](https://www.rfc-editor.org/rfc/rfc9700.html)
- [OAuth for native apps](https://www.rfc-editor.org/rfc/rfc8252.html)
- [OWASP session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [Google OIDC](https://developers.google.com/identity/openid-connect/openid-connect)
- [Entra authorization code flow](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow)
- [Entra workforce and external tenants](https://learn.microsoft.com/en-us/entra/external-id/tenant-configurations)
- [Cognito authentication](https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-how-to-authenticate.html)
- [IAM Identity Center SAML](https://docs.aws.amazon.com/singlesignon/latest/userguide/customermanagedapps-saml2-setup.html)

[boundary]: https://github.com/protossai/strategy/blob/main/oss-protoss-feature-boundary.md#identity-one-slot-two-suppliers
[registrations]: https://github.com/protossai/strategy/blob/main/google-connection-ownership.md
