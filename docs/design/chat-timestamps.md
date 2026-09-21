# Chat timestamps

Message captions show a small localized time beside the speaker. Supporting
information uses the existing secondary text style without another row or icon.
The shared tooltip reveals the full date, seconds and UTC offset on hover or
keyboard focus. Clicking, tapping or activating the time opens Message details
in the resizable bottom pane. This replaces the extra attachment-only details
link and makes metadata available for ordinary messages too.

Message details labels user submissions **Sent** and assistant output or Desk
notes **Recorded**. It includes interrupted status where recorded. Exact sent
context is available only when the message has a saved input snapshot. Source
citations and the read-only source reader retain their existing behavior.

## Calendar and accessibility behavior

- Short times follow the app's language and the operating system's timezone.
  System language preference preserves regional conventions such as en-GB's
  24-hour clock. All 12 supported language catalogs include metadata labels.
- Dates appear at local calendar-day boundaries in multi-day conversations.
  Single-day chats have no date separator. There are no live relative-time
  timers. Regional/timezone settings are reread on return to the application.
- Full times include a UTC offset to distinguish repeated hours at the autumn
  daylight-saving transition. Calendar boundaries use local dates rather than
  UTC date strings or elapsed 24-hour periods.
- The timestamp is a real button with a semantic `time` element, a descriptive
  accessible name, a keyboard focus indication, and shared tooltip behavior.
  Exact time is available through click/tap, without depending on hover.
- The Details reader preserves the unsent composer and reading position, and
  Escape returns focus to the timestamp. Rendering does not sort messages.

## Persistence semantics

`Turn.at` remains the original application-generated instant. User turns are
stamped when accepted by the run; assistant turns are stamped when a completed
message event arrives, or when an interrupted partial response is recorded.
Streaming text does not gain an invented generation-start timestamp. The raw
stored value survives reload, export and restore.

New chats gain `createdAt` when their first model submission is accepted by the
store, immediately before dispatch to the run. Home visits, typing and blocked
submissions remain unsaved drafts without a creation timestamp. Later messages,
renames, pinning and archiving do not change `createdAt`. Existing history
without this field stays without it, including when continued; the application
does not manufacture a historical creation time. `updatedAt` continues to track
recorded message activity, not metadata edits.

A present `createdAt` must identify a valid explicit instant. Invalid legacy
message timestamp strings remain loadable and unchanged, display “Time
unavailable,” and still allow Message details to open. Equal times and clock
regressions preserve stored sequence. Neither timestamp is a unique message ID.

These are local application clock values, not verified gateway receipt times.
Run-start/end diagnostics and execution duration would require separate fields;
the gap between a question and response is not a model execution duration.

## Design references

- [Linear's design refresh](https://linear.app/now/behind-the-latest-design-refresh):
  supporting information should remain quiet and consistently placed. Linear
  does not prescribe this particular chat timestamp component.
- [Atlassian date/time guidance](https://atlassian.design/foundations/content/date-time):
  localized short times and access to precise timestamps.
- [WAI tooltip pattern](https://www.w3.org/WAI/ARIA/apg/patterns/tooltip/): hover and
  keyboard focus disclosure with Escape dismissal; click/tap Details provides
  the alternative for interaction without hover.
