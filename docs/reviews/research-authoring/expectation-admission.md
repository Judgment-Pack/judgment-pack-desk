# Review impossible expectations before testing the candidate

The remaining PR #76 smoke case expected an unresolved disposition with no
reasons. Core 0.2.0-draft §8.3 cannot produce that value. A repair loop cannot
resolve that disagreement by changing a pack. This change rejects the expectation
at admission, preserves it for review and leaves exact evaluation unchanged.

## User flow

1. The case reviewer proposes source-grounded cases. Desk asks the runtime's
   `experimental_validate_expectations` tool to validate each complete expected
   disposition before establishing it as an assertion.
2. Invalid expectations stay visible in Tests, with the original value,
   rationale and cited excerpt. The tab count includes blocked cases. Testing,
   repair and Create remain blocked; there is no reduced-suite success.
3. **Suggest correction** asks a fresh reviewer to propose only an expectation
   and explanation, using the original case, verified excerpt and declared
   outcomes/handoff. It is not given actual test results or candidate rules.
   No proposed correction is used automatically. Undetermined or still-invalid
   corrections stay blocked.
4. Tests shows original and proposed complete values and the correction rationale.
   **Approve correction and retest** is bound to that proposal and draft digest.
   Approval revalidates it and checks the source is still verified. It preserves
   the case id, facts, evidence, source and original rationale. Changing the draft
   clears pending proposals so they must be reviewed again.
5. Approval invalidates old checks and rehearses every established case against
   unchanged candidate bytes. A remaining mismatch is shown for human judgment;
   this action does not silently repair the pack. Create requires a current,
   complete passing check, no blocked expectation and traced citations.
6. The research record retains original and approved values, the correction
   rationale and approval timestamp. The saved matrix contains the approved exact
   assertion. This history is retained in the current run and saved research
   record; browser-reload checkpoint/resume remains outside this change.

## Runtime boundary and compatibility

Depends on [runtime PR #150](https://github.com/Judgment-Pack/judgment-pack-runtime/pull/150),
implementation candidate `b174132` (ADR-0035). Merge the runtime first.

The new capability is required for research authoring. Older runtimes show an
update message before a run starts; other Desk views still work. The runtime
owns §8.3 decoding and canonical reason/trigger sets. Desk does not carry a
second disposition validator. Missing, malformed, inconsistent or incomplete
runtime reports fail closed. Validation limits are reported as admission
failures, not proof that Core prohibits the meaning. Pack-specific reachability
and policy correctness still require tests and human review. The gateway and
specification require no change.

The existing research test fixture also incorrectly placed `no-match` on a
fallback outcome; outcome reasons must be empty. Both its expectation and fake
runtime answer are corrected and checked in the native replay. Other historical
UI fixtures are not used to define this admission contract.

## Reproduction

Build the runtime branch implementing ADR-0035, then from Desk:

```sh
npm run build --prefix web
npm test --prefix web -- --maxWorkers=4
JPACK_EXPECTATION_BINARY=/absolute/path/to/jpack npm test --prefix web -- --maxWorkers=4 src/research/run.test.ts
```

The optional native replay uses scripted model turns and a signed fixture gateway
response. It performs real stdio expectation admission, pack validation and all
three rehearsals; it first blocks the reasonless missing-fact case, then approves
its corrected unknown reason and verifies every case with identical draft bytes.
It uses no model credentials, external research or live immigration advice, and
is a regression replay, not a rerun of the original live 16-case smoke.

`web/src/research/__fixtures__/expectations.json` records native MCP answers from
the ADR-0035 implementation for the runtime's disposition witnesses and this
replay's three assertions. `expectationRuntime.ts` only looks up recorded answers;
it deliberately throws on unrecorded values instead of implementing §8.3 again.
Controller tests cover invalid/undetermined corrections, stale approval, changed
draft, failed source verification, Stop, a corrected but disagreeing assertion,
full case retention, creation gating and correction history. UI tests exercise
review navigation, exact displayed values, approval token and disabled actions.

## Validation recorded for this change

- Production build and type checking passed.
- Full Desk suite: 3,286 passed; the optional native replay skipped by default.
- Native replay enabled explicitly: all 58 focused tests passed, including actual
  runtime admission, validation and evaluation.
- Chrome fixture review: 320, 480, 720, 900 and 1,280px in light and dark themes;
  no horizontal page overflow, tabs remain fixed while the panel scrolls, and
  approval sends the displayed case/proposal token. These are component fixture
  checks, not a claim that a new live model smoke was run.
