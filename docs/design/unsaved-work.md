# Unsaved work

A single shell registry tracks buffers, not their contents. The shell owns one
router blocker, one typed confirmation dialog, and the browser-title marker.
Each form compares against its saved baseline; no global boolean can be cleared
by saving an unrelated editor. An accepted write clears its registration before
a deliberate redirect. Failed writes keep the buffer and its warning.

## Covered workflows

- Pack forms and JSON, including operands that are not valid JSON yet. Existing
  Save and return remains available; discard requires the pack name.
- Project file switching, discard, reload, and route exits.
- Test cases and proposed-case edits, including partial field input. Deleting a
  case requires its name and retains run history.
- Job creation, mapped input configuration, per-run inputs, and trigger editing.
- Project and assistant settings, document processing, sign-in configuration,
  folder edits, and storage migration/restore setup.
- Chat writes waiting to persist contribute to the title marker. Chat state
  survives in-app navigation, so switching pages does not discard it.

Retained tabs and collapsed panes keep their buffers and do not prompt merely
for hiding them. Query navigation is guarded where it actually removes a form
(such as closing job run inputs or leaving trigger editing). Search, filters,
layout preferences and already saved chat drafts are not unsaved document work.
Reload actions that preserve entered fields remain prompt-free.

## Confirmation

Named work uses the displayed name; unnamed or multiple editors use localized
“Yes”. The input must match before Discard becomes available. Keep editing and
Escape cancel, retain values and restore focus. Saving a draft never requires
the destructive confirmation. Save and leave is offered only when it can save
all affected buffers; a failed save leaves the dialog open with its error.

Browser reload, tab close and cross-origin navigation use beforeunload. Browsers
do not allow custom styled dialogs or typed confirmation for those exits.

## Job drafts

Job drafts live in `.desk/job-drafts/<id>.json` in the current project. The existing
file API supplies authentication, containment, atomic writes, compare-and-swap
digests and read-back. The client verifies the returned content before marking a
draft clean. A stale write is reported and never silently overwritten.

Drafts contain a name, pack selection, manual sample inputs, mapping text, case
input text and applied trigger configuration. They can precede release validation.
Incomplete source editor fields must be applied before saving; partial local
field values cannot silently disappear into a saved draft. Picker grants, source
snapshots, verified previews and release approvals are excluded. Connection
credentials remain in their existing credential storage. Explicit values typed
into configuration remain project data, so protect project backups accordingly.

Resuming always begins at the Job step and requires a new input preview and
release check. Successful job creation marks its saved draft as consumed.
Drafts use no scheduler and cannot execute. They are project files, separate
from operational runner records and chat backups.

## Verification

Guard tests cover multiple dirty editors, names versus Yes, cancellation, focus,
failed draft saving, successful navigation, pending writes, beforeunload, and
retained query navigation. Draft tests cover filesystem paths, malformed records,
optimistic concurrency, read-back verification and exclusion of runtime artifacts.
Browser smoke checks exercise job draft save/resume and typed discard with file
writes intercepted, leaving real user data untouched.
