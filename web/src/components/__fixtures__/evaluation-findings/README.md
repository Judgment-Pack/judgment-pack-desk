# Evaluation findings fixtures

The four `*.result.json` files were captured from Runtime v0.24.0 at revision `bae473161b0039aae292f8e7c7605e7dbbce6ead`, running the corresponding synthetic pack, facts and evidence files. They are rehearsal evaluations; no decision was recorded.

From this directory, reproduce each fixture with the v0.24.0 binary:

```sh
jpack experimental evaluate type-mismatch.pack.json --facts type-mismatch.facts.json --evidence type-mismatch.evidence.json --rehearsal --format json
```

Replace `type-mismatch` with `unknown-facts`, `unknown-evidence` or `unmet-evidence` for the other cases. Output is formatted for readability; the payload fields are unchanged. Supplemental component tests cover root pointers, aggregate scope, unsupported and future diagnostic causes without claiming those synthetic payloads were produced by this CLI invocation.
