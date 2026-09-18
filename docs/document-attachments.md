# Document attachments

Desk integrates the gateway document adapter at gateway main
`f9f05cc72f85ddda32f994b3b3eb2b75dcfc85ab` (PRs #134–#137).
The v1 contract is `docs/design/attachments.md` in that repository.
No runtime or JPS format change is involved.

## Setup and use

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

2. Open **Admin → Documents**. Set the gateway URL, authority, signing **public**
   key (pinned from `gateway keygen`), source name, and enable document uploads.
   Save. These personal settings cannot be supplied by a project file. Existing
   research search/read settings and the Assistant configuration are preserved.
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

Google Drive/OAuth is not implemented by this connection. Desk does not display
an enabled cloud provider that the gateway cannot serve.

## Verification and citations

One upload uses a fresh gateway session, then seals it. Before a document's text
can be used, Desk verifies the session under the **currently configured** public
key and authority, checks the command acquisition/source and v3 argument
commitment, and matches the record's name, media type, size and original digest
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
base64 once plus the exact acquire response, its argument salt, and the session
seal. The request is reproducible from the original and fixed `ocr: auto` option.

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
  It must fit base64 expansion plus 4096 bytes. Without documents configuration,
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
