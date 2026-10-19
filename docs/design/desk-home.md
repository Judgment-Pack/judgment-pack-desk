# Desk home: platform and Desk storage

Admin → General now offers **Desk home** with **Open folder** and **Move…**,
plus the selected Desk's name, folder and Open folder action. The move dialog
uses a native folder chooser, a read-only destination and **Use default
location**. Review is read-only; **Move Desk home** is a separate confirmation.
The shared settings controls, scrollable dialog body, footer actions and bottom
restart notice follow [Desk's design system](../design-system.md) and
[Linear's supporting-content hierarchy](https://linear.app/now/behind-the-latest-design-refresh).

## Layout

The Linux default is `$XDG_DATA_HOME/jpack-desk`, or
`~/.local/share/jpack-desk` when unset. macOS uses
`~/Library/Application Support/jpack-desk`. Native selection chooses a containing
folder; Desk appends `jpack-desk` and shows the complete destination.

```text
jpack-desk/
  _platform/
    desk.json                     installation preferences and startup selection
    desks.json                    stable identities and folder registry
    secrets/                      password, signing and connection credentials
    accounts/codex*/              private AI account profiles and their runtime
    connections/                  shared source connections
    services/gateway/             local Gateway state
    install/                      managed updater and its releases
    releases/                     existing standalone releases
    local-builds/                 existing development builds
    migrations/                   completed migration records
    ...                           other installation settings, original filenames
  unveil/
    jpack-desk.json
    jpack.json
    jpack.lock.json
    packs/ ...                    existing project files, including its repository
    .desk-private/
      project.json                preserved original project signing identity
      data/                       chats, drafts, briefs, attachments and reviews
      input-preparations/
      input-research/
      input-evaluations/
      input-handoffs/
      jobs/                       Runner database, retained releases and run chain
      audit/
      stamping/
  contractor-order-readiness-demo/
    ...
    .desk-private/desk.json        preserved managed Desk ID
  gtm-desk/
    ...
```

Friendly folder names are distinct from identity. Renaming or moving a folder
never changes a Desk ID, Runner workspace, history namespace or signing key.
New Desks receive a short ID suffix to handle equal names. Migration assigns
readable unique names to existing Desks; Windows device names are refused.
The original project remains selectable when another Desk is opened at startup.
Changing the startup selection does not move a folder.

An owner-only `desk-home.json` locator remains in the original platform config
location (normally `~/.config/jpack-desk`). It is needed to find a custom home
before settings are loaded. `JPACK_DESK_HOME` can explicitly select a home.
Invalid locators fail visibly instead of initializing another installation.
An explicit external project is refused after consolidation; importing another
project is separate work.

## Migration contract

The preview enumerates startup files, every saved Desk, active startup chat
namespace and attachments, startup Jobs, timestamp settings, account profiles,
connections, Gateway state, releases, development builds and a recognized managed
installation. No credentials or retained record contents are returned to the UI.

Confirmation binds the locations, directory identities, registry and retained
chat revision. It refuses occupied destinations, symlinks, unsafe parents,
changed review tokens and cross-filesystem moves. An existing home root must be
private (0700); confirmation can create an unused root privately. No recursive
copy/delete fallback is attempted.

Save edits and finish work in all windows. Pause automatic Job triggers and
finish or cancel pending source preparations. Every saved Desk must be available.
Current Desk processes hold a shared installation lease; the confirmed move
requires exclusive access. Close older Desk processes before migration as those
versions do not participate in that lease. Desk freezes new work, drains HTTP,
stops its workers and Runner, then performs the move outside the running server.

An owner-only journal outside the moved folders records no-replace renames and
compare-and-swap metadata writes. Restart resumes an interrupted transaction.
The original config location gets its locator last. Recovery refuses changed
metadata or destination identities and preserves the journal for diagnosis;
there is no automatic destructive rollback or merging of histories.

Retained sources, research records, chat payloads, signed records and run-chain
bytes are not rewritten. Mutable paths are updated in startup settings, project
identity, the exact Desk-managed AI sandbox config, and signing-key locations.
When a signing-key location changes, the current project lock's configuration
digest is updated only if it matched the previous configuration. Pack and graph
pins remain unchanged. An already stale lock is retained byte-for-byte and stays
stale after relocation; moving folders never approves its configuration. A move
that would accidentally make a stale configuration digest current is refused.

Runner relocation is an offline versioned command implemented in the companion
worktree. It checks workspace/owner, old root, paused triggers, idle runs and
source preparations, then changes only the input-root binding and appends an
idempotent relocation entry to database metadata. It does not rewrite retained
releases, decisions, signatures, or the run chain. Desk checks capability and the
exact Runner binary digest. A bundled Runner without this command blocks a move
of saved Jobs. A retained Runner executable that moves with the installation is
resolved at its destination and checked against its original digest before use.

The selected launch bundle must advertise layout version 1, and its Runner must
support workspace relocation. An older managed release blocks the move unless a
verified local bundle has been explicitly installed with `scripts/desk-local.py`
and the normal shell launcher points to it. Local builds have a distinct
`local-development` manifest, are retained by digest in `install/local-builds/`,
and do not overwrite stable releases or change `installation.json`. Every launch
checks the helper, manifest, complete file list, Gateway checksums and capabilities.
An invalid local selection fails closed instead of starting an older release.
Local launches hold the managed running lock and do not run release auto-updates.
The launcher is temporarily bridged to both reviewed locations before the install
rename, so an interrupted move can still start recovery. Custom launch scripts
are refused rather than rewritten heuristically. A final launcher uses an
explicit updater root at the new location.

## Deliberately retained data

Unknown legacy history namespaces and other workspaces remain in the migrated
platform folder; the move does not guess which Desk owns them. Unreferenced old
artifacts and duplicate runtime copies are retained. This change does not add
artifact garbage collection, runtime deduplication or full-Desk backup/restore.
The existing chat export remains a chat export. Those optimizations need their
own reference and retention policies before files can be deleted.

## Local end-to-end validation

Use disposable installations for confirmation and crash tests. On the real local
Desk, preview first; do not move the user's project as an automated test.

1. Save a chat and attachment. Create two Desks and complete a small Inputs
   preparation and Runner evaluation. Record identities and checksums of their
   retained records. Pause automatic triggers.
2. In Admin → General → Desk home → Move…, choose a containing folder. Confirm
   there is no free-text path input. Cancel the native chooser once, then select.
   Also try Use default location. Check the exact displayed path.
3. Choose Review move. Check every old/new folder pair and any blockers. Cancel
   and verify that no folders were moved or created by preview.
4. Review again and confirm Move Desk home. The bottom strip requests a restart.
   Desk stops; wait for its process to exit. Launch it again without the old
   project argument. Refreshing the browser alone does not restart the server.
5. Confirm the same password works, the saved AI connection remains available,
   all Desks are listed and histories/Inputs/Jobs still open. Confirm record
   checksums and signing identities. Run another evaluation in the disposable
   installation and check that the previous run IDs were not duplicated.
6. Create a new Desk, make it the startup Desk and restart. The original project
   must remain selectable. Move the complete home again; the selection, newly
   created Desk and all historical namespaces must survive.
7. Exercise refusal cases: occupied destination; another live Desk process;
   active research; unpaused trigger; pending source preparation; unsupported
   Runner or selected launch bundle; a move that would approve a stale lock. A refusal must
   not silently change those settings or run work.
8. At 1440, 390 and 320 pixels in both themes, check long paths, scrolling,
   keyboard focus, chooser cancellation and visible dialog footer actions.

Automated checks use temporary files and a controlled native-picker subprocess:

```sh
JPACK_BIN=/absolute/path/to/jpack \
JPACK_RUNNER_TEST_BIN=/absolute/path/to/pinned-jpack-runner \
JPACK_RUNNER_HOME_TEST_BIN=/absolute/path/to/local-relocation-runner \
go test ./...

npm test --prefix web -- --run src/admin/DeskHomeSettings.test.tsx
npm run build --prefix web
JPACK_BIN=/absolute/path/to/jpack \
JPS_HOME_BROWSER="$PWD/web/scripts/desk-home-browser.mjs" \
go test ./internal/desk -run '^TestDeskHomeBrowser$' -count=1 -v
```

The Go cases cover interrupted parent renames and locator publication, launcher
continuity, later whole-home moves, exact retained bytes, new Desk creation,
startup selection, stale metadata and real Runner reconciliation. Interactive
native chooser smoke tests still need the relevant Windows/WSL, macOS or Linux
desktop; the automated browser fixture does not open windows on the user's desktop.

## Local validation, 2026-10-12

The Desk full Go suite passed with the pinned Runtime and Runner plus the local
relocation companion for the migration integration. Final focused race tests
passed, including agent routing and runtime environment isolation when the
original project is opened beneath a different startup Desk. Runner relocation
race tests and its command tests passed. The frontend settings tests passed
(18 cases across the location and chat-storage controls), and the web build
passed. The browser fixture moved and reopened temporary installations via both
custom and default destinations with no page errors at the three viewport sizes.

The real-installation inventory is read-only. It found `unveil` and nine managed
Desks. Its two current blockers are the managed v0.12.0 installation, which does
not advertise home support, and GTM Desk's stale configuration lock. The local
Runner change is based on the installed v0.8.0 revision in an isolated worktree;
release component pins have not been changed. No user home migration, artifact
cleanup, pack review or release publication was performed.

## Local launcher follow-up, 2026-10-12

The initial preview correctly refused the old managed v0.12.0 launcher, but that
made local migration testing unavailable. `scripts/desk-local.py` supplies a
durable development launch without publishing a release. `install ROOT_BUNDLE`
validates and retains a complete locally built bundle under the explicit
`--root`; `check` is a read-only capability probe; `run -- [Desk flags]` launches
its saved components. The reviewed shell launcher must point to `desk-local.py`
with an explicit root. Its existing local-access choice is preserved.

Stable release files and managed update settings remain intact. To switch back
to managed updates after migration, first install a released Desk and Runner
that support this storage layout, then deliberately restore the managed launcher.
Do not point the moved home at an older Desk.

A stale lock now remains stale and byte-for-byte unchanged through the move.
This avoids requiring unrelated project approval merely to relocate storage.
The matching-lock path still updates only the configuration digest for a moved
signing-key path. Pack/graph pins are never re-resolved.

Validation for the local launcher follow-up passed: seven local-launcher cases,
13 existing managed-updater cases, all Desk home race tests including real
Runner migration with its executable inside the moved platform directory, and
browser move/reopen checks at custom/default locations and three viewport sizes.
The installed local preview reports 18 moves and zero blockers. Password sign-in
and the retained ChatGPT account were checked without inference requests. The
GTM lock, managed release metadata, and original Desk locations are unchanged.
The user must refresh the move review and explicitly confirm the real migration.
After it exits, `jpack-desk --port 5173` starts the selected local build through
the relocated launcher. No release was published.

## Confirmation wait recovery

The review, confirmation and status requests now have a 30-second client deadline,
covering session bootstrap and response-body reads. A timed-out confirmation is
reported as uncertain and exposes the existing Check move status action; it is
never automatically resubmitted. A stalled status check also returns control.
Confirmation synchronously rechecks the current chat snapshot instead of calling
a redundant flush on an already clean store. Server-side confirmation refuses
canceled requests before journaling and promptly refuses a concurrent confirmation.
The local log records confirmation entry, refusal and queueing without credentials.

The original browser stall was not reproduced: a captured local server stack had
no move request in progress and no journal had been written. Added tests cover
hung responses, hung status checks, unsaved chat races, canceled/concurrent
confirmations, and confirmation with multiple Desks and a real Runner. Eight
frontend tests, home race tests and the default/custom browser migration checks
passed; the web build passed. The recovery changes are installed in the local
bundle. User files remain at their original locations pending a fresh confirmation.

## Live blockers during move review

Move review now checks active work and Jobs triggers across every open Desk, using
bounded, read-only Runner requests. The existing blocker feedback disables the
confirmation button. An active trigger identifies its Desk, job and trigger name
and directs the user to Jobs → the job → Triggers. Confirmation repeats the check
because automation can be enabled after review. Neither check pauses automation.
A missing trigger state remains an unverifiable-state blocker.

The reported HTTP 409 was an intentional refusal: the Sales Deal Readiness demo
had one active file trigger. No migration journal or destination was created.
The earlier zero-blocker preview covered storage checks only; it did not establish
that Jobs triggers were paused. Regression coverage now includes an active trigger
in a different Desk from the selected one, clearing after a pause, reactivation
after review, unknown trigger state, and rendering a 409 as actionable feedback
without remaining in Working or treating a refusal as an uncertain submission.
