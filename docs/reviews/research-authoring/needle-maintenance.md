# Existing mutation needle maintenance

Recorded during the research handover continuation on 2026-09-14. This is
pre-existing test-maintenance work, separate from the expectation admission
and Create handover fixes.

`scripts/needle-check.sh .` examined 885 rows and exited nonzero with 16 stale
needles. It also reported one ambiguous needle matching twice. The mutation
script, checker and every file listed below are unchanged from the PR base,
`19ec074850f848010ced3a36aeddc1cf77d33391`.

| File | Stale needles |
| --- | ---: |
| `internal/desk/deskfile.go` | 1 |
| `web/src/shell/LeftRail.tsx` | 2 |
| `web/src/shell/paneState.ts` | 3 |
| `web/src/shell/RightPane.tsx` | 2 |
| `web/src/shell.css` | 1 |
| `web/src/shell/AppShell.tsx` | 2 |
| `web/src/packs/document/ConditionTree.tsx` | 1 |
| `web/src/routes/PackView.tsx` | 1 |
| `web/src/packs/PacksPane.tsx` | 1 |
| `web/src/packs/edit/EditToolbar.tsx` | 1 |
| `web/src/routes/GraphView.tsx` | 1 |

The ambiguous needle in `internal/desk/assistant.go` is
`if _, err := decoder.Token(); !errors.Is(err, io.EOF) {`. The checker prints
the ambiguity but does not count it as a failure, despite its documented
exactly-once contract.

Follow-up work should map each stale row to its current behavior, then repair
the needle and run the corresponding mutation to prove its test discriminates.
Retire a row only with an explicit explanation if the behavior is gone. Make
ambiguity fail the checker and reproduce both missing and multiple-match
conditions. Do not equate a repaired needle with a killed mutation.

No unrelated mutation row is rewritten or removed in this feature PR, and this
record does not waive the handoff's before-merge check.
