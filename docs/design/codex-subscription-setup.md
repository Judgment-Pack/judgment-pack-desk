# ChatGPT subscription setup in Desk

Implemented locally on 2026-09-25. The managed Codex path now includes configuration,
account setup, model discovery and all assistant consumers. Local protocol,
ToolGate, lifecycle and UI tests pass; real account login and paid inference are
still untested. This is an experimental local integration, not a release claim
for an authenticated subscription or additional operating systems.

## Starting and connecting

1. Start Desk normally. On supported Linux x86-64 computers, no separate Codex
   installation, executable path or feature flag is required. Desk manages its
   compatible runtime; an API-only Desk does not acquire the subscription
   profile or launch Codex.
2. Sign in to Desk normally. Open **Admin > Assistant**, or **Configure
   Assistant** from chat. Choose **Access > ChatGPT subscription**.
3. Choose Browser or Device code and click **Connect ChatGPT**. On first use,
   Desk downloads and verifies its pinned official runtime, showing cancellable
   progress. It then begins sign-in in the same request, without a Desk restart.
   A failed preparation can be retried with the same button. Follow the
   returned official sign-in link; enter the code if using device sign-in.
   Return to Desk. The page polls account state without running a model.
4. Choose a model from the account's native Codex catalog. Keep **Model default**
   reasoning or select an effort that this model advertises. Review **Allowed
   pack tools** and click **Save selection**.
5. Optionally click **Test connection**. This explicitly starts a bounded model
   run and requires an actual successful `get_schema` tool result through Desk.
   It is not run automatically after login, model discovery, save or refresh.
   The check requires `get_schema` in the saved allow-list; it does not add that
   permission. A model saying “connected” alone is insufficient.

This uses a Desk-owned private Codex profile. It does not read or import the
terminal client's account. The sign-in URL/code live only in component memory,
not query caches, configuration, chat history or backups. If that page is lost,
its initiating Desk session can cancel the pending attempt and start again;
other sessions do not receive its challenge or cancel ID.

Disconnect uses the standard styled confirmation dialog. It stops native work
and signs out this Desk's account. It does not delete API credentials or claim
account-wide revocation. The backend preserves its existing durable cleanup and
profile-lease behavior.

## Managed runtime and advanced installation controls

Only the explicit **Connect ChatGPT** action may prepare a missing runtime.
Catalog/status/model reads and ordinary runs never download, install or repair
one. Catalog reads do not acquire a profile lease. Once installed, Desk reuses
its runtime and completed account across restarts. Opening settings may start
an installed runtime to read account status, but cannot trigger inference.

The first managed artifact targets Linux x86-64 and pins official Codex
`0.157.1`. Its fixed GitHub release URL, archive byte count/SHA-256 and extracted
binary byte count/SHA-256 live in `internal/codexbridge/runtime.go`. The private
cache is `<DeskConfigDir>/codex/runtime/codex-0.157.1`. There is no PATH lookup,
project-local executable discovery, package-manager command or downloaded
version manifest. No terminal login or installation is read or changed.

Downloads have a three-minute deadline, bounded size and HTTPS redirect host
allow-list. Extraction accepts exactly the nominated regular executable; it
never extracts archive paths or links. Preparation stages non-executable files,
verifies both digests, sets owner-only permissions, then publishes atomically.
Cancellation, request disconnect, Desk session/policy revocation and shutdown
stop preparation. Unfinished temporary files are removed. Cached executables
are reverified before launch; a damaged regular file is repaired only by another
explicit Connect action. Unsafe symlink/hardlink/permission states refuse.

Installation owners may still set `--codex /absolute/path/to/codex` for an
explicit trusted executable, or `--codex off` to disable subscription access.
These are advanced overrides, not normal UI steps. The native protocol version
check remains mandatory. Other operating systems/architectures are reported as
unsupported by managed installation until their runtime/process boundary is
verified. An invalid override never switches silently to a managed or API target.

## Configuration and compatibility

The desk-level `assistant` object accepts an optional credential-free `agent`:

```json
{
  "assistant": {
    "engine": "codex",
    "endpoint": null,
    "thinking": "off",
    "agent": {
      "provider": "openai",
      "authMethod": "subscription",
      "model": "an-explicitly-selected-catalog-model",
      "tools": ["get_schema", "list_examples", "get_example", "validate"],
      "effort": "high"
    }
  }
}
```

