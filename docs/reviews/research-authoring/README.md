# Research and draft: live smoke

This is the #76 record. The runs made after the expectation-admission fixes,
the first of which reached `ready`, are recorded in
[admission-smoke.md](admission-smoke.md).

One run of `scripts/research-smoke.mjs` against a desk built from this branch,
a judgment-pack gateway serving `read=adapter-http --endpoint https://r.jina.ai`
(keyless), Gemini through the model relay, and jpack 0.21.0, in a headless
Chrome 130. What it proves is that the whole path ran once against the live
IRCC page named; it is a smoke, not a certification, and `smoke.json` records
the session, the receipt, the seal, the case counts and the outcome.

| What | Where |
| --- | --- |
| Settled, wide: conversation beside the draft, sticky header with the status | [settled-wide.png](settled-wide.png) |
| Tests: 16 cases established from the excerpts, 15 agree, the disagreement named | [tests.png](tests.png) |
| Review: assumptions, open questions, 8 of 8 citations traced, why Create is withheld | [review.png](review.png) |
| Inspector: the source, retrieval time beside the page's declared dates and the reader's, the receipt verified under the pinned key | [inspector.png](inspector.png) |
| Narrow (390px): the Inspector as a drawer over the conversation | [narrow-inspector.png](narrow-inspector.png) |

The run ended `stalled`: the reviewer had written one expectation the format
cannot produce (an unresolved disposition with `reasons: []`), the assistant
declined to change the candidate to satisfy it and said so in its open
questions, and the desk reported the repeated candidate rather than looping.
That is the designed outcome for a disagreement between a source-grounded
expectation and the draft: the person decides. Two earlier runs of the same
script found two defects this branch then fixed — tools bound to the defaults
at first render, and Chrome 130's missing WebCrypto Ed25519 — and are not
kept here.

Not exercised live: `search_sources` (no search key on the machine that ran
this; the run worked from the URLs given), the Create handover (the run did not
reach `ready`), and the narrow Draft switch (the Inspector drawer was open in
front of it when the script looked).
