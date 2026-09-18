# Document attachments

Desk pins gateway `e649f098b1143de69fb5bc8558f2686a4934acd9`
(Drive implementation PR #139, pending independent review).
The v1 contract is `docs/design/attachments.md` in that repository.
No runtime or JPS format change is involved.

## PDF setup and use

The complete Desk bundle starts local processing automatically on Linux/macOS.
The manual gateway steps below apply to explicitly managed external deployments.
Local Drive setup is described separately below.

1. Install/build `adapter-document` from the gateway adapters module. Start the
   gateway with v3 receipts and an operator-configured **command** source:

   ```sh
   gateway serve ./store ./gateway.seed gateway:local ./registry.jsonl \
     --receipt-version 3 --max-request 33554432 \
     --source-timeout documents=40 \
     --source documents='adapter-document --max-bytes 16777216 --max-output 8388608 --timeout 30s' \
     --source-max-output 8388608
   ```

   Keep the source without `--source-shape`; the receipt shape is `command`.
   Configure the gateway's least-privileged source identity when your installation
   uses separate source accounts. OCR is an optional operator-supplied program,
   not bundled with Desk or the adapter. The gateway's default 1 MiB request limit
   is too small for the default Desk PDF allowance; configure all layers together.

2. Open **Admin → Connections**. Set up the shared **Gateway** with its URL,
   identity (authority), and verification **public** key from `gateway keygen`.
   Then manage **PDF processing** to enable it and choose the maximum file size.
   Source name and request/response limits are under **Advanced settings**. Limits
   are displayed in MiB and stored as exact bytes. These personal settings cannot
   be supplied by a project file. Research sources and Assistant settings are
   preserved. Gateway configuration alone does not enable PDF processing.

   Turning PDF processing off saves `research.documents.enabled: false` and
   retains its source and limits. Previously saved documents remain available
   under the configured verification identity; new PDF originals are refused by
   both the browser and server. Re-enabling restores the same limits. Legacy
   document configurations without `enabled` remain enabled; absent/null
   `documents` keeps processing off. Old `/admin#documents` links still work.

   **Configured** means settings were saved, not that a live connection was tested.
   Availability is checked when used. Changing the gateway identity/key may
   prevent existing documents from verifying. Explicit external gateways do not
   silently activate personal local Drive connections.
   These settings are personal preferences, not organization policy controls.
3. In home chat or a pack's Assistant, use **+ → Upload files**, or drop a PDF.
   Local TXT/Markdown/JSON/CSV attachments still work without a gateway (200 KB each).
4. Open an attached PDF to inspect extracted page text, select pages, and download
   its original. Partial extraction requires explicit consent. Blank pages,
   unavailable OCR, extraction failures and missing content are distinguished.
   Any page with unmapped glyphs is withheld conservatively. Failed extraction
   supplies no text. Desk supports up to 500 listed pages per PDF.
5. Send after reviewing. Only selected readable pages enter model context;
   original binary/base64 data and receipt internals never do. The combined
   serialized attachment context is bounded to 800,000 UTF-8 bytes per message.
   Messages can contain at most four pending attachments.

## Personal Google Drive

The complete bundle includes gateway-owned connection and retrieval companions.
In Admin → Connections, choose Google Drive → Set up. Register a Google Cloud
Desktop app with Drive and Picker APIs enabled and choose its downloaded
credentials JSON. The gateway stores the registration privately. Then choose
Manage → Connect, or Google Drive in the chat attachment menu, and complete
Google consent. Select individual files through Google's browser picker.

Only selected-file access is requested (`drive.file`). The gateway handles tokens,
refresh, revocation and retrieval. Desk receives account display information and
short-lived file grants, never Google access/refresh tokens. Disconnect removes
local access and attempts upstream revocation; a failed revocation is reported.
An in-flight read may finish after disconnect.

PDF, TXT, Markdown, JSON and CSV are supported. Google Docs, Sheets and Slides
are exported as PDFs. The source limit is 4 MiB per original/export, four pending
attachments per message, and 16 MiB per extraction response. Desk's configured
file limit can be lower. New Drive attachments require document processing enabled.
Page preview, partial-extraction consent and citations are the same as for uploads.

These are personal OS-account connections on Linux/macOS. Shared enterprise
connection identity and policy enforcement are not implemented. See
[ADR 0006](adr/0006-gateway-drive-connections.md). Live Google consent has not yet
been validated with a registered application; automated tests use a fake provider.

## Verification and citations

One upload uses a fresh gateway session, then seals it. Before a document's text
can be used, Desk verifies the session under the **currently configured** public
key and authority, checks the acquisition/source (command for uploads, HTTP for
Drive) and v3 argument commitment, and matches the record's name, media type, size and original digest
to the retained bytes. Saved flags never confer trust: opening a preview and
preparing each send repeat verification from held bytes. Pin changes therefore
withhold old documents until the trusted pin is restored or documents are
acquired under the new gateway.

The consumer tolerates additional record members at every level. Unsupported
versions, unknown states/codes, missing required values, and inconsistent page
counts/statuses/text are refused. Stored normalized text is never normalized
again. Unicode scalar counts, not UTF-16 string length, validate `chars`.

A citation identifies the document object, signed extraction digest and page.
The assistant is told to use an exact page quote as a Markdown link label. When
opened, Desk verifies the retained record and checks that quote against the
selected page, exactly or with whitespace folded. A fabricated or stale quote
is refused. This verifies lineage and quotation, not factual accuracy,
completeness, legal authority, or the safety of following document instructions.
Attachments are explicitly marked untrusted reference material in model input.

## Private storage and limits

Originals and acquisition packets are separate objects beside private
conversations. Chat JSON and the unsent home draft hold small references; visiting
home still does not create a conversation. Each object contains the original
base64 plus the exact acquire response, its argument salt, and the session
seal. Drive responses also retain the signed original inline, so those objects
contain a second serialized copy within the same storage bound. Upload arguments
are reproduced from the original and fixed `ocr: auto` option; Drive arguments
are retained selected-file grants, which cannot be replayed for another acquisition.

Object names are opaque UUIDs in the server-resolved project-history namespace.
Paths cannot be supplied by callers. Reads/writes use existing session/origin
checks, owner-only file custody, symlink refusals, storage locks and atomic writes.
Uploads first retain the original; completed acquisition packets and originals
cannot be replaced through the API. Storage migration and backup/restore include
these objects and preserve their references, including explicit project relinking.
Chat export includes referenced originals and packets; ordinary text-only exports
retain their previous format.

- Original limit: 16 MiB hard ceiling; configurable lower in Admin.
- Default relay request limit with documents enabled: 32 MiB; ceiling 64 MiB.
  It must fit base64 expansion plus 4096 bytes. With documents absent or disabled,
  and for seal/registry requests, the existing 1 MiB limit remains.
- Default extraction response limit: 8 MiB; ceiling 16 MiB. Registry read: 4 MiB.
- Retained object: at most 64 MiB. Private upload quota: 1 GiB; file count: 4096.
- Storage backup and individual chat export: 128 MiB. These bounds fail explicitly;
  no truncated backup is published. A large store may exceed the backup limit.
- Up to 256 retained document references per chat.

Removing an attachment from a message, deleting a chat, or canceling an upload
**does not erase the retained original**. Unsent/failed uploads remain in private
storage and backups, bounded by the quota. Automatic expiry and a storage cleanup
UI are separate work; there is no silent deletion policy in this release.

Cancel and navigation stop Desk's wait and discard late results. The gateway
may finish and mint a receipt after caller cancellation. Desk never claims it
killed upstream work and never automatically retries an acquisition (a retry
would mint another receipt).

## Gmail email attachments

Admin → Connections configures Gmail alongside Drive through the gateway-owned
connection companion. The chat + menu opens a searchable email picker. Up to four
selected emails become retained, verified plain-text exports, reusing document
preview, citations and chat storage. Search previews are not sent to the assistant.
Separate mail attachments are excluded from this version. Provider tokens stay in
the gateway; Desk stores exported text and signed acquisition proof.

The fixed Google scope is Gmail read-only, which grants mailbox-wide reading at
Google. User selection determines what is attached to chat, not Google's scope.
Operator Desktop app registration must enable Gmail API. Google revocation applies
to a Cloud project, so disconnecting can also revoke other Google connections under
that project; Admin explains this. Shared enterprise identity/policy remains future
work, and explicit external gateway settings do not activate a personal fallback.

Gmail uses its own companion process and custody namespace, with the same private
root, expiry, cancellation and single-use grants as Drive. Acquisition is constrained
to the current managed local gateway; Gmail search/selection also refuses disabled
document processing. Original export and proof are checked before supplying any page
to a model. Live Google authorization/retrieval remains unvalidated until an operator
supplies registration and consent. The current installation is not modified by this PR.
