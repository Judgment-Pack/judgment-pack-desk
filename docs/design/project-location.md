# Project location and retained history (#388)

The current Admin UI uses the consolidated [Desk home flow](desk-home.md),
including native folder selection and moves of every registered Desk. The
startup-only API and implementation below remain as the legacy project-move
contract; its separate Runner restriction does not describe Desk home relocation.

Admin → General → Project location shows the **Current Desk folder** from the
selected Desk's configuration and the **Startup project folder** from the
installation. Only the startup row offers Move to…, even when a named Desk is
selected. It reuses SettingsSection, CardField, Button, FieldGroup,
Dialog and the bottom status strip. The move has two steps: Review move, then
Move project. No filesystem move occurs during review. The last action stays in
the dialog footer; long paths wrap and the body scrolls on narrow screens.

## Native folder selection

Move startup project uses **Choose folder…**, with a folder icon, instead of a
free-text path. Select a containing folder; Desk keeps the startup project's
folder name and displays the full resulting destination as read-only text.
Selection alone does not create or move anything. Review move validates the
exact destination; Move project remains the separate confirmation. An occupied
project folder is refused rather than merged or overwritten.

The authenticated, loopback-only `POST /api/project-location/choose-folder`
endpoint opens one chooser at a time on the computer running Desk. It is always
installation-scoped, even when a managed Desk is selected. Cancellation preserves
any previous choice. Closing the move dialog cancels the request. The UI shows
Choosing… while the native window is open; failures stay beside the actions.

Adapters use Windows Forms through PowerShell and `wslpath` on WSL, Zenity or
KDialog on Linux desktops, and AppleScript's choose folder on macOS. Unsupported
or headless sessions receive a clear refusal and can use the default-location
shortcut. Paths are passed as process arguments or encoded data, never shell
source. Browser directory uploads are not used because they do not supply a
trusted server filesystem destination. The move's existing same-filesystem and
ownership checks remain authoritative, including for Windows-drive selections.

