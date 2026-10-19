# Project memory

Implemented in Desk, 2026-10-14. This complements bounded per-execution context and the optional `jpack-assistant.json` project brief.

## Controls and ownership

Admin → Assistant → Project memory has independent **Use saved memory** and **Suggest memories** switches, both initially off. Turning either off retains the stored entries. Add, edit, review, forget and clear operate in the same project. Inferred suggestions remain pending until the person chooses Remember; an exact explicit `remember: …`, `remember this: …`, `remember that …` or `/remember …` command can save directly. The AI cannot approve its own inferred entry. A source-chat link and updated date remain visible. Chat also exposes memory review after a remember tool call.

Desk stores preferences and corrections in an owner-only `ai-memory-<history identity hash>.json` in the active conversation storage directory (normally the Desk's `.desk-private/data`). It is not a repository instruction file and is not committed or shared automatically. Project moves retain the original history namespace. Storage relocation and backups include it. Deleting one topic forgets its superseded versions as well. Hashed tombstones block reconstruction from the same old message; Clear also sets a timestamp cutoff. An explicit new remember command can restore a topic, while old sources stay blocked.

## Context and reliability

Every API dispatch and Codex request uses the canonical server context preparation functions. Relevant approved entries are selected locally from the latest user request, bounded to five entries and 4,000 bytes. The model can also call `recall_memory`. Recall is keyword based, without a vector database or a second paid model call. This means paraphrases without shared terms may not retrieve a memory. Current instructions and saved artifacts always take precedence; memory is never evidence or approval.

Suggestion provenance is bound by the host to the latest saved user turn, checked again on the server. Independent critics and child tasks cannot write memories. Plan mode permits recall but cannot call the write tool. Only explicit management actions and the verified direct command create active memories. Common credential patterns are refused; this is not an exhaustive secret detector.

File-dependent entries carry digests; changed sources mark them stale and exclude them from recall. Existing tools currently suggest stable textual preferences, not automatic artifact summaries. There is no background transcript crawling or automatic import of CLAUDE.md, AGENTS.md, old context files, or other providers' memory stores. Stale entries can be forgotten and replaced with a current reviewed memory. Clear removes memory text; chats and user project files remain intact.

The store uses bounded strict JSON, owner permissions, symlink checks, atomic replacement/readback, shared storage locks and revision checks for management writes. It holds at most 256 entries. There is no silent overwrite or provider-specific bypass. New AI features inherit the shared recall boundary and must pass its architecture tests.
