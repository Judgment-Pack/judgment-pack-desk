# JPS Desk model-provider authentication: feasibility assessment

Date: 2026-09-25. Assessment only; no application implementation has been made.

Update: the selected direction is now a direct Codex adapter in Desk’s existing
engine slot. OpenHands is not required. The [ChatGPT subscription implementation
plan](../design/chatgpt-subscription-plan.md) supersedes the proposed OpenHands
implementation and outstanding checkout question below; the original architecture
findings remain useful.

## Decision

Subscription-backed agents are technically feasible, but the attached request
assumes an OpenHands integration that is absent from the inspected JPS Desk.
It cannot be implemented here by exposing existing OpenHands settings. It needs
a new optional backend agent integration, an enforced MCP bridge, and engine
conformance work before login buttons would represent a working feature.

The outstanding scope question is whether to extend this checkout with that
backend or use another checkout that already integrates OpenHands.

## Checkout and actual architecture

- Active workspace: `judgment-pack-runtime`, the independent Go runtime.
- Desk checkout inspected: `/home/onword/repo/judgment-pack/judgment-pack-desk`.
- Desk HEAD: `9c9e92d59ba2c572acb049b6fa3961f7daea058b`, plus substantial existing
  working-tree changes. Those changes were included in the inspection and preserved.
- No OpenHands, Agent Canvas, or ACP dependency or implementation was found in
  Desk. OpenHands and the Python ACP package are also absent from the current
  Python environment. There is therefore no installed Desk OpenHands version
  whose registry or secret store can be reused directly.
- `web/package.json`: Vercel AI SDK `ai@7.0.93` with OpenAI-compatible,
  Anthropic, and Google model clients.
- `web/src/assistant/engines/index.ts`: the only registered engine is `vercel`.
- `internal/desk/assistant.go` and `modelrelay.go`: machine-held API-key custody,
  endpoint probes, and a relay that injects the configured key.
- `internal/desk/custody.go`: pinned secret directories, ownership checks,
  directory mode 0700, file mode 0600, and protected file operations.
- `internal/desk/deskfile.go` and `web/src/config/deskConfig.ts`: shared strict
  configuration contract, exercised using the same JSON fixtures. Neither has
  model-provider subscription authentication configuration.
- `web/src/assistant/toolGate.ts`: browser MCP transport enforces the tool
  allow-list and rewrites evaluation to rehearsal mode.
- `internal/desk/relay.go` and `project_{linux,other}.go`: each browser MCP session
  starts `jpack mcp`; it is not an OpenHands Agent Server connection.
- `scripts/build-bundle.py`: builds Desk and local gateway/runner companions,
  with no OpenHands/ACP distribution or Agent Server Docker credential wiring.
- Desk sign-in under `internal/desk/auth_*.go` is application identity, not model
  authentication. Its OIDC flow must not be repurposed for provider subscriptions.

The existing assistant proposes changes for user acceptance. Giving an external
coding agent raw project access or an ungated runtime connection changes that
behavior. The request itself requires preserving it.

## Upstream verification

Public upstream source was inspected separately from Desk. The SDK source
advertises version `1.49.6` and requires Python >=3.12. Observed upstream main
heads were SDK `fcc102a697874d54a357e36004e02c95040dbdc0` and OpenHands
`7dc6805406ea3c76cb4a3ce407c3c72d481b0ac6`. These are assessment references, not
dependencies installed or certified for Desk.