References: [Windows folder chooser](https://learn.microsoft.com/en-us/dotnet/api/system.windows.forms.folderbrowserdialog),
[macOS folder chooser](https://developer.apple.com/library/archive/documentation/LanguagesUtilities/Conceptual/MacAutomationScriptingGuide/PromptforaFileorFolder.html).

## Default locations

Move startup project offers **Use default location** beside New folder. It only
fills the server-resolved path; Review move and Move project remain separate.
The default is the platform application-data directory plus
`jpack-desk/projects/<startup-project-folder-name>` (on Linux,
`$XDG_DATA_HOME/jpack-desk/projects/<name>` or
`~/.local/share/jpack-desk/projects/<name>`). Existing destinations are refused;
the shortcut never invents a suffixed directory or overwrites one.

Review does not create default folders. For this exact server default only,
review pins the nearest existing, operator-owned parent. Confirmation rechecks
its identity and creates missing parents privately through pinned handles. A
parent that appears after review invalidates the review. A failed confirmation
can leave empty default parent folders; it does not replace an existing folder.
Custom destinations still require an existing parent.

Shared chat storage now defaults to a dedicated `history` child of the platform
application-data directory. Releases and local builds can coexist in the parent.
Existing storage pointers (including the old application-data root and legacy
config-folder storage) remain authoritative. No history is moved automatically.
Managed Desks keep their existing relative `.desk-private/data` location.
A project move still refuses overlap with the active chat store; a legacy store
using the whole application-data directory must be relocated separately before
that root can also contain projects.

The rows and dialog reuse the existing neutral settings controls and footer
placement. Supporting scope text follows the hierarchy in
[Linear's design refresh](https://linear.app/now/behind-the-latest-design-refresh)
and [Desk's design system](../design-system.md).

## What follows a move

A recognized same-filesystem manual move links retained history before updating
`.desk-private/project.json`. An owner-confirmed unresolved **moved** identity does
the same. **Copy** keeps separate history. Device/inode evidence, the former
folder checks, and the existing identity confirmation tokens retain their roles.

The binding preserves chats, draft packs, briefs, test results, source reviews
and retained attachments. Their bytes do not change. If there are sibling records
without chats, an empty chat anchor is created at the original namespace. A
repeated move follows the existing binding. A retry after binding but before
identity publication is idempotent. Destination history is never combined or
hidden, including destination records without a chat file. A conflict preserves
the former path in the identity and blocks history access until resolved/restarted.

Inputs and decision trails already inside the project move with the directory.
Signing identities, signing keys and decision records are not rewritten.
Runner jobs use a separate path-scoped workspace and key. The Admin move refuses
startup projects with Runner jobs; moving that workspace is a separate contract.
Named desks remain in their managed locations.

## Transaction and recovery

The confirmed move holds the project-directory lock, excludes in-process project
requests and preparation workers, and takes the shared private-data work lock.
Active MCP requests and scheduled stamping block the move. Idle sockets stop at
commit. New project work is refused after rename until restart. Managed desks
remain separately routed. Existing chat save/dirty/run guards also block the UI.

An existing destination parent must be owned by the operator and not be writable
by others. Only the server-resolved default supports creating missing parents
after confirmation. It must be separate from the project, installation data and chat data.
A preview token binds the paths, folder identities, project identity, project
configuration, installation settings and history-binding revision. Confirmation
rechecks them. Native no-replace rename (Linux/macOS) refuses a destination that
appears after review. Cross-device moves and unsupported platforms fail closed.

An owner-only installation `project-move.json` retains the original path, new
path, folder/project identities and original configuration revision before rename.
After rename, Desk binds history, publishes the identity's new path, and updates
only `desk.json`'s `project.file`. Unrelated settings are preserved. If the project
has no `jpack-desk.json`, the move creates the minimal versioned settings file so
an argument-less launch can open it. Existing configuration is not replaced.

A crash before rename leaves the original project. Startup there clears the unused
intent. After rename, an argument-less launch can follow the intent only after
validating the exact folder and project identity, carrying that folder identity
through the existing pinned launch check. Startup completes pending history and
configuration work. A later installation-setting edit is never overwritten;
recovery reports the conflict and retains the intent. Do not delete it to hide a
failure. Open the new folder explicitly and inspect the startup settings.

No automatic restart or move retry occurs. If an HTTP result is lost, the dialog
requires a location read before a new review. A landed-but-incomplete move returns
the actual new location with recovery instructions. The status strip prioritizes
**Restart required · Project moved** over reconnect notices. Refreshing the page
is not a server restart. A launch with an explicit old folder must be changed to
the new folder; a launch without a folder uses the saved startup setting.

## Local end-to-end checks

Use disposable projects for filesystem tests. Do not move an active user project
as part of automated validation.

1. Start Desk on a temporary project; save a chat, draft and brief. Record their
   private-storage filenames and checksums.
2. In Admin → General, compare Current Desk folder and Startup project folder.
   Choose Move to… → Choose folder… and select a containing folder on the same
   filesystem. Check the read-only destination, then choose Review move. Confirm both paths. Cancel once and verify
   the original folder still exists and no destination was created.
3. Review again and confirm Move project. The source name disappears, the folder
   appears at the destination, and the bottom strip requests a restart. Project
   writes are refused. Check that the startup setting names the new project's
   `jpack-desk.json` and the private history payload checksums are unchanged.
4. Stop Desk. Start it without a project argument using the same installation
   config. Confirm the new project opens, history is available, and the move
   action is available again. Repeat the move to a third folder and check history.
5. Separately, stop Desk and rename an identity-bearing project manually on the
   same filesystem. Start it at the new path. History must open without manual
   relinking. Repeat with only a retained draft and no saved chats.
6. Create destination history before a move. The move/relink must refuse to combine
   it. The old identity path and all history files stay available for inspection.
7. Check occupied destinations, cross-device destinations, changed settings after
   review, active research/chat work, and a startup project with Runner jobs.
   Review or confirmation must explain the blocker.
8. Check both owner-confirmed moved and copied identities. Only moved inherits
   the former history. The existing former-folder identity checks remain active.
9. At 1440, 390 and 320 pixels, check paths, footer actions, keyboard focus and the
   gold restart notice. No page-wide horizontal scrolling should appear.

Automated coverage: `project_move_test.go`, `project_move_default_test.go`,
`project_folder_picker_test.go`, the existing identity/history suites,
`ProjectLocation.test.tsx`, and the real browser fixture:

```sh
npm run build --prefix web
JPACK_BIN=/absolute/path/to/jpack \
JPS_MOVE_BROWSER="$PWD/web/scripts/project-move-browser.mjs" \
go test ./internal/desk -run '^TestProjectMoveBrowser$' -count=1 -v
```

The browser fixture moves only its temporary project and starts a second server
through the saved default. Fault tests cover both sides of rename, occupied
late-arriving destinations, retained sibling records, and stale recovery settings. The browser fixture covers both custom and default
destinations, then restarts through the saved project setting. Default-path
tests also cover read-only review, missing parents, occupied destinations,
parent changes, symlinks, unsafe permissions, and the separate chat-history
default alongside installed releases.

Native chooser validation uses a controlled subprocess for API/browser tests;
those tests never open unattended windows on the user's desktop. The opt-in
`JPS_NATIVE_PICKER_CHECK=1` bridge test checks real Windows Forms, encoded Unicode
paths and WSL path conversion without showing a dialog. Manually check the final
native selection on each supported desktop: choose a folder, cancel once, then
choose again; the displayed path and review must agree. macOS and Linux desktop
chooser interaction still require their platform smoke tests.
