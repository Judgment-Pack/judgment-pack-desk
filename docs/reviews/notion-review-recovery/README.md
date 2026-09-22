# Notion/Obsidian review remediation

The supplied Claude review covers gateway `5346eb6` and Desk `06a7bcd`.
The gateway PR records [the review and each disposition](https://github.com/Judgment-Pack/judgment-pack-gateway/pull/143).
It does not cover subsequent catalog/public-web material decisions.

This patch protects a started token refresh from Desk cancellation, preserves typed
connection failures, adds explicit Reconnect, translates actionable errors in all
12 locales, clears stale selections on connection-epoch changes, and tests signed
Notion/Obsidian acquisition relationships. It does not install or restart live Desk.

## Reproduce the browser check

Install `web` dev dependencies and build an isolated complete bundle. Use a
synthetic fixture project, never the user's working project. Set:

```sh
DESK_BUNDLE=/absolute/path/to/bundle \
JPACK_BINARY=/absolute/path/to/jpack \
JPACK_FIXTURE_PROJECT=/absolute/path/to/runtime/internal/graph/testdata/project \
CHROMIUM_PATH=/absolute/path/to/chromium \
SMOKE_OUTPUT=/tmp/desk-connections-review \
node scripts/check-connections.mjs
```

`SMOKE_PORT` optionally overrides 18889. The script creates a temporary private
config/data directory and vault, then connects, selects and verifies a real
Obsidian attachment through the bundled gateway. It checks composer focus, draft
retention, zero unsent chat creation, retained content after edit/disconnect,
route-change dismissal and all 12 locales at 390px. No assistant request is sent.
All temporary project/custody data is removed on completion; only screenshots and
assertion results remain in `SMOKE_OUTPUT`.

## Mutation interpretation

The new signed cases detect removal of resource/provider/original/kind bindings,
connected arguments commitment, Notion's MCP shape and ingestion kind checking
before persistence. Version-vs-document identity is tested independently from
version-vs-source equality. The explicit source allowlist and early multiple-proof
check are redundant with the later provider/kind requirements; their single-check
removals still refuse invalid input. This is recorded, not counted as killed.

## Results

Browser assertions in `findings.json` passed with no page errors, zero saved
unsent chats and all 12 locales. Screenshots use synthetic content. The tested
gateway implementation is `ff291e9`; the final pin includes its documentation-only
follow-up. Backend tests/vet, typecheck, build, locale checks, 926 mutation needles,
and the needle/bundle script tests passed. The full frontend suite passed its
3,783 non-bundle tests (one existing skip); the two bundle assertions that needed
a first build then passed in the complete three-test bundle file.
