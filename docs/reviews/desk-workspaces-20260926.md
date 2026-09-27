# Desk workspace and Jobs stabilization

This review covers the accumulated named-desk, unsaved-work, project-files,
branding, input-mapping and durable Jobs changes. The work is split between
Desk (UI, scoping and local companion authority), Runner (durable execution),
and Runtime (acceptance of an empty initial project).

## Findings addressed

- Named desks inherited the startup desk's Google pull subscriptions. Because
  unmatched signals are acknowledged by Runner, another desk could consume an
  occurrence. Each installed cloud connection now has an optional desk selector;
  unscoped connections belong only to the startup desk, and duplicate subscription
  assignments are refused. Shared Gateway operations do not consume queues and
  retain their installation scope.
- The file browser and editor scroll containers did not establish containing
  blocks. They now position their descendants, and border declarations follow the
  shared token convention.
- The shared logo component lacked its own style module. Its image presentation
  now follows the same component boundary as other UI primitives.
- Eleven mutation anchors still referenced retired controls. They now target the
  desk-name link, shared dirty-state guard, retained file editor and scoped relay.
- CI pinned companions that predated durable triggers and empty initial projects.
  The integration job now builds the exact tested revisions.
- The browser layout gate omitted the Runs route, expected the previous footer
  gap, and tried to open nested input JSON before its parent disclosure. It now
  checks the centered footer band and follows the current wizard.
- Documentation still described cloud acquisition as future work and drafts as a
  separate section. It now describes installed cloud connections and table rows.

## Verification scope

Local checks cover Runtime tests and conformance, Runner race tests against the
real Runtime, Desk Go race tests with both companions, TypeScript, translation
coverage, source-message audit, production build and mutation needle validation.
The initial full web run found five style-guard failures; the corrected styles and
file/identity flows passed a focused 619-test run. The full CI run passed all 238 frontend test files (4,457 tests passed, one skipped).
An isolated browser fixture passed named desk switching, typed discard, draft
save/reload/resume, logo and favicon persistence, and separation of files,
conversations and jobs. Two mapped local files supplied facts and evidence. After
one file changed, a one-time schedule completed with the browser closed, producing
a different decision and preserving the new inputs and lineage. The run and brief
survived a server restart and rendered in a fresh browser. Brief text was synthetic;
this check exercised shared persistence and rendering without an AI provider call.
The responsive layout sweep and final CI outcomes are recorded on the pull request.

The Google relay-to-Runner path is tested with a local cloud transport. No cloud
account is provisioned, and this is not a claim of a deployed Google scheduler.
Legacy storage migration and scheduled discovery of incremental object-storage
files remain separate work. The local host and Runner must remain running.
