# Gateway document integration review

Reviewed against gateway `f9f05cc72f85ddda32f994b3b3eb2b75dcfc85ab`, including
merged PRs #134–#137. Runtime and JPS contract files are unchanged.

## Behavior and custody

- PDF uploads use an explicit personal Admin → Documents connection. Text files
  retain their existing local path. The composer uses the shared Radix menu.
- Originals are retained before acquisition in private chat storage. History and
  unsent drafts carry references. No empty conversation is created by an upload.
- Every preview/send verifies the original digest, exact command arguments,
  signed v3 receipt, current personal signer pin, session seal and v1 page record.
- Only selected readable pages enter model context. Partial extraction needs
  consent; failed pages and pages with unmapped glyphs supply no text.
- Citation previews verify the signed record identity, selected page and quoted
  text. Reusing a document preserves earlier turns' cited pages.
- Cancel, chat switch and unmount discard late completions. There is no automatic
  acquisition retry and no claim that cancel kills upstream work.
- Conditional original/proof writes refuse mutation. Backups, restores, migration
  and chat exports retain original bytes and acquisition packets. Quotas leave
  room for chat bookkeeping; project storage totals include documents.

## Checks

- Full Go suite and `go vet ./...` passed; targeted storage checks repeated after
  the final project-byte accounting change.
- Web: 155 test files; 3,547 passed, one existing skip. TypeScript and production
  build passed. The existing large-chunk build advisory remains.
- Localization: 2,042 messages covered in each of 11 non-English catalogs;
  no missing strings or placeholder mismatches. New copy was translated manually.
- Existing mutation needle guards: 926 valid, zero invalid. This is a needle
  applicability check, not a claim of a new full mutation campaign.
- Real gateway + document adapter browser smoke used synthetic upstream fixtures,
  an isolated private store and a test-only signing identity. No model/API call.
  Normal PDF verified complete; mixed PDF required consent and survived reload.
  Encrypted and malformed PDFs withheld all pages. Scanned PDF reported missing
  OCR and offered no usable pages.
- Widths 1440, 768 and 390 had no horizontal overflow. German, Japanese and
  Cantonese Admin views were also checked at 390px. No browser page errors.

Evidence: [normal preview](document-attachments/preview.png),
[partial preview](document-attachments/partial.png),
[browser results](document-attachments/browser.json),
[edge and locale results](document-attachments/edges.json).

## Explicit limits

Google Drive/OAuth and an original-file cleanup UI are not included. Removing an
attachment or chat does not erase its retained original; canceled and unsent
uploads also remain, bounded by private storage quotas. OCR must be configured
on the gateway. Read the [operator setup and limits](../document-attachments.md).
