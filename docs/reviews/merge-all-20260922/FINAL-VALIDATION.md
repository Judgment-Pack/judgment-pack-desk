# Merge and local restart validation — 2026-09-22

Authorization: user requested an independent Codex clean-room review, then “go ahead and merge all and restart”; continuation confirmed with “restored”. Same-vendor review is an explicit user exception, not cross-vendor compliance.

All-gateway candidate: 3912e7e4cb1a1f44349032cf57ee6ee0e70977fa. Independent final PDF review approved this tree after fixing actual inferred-fragment counting. Full corrected document suite passes: document 8.469s, PDF 58.237s. Source modules, connections, attachment validation, public-web acquisition, source command, connection command and MCP HTTP client all pass. Core full tests, vet, 30 canonicalization plus 41 store vectors, and four smoke parity tests passed on the identical core tree.

Desk parent review approves exact catalog/public-link deltas and localization correction. Foundation reviews approve both repository deltas; the OAuth continuation regression is fixed. Translation statuses pass independent Japanese error/progress probes. Public-link acquisition respects lifecycle/current-pin constraints.

Repository policy permits squash merges and requires current-main checks. Stack ancestry reconciliation commits are metadata only: merged main's tree is first checked against the reviewed parent tree, and resulting head tree is checked against its original reviewed tree. Material PDF overlaps were resolved separately and independently reviewed.

No live third-party credentials were requested or used for tests. Google Drive was previously user-tested. New cloud providers are not claimed to exist; this merges the foundation and implemented Notion/Obsidian/public-web support.

Final merge SHAs, bundle manifest, restart checks and preserved configuration assertions will be appended after deployment.

## Final concurrent bounds integration

Concurrent upstream `dafcb07` is retained. Independent review approves `ab047f4` after correcting the integrated test helper and making frozen-clock exhaustion terminal. Full final document, PDF and document-command tests pass (17.474s / 49.553s / 3.621s). Metadata-only `0399239` preserves the same tree and refreshes DCO against the current base; DCO passes.

Gateway merged main: `a986bef5ca9a52349938750ffc2e9e0086f7f108`. Its tree is byte-identical to the final reviewed/tested candidate. Desk pins this exact revision.

The local runtime is built from CI-passing main `d891b056133fea952ceb2c39e38a2ee16fee24ee`, labeled `0.22.0-dev+d891b05`; all 47 JPS 0.2.0-draft conformance cases pass. Desk MCP listing/evaluation relay tests pass with a synthetic project declaring explicit applicability. The initial generic graph fixture produced a legitimate empty trace, so it was adapted only in `/tmp` to exercise the trace transport. No production test or runtime contract was weakened. The user's `.vscode/tasks.json` diff remains unchanged.