`effort` is optional: omission requests the selected model's supported default.
`model: null` is valid saved configuration, but cannot run. `tools` is mandatory
and may be empty. Unknown provider/auth combinations, native tool names,
credentials and unsupported effort values refuse the whole file. Agent settings
are prohibited in project configuration by the existing assistant ownership rule.
Shared Go/TypeScript fixtures check both accepted values and refusals.

An existing API endpoint may be retained beside an active subscription target,
and agent settings may be retained when explicitly switching back to API access.
Only the selected engine runs. The API endpoint relay/probe refuses the retained
endpoint while Codex is selected; no subscription failure falls back to API
billing. Legacy files and API saves retain their existing behavior.

The setup form renders the same shared controls in Admin and the chat dialog.
Switching the previewed access method retains unsaved fields; saving activates
that choice. Chat, pack authoring, research, test design and briefs use the same
selected-target/readiness helpers. Subscription readiness does not depend on an
API-key request. Brief generation still has no runtime tool capability.

The browser binds a model endpoint capability or an agent-run capability. It
never creates a fake endpoint for Codex. Each run captures its target, model and
effort before starting; later UI/configuration changes cannot retarget it.
Existing tool, source, proposal acceptance, cancellation and critique ownership
remain unchanged. The chat composer identifies the subscription engine, and its
settings show native effort separately from adversarial review.

## Discovery and limits

`GET /api/model-providers/openai/models` is session-guarded and returns only model
ID, display name, supported effort values and default effort. It requests native
`model/list` only after verifying ChatGPT account presence. Native provider prose,
account identifiers, service tiers and upgrade links are not forwarded.

Discovery is limited to ten pages of 100 rows, a 30-second request deadline,
bounded cursors and duplicate/cycle checks. Hidden models and models with an
unsupported implicit effort default are excluded. The bridge rechecks catalog
membership and effort support when admitting each run, and makes the default
explicit. It never selects a replacement model or permits the native `ultra`
mode. Listed availability is not proof that an authenticated inference will
succeed; subscription limits and entitlement remain provider-controlled.

The candidate's existing run deadlines, text/tool bounds and one-run concurrency
are recorded in [the run bridge](codex-run-bridge.md).

## Validation and release boundary

Tests cover shared configuration parity, inactive API refusal, subscription-only
model discovery, unsupported/duplicate/cyclic catalogs, stale model/effort
refusal, account challenge ownership, model selection/save, styled disconnect,
absence of automatic inference and tool permission preservation. Browser checks
use synthetic account responses, inspect desktop/narrow layouts and exercise
real controls; they do not sign in to an account.

The registry now contains separate model-endpoint and provider-agent engines.
The endpoint protocol matrix runs over model engines. The Codex matrix loads the
agent through that same registry and runs the recorded JPS schema/result scenario
through the real ToolGate, along with host tools, proposal/critique, no-network,
cancellation and no-fallback tests. The native and WebSocket protocol matrices
remain separate. This separation does not claim the Codex protocol implements
Anthropic/Gemini/OpenAI-compatible HTTP dialects.

Release validation still requires a user-initiated real sign-in and Desk-to-Codex
schema/validation run, proposal acceptance/rejection, cancellation, restart and
logout on each supported host. No paid inference or existing account import was performed during this implementation.

### Local verification record — 2026-09-25

- Full Go suite and `go vet ./...` passed.
- Codex bridge race tests (including the pinned native client with an empty
  private profile) and the provider/agent HTTP race checks passed.
- Full frontend run: 4,337 passed, one skipped, two stale assertions failed.
  Updated the assertions for the access selector and agent WebSocket proxy;
  the complete Admin test file then passed (85 tests), and the proxy file
  passed (six tests). No remaining failure from that full run is unresolved.
- TypeScript checking and the production build passed. The build retains the
  existing large-chunk advisory.
- All 2,813 message keys passed locale checks in each of the eleven translated
  catalogues, with zero placeholder errors.
- Chromium fixture checks passed at 1,200 × 900 and 390 × 844: model/effort
  selection, styled disconnect/cancel, no horizontal overflow or page errors.
  Account responses were synthetic; this is not real authentication evidence.
