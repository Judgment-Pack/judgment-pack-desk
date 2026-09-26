# Pack folders

Packs has a folder browser within its workspace. The main application sidebar retains one Packs entry. This follows the separation between global navigation and contextual workspace navigation in [Databricks’ workspace browser](https://docs.databricks.com/aws/en/workspace/workspace-browser), with compact controls informed by [Linear’s design refresh](https://linear.app/now/behind-the-latest-design-refresh).

The browser starts at 220 px, can be resized between 180 and 320 px by pointer or keyboard, and can be collapsed. The browser shares the right pane’s divider, surface, header, and drawer styling. When collapsed, its toggle stays in the Browse header. When the remaining collection area would be narrower than 560 px (or a larger working width requested by a map), it becomes a drawer. Returning to a wider workspace restores the desktop preference. Width, collapse, expanded folders, and the selected browsing location are stored per project in this browser. Folder-name drafts survive responsive transitions.

The default home is `local.user@example.com`, a temporary display identity for Desk's current local user. It is associated with the stable `home-local` ID and `local` owner, not a connected Google account or an authentication claim. Existing packs without assignments appear here. Workspace folders may be created alongside this home; all folders except home may be renamed or moved. All packs provides a project-wide view. In a folder, the unfiltered list shows immediate contents; searching includes descendants, with an explicit action to search all packs.

Folder membership is organizational metadata. It does not grant permissions, change a pack ID, move a pack or its companion files, or change runtime/gateway contracts. Saved-case tests stay inside each pack; Judgment Graphs has its own Graphs destination in the primary navigation. Opening a pack highlights its containing folder and shows its breadcrumb; Back preserves the originating collection scope and search.

## Location and folder navigation

Overview, Logic and Tests share the same folder location row above the pack title.
The breadcrumb stays visible when the folder tree is open or collapsed. The tree
is for browsing folders; the breadcrumb identifies the pack's location and returns
to the containing folder or an ancestor. Changing pack tabs preserves the folder
selection, expanded ancestors and pane preference.

The location uses a single quiet row. All packs and the current folder remain
visible. Wide panes also show the immediate parent; earlier ancestors appear in a
keyboard-accessible “Show parent folders” menu, with their complete paths. Below
560 px of breadcrumb space, all ancestors move into that menu. Long visible names
truncate with a tooltip and retain their full accessible name. This responds to
available pane width, including when Details opens, without expanding the folder
browser or increasing header height.

## Persistent project data

`jpack-folders.json` at the project root holds version 1 metadata:

```json
{
  "version": 1,
  "folders": [
    { "id": "home-local", "name": "local.user@example.com", "parentId": null, "ownerId": "local" },
    { "id": "example-team", "name": "Operations", "parentId": null }
  ],
  "assignments": { "example-pack": "example-team" }
}
```

Visiting Packs does not create this file. An explicit folder edit or assignment saves it through Desk's existing authenticated file API, using the digest of the version read. The backend's path containment, serialized writes, and atomic replacement apply. A conflict requires reload and retry; no force overwrite or silent replay occurs. Invalid, unreadable, or unknown-version metadata is preserved and disables folder edits; All packs remains accessible.

Empty folders persist. Deletion only accepts an empty non-home folder. Assignments to packs no longer listed by the runtime are retained deliberately, so a temporarily absent pack recovers its membership. Such assignments also prevent deleting their containing folder. Home cannot be renamed, moved, or deleted. Names are NFC-normalized, 1–120 Unicode characters, with no slashes or control/format characters; sibling names are unique ignoring case. Limits are 1,000 folders, 16 levels, 10,000 assignments, and 2 MB of serialized metadata. Moves validate the complete resulting tree, including descendant depths.

Include `jpack-folders.json` with project backups and version control as appropriate. The separate chat-history export does not back up project files. Admin’s Pack storage setting still controls where pack files are written on disk.

## Creation

Create pack carries the selected folder into the AI draft and final review. An existing unsubmitted home draft is reused, preserving its text and attachments; merely visiting or selecting a folder does not create chat history. The final review allows changing the destination. An unavailable or deleted destination must be resolved before creating the pack.

After the pack and its runtime registration are saved, Desk re-reads the folder document and assigns the new pack. If that last save fails, the pack remains created and its chat remains bound. The destination page shows a retry action for folder assignment only; it never reruns pack creation. The pack is always available through All packs and can also be moved manually.

Drag-and-drop reorganization and folder permissions are not included. The explicit Move command is keyboard accessible and preserves stable IDs.