[OpenHands ACP documentation](https://github.com/OpenHands/OpenHands/blob/main/docs/ACP_AGENTS.md)
describes the Codex, Claude Code, and Gemini CLI adapters; existing local CLI
logins; `CODEX_AUTH_JSON`; `CLAUDE_CODE_OAUTH_TOKEN`; and the Vertex credential
deployment path. It also describes forwarding configured MCP servers. These
mechanisms are reusable in a new backend, but are not already present in Desk.

The inspected [SDK ACP implementation](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-sdk/openhands/sdk/agent/acp_agent.py)
forwards MCP configuration to the subprocess, materializes file secrets with
restricted permissions, and owns authentication selection. It also gives the
external agent control over tools, refuses custom OpenHands tools, and its
permission callback auto-approves requests. Its credential selection can fall
back to available API credentials. Those defaults do not establish Desk's
propose-only contract or the requested strict authentication-mode separation.

The [provider registry](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-sdk/openhands/sdk/settings/acp_providers.py)
and its install catalog should supply adapter identifiers and pinned launch
definitions. Recreating their OAuth implementations in Go or React is unnecessary.

## Authentication feasibility

| Provider | Supported direction | Qualification for Desk |
| --- | --- | --- |
| OpenAI / ChatGPT | Official Codex login, including browser and device-code login; Codex ACP for execution | Requires an agent backend. Login status must be checked through the official client, with expired/revoked state handled separately from cached-login presence. |
| Anthropic / Claude | User's own unmodified Claude Code runtime and official sign-in, subject to Anthropic's hosting conditions | Do not add a Desk-owned Claude.ai OAuth client or assume that collecting subscription tokens is permitted merely because OpenHands supports a token environment variable. |
| Google / Gemini | Gemini CLI Google account login locally; API key and Vertex authentication where supported | Google account login is not evidence of a paid plan. Disable unavailable remote account-login methods. |
| Custom / local | Existing compatible model endpoint path | Preserve current configuration and key custody; do not assign subscription semantics to it. |

[OpenAI authentication documentation](https://developers.openai.com/codex/auth/)
documents `codex login`, `codex login --device-auth`, credential storage, and
headless options. The supported cache may be an OS credential store instead of
`auth.json`; file existence alone is not sufficient status verification.

[Claude Code authentication](https://code.claude.com/docs/en/authentication)
documents the official client's login and credential handling.
[Anthropic's authentication and hosting conditions](https://code.claude.com/docs/en/legal-and-compliance)
distinguish users signing into an unmodified hosted Claude Code binary from
third-party applications offering their own Claude.ai login or intermediating
subscription credentials. The concrete adapter/hosting arrangement needs to
meet those conditions before advertising subscription support.

[Gemini CLI authentication documentation](https://geminicli.com/docs/get-started/authentication/)
supports local Google sign-in, Gemini API keys, and Vertex credentials. Headless
use can reuse existing authentication; without it, the documented alternatives
are API keys or Vertex. Do not invent an OAuth-cache import mechanism.

## Minimum implementation required in this checkout

1. Add an optional, explicitly pinned OpenHands/ACP backend and a Desk engine
   adapter. Keep the existing Vercel/API-key path compatible. Define backend
   startup, shutdown, cancellation, deployment capabilities, and packaging.
2. Keep provider credentials in the provider/backend boundary. Reuse official
   login, refresh, and logout implementations and upstream secret materialization
   where applicable. Expose only sanitized status and supported login challenges.
   Do not return arbitrary CLI output. Distinguish cached login, verified access,
   expired credentials, unavailable backend, and unauthenticated state.
3. Enforce the selected auth method before spawning an agent. A subscription
   session must not inherit API keys, cloud-identity overrides, custom proxy
   URLs, or provider settings that silently change billing. API mode stays on the
   existing API engine. Bind credentials and login attempts to the backend owner.
4. Expose JPS tools through a per-run mediated MCP bridge. Preserve the existing
   tool definitions, allow-list, rehearsal rewrite, host research tools,
   cancellation, proposal parsing, critique, and user acceptance. Also constrain
   agents' built-in shell/filesystem tools; MCP gating alone does not constrain
   those tools. Do not mount writable projects or credentials into agent-accessible
   workspaces. Prove the supported provider sandbox arrangements before enabling
   each adapter.
5. Add a capability-driven provider/auth catalog, sanitized status/login/logout
   routes guarded by Desk sessions, matching Go/TypeScript configuration changes,
   provider-first setup, and visible active agent/authentication. Unsupported
   methods should report unavailability instead of activating another mode.
6. Add negative security tests, scripted ACP contract tests, and authenticated
   provider smoke tests. Only admit an engine to the registry after conformance
   coverage exists for it. Keep missing live credentials explicitly reported as
   untested, not as a successful smoke run.

Expected existing integration points are `internal/desk/server.go`,
`assistant.go`, `deskfile.go`, process/session lifecycle handling,
`web/src/config/deskConfig.ts`, `assistant/engine.ts`, `assistant/engines/index.ts`,
`assistant/session.ts`, `assistant/EndpointForm.tsx`, shared configuration fixtures,
engine conformance tests, localization, and distribution scripts. New provider
catalog/auth/backend modules should contain provider-specific logic rather than
embedding it in React components.

## Security details that affect implementation

The existing runtime subprocess constructors leave `cmd.Env` unset, which
inherits the parent environment. Supplying subscription secrets to the Desk
process without changing child-process environment isolation would also make
them available to `jpack`. New credential injection must account for this on
every supported platform and for MCP processes started by the agent backend.

Credentials must remain outside projects, exports, backups, prompts, model-visible
tools, browser storage, telemetry, and diagnostic output. Provider status must
not serialize SDK settings or secret registries. File secrets need writable,
owner-only storage for refresh and protection against symlink replacement and
stale concurrent writes. Logout must cancel pending login and agent work before
removing access; local disconnection and provider-wide revocation are distinct.

## Validation performed

- Targeted existing browser suite: **4 files, 340 tests passed** (Node 22.23.1).
  Files: `toolGate.test.ts`, `enforcement.test.ts`,
  `conformance/conformance.test.ts`, and `endpointForm.test.tsx`.
- Existing Go suite: **`go test ./...` passed** for the main and `internal/desk`
  packages. This is baseline validation, not provider authentication validation.
- No live provider login, credential read/import, authenticated ACP run, or
  per-provider JPS MCP smoke test was performed. The requested backend does not
  exist in this checkout. Codex is on PATH; Claude and Gemini are not.
- No application files, dependencies, model configuration, or existing user
  credentials have been changed. This assessment is the only authored artifact.

For implementation validation, test each provider with no login, valid login,
expired/revoked login, successful refresh, failed refresh, login cancellation,
and logout. Exercise both authentication modes with conflicting credentials
present, then prove which mode was used. For each enabled ACP provider, discover
and call a real allowed JPS MCP tool and prove that disallowed calls and writes
are blocked. Test container injection independently from local cached-login
reuse. Run the full Go, TypeScript, localization, build, and browser suites after
the adapter is implemented.
