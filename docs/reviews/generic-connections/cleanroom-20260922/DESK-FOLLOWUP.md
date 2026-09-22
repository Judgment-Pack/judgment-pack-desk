# Independent Desk follow-up for D1

**D1 disposition: resolved. Recommendation changes from request changes to approve within the Desk scope of the initial review.** No new finding was identified in this focused follow-up.

Date: 2026-09-22. Reviewer vendor: **OpenAI**. Model identity exposed in this session: **Codex, based on GPT-6**; a more specific deployment/model identifier was not exposed. This remains the authorized **same-vendor clean-room exception**, not cross-vendor review compliance.

Exact reviewed change:

- Previous reviewed Desk head: `c67f6096c4285a62726d7c39b4615ee2a99803c6`.
- Fixed Desk head: `4987cdb1d47672e7b1621d158fd1cdb4ffb5a2c8`.
- Gateway unchanged: `a36190272b012dd5f9d4e5fcaab55a5421fc2eaf`.

I inspected the actual two-file commit diff. The production fix in `web/src/connections/ConnectionsPane.tsx:46` introduces `needsSetup`, matching the configuration phase, and uses it to gate required-field validation at line 47, the configure action at line 87, and form rendering at line 193. Thus an OAuth provider with saved registration no longer demands values from hidden fields before consent or reconnect. Required values are still enforced when configuration is needed.

I extracted the exact fixed commit with `git archive` into a fresh disposable tree at `desk-followup/`. I confirmed that its changed production file and committed regression test match the fixed commit byte-for-byte. No original implementation source, initial detached snapshot, or initial report was modified.

## Independent evidence

The **original independent reproduction**, copied byte-for-byte without changing its assertions, passes against the fixed production code. It had failed against the initially reviewed head because the Continue button stayed disabled after successful configuration. The test file's SHA-256 is `42f603296bca9cb245da10b3ad254a942c0757ab870c6bfbf27cb1483d6e3e43`.

I also wrote and ran five fresh checks independently of the author's added regression test:

1. An already configured, initially `not-connected` OAuth/form provider offers enabled consent with no configuration fields or configure request.
2. A connected OAuth/form provider whose search returns `reconnect-required` offers enabled reconnect, calls the declared authorization endpoint only after the user clicks, and does not reconfigure saved registration.
3. An expired registration followed by `setup-required` returns to required form configuration; after saving, `not-connected` allows consent again.
4. A credentials/form provider still requires every required field before configuring and never starts OAuth.
5. An OAuth/form provider in `setup-required` still requires every required field before configuring and does not start OAuth during that configuration step.

**Result: 2 independent test files / 6 tests passed.**

Files:

- `desk-followup/web/src/connections/cleanroom-oauth-form.test.tsx` — original reproduction, unchanged.
- `desk-followup/web/src/connections/cleanroom-oauth-phase.test.tsx` — fresh follow-up checks.
- `desk-followup/independent-followup-tests.log` — actual execution output.

## Repository regression tests

I separately executed `ConnectionsPane.test.tsx` and the newly committed `ConnectionsPane.oauth-registration.test.tsx`: **2 files / 31 tests passed**. The new committed test was derived by the author from my original reproduction and extended by the author; it is counted here as repository-authored regression coverage, not as fresh independent evidence. Output: `desk-followup/repository-focused-tests.log`.

Reproduction commands:

```bash
export PATH=/home/onword/.nvm/versions/node/v22.23.1/bin:$PATH
cd /tmp/jp-foundation-cleanroom-20260922/desk-followup/web
npm test -- --reporter=verbose src/connections/cleanroom-oauth-form.test.tsx src/connections/cleanroom-oauth-phase.test.tsx
npm test -- --reporter=verbose src/connections/ConnectionsPane.test.tsx src/connections/ConnectionsPane.oauth-registration.test.tsx
```

This follow-up is limited to D1 and nearby setup/consent/reconnect behavior. The initial report's broader coverage and limitations remain applicable. I did not repeat unchanged backend tests, perform a real provider OAuth exchange, or run a browser end-to-end flow. The author's reported full frontend/build/browser checks were not counted as independent reviewer execution. No real accounts, credentials, external provider requests, posting, source edits, or merges were performed.
