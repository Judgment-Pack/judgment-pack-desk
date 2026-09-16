# Expectation admission: live smoke

Three runs of `scripts/research-smoke.mjs` on 2026-09-16 against a desk built
from this branch at `6517969`, a `judgment-pack-gateway` at `9d1625c` serving
`read=adapter-http --endpoint https://r.jina.ai` (keyless, version 3 receipts),
Gemini through the model relay, and a `jpack` built from runtime #150's head, in
a headless Chrome 130. They exercise the flow this branch changes — expectation
admission before rehearsal, the canonical assertion stored with each case, and
the repair loop — against the live IRCC page. `admission-smoke.json` records
the third run's session, receipt, seal, counts and outcome, and the two earlier
runs beside it; the screenshots are the third run's.

| What | Where |
| --- | --- |
| Settled, wide: `ready` after one repair, every case agreeing | [admission-settled-wide.png](admission-settled-wide.png) |
| Tests: 10 cases established from the excerpts, 10 agree, none blocked at admission | [admission-tests.png](admission-tests.png) |
| Review: the unknowns, 4 of 4 citations traced, Create offered | [admission-review.png](admission-review.png) |
| Inspector: retrieval time beside the page's declared dates, the receipt verified under the pinned key | [admission-inspector.png](admission-inspector.png) |
| Narrow (390px): the conversation | [admission-narrow.png](admission-narrow.png) |

The third run reached `ready` in 366 seconds: revision 1 from research agreed
with 0 of 10 cases, one repair turn produced revision 2, and every case agreed
with the runtime on unchanged expectations. That is the outcome the #76 smoke
did not reach: it stalled at 15 of 16 on an expectation the format cannot
produce, which is the defect this branch turns into a blocked case for review.

What the three runs say about admission: the runtime's check admitted 8, 11 and
10 expectations and blocked none. The reviewer wrote every expectation as a
legal §8.3 disposition this time, including the missing-fact case as an
`unresolved` result, so the correction path — a blocked expectation, a proposed
correction, an approval and a retest on unchanged bytes — was never reached
live. Its evidence remains the native replay and the controller tests, which
this record does not replace.

The first run found a desk defect the fixtures could not. Its `jpack.json`
declared no packs, which the runtime's configuration schema refuses, and the
runtime holds a present project configuration to that schema even for an
inline rehearsal; every rehearsal was therefore refused. The refusal is prose,
and the desk parsed it as a report before reading it as a refusal, so the
Tests tab showed `Unexpected token 'T' … is not valid JSON` and never the
sentence that said what to fix. `6517969` reads the text first, with a test
that fails on the previous code. The second run ran under a 600-second budget
rather than the 1,200 intended, because the smoke's own launcher regenerated
the configuration, and expired inside the first repair with all 11 rehearsals
answering `not-applicable`; the third run is the second with the budget it was
meant to have.

Not exercised live: `search_sources` (no search key on this machine; the runs
worked from the URLs given), the second seed URL (the model worked from the
FSWP page's own proof-of-funds section), the Create handover (the script stops
at the settled page; the browser-to-disk drive recorded in
[expectation-admission.md](expectation-admission.md) covers it), and, as above,
the correction path. This is a smoke and not a certification: it proves the
whole path ran, three times, and says what each run did.
