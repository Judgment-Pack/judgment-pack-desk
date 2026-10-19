# Policy source browser verification harness

Verified for Desk #461 on 2026-10-18. The rebuilt SPA and isolated Desk passed
the desktop and 420px checks below. `smoke-result.json` records the assertions;
the screenshots show the source page and frozen review. `provenance.json` records
the actual binary, uncommitted source manifest and build identities. This is
browser presentation evidence with explicit source API fixtures, not live Google
end-to-end verification.

The harness uses the built SPA and a real isolated Desk listener. Its local
administrator sign-in, draft creation and final unchanged-policy read use the
actual backend. Before installing any source mocks, it requires the actual
`source-sync` endpoint to report `supported: false` and checks that the source
connection control is disabled or hidden.

It then intercepts only that policy's source API routes with Playwright. Those
responses are **synthetic source presentation fixtures** derived from the checked
test attachment. They display snapshot A, open a review, deliver B through the
normal private-query refresh, and prove that the open review remains frozen on A
with its save control disabled and no source command sent. The harness checks the
source table, extracted text, unchanged saved candidate and desktop/420px layout.
It captures desktop source-table and review screenshots, plus both narrow views.

The mocked source responses establish no real Gateway grant, backend source
artifact retention, scheduler behavior or live Google access. Real backend,
Gateway contract, authority and race tests cover those separate boundaries. The
cross-repository `policy-source-contract.yml` job generates unmodified observations
from the exact Gateway source pin and verifies them with the real canon program;
this browser harness is not a replacement for that job.

Use only a fresh temporary project with separate `XDG_CONFIG_HOME`,
`XDG_CACHE_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME`. Set
`JPACK_DESK_LOCAL_ACCESS=1`, disable subscription/model execution, and launch the
built Desk with a compatible Runtime. The project's minimal `jpack.json` is
`{"configVersion":"3","packs":{}}`. Use an installation that actually lacks
the new source capability for the unsupported-control check. The parent runner
can follow `/tmp/jps-policy-import-browser-run.py`, changing the Desk binary to
`/tmp/jps-policy-source-desk-bin` and the smoke script to this directory.

With Node 22 and `web/node_modules/playwright-core` installed:

```sh
JPACK_POLICY_SOURCE_BROWSER_ISOLATED=1 \
  node docs/reviews/policy-source-sync/smoke.mjs \
  http://127.0.0.1:PORT /tmp/policy-source-browser-evidence
```

`CHROMIUM_BIN` optionally selects the local Chrome executable. In the current
workspace Node 22 is `/home/onword/.nvm/versions/node/v22.23.1/bin/node` and Chrome
is `/usr/bin/google-chrome`. The parent runs listeners/browser outside the
restricted sandbox, inspects the evidence and stops the isolated listener.

`smoke-result.json` explicitly labels the source API mock and records the real
unsupported result, local sign-in status, actual policy mutations, intercepted
source reads/writes, page errors and overflow measurements. The harness blocks
external HTTP requests, sends no model request, creates one real draft, and never
approves or publishes it. Failures retain `failure.png` and `failure.txt` for
inspection. Evidence must retain the distinction between these synthetic browser
fixtures and actual producer/consumer acceptance; do not describe this run as a
live Google end-to-end test.
