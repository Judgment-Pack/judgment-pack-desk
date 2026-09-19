# Chat reading and typing stability

The September 19, 2026 review reproduced three visible defects using synthetic
conversations. No private documents or live model requests were needed.

| Problem | Cause | Change |
| --- | --- | --- |
| Composer shifts while typing | A “Saving chat…” footnote mounts and disappears around every debounced save; measured movement was 26 px. | Save quietly. Keep a stable footer space. Actual save failures still show an alert and Retry saving. |
| Citations break the rhythm of a paragraph | Citation buttons use 13 px text and 12 px padding on each side. | Inherit the surrounding typography, remove padding, and use a subtle dotted underline. Hover identifies the document and page; clicking opens the verified page preview. |
| Source previews or code preferences reset | Inline Markdown renderer functions acquire new component identities on rerender. | Memoize renderers and completed messages; preserve an open citation and a code block's wrapping preference as a response grows. |
| Attached documents sit against the pane's left edge | They are outside the constrained message column. | Place transcript content, work summaries, and attachments in one centered reading column. |
| Reading space changes when scrolling upward | Jump to latest occupies layout space; baseline viewport movement was 14 px. | Float the control over the transcript. Follow content growth only while the reader is at the bottom. |
| Composer height lags behind wrapping or pane resizing | Textarea measurement happens after paint and only when text changes. | Measure before paint; remeasure on width/window changes and cap height so long drafts scroll inside the input. |

## Reference patterns

[Linear's design refresh](https://linear.app/now/behind-the-latest-design-refresh)
emphasizes predictable placement, less visual competition, and keeping the main
work prominent. Its [Agent Interaction Guidelines](https://linear.app/developers/aig)
call for unobtrusive feedback and visible agent state. Routine save activity
does not need to displace the conversation; failures and active work still do
need a clear indication.

[ChatGPT search](https://help.openai.com/en/articles/9237897-chatgpt-search) supports
inspectable citations and a source overview. [Claude Research](https://support.claude.com/en/articles/11088861-use-research-on-claude)
also provides citations for checking supporting material. Desk applies that
pattern to retained document pages, preserving its receipt and quote checks.

These are product patterns, not claims about the products' internal rendering
or scrolling implementation. The specific sizing and scroll behavior above are
Desk decisions based on the reproduced problems.

## PDF processing boundary

The default local bundle starts `adapter-document` without an OCR program. It
reads embedded PDF text and does not ask an LLM to extract it. It does not render
diagrams or page images. Image-only pages report `needs-ocr`; a separate gateway
can be configured with an OCR executable. Selected page text is sent with a
message to the configured AI model. The UI must explain both boundaries rather
than letting “local” imply that model inference also stays on the machine.

## Validation

Component coverage checks citation verification, preserving an open preview
while a response grows, code wrapping state, and refusal of executable HTML,
remote images, or unsafe links. Browser checks compare single-line typing and
autosave cycles, reading history, and long drafts in short and narrow windows.
After the fix, the browser measured 0 px movement during typing/autosave and
0 px viewport movement when Jump to latest appeared. Content growth followed
the bottom when requested and left history readers in place. A simulated failed
save showed its alert; retry preserved the draft. Checked viewports: 1440×900,
1440×460, 800×700, 390×720, and 320×640, plus Chinese/Korean landing-page input.
The PDF explanations are included in all twelve locale catalogs.
