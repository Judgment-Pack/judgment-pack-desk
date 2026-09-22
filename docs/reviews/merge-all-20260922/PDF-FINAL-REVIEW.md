# Independent PDF final-delta and integration review

Review date: 2026-09-22. Final scoped verdict: **approved after independent fix recheck** at `3912e7e4cb1a1f44349032cf57ee6ee0e70977fa`. The one P2 finding below is resolved. No outstanding finding remains in this review scope; the root coordinator owns the full-suite and overall merge decision.

## Provenance and scope

This is an independent Codex clean-room review under the user's explicit **same-vendor exception**. It is not a cross-vendor review. The reviewer used its own clone at `/tmp/jp-merge-all-20260922/pdf-final-review`, inspected the production deltas before reading the prior maintainer dispositions, and did not implement production fixes or mutate another worktree. Prior review narratives are context, not independent evidence.

Exact branch heads reviewed: #145 `14fda49e344c10d756ba8fe43927c0eda462835b`; #146 `e91190e1a498aacdaf1f7a83a7e2a15b7ef00d29`. The integrated source reviewed and tested was `98fad04795fa55c87c26e115db4e725531ccf769`, incorporating #145, #146 and #147. The material merge was `acfdeb939033cf283608509e10b534b1991ce09b`; all 27 conflict regions in the saved `pdf-resolution.diff` were inspected, including nearby automatically combined code. #147's HTTP behavior was outside this PDF review.

## F1 — P2: an estimated fragment count rejects an admissible CMap and can exceed its declared cap

Source: `adapters/document/pdf/cmap.go:369–374` at integration `98fad04795fa55c87c26e115db4e725531ccf769`, especially line 370 (`len(out)+2*nbytes > maxInferredCodespaces`). Introduced in #145's final commit, retained by integration.

The estimate is neither the actual number of fragments nor a universal upper bound. With 64 isolated one-byte sources `<00>, <02>, …, <7E>`, each source needs one range and the declared per-length allowance is 64. Before adding the last source, the code computes `63+2>64`, widens all one-byte runs into `00..7E`, and incorrectly absorbs the prefix `01` of a separate two-byte source `<0100>`. Ambiguity detection then discards this otherwise prefix-free encoding.

An independent end-to-end PDF probe maps `<00>` to X and `<0100>` to Y and shows `000100`. With 63 one-byte runs the extraction is `XY` with zero unmapped glyphs. With 64, the same shown text becomes `��` with two unmapped glyphs. The extra mapping is unused on the page. This is silent text degradation for an encoding that fits the documented allowance, rather than a necessary consequence of exceeding it.

The same estimate can undercount: 58 isolated three-byte singleton runs followed by `8181E9..C09D25` leave 65 ranges; 56 isolated four-byte singleton runs followed by `818181E9..C09D9D25` leave 71, beyond the per-length cap of 64. These direct helper probes exercise joined numeric runs; contiguous source ranges can produce such joined runs.

Required correction: measure the actual carry-split fragments before deciding whether to widen. Preserve exact coverage through the cap, widen only when the actual result exceeds it, and test both sides of the boundary plus a joined interval whose split exceeds `2*nbytes`.

Reproduction file: `/tmp/jp-merge-all-20260922/pdf-final-review/adapters/document/pdf/final_independent_review_test.go`. Relevant tests: `TestFinalIndependentInferredCap` and `TestFinalIndependentInferredCapMaximum`.

```sh
cd /tmp/jp-merge-all-20260922/pdf-final-review/adapters
GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go test ./document/pdf -run '^TestFinalIndependent' -count=1 -v
```

Initial evidence: `/tmp/jp-merge-all-20260922/pdf-final-independent-probes-before.log`. The same failure was independently reproduced on original #145 `14fda49`, with only the probe call adapted to that commit's two-argument `parseCMap`; evidence: `pdf-final-independent-original-145.log`.

## Other review results

- All inspected conflict resolutions preserve the two branches' relevant semantics: whole-operation generation scopes, cache invalidation before encryption handler replacement, generation-guarded bound/cache publication, persistent file-bound propagation, explicit decoder ownership, object-stream slot and duplicate-map charges, strict inline parsing with allowance restoration, and deadline errors when the clock has passed before cancellation is delivered.
- Focused integrated regressions passed: 26 top-level PDF tests, 123 cases including subtests, in 6.253 seconds. They cover the final generation/cache/bound paths, header charges, unusable CMap charges, lexer/header bounds and the four integration regressions. Evidence: `pdf-final-focused.log`.
- Eight request arbitration tests, 25 cases including subtests, passed normally and under `-race` (0.029 and 1.050 seconds). Evidence: `pdf-final-request-focused.log` and `pdf-final-request-race.log`.
- A reviewer mutation changing strict `After(cutoff)` to non-strict `!Before(cutoff)` was rejected by the equal-cutoff regression, confirming discrimination of the final deadline fix. The original source was restored. Evidence: `pdf-final-request-mutant.log`.
- Independent carry-split coverage checking passed for 20,000 deterministic randomized intervals across one to four bytes, with endpoints, adjacent outside values, midpoint and random membership probes. The error is cap arbitration, not the sampled interval coverage.

## Limitations

This is a focused final-delta and semantic integration review, not a fresh audit of the entire PDF reader or PDF specification. It did not duplicate the full PDF suite that the integration author was running, exhaustively fuzz all PDFs, independently measure peak memory, or validate historical review assertions. No network data, private documents, credentials, remote writes, or subagents were used. Only synthetic/local fixtures and the isolated review clone were used. Test logs are under `/tmp/jp-merge-all-20260922/`.


## Independent fix recheck and final disposition

Fetched the corrected candidate from the local integration checkout and checked out exact commit `3912e7e4cb1a1f44349032cf57ee6ee0e70977fa` in the isolated review clone. Inspected fix `3f487dc25add93c93dde0e1b5f0457091323ecee` and the following trailer-bound test correction `3912e7e` before testing.

**F1 resolved.** The fix counts actual carry-split fragments, compares their total with the cap, and widens only when that total exceeds it. The temporary split is bounded by 15 fragments for four bytes. Both the independently preserved 63/64 end-to-end reproduction and the 65/71-range overflow probes now pass. A new independent matrix checks 63, 64 and 65 singleton runs at each of the four code lengths: every mapped source remains covered; holes remain excluded through 64; the documented widening occurs at 65. The 20,000-interval randomized coverage check still passes.

The committed new regressions and existing inferred-code coverage regressions also pass. The trailer-rescan assertion now recognizes the existing file-scoped bound; the production scanner behavior was not altered by that test-only commit. Its five synthetic cases pass. The complete focused recheck took 0.247 seconds; evidence: `/tmp/jp-merge-all-20260922/pdf-final-independent-probes-after.log`.

No production files remain changed in the review clone. The untracked independent probe file is retained as review evidence. No additional full PDF suite was run during recheck, because the coordinator was running it on the final candidate. **No outstanding review finding remains; this scoped review approves the corrected candidate.**
