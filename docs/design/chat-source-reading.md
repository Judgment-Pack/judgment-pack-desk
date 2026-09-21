# Chat source reading and message details

The September 21, 2026 review found that the sent-context action inherited toolbar
padding and an end-aligned popup, while citations reused the page-selection form.
Both made long source material difficult to read beside an active conversation.

## Interactions

- **View message details** aligns with the message text. It opens the existing
  resizable bottom Details pane, displaying the saved user message and an
  expandable Exact sent context section. Copy preserves the submitted snapshot,
  including escapes. It never rebuilds that snapshot from current attachments.
- Document citations keep their quotation as ordinary selectable text and append
  numbered source markers. Numbers follow actual Markdown citation links within
  each response, including while an answer grows. Hover identifies document/page;
  click opens a bounded, read-only excerpt with a highlighted match and Close.
- Open source text moves from that popup to Details. The source reader shows the
  retained selected pages, highlights the quote, and offers the verified original
  for download. It is extracted text, not a rendered PDF. The reader title stays
  visible while its body scrolls. The right Assistant remains available.
- Pending attachments still use the attachment review form, with usable-page
  selection and explicit partial-extraction consent. Sent sources have no page
  checkboxes. Attached documents and Work use the shared disclosure treatment.
- Pack context uses the same bottom reading area. Selecting a pack rule replaces
  a temporary reader; opening another source replaces the current one. Closing
  restores focus to the initiating action. Route/chat changes release the reader.

## Verification boundary

Opening a source rechecks the held bytes against the current gateway pin and exact
record digest. A citation must match a usable selected page. Whitespace folding
maps back to offsets in the original text; it does not alter signed bytes. Failed
checks never expose source text as verified. Loading can be retried and is aborted
when the preview closes or its identity changes. No inspection starts an AI run.

“Quote found on page…” describes a text match. Receipt details are available in
Technical details and establish byte lineage, not truth or authority. Missing
content, truncation and OCR notices remain visible.

## Conversation styling

The composer uses one outer focus indicator while typing, with independent focus
indicators for its other controls. Mode/model triggers are quiet, truncated model
names have an overflow tooltip, and Send/Stop share stable icon-button geometry.
The message input uses body typography. Markdown headings use the existing type
scale and separators use theme colors. Code blocks retain fence-language labels,
Wrap and exact-text copying, with icon/check feedback that does not add a success
row. Existing following/reading-scroll and quiet-autosave behavior is preserved.

## References

[Linear's design refresh](https://linear.app/now/behind-the-latest-design-refresh)
informs quieter supporting controls and consistent placement. Its
[Agent Interaction Guidelines](https://linear.app/developers/aig) inform clear,
inspectable work and unobtrusive feedback. The specific citation marker, excerpt,
and bottom-reader choices are Desk design decisions. Popovers use
[Radix focus and dismissal behavior](https://www.radix-ui.com/primitives/docs/components/popover);
tooltips carry only brief supplemental information.

## Validation

Component checks cover whitespace/Unicode quote offsets, absent/unselected quotes,
verification failures and retry, aborting late results, exact submitted snapshots,
reader replacement, draft preservation and focus restoration. Browser checks use
synthetic chats and signed fixtures, with no private files or live model requests.

The browser sweep passed 24 source/message-detail layouts across all twelve
locales at 1440px and 320px, plus 1440×460, 800×700, 390×720 and the docked
Assistant. Message text and its details action shared the same left edge.
Typing/autosave, opening message details and reading-history updates measured
0px scroll/layout drift. Source headers stayed fixed during body scrolling.
