# Independent Desk parent-layer review

Date: 2026-09-22. Reviewer: OpenAI Codex, GPT-6 (the session identifies itself as GPT-6; no more specific backend variant is exposed). This is a **same-vendor independent-context review**, under the user's explicit same-vendor exception relayed in the review task. It is not an Anthropic or other cross-vendor review. I formed the findings by inspecting code and running tests before consulting author reports; no author acceptance/review reports were consulted.

**Recommendation: APPROVE both reviewed heads.** No merge-blocking introduced correctness or security defect was found in the bounded scope below. One P3 localization defect was found at #124 and is **resolved in the independently rechecked supplemental patch** described below.

## Exact scope

| Layer | Base | Reviewed head |
| --- | --- | --- |
| Desk #123 catalog | `977f9a70fda99fed20b0ce999149ac12b87b7086` (#122) | `31af425aba4fccc349cdb21dd3d3bc64123fcc76` |
| Desk #124 public links | `31af425aba4fccc349cdb21dd3d3bc64123fcc76` | `88009f99324e19a7b8b14f6a38d4891e5b99e531` |

Gateway pins inspected for interface compatibility: catalog `f0b469aabc175192f9b29e2c8169006cdb336472`; web `f62de27b82c8f31d90bffc2f11a2be37ee4d9962`. Gateway implementation security is the separately assigned gateway review's scope. This report does not claim a new blanket review of #122 or the generic foundation.

Isolated clone: `/tmp/jp-merge-all-20260922/desk-parent-review` at #124. Exact #123 test worktree: `/tmp/jp-merge-all-20260922/desk-catalog-review`, created from that isolated clone. No integration-checkout source was changed. Reviewer repro/test files were added in the isolated #124 clone; the exact supplemental localization fix was subsequently applied there solely for remediation verification. No repository-local `AGENTS.md` was found. No subagents, live changes, posts, provider accounts, private credentials, or paid APIs were used.

## Findings

**P3, RESOLVED by supplemental patch — translate retained error/progress copy in the new link pane.** Introduced by #124 at `web/src/connections/WebSourcePane.tsx:52` and `:55`.

Trigger: choose Japanese, open Add link, and have link acquisition return the normal Desk error `Could not read this link. Use a public HTTPS page, PDF, or text file under 4 MiB.` The pane displays that English sentence even though its Japanese catalog entry is loaded. `useChatAttachments` intentionally retains canonical English `sourceMessage` strings, and the new pane renders them directly; the existing connections pane calls `systemMessage` at presentation time. Progress uses the same direct rendering and has the same issue. Effect: failure/recovery and progress text ignore the chosen language; attachment safety and cancellation are unaffected. Suggested correction: apply `systemMessage(upload.error)` and `systemMessage(upload.progress)` when displaying them.

Reproduction retained at `web/src/connections/review-public-links.test.tsx`, test `review: localizes link acquisition errors in Japanese`; a second test now checks progress explicitly. The original-head failure is retained in the final-tests log below. Run the regression tests from the isolated clone (the current copy includes the fix and passes):

```sh
PATH=/home/onword/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web test -- src/connections/review-public-links.test.tsx
```

Expected localized alert: `このリンクを読み取れませんでした。4 MiB 未満の公開 HTTPS ページ、PDF、テキストファイルを使用してください。`
Actual alert: the English source sentence above. This is nonblocking P3, not a security or data-integrity blocker.

No inherited issue is presented as an introduced finding.

## Coverage and evidence

- Catalog relay: bundle revision/artifact verification, fixed `--catalog` invocation, no provider parameters or query, session guard, no external-gateway fallback, 32 KiB output limit, five-second timeout, one-second process-wait grace, discarded stderr, exact keys/duplicate handling, provider/source count bounds, operation/media identifiers. A reviewer test with a child retaining inherited stdout confirms cancellation finishes within the deadline plus the explicit wait grace.
- Discovery and menus: implemented-handler intersection, missing/unsupported operations, unknown protocols, failed refresh dropping cached actions, provider removal canceling pending status, stable open-menu rows disabling removed choices, unavailable/retry states, and query-required behavior. Version 1 is tested at the actual #123 head; version 2 is tested at #124.
- Link ownership: URL validation before ingestion, no implicit send, capacity checks, cancel/unmount/disable, preservation through dock/drawer target swaps. Independent tests resolve a deliberately late acquisition after chat, signer pin, gateway authority, document-enable, and capacity changes; none attaches the late result or calls the success callback.
- Retention/proof: retained bytes and digest, signed response/session seal, request argument commitment, selected requested URL, source kind, original bytes, current authority/signer, expected result digest, format consistency, and cross-provider substitutions. Independent tests reject changed current authority, signer, and expected digest; an ingestion test passes a valid signed fixture through real document verification and rejects a different selected URL.
- Network boundary: source inspection traces `ingestWeb` through the Desk research relay with `local-documents` constraints for acquisition, sealing, and registry access. Independent mocks assert those constraints and local attachment PUT paths. No automatic browser fetch of the selected remote URL was introduced; opening the original source is an explicit link action.
- Presentation: retained static snapshot labeling, download naming, source URL context, error preservation, and locale catalogs. Japanese error rendering was the P3 exception above and is corrected by the supplemental patch.

