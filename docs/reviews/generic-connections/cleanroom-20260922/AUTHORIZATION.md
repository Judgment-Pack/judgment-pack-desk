# Review authorization and isolation

User request: "let's use codex clean room for independent review".
Date: 2026-09-22. Drafter: OpenAI Codex. Reviewer vendor: OpenAI.
This is a user-authorized same-vendor exception to the repository's usual
cross-vendor review rule, not a claim of different-vendor compliance.

Reviewers start with no inherited conversation, work from exact detached commits
in manifest.json, and form findings before consulting drafter validation or
prior review responses. They must state what they actually inspected and ran.
Original implementation checkouts remain untouched. Reproduction edits belong
only in disposable copies or untracked temporary tests, with exact patches kept.
No real accounts, credentials, paid model API, merge, publish or live install.
