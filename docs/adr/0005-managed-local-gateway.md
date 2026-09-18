# ADR-0005: Desk-managed local document processing

Status: accepted for personal, local Desk installations.

## Decision

A complete Desk bundle contains `jpack-desk`, `gateway`, `adapter-document`,
`gateway-bundle.json`, and the upstream licenses. `scripts/build-bundle.py`
builds the companions from the exact reviewed gateway revision recorded in
`desk.GatewayRevision`, regardless of a supplied checkout's branch or uncommitted
changes. Desk never downloads executables at runtime or searches a project/PATH
for an automatic gateway. The manifest pins the source revision and the SHA-256
of each companion. It detects incomplete/mixed installations; it is not a
signature from an independent release authority.

With no configured `research.gateway`, the first authorized configuration read
or document request starts local processing. Desk creates one private Ed25519
identity under its existing, validated credential custody root. The seed is
0600, initialization is serialized across processes, and the public identity is
recorded separately. Missing, corrupt, or changed established identities are
refused instead of silently rotated. No private key reaches the browser or a
project file. Readiness checks compare the serving gateway with that locally
derived public pin; `/publickey` never establishes trust by itself.

An explicitly configured gateway always wins, including an unreachable one.
There is no fallback from an invalid configuration or failed external gateway.
Admin → Connections offers Local (automatic) or Existing gateway. Existing URLs,
identities, source mappings, disabled PDF settings, and unrelated configuration
are retained. Switching identity can make old documents unverifiable until the
previous settings are restored. Local processing supplies only the `documents`
source; it does not pretend to install web research, Drive, or OCR providers.

The local URL/public pin are process facts in `GET /api/desk-config`, separate
from the file bytes and their revision. The browser layers them into effective
settings and never writes the transient URL back. Missing document preferences
use the bundled defaults; explicit `documents: null` or `enabled: false` disables
new uploads. Saved document verification still uses the pinned identity.

## Lifecycle and storage

Each Desk process owns a companion worker. The worker monitors an inherited
stdin pipe; normal exit and a parent crash both close it and shut down the owned
gateway. It never signals a PID discovered from disk. A crashed companion is
restarted on the next request; failures have a short retry cooldown. Multiple
windows of one Desk share its worker; separate Desk processes use separate ports
and append-only receipt registries, avoiding concurrent writers to one registry.

The gateway CLI currently takes a nonzero port and no inherited listener. Desk
chooses a candidate loopback port, requires its own child's bound-address
announcement, checks the pre-pinned identity, and retries bind collisions.
Arguments are passed without a shell. The document adapter has a fixed basename
on a dedicated PATH so installation paths containing spaces work.

The signing key is `secrets/local-gateway.seed` in the personal Desk settings
directory; its public identity is `local-gateway-identity.json`. Gateway receipt
stores and registries are retained in private `local-gateway/run-*` directories
there. They are not project files or part of a chat-storage migration/backup.
Original uploads and independently verifiable document proofs remain in chat
storage. A chat backup does not contain the signing seed: keep the existing
protected settings directory when restoring chats on the same machine. No
automatic key rotation, receipt deletion, or shared organization storage is
introduced here. Retention controls for gateway receipt archives remain a
separate storage feature.

This follows Desk's existing Unix custody and locking support. Windows builds
continue to refuse private credential storage where ownership cannot be checked;
this change does not claim to add Windows credential custody. An organization
can supply its own gateway, but this personal setting is not an enterprise
policy-enforcement mechanism. Authenticated organization gateways and Drive
consent remain separate capabilities.

## Validation

Unit tests cover concurrent/stable identity creation, loss/corruption/rotation,
private permissions, bundle mismatch, pre-pinned readiness and redirects, refused
configuration and external precedence. Browser tests cover defaults, disabled
processing, transient-setting isolation, and unchanged external sources.
`scripts/local-gateway-check.py` runs a real bundle against isolated synthetic
settings: signed PDF extraction, concurrent Desk processes, graceful and forced
parent exit, stable restart identity, disabled uploads and unchanged external
configuration. CI runs it on Linux and macOS with an installation path containing
spaces. No model call or user document is involved.
