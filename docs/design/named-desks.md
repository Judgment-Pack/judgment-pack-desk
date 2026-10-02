# Named desks and contained data

The header selects a named desk; the configuration filename is an advanced file detail. Jobs and saved job drafts share one table. A draft carries a neutral Draft badge and a compact Resume action. It has no active trigger or runs.

Create desk creates a fresh owner-only folder below the installation's `desks/` directory (normally `~/.config/jpack-desk/desks/<id>`). The name is independent of its opaque storage ID. New desks contain:

- `jpack.json`, `jpack-desk.json`, `packs/`, and `sources/` for project documents and configuration.
- `jpack.lock.json`, the runtime's reviewed-set lock of the new, empty project.
- `.desk/job-drafts/` for saved job configurations.
- `.desk-private/desk.json` for identity; `.desk-private/data/` for conversations, draft packs, tests, briefs, and retained attachments; `.desk-private/jobs/` for runner state, releases, captured inputs, and run records; `.desk-private/audit/`, owner-only, for the runtime's audit trail of deciding runs.

A new desk starts gated ([ADR-0009](../adr/0009-gates-on-by-default.md), section 1). Its `jpack.json` is:

```json
{"configVersion":"5","requireReviewed":true,"requireComparableFacts":true,"audit":{"dir":".desk-private/audit"},"packs":{}}
```

Before writing it, Desk asks the runtime it runs which configuration versions it reads (`packs schema --format json`, `supportedConfigVersions`). A runtime that reads `"4"` but not `"5"` gets `{"configVersion":"4","requireReviewed":true,"audit":{"dir":".desk-private/audit"},"packs":{}}`, without `requireComparableFacts`, and the creation's answer and Desk's log say so. A runtime that reads neither cannot hold a desk to its reviewed set, and no desk is created. Desk then runs `packs lock --config jpack.json` in the new folder, after writing `jpack.json` and before the manifest. Both commands are started like the relay's `jpack mcp`: the same binary and environment, and on Linux the same change of directory through a held descriptor. Each is bounded to 20 seconds. If either fails, the folder is removed and no desk is created.

The empty lock reviews no pack. It pins only the configuration's bytes, so that a deciding run is refused for a draft rather than for every run. A deciding run of a pack Desk adds is refused until the project is locked again.

The private directory is excluded from the file editor, file listing, and watcher. Restores use another private subfolder, verify the copy, retain the previous data, and update a relative storage pointer. Desk IDs keep conversation and runner identities stable when a folder is moved. A direct CLI launch recognizes the manifest. Credentials and installed executables remain machine-owned: source-system references and connection credentials are not copied into a desk folder.

Existing projects keep their current storage locations. Opening this version does not migrate or duplicate existing chat stores or interrupt their Jobs runner. Their initial desk name is the project folder name. Named desks are listed alongside the startup project. Migration of legacy operational data is separate from creating a fresh desk.

## Request and lifecycle boundary

`GET /api/desks` lists the startup desk and registered desks. `POST /api/desks` accepts only a name; the server chooses and creates a new folder. It accepts no arbitrary path or executable. Folder identity is checked against its pinned root. Invalid, missing, or unsafe desk IDs fail rather than falling back to the startup project's data.

Each document freezes its selected ID. HTTP requests carry `X-Jpack-Desk`; runtime and agent WebSockets carry the nonsecret `desk` selector. Authentication and origin checks precede desk selection. Authentication is installation-wide, including revocation. Model-relay requests carry the same selector. Switching reloads the document, clearing all query and editor state, after checking active assistant work, flushing pending chats, and confirming unsaved editors through the existing typed modal.

Event senders use `/api/desks/<id>/job-events/<trigger>` with the trigger-scoped bearer, and read an occurrence's result at `/api/desks/<id>/job-events/<trigger>/occurrences/<occurrence>`. They do not acquire a browser session. The endpoint resolves only already registered desks and retains the runner's event admission checks.

Each registered desk owns its runtime relay and Jobs companion. Cloud pull subscriptions
are assigned to one desk in the installation connections file using the optional
`desk` ID. An unscoped subscription belongs only to the startup desk. Duplicate
subscriptions are refused, so another desk cannot acknowledge the owner's signals.
Gateway operation connections remain installation-owned and shareable. Switching tabs or desks does not stop accepted runs. Startup resumes registered desks and their durable queues/schedules; stopping Desk shuts all companions down. Registry admission is bounded at 256 desks.

## Empty projects

A new desk deliberately starts with zero packs. The companion Runtime must support an empty `packs` object in `jpack.json`; the accompanying runtime change adds this without altering pack schemas or evaluation semantics. Zero-case test runs remain `skipped`, never `passed`.
