# Chat source reading and message details

The September 21, 2026 review found that the sent-context action inherited toolbar
padding and an end-aligned popup, while citations reused the page-selection form.
Both made long source material difficult to read beside an active conversation.

## Interactions

- **View message details** aligns with the message text. It opens the existing
  resizable right Details pane, displaying the saved user message and an
  expandable Exact sent context section. Copy preserves the submitted snapshot,
  including escapes. It never rebuilds that snapshot from current attachments.
- Document citations keep their quotation as ordinary selectable text and append
  numbered source markers. Numbers follow actual Markdown citation links within
  each response, including while an answer grows. Hover identifies document/page;
  click opens a bounded, read-only excerpt with a highlighted match and Close.
- Open source text moves from that popup to Details. The source reader shows the
  retained selected pages, highlights the quote, and offers the verified original
  for download. It is extracted text, not a rendered PDF. The reader title stays
  visible while its body scrolls. Assistant and Details share one full-height right pane; the tool rail switches between them.
- Pending attachments still use the attachment review form, with usable-page
  selection and explicit partial-extraction consent. Sent sources have no page
  checkboxes. Attached documents and Work use the shared disclosure treatment.
- Pack context uses the same right reading area. Selecting a pack rule replaces
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
and right-reader choices are Desk design decisions. Popovers use
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

## Message ownership (September 24, 2026)

- Submitted attachments retain their exact document digest and selected pages on
  the user message. Text files retain their submitted text. Pending files stay
  inside the composer; sent chips open the existing read-only Details reader.
- Each engine response has a stable identity and an owning assistant message.
  Its collapsed Work summary stores invocation names, outcomes and notices, without
  storing tool arguments/results in that summary. Parallel invocations remain distinct.
  Interrupted work is kept beside its preceding turn when no prose was produced.
- Sources contains the documents actually returned by read tools or explicitly
  cited by that response, including cached reads and selected pages. Research
  source IDs and website discovery references use their existing readers. A
  discovery map is distinguished from pages whose text was read.
- A draft candidate records which response produced it. The historical title and
  revision remain there. Open draft becomes quiet Open when the draft is visible;
  older revision references explicitly open the latest draft. Finalization stays
  in the pack header. This does not add a historical-revision restore operation.
- One live status and its retry actions sit immediately above the composer,
  outside the scrolling transcript. Finished progress disappears.
- Older context blocks and exact citation identities recover ownership where
  possible. Unmatched files remain in the conversation Sources control in the
  chat header, never guessed onto the latest answer. Missing historical work is
  not reconstructed from prose.
- Conversation ownership is saved in chat checkpoints and excluded from standalone
  pack artifacts. Pack Sources remains the complete collection. Chat-only work
  updates do not advance the artifact's concurrency generation.


## Streaming feedback and open questions (September 24, 2026)

Running status uses a small activity dot and a gentle change between existing
text-color tokens. It changes no geometry and produces no repeated live-region
updates. Reduced-motion settings disable both animations. Errors and settled
states never animate.

The proposal's unknowns are shown as a collapsed **Assumptions and open questions**
list with an explanation of their meaning. The original saved text and message
details remain available. These are unresolved points from the supplied material.

The stream no longer cuts off at the first fence. Ordinary code and following
Markdown render during generation. Explanatory JSON is requested with a
`json example` fence; its displayed language stays JSON. Legacy bare/json root
objects remain buffered until the JSON value is distinguishable from a proposal,
then display without waiting for the answer to finish. Actual proposal envelopes
stay hidden. Fence markers, indentation, nested JSON keys and escaped root keys
are handled consistently with the completed-answer parser.

A temporary message ID connects streamed prose to the final saved message. This
keeps code DOM, wrapping choices and readers mounted through completion; the
transient streaming state is not saved in checkpoints or pack artifacts.

## Refresh review

Verified readers expose Source refresh review. Web refresh reacquires the bound
URL; connected sources require a new selection grant for the same resource.
Fresh receipts/attachments are immutable alternatives to the original evidence.
The project-scoped source-reviews index retains their relationship and review
state through the existing private storage/backup boundary. Refresh and marking
a review never rewrite earlier messages or citations. Test-case source replacement
is an explicit unsaved edit, followed by Save and an explicit rerun.

See [workflow review](../reviews/desk-workflow-features-20260924.md) for limits,
known impact scope and verification evidence.
