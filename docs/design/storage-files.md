# Connected storage files

`Admin → Connections → <connected provider> → Manage files` opens
`/connections/<provider>/files`. The same action appears in the connection pane
opened from chat. Its presence depends on the installed Gateway catalog advertising
`files-list`; older Gateway bundles keep working without offering these controls.

The page follows Project files: browse on the left, editable detail on the right,
a shared resizable divider, and a single pane with a Files back button at narrow
widths. Search loads one bounded page; it does not load contents or append an
unbounded result list. Use a folder ID in Drive, a relative folder in a local
vault, or a literal prefix in S3. An empty location uses the connection's default
scope. New files keep the destination they were started in, even if the browser
search changes while an edit is open.

Drive searches its existing index within the files authorized for the app. S3
and local search match names/paths, not semantic relevance. A cursor can exist on
an empty filtered page. Narrow the folder/prefix for large stores. Local traversal
limits and Drive incomplete searches are disclosed instead of implying exhaustive
results. No background scan, model call or indexing service is started.

Opening a file loads at most 4 MiB. UTF-8 text is editable; binary files can be
downloaded and replaced with an upload. Google-native documents use their source
application. File content is text only in the editor, never rendered as active
HTML. New files and replacements first create a private Gateway plan and then
open a review dialog. The server holds the exact proposed bytes and base version. Every file selection
and new-file destination also carries the Gateway connection generation; changing
a connection cannot silently redirect an open edit to another account or folder.
The browser session retains only a plan ID for recovery, not credentials or file
payloads. Temporary buffers are not saved drafts.

Deletion always opens the shared styled dialog and requires the exact filename,
or full S3 object key. No browser `confirm`, blanket consent, recursive directory
delete or model-call commit exists. Existing grants are not broadened. S3 IAM must
separately allow PutObject/DeleteObject for writes; readonly credentials can still
browse/read. Drive uses the whole-Drive `drive` scope as of Gateway v0.7.0.
Its listings report `account-files`; search metadata is not model context.

A lost mutation response offers **Check status**, which asks only for the durable
Gateway state. It does not repeat a write. The Gateway claims a plan before sending
it, refuses stale revisions and never automatically retries a mutation. An
uncertain result must be inspected at the provider. A process crash with an
executing claim requires that inspection and a deliberate reconnect before new
changes. Close/reload is not evidence that a write failed. The latest plan alone
is retained; it is not a historical audit ledger. While a retained uncertain plan
is shown, this page blocks starting another change.

Local deletion retains bytes in `.jpack-trash`; updates preserve previous bytes
in `.jpack-history`. Both directories are hidden from browsing. There is no restore
or retention UI yet. Local revision checks cannot eliminate races with independent
external editors. Drive updates/deletes require a strong ETag; otherwise they are
refused. S3 uses conditional object operations; deleting an unversioned object can
be permanent. These limitations also appear in the Gateway contract at
`judgment-pack-gateway/docs/design/storage-files.md`.

The authenticated Desk relay performs no provider IO: the Gateway companion owns
credentials, traversal, version checks and mutations. Storage management does not
create verified evidence/action receipts and must not be added to the assistant's
tool registry. Runner source acquisition continues using the existing read path.

Review hardening: recovery hints are scoped to each Desk and provider and checked
before listing. Completed changes release the editor; unresolved changes remain
blocked even when listing or status calls fail. Uploaded UTF-8 BOM bytes and
unchanged source line endings are retained. Bounded metadata replies allow
512 KiB for a page of long JSON-escaped keys; read replies remain capped at 6 MiB.