Actual executions:

1. #124 targeted shipped Vitest tests (`src/connections`, `src/chat/AttachmentMenu.test.tsx`, `src/chat/useChatAttachments.test.tsx`, `src/documents`, `src/shell/icons.test.tsx`): **18 files, 225 tests passed** before adding reviewer tests.
2. #123 exact-head targeted shipped Vitest tests (catalog, ConnectionsPane, AttachmentMenu): **3 files, 34 tests passed**.
3. #124 `go test ./internal/desk -run 'Test(ConnectionCatalog|LocalDocumentConstraint|Attachment|LocalGateway)' -count=1`: **passed**. Initial sandbox attempt could not create a local test listener; rerun with approved loopback-listener escalation passed.
4. #123 `go test ./internal/desk -run 'TestConnectionCatalog' -count=1`: **passed**, using synthetic local servers with approved escalation.
5. Reviewer `TestReviewCatalogClosesInheritedStdoutOnCancellation`: **passed** (about 1.17 seconds, including the one-second wait grace).
6. Original-head reviewer browser suites `review-public-links.test.tsx` and `review-web-proof.test.ts`: **9 passed, 1 intentionally failing regression expectation reproducing P3 above**. Final clean output: `/tmp/jp-merge-all-20260922/desk-parent-review-final-tests.log`.
7. #124 TypeScript `typecheck`: **passed**. Locale check: **all 11 non-English catalogs have 2205/2205 entries and zero placeholder errors**; this check does not detect the P3 runtime rendering omission.

Tests used Node 22.23.1, dependencies at `/tmp/jp-chat-workspace/web/node_modules`, `GOCACHE=/tmp/jp-authoring-go-cache`, and `GOFLAGS=-buildvcs=false`. Reviewer test development had harness mistakes (draft reuse, reused Response body, and initially ignoring the documented wait grace); those were corrected, and are not product findings. The final evidence log and retained test sources are the reproducible results.


## Supplemental remediation verification

After the independent finding was delivered, the integration agent supplied a minimal `WebSourcePane.tsx` correction: import `systemMessage` and apply it to the error and progress renderings. I inspected the exact diff, copied that diff to the isolated review clone, and independently reran the tests. The remediation introduced no other source changes in that clone.

- Exact patch: `/tmp/jp-merge-all-20260922/desk-parent-localization-fix.patch`, SHA-256 `f667dd1d611295117eac4f392b36ce4defc9682ce6d77c4d045a3f4dfad54451`.
- Corrected `WebSourcePane.tsx`: SHA-256 `38ae4019b226deebb2c53489331844e83bdd8fc341f985aa342a4e7f79e56822`, identical in the integration and isolated review copies at verification time.
- Japanese failure and progress probes, independent lifecycle/proof tests, and shipped WebSourcePane tests: **3 files, 18/18 tests passed**.
- Corrected-tree TypeScript `typecheck`: **passed**.
- Output: `/tmp/jp-merge-all-20260922/desk-parent-remediation-tests.log`.

The fix was a supplemental working-tree diff at verification time, not a newly supplied commit head. Thus the exact-head findings above remain attributable to their listed original heads; remediation acceptance covers the exact patch/file hashes recorded here. **No unresolved findings remain in the reviewed content plus this patch.**

## Limits

This was a bounded code and synthetic-test review, not live-provider certification. There was no real public-web acquisition, DNS/network attack exercise, OAuth flow, installed desktop restart, screenshot/visual-layout audit, or full repository suite. Server tests used only local synthetic listeners; browser tests used JSDOM and fixture/mocked transport with real proof verification. SSRF/redirect/DNS admission and the gateway adapter's actual fetch behavior belong to the separate gateway review. Existing author browser screenshots and review claims were not used as acceptance evidence.
