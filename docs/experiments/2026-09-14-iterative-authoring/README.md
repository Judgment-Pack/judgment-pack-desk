# Iterative authoring proof

This first milestone exercises repeated authoring through Desk's installed Vercel
adapter and a real JPS runtime. The model responses and reviewer challenge are
scripted. It proves integration, repeated checking and checkpoint recovery; it is
not a live-model quality or policy-correctness benchmark.

The production Create route is unchanged. See the
[architecture and cross-repository plan](../../adr/0002-iterative-authoring.md) and
[interactive design mocks](../../design/authoring/README.md).

## Observed result

The [recorded result](result.json) contains the binary identity, exact candidate
digests and counts. The run completed 34 model requests and 66 tool calls over five
candidates. The transport gate rewrote 24 omitted rehearsal flags. After the second
candidate's checks, the controller stopped, read its checkpoint from disk and
resumed without redrafting that candidate.

| Candidate | Structural validation | Passing cases | Remaining seeded defects |
| --- | --- | --- | --- |
| 1 | Invalid | Not evaluated | Missing question, two amount boundaries, receipt outcome |
| 2 | Valid | 3 / 6 | $50 boundary, receipt outcome, $1,000 boundary |
| 3 | Valid | 4 / 6 | Receipt outcome, $1,000 boundary |
| 4 | Valid | 5 / 6 | $1,000 boundary |
| 5 | Valid | 7 / 7 | None in this fixture |

The independent scripted reviewer added a missing-amount case after the six
established cases passed. The same candidate was checked again against all seven.
The initial six cases and their expectations remained unchanged. The temporary
project stayed empty; no pack was saved.

## Reproduce

Use Node 22 or newer and the locked web dependencies. Build the runtime from the
inspected revision, outside the Desk repository:

```sh
git clone https://github.com/Judgment-Pack/judgment-pack-runtime.git
cd judgment-pack-runtime
git checkout 0c8a4b63a3868ce7a96d0ce012b59f7a5611f430
go build -ldflags '-X github.com/Judgment-Pack/judgment-pack-runtime/internal/result.CLIVersion=0.21.0' -o /tmp/jpack-authoring-runtime ./cmd/jpack
```

Then, from Desk's `web` directory:

```sh
npm ci
npm run authoring:proof -- /tmp/jpack-authoring-runtime /tmp/authoring-result.json
npm test -- src/assistant/authoring/run.test.ts
```

No API key or live provider is used. The script creates a temporary project and
checkpoint, opens the runtime's stdio MCP transport through the real tool gate,
and feeds scripted OpenAI-compatible responses through the real Vercel adapter.
Repairs are chosen from the previous check feedback carried in the model request,
not a request counter. Each adapter invocation stays bounded; the logical task
continues across invocations and therefore exceeds a single 20-step loop budget.

The report printed by a new run includes its checkpoint path for inspection. That
machine-specific path is omitted from the committed result. Binary hashes can vary
with the build toolchain/path; every run records the binary it actually used.

## Bounds of the evidence

- Runtime snapshot: `0c8a4b6` (reported version 0.21.0). Desk baseline: `fdaaee8`.
  Installed SDK: `ai@7.0.93`. No SDK dependency was upgraded.
- Recovery is a controller stop followed by checkpoint reload. Hard-kill mid-call,
  host restart, power loss, concurrent runs and a production store are not exercised.
- This uses explicit inline cases, not the runtime's complete matrix/history API.
  The empty project assertion is not an audit-enabled mutation test; the existing
  adapter conformance suite carries that separate guardrail evidence.
- The controller has revision/review budgets and cancellation. A production host
  still needs elapsed-time, token/cost, retry and retention limits.
- A test agreement is agreement with that case's established expectation. It is
  not evidence that the policy is correct or complete. The scripted reviewer and
  model do not measure adversarial or open-ended reasoning ability.
- The adapter bridge still consumes proposal events. Ordinary assistant replies,
  clarification continuations, real attachment extraction, provider switching and
  live model quality need the next conversation milestone.
- Gateway sources, receipt verification and live actions are not invoked here.
  Their proposed contracts are explicit in ADR-0002.
