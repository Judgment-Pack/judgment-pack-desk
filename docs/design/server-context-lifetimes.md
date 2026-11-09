# Server context lifetimes

This protocol selects ephemeral reuse scope for the server's existing managed
context. It supplies no model, permission, source identity, history, checkpoint,
or execution capability. Authorization independently binds every lookup to the
current authenticated principal, session, policy generation and pinned project.

## Closed protocol

All routes use the normal authenticated assistant storage/private-workspace
checks. Query parameters, duplicate members, unknown members, case aliases,
invalid Unicode and oversized request bodies are refused. Requests and responses
are JSON objects. No registration or release can invoke a provider or tool.

| POST route | Exact request members | Response |
| --- | --- | --- |
| `/api/assistant-contexts/documents` | `registrationId` | `{ "document": "<token>" }` |
| `/api/assistant-contexts/panes` | `document`, `registrationId`, `purpose` | `{ "document": "<token>", "pane": "<token>" }` |
| `/api/assistant-contexts/documents/release` | `document` | `{ "released": true }` |
| `/api/assistant-contexts/panes/release` | `document`, `pane` | `{ "released": true }` |

A registration ID is a nonempty UTF-8 string of at most 128 bytes without CR, LF
or NUL. The browser creates a random ID once per document or actual pane mount.
The server returns opaque tokens of exactly 64 lowercase hexadecimal characters.
Pane purpose is exactly `author-pack`, `describe-pack` or `author-graph` and cannot
change on registration replay. The same authorized registration returns the same
live token. Explicitly retired IDs cannot recreate a token within that service.
Release is idempotent for the original authority; release grants no reuse.

`POST /api/assistant-runs` adds optional closed `context`. Saved chat uses
`{ "document": "<token>" }`; an authoring start uses both `document` and `pane`.
Even an empty `pane` member is forbidden for saved chat. Brief, connection
diagnostic and settings observation purposes forbid `context`. `null`, unknown
members and malformed selectors refuse. Historical records remain readable.
New production chat and authoring admissions require a currently authorized
selector before Prepare. Historical lookup precedes that requirement; parsing a
selector alone grants no source, provider or tool authority.

The complete start body, including `context`, participates in its admission
digest. The existing-admission lookup precedes new allocation and preparation.
The same admission with a different selector conflicts. Read-only discovery can
still recover the originally admitted run; it never adopts a new browser context.

## Immutable authoring capture

Capture, read and cancel keep the original request, snapshot, revision, owner and
admission IDs. They contain no ephemeral selector. Authoring source matching and
capture cancellation compare all original host-authored reference members while
omitting only `context`; admission body hashing never omits it. Pointer equality
is not used to compare selectors.

A recovered, unadmitted capture can use a separately registered current document
and pane after service replacement, subject to every original source/currentness
check. It starts with an empty reuse context. Durable metadata cannot resurrect
an old ChatContext or rebind an admitted/paused run. The capture-restart test stops
at Prepare; it is not evidence of provider admission or execution.

## Browser and host ownership

One module-memory document scope survives saved-chat navigation and reader
reconnection. Reload creates another registration. Tokens are never written to
localStorage, sessionStorage, saved chat records or prompt material. A pagehide
that enters the back/forward cache keeps the same document; an actual document
exit sends best-effort authenticated release. There is no beacon authentication
exception, TTL or heartbeat. Lost close acknowledgements may delay physical
reclamation; bounded capacity refuses new allocation without evicting live scope.

One pane scope belongs to an actual hook mount and is fixed to its first authoring
purpose. Deferred cleanup distinguishes React effect replay from a real unmount;
cleanup from an old mount cannot retire a new mount. Release sends no run cancel
and reader abort does not settle server work. An in-flight registration can be
released once its acknowledgement arrives, without starting a run.

Registration can reconcile one lost acknowledgement by replaying exactly the
same registration ID. Further uncertainty retains the original attempt. A lost
capture acknowledgement is read using the original request ID. An uncertain start
is discovered once and never automatically resent with another selector. A
context/service refusal does not authorize a replacement run.

The per-service registry bounds 32 documents, 16 panes per document, 256 context
entries, 512 live/retired registration records and 128 retained runs. Session or
policy end and service shutdown retire future reuse. Pending and retained owner
references postpone disposal until their actual release/settlement. Authoritative
saved-chat deletion is integrated with C1-D's owner lifecycle.

## Retained run ownership

After the durable existing-admission lookup and fresh source authorization, the
service retains the exact Chat and reserves one of the 128 run slots before
Prepare. Preparation, initial durable input and execution receive that same
pointer. A successful admission promotes the pending handle to the final run
binding, allowing only Prepare's finalized execution key and unit to change.
Document/pane release during preparation can retire membership while the pending
handle transfers atomically. A closed registry or invalid final binding refuses
promotion; an already admitted run is stopped without launching a provider. A
lost response still discovers that original durable admission.

Pending handles release on every preparation/admission failure. No adapter or
provider has been entered at that boundary, so these failures do not quarantine
the Chat. Invocation handles and independent run references have separate
lifetimes. Ordinary settled question/budget pauses retain their run reference;
continuation names the exact original record and cannot reconstruct a missing
in-memory owner from a browser selector or durable metadata. Runtime-scoped ask
parking additionally preserves P3's original Store, ID, lease, Gate and Calls,
with immutable generation callbacks and fresh ingress only after settlement.
Ordinary/source asks retain their existing classification and authorization;
this change does not activate E6 source capabilities.

Once an actual adapter is entered, Chat reuse and disposal require positive
settlement of the complete outer adapter/delegate, physical provider release,
callback joins, persisted receipts and drained notices. Nil return or an End
alone is insufficient. Unknown settlement quarantines the existing entry and
keeps a bounded run reference; future retention refuses instead of silently
allocating a replacement. Quarantine has no timeout, eviction or forced disposal
at shutdown. Model-free preparation failures and owners that never entered an
adapter release normally. Model/Runtime receipt failures remain visible through
the original journals and failure paths.

Document/pane release does not cancel a run. Session/policy end and service
shutdown retire reuse and settle idle known owners; active cancellation and
settlement still belong to the original owner. Successful authoritative saved
conversation deletion retires only removed saved-chat IDs under the authenticated
principal and pinned project, after workspace locks release. Failed CAS or
unrelated workspace writes do not retire contexts. Active deleted owners are
canceled; unknown owners remain quarantined. Source authorization remains
independent of context retirement.

The actual describe_runtime reply supplies the stable Runtime cache descriptor
when valid; unknown/development identity disables cross-turn Runtime reuse.
Process ownership uses its separate launch identity and is never inferred from
that cache descriptor. Root Chat equality checks do not replace the existing
narrow delegated child Context/Persistence binding.

## Qualification boundaries

Bounded host fixtures cover retain-before-Prepare, retirement during preparation,
capacity, promotion failure after durable admission, original-store continuation,
known versus unknown settlement and authoritative deletion. They do not qualify
provider release. Separate two-turn API/native authoring fixtures use the actual
closed production workflow and handwritten offline peers. The full saved-chat
registry two-turn API/native and actual retained Runtime approval fixtures remain
required final qualification. Their graph241/factory243 dependency gates must be
resolved; neither narrowing the registry nor substituting settlement qualifies
those paths. Focused source readiness does not waive independent review, race
coverage or the integrated release checklist.

Manual request/selector vectors live in
`internal/desk/testdata/chat-context-protocol.json`; Go and TypeScript readers
consume every row. Focused protocol fixtures exercise registration/release,
React replay/remount, uncertain delivery, capture recovery and exact admission
idempotency. They do not replace full production ownership qualification.
