# Same-vendor clean-room review — foundation

The user authorized this review on 2026-09-22: "let's use codex clean room for
independent review". This is an independent-context OpenAI review under that
specific exception, not different-vendor compliance or a standing policy change.
Two fresh reviewer agents started with no inherited conversation and were asked
to form findings before reading drafter validation or prior review responses.
Both initial source snapshots were detached and stayed unchanged during review.

## Initial recommendations

- Gateway #149 at a36190272b012dd5f9d4e5fcaab55a5421fc2eaf:
  [approve within reviewed scope](GATEWAY-REVIEW.md), no reproduced introduced blocker.
- Desk #125 at c67f6096c4285a62726d7c39b4615ee2a99803c6:
  [request changes](DESK-REVIEW.md), one P2 OAuth/form continuation defect (D1).

The reports are preserved verbatim, including attribution, actual tests,
limitations and parent-stack coverage. Their original source snapshots and diff
identities are in manifest.json and snapshot-verification.json. Reproduction
sources/logs are retained here. Large generated PDF record outputs and whole
copied working trees are omitted; the commands recreate them. The reproduction
scripts use the original /tmp review layout documented in their instructions.
The copied standalone Go probe has an ignore build constraint so it is not a
Desk package; explicit `go run producer_probe.go` still runs it as documented.

## Author disposition for D1

**Accepted and fixed** in Desk 4987cdb1d47672e7b1621d158fd1cdb4ffb5a2c8.
The required-field check now applies only while the current action configures a
form, using the same condition as form rendering and request dispatch. OAuth
consent and reconnect no longer require invisible registration fields. Setup
still requires its declared fields, and successfully submitted values clear.

The regression test is derived from the independent reviewer's reproduction,
with author-added configured-initial-state/reconnect cases and a check that
missing registration fields still disable Save. All three cases fail on the
original head and pass on the fix; 31 focused pane tests and typecheck pass.
This test-code adoption is recorded explicitly; author validation is not an
independent reviewer conclusion.

## Follow-up recommendation and final validation

The [independent follow-up](DESK-FOLLOWUP.md) resolves D1 and recommends approval
within the original Desk review scope at the exact fixed commit. The original
independent reproduction now passes unchanged; five fresh reviewer-written
setup/consent/reconnect probes also pass. The author regression test is separately
identified and is not counted as independent follow-up evidence.

Author validation after the fix: production build/typecheck, 179 frontend test
files (3,886 passed; one existing skip), and rebuilt gateway-only replacement
browser acceptance pass. The browser retained the draft/focus, saved no empty
chat, and verified the signed attachment. These are author checks, separate from
the independently executed checks in each report.

Gateway implementation remains a361902; reviewed Desk implementation is 4987cdb.
The commit adding this review record changes documentation/evidence only. No PR
is merged or live Desk installed by this record. Parent PRs remain separate review
scope; foundation approval does not approve the full ancestor stack.