- Whitespace and Go formatting checks passed. Temporary browser fixture entry
  files were removed; existing Desk backend processes were left running.

### Managed dependency follow-up

The flag-gated setup above was replaced by automatic preparation on Connect.
New tests cover passive checks without installation, checksum and archive
refusals, cache custody, interrupted downloads and retry, installation reuse,
account persistence, administrator disablement and UI progress/cancellation.
The pinned official archive was downloaded, independently checked and exercised
through the actual native app-server in an empty Desk profile, without sign-in.

Managed-runtime verification: the full Go suite, vet, production build and full
frontend suite passed (4,343 passed and one pre-existing skip). The final recovery
case brought the focused settings suite to 12 passing tests. Installer, lazy
provider and agent HTTP race checks passed, and the native verified-archive
smoke passed. All 2,818 UI strings passed the eleven locale catalogues. Chromium
passed the preparation/sign-in/cancel flow at desktop and mobile widths after
correcting its synthetic session bootstrap. Existing Desk processes were not
restarted, and no commit or real account sign-in was performed.

### Runtime pin update — 2026-09-26

The managed pin moved from `0.145.0` to `0.157.1` (release `rust-v0.157.1`;
archive and binary digests in `internal/codexbridge/runtime.go`). The records
above were made against `0.145.0`. For `0.157.1`, the generated app-server
schemas show only additive changes (new plan types and optional members) to the
methods and fields the bridge sends or reads. The real client's initialize
reply has the `jps_desk/0.157.1 (` form. The bridge race tests passed with the
real binary and an empty private profile, including absent-account status and
signed-out logout. The managed-archive smoke test passed with the official
archive served by a local fixture, and failed when either pinned digest or the
archive size was changed. The scripted-model probe now counts every tool list
in a model request and passed all eight scenarios for each of the seven models
the release lists, with the model catalog described below; the
[proof record](../reviews/codex-subscription-proof.md) has the results.

A profile the earlier Desk prepared carries a `config.toml` that names no
catalog. Desk recognises exactly that configuration on the next open and
rewrites it, writing the catalog beside it and keeping the account files; a
configuration that differs in any other way is refused as before. The next
Connect then prepares `0.157.1`. Whether `0.157.1` accepts a sign-in made with
`0.145.0` was not tested, since no real sign-in was performed. The `0.145.0`
executable stays in the private cache, because staging cleanup never removes a
published version.

`0.157.1` registers no skills utility and no `update_plan` without an
environment, where `0.145.0` registered all three. The probe accepts either
form. The `0.156.x` releases were probed with the release catalog only; none was
pinned.

### Model catalog

Codex decides a model's tool mode, sub-agent version and experimental tools
from the model catalog before it reads the feature flags in `config.toml`, and
a signed-in client fetches that catalog from the account. With the release's own
catalog, every listed model except `gpt-5.5` selects code mode: the model
requests advertise code-mode `exec` and `wait` and, for most of them, the
sub-agent tools, although those features are disabled. This held at `0.145.0`
as well, where `gpt-5.2` was the other model without a tool mode; the earlier
proof had probed `gpt-5.5` and one name outside the catalog.

Desk therefore supplies its own catalog through `model_catalog_json`:
`internal/codexbridge/model-catalog.json`, which `scripts/codex-model-catalog.py`
derives from the pinned release's bundled catalog
(`codex-rs/models-manager/models.json` at `rust-v0.157.1`, SHA-256
`0178d235c589a31abd6ed0ea1e870935dc5819240eb0e813e178d3ebedf534f4`, Apache-2.0)
with the hidden models dropped and `tool_mode`, `multi_agent_version` and
`experimental_supported_tools` cleared for each listed model. Everything else,
including each model's instructions, is as published. The bridge embeds the
catalog, writes it into the private profile beside `config.toml`, refuses a
changed copy before every launch, and refuses a launched process that does not
list exactly the catalog's models.

A supplied catalog is static: Codex never refreshes it from the account, so the
models Desk offers are the pinned release's listed models until the pin moves,
whatever the account's server catalog says. Whether the account's plan admits a
run with a listed model is decided upstream and is not checked locally. For the
models that select code mode by default, the host tool is advertised inside a
`functions` namespace and the tool call arrives without a namespace; the probe
accepts both forms of the name.
