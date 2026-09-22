# Independent review of concurrent #146 bounds update

Final scoped verdict: **approved after fixes**, exact combined candidate `ab047f477237cc344ad0b9677f29bdcc151b08d1`. Both findings below are resolved; no outstanding finding remains in this scope.

## Provenance and reviewed scope

This continues the independent Codex clean-room review under the user's explicit **same-vendor exception**; it is not cross-vendor validation. Work remained in `/tmp/jp-merge-all-20260922/pdf-final-review`. Production source was read before author dispositions. No source fix was implemented by the reviewer and no other worktree was modified.

Reviewed upstream delta: #146 `e91190e1a498aacdaf1f7a83a7e2a15b7ef00d29` to `dafcb070b6bba63e7148d3d9a7f580df2259cb92` (seven files). Reviewed combined merge `ce3f35b2de55b6212b4f51757f6efd5191f04be6`, its one explicit conflict resolution, and corrective commit `ab047f477237cc344ad0b9677f29bdcc151b08d1`.

The production delta adds an odd-nibble hexadecimal string bound, explicitly counts 4,096 request-arbitration looks, and adds scheduling seams. The remaining changes concern regression discrimination, a smaller calibration fixture, short-mode calibration skipping and documentation. The existing generation/cache, inferred-CMap fix, decoder ownership and bounds-propagation implementations are unchanged relative to independently approved `3912e7e`.

## U1 — P2: automatically merged test helper calls removed function — resolved

At combined merge `ce3f35b`, `adapters/document/pdf/bounds_followup_test.go:1560` calls `walk(ctx, data, ...)`. #145 replaced that function with `openDocument` and `walkPages`. The new test therefore prevents the entire PDF test package from compiling.

Independent reproduction:

```sh
GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go test ./document/pdf -run '^TestBoundsHexStringPaddingMeetsTheStringBound$' -count=1
```

Observed: `document/pdf/bounds_followup_test.go:1560:13: undefined: walk`. Evidence: `pdf-upstream-focused-before.log`.

Corrective commit `ab047f4` adapts the helper to the actual opening/walking sequence and asserts a stable generation for its ordinary fixture. The PDF package compiles, and the new small balance-calibration test passes independently.

## U2 — P3: exhausted frozen-clock refusal can be reversed by publication — resolved

At `dafcb07` and its unchanged integration, `adapters/document/adapter.go:382–412` ends an empty arbitration after 4,096 looks, calls `readArbitrated(false)`, then still accepts an equal-cutoff result from the final nonblocking receive. The new comments, README and design note explicitly say that exhaustion commits a refusal even if a read subsequently stamps exactly the cutoff. The new upstream regression holds publication back and therefore misses the opposite interleaving.

The independent reproduction freezes the stamp clock at the cutoff for every look, releases the read from `readArbitrated(false)`, and waits for `readStamped` before returning from that hook. The final receive then finds the result and returns `"hello", nil` instead of `errRequestNotRead`. Exact 4,096-look exhaustion is asserted. This failed on both standalone `dafcb07` and combined `ce3f35b`.

This is a test-clock/documented-exception defect, **not a demonstrated production `time.Now` failure**. Severity is P3 for that reason.

Probe: `/tmp/jp-merge-all-20260922/pdf-final-review/adapters/document/final_bounds_upstream_review_test.go`, `TestFinalUpstreamStalledClockRefusalAfterPublication`. Evidence: `pdf-upstream-stalled-clock-before.log` and `pdf-upstream-request-before.log`.

Corrective commit `ab047f4` records exhaustion under the mutex and returns the refusal before the fallback receive. The independently preserved failing reproduction now passes under `-race`. A read already stamped by the last arbitration remains eligible; the exception applies only when no stamp exists and the clock has not passed the cutoff.

## Remaining source review and validation

The explicit object-count-test conflict was resolved correctly: it retains the incoming real-parse/cached-reread assertions while checking the file-scoped bound introduced by #145. The independent focused run passes these cases.

The automatic lexer merge preserves strict rejection of invalid hexadecimal characters and routes both explicit `>` termination and EOF recovery through `padHex`. Padding retains its memory reservation and now checks the padded length. An independent eight-case intersection probe covers both lexical modes, both termination routes, and `maxStringBytes-1` versus `maxStringBytes` complete bytes followed by a nibble. The final allowed byte is accepted as `B0`; one byte beyond the bound is refused without returning a token. Existing padding-reservation, string-bound, page-failure and small balance-calibration regressions also pass.

Final focused PDF check: six top-level tests / 25 cases including subtests, **3.690 seconds**, `pdf-upstream-focused-after.log`. Final request check under `-race`: six top-level tests, **10.286 seconds**, including on-time/late publication, equality after empty arbitration, stalled-clock exhaustion, mutex contention and the independent post-exhaustion publication probe; `pdf-upstream-request-after.log`.

All commands used `GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false`. Test logs are in `/tmp/jp-merge-all-20260922/`. The review clone retains only two untracked independent probe files; tracked source matches the reviewed commit.

## Limits

No broad PDF suite was duplicated while the coordinator ran it on the final candidate. No independent peak-memory calibration, exhaustive PDF fuzzing, OS-clock anomaly analysis or fresh audit of unrelated HTTP/connection changes was performed. No network data, private files, credentials, remote writes or subagents were used. Final overall merge approval remains subject to the coordinator's full-suite check.
