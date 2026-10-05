# Named desks and contained data

The header selects a named desk; the configuration filename is an advanced file detail. Jobs and saved job drafts share one table. A draft carries a neutral Draft badge and a compact Resume action. It has no active trigger or runs.

Create desk creates a fresh owner-only folder below the installation's `desks/` directory (normally `~/.config/jpack-desk/desks/<id>`). The name is independent of its opaque storage ID. New desks contain:

- `jpack.json`, `jpack-desk.json`, `packs/`, and `sources/` for project documents and configuration.
- `jpack.lock.json`, the runtime's reviewed-set lock of the new, empty project.
- `.desk/job-drafts/` for saved job configurations.
- `.desk-private/desk.json` for identity; `.desk-private/data/` for conversations, draft packs, tests, briefs, and retained attachments; `.desk-private/jobs/` for runner state, releases, captured inputs, and run records; `.desk-private/audit/`, owner-only, for the runtime's audit trail of deciding runs.

A new desk starts gated ([ADR-0009](../adr/0009-gates-on-by-default.md), section 1), and signed where it can be ([ADR-0010](../adr/0010-defensible-decision-records.md), section 1 and question 3). Signed, its `jpack.json` is, with the seed's absolute path:

```json
{"configVersion":"6","requireReviewed":true,"requireComparableFacts":true,"audit":{"dir":".desk-private/audit","signingKey":"<Desk's configuration folder>/secrets/signing/<id>.seed"},"packs":{}}
```

Before writing it, Desk asks the runtime it runs which configuration versions it reads (`packs schema --format json`, `supportedConfigVersions`). Where it reads `"6"` and Desk's custody can keep a key, Desk first has the runtime generate the desk's seed (`jpack audit key generate <path> --format json`) in `secrets/signing/`, and keeps its public key beside it, in `<id>.keys.jsonl`; then it writes the configuration that names the seed; then it locks it. Otherwise:

| The runtime reads | Custody keeps a key | `jpack.json` | Signed | The creation says |
|---|---|---|---|---|
| `"6"` | yes | `"6"`, with `signingKey` | yes | nothing |
| `"6"` | no | `"5"` | no | that it is not signed, and why |
| `"5"`, not `"6"` | not asked | `"5"` | no | that it is not signed: the runtime does not read `"6"` |
| `"4"`, not `"5"` | not asked | `"4"` | no | that it has no `requireComparableFacts`, and that it is not signed |
| neither `"4"` nor `"5"` | not asked | no desk | | why |

The `"5"` configuration is `{"configVersion":"5","requireReviewed":true,"requireComparableFacts":true,"audit":{"dir":".desk-private/audit"},"packs":{}}`, and the `"4"` one `{"configVersion":"4","requireReviewed":true,"audit":{"dir":".desk-private/audit"},"packs":{}}`. Each notice is a paragraph of the creation's `notice`, in its answer and in Desk's log, and the page shows it before the desk opens. A notice names Desk's folders by name, never by path. If the runtime fails to generate the key, no desk is made, and no seed or list of keys is left. Desk then runs `packs lock --config jpack.json` in the new folder, after writing `jpack.json` and before the manifest. Every command is started like the relay's `jpack mcp`: the same binary, and on Linux the same change of directory through a held descriptor. Each is bounded to 20 seconds. The registry is not held while they run, so a slow runtime does not hold up other desks. A creation in progress counts toward the 256-desk bound, and one that finishes during shutdown is not published.

If a command fails, no desk is created, and Desk removes what the creation made, the desk's seed and list of keys included, each only while its name still holds the file the creation made, and only through the signing folder Desk holds. A creation keeps a marker, `secrets/signing/<id>.creating`, from before the runtime makes the key until the manifest is written. The manifest is published as one event, staged in `.desk-private`, synced and renamed into place. The next start removes the seed, list and marker of a creation that was never published, and the marker alone of one that was, deciding which by the same manifest reader the registry opens desks with. The signing folder's path must name the folder Desk holds immediately before the runtime runs, and the seed's path the seed Desk found, after the run and before publication; a creation that finds otherwise is refused, never answered as signed. It removes it through the directory it created, never through the folder's name, one item at a time and never recursively. It then removes the folder's entry with `rmdir`, which removes only an empty directory. So if the name was pointed at another desk meanwhile, that desk is untouched. Anything Desk cannot remove this way is left in place, and the answer and Desk's log say where. A folder left so has no manifest. It is not a desk: a start names it, and neither opens it nor counts it toward the bound.

**A desk Desk made reads only its own configuration.** `jpack mcp` has no `--config` option. It reads `$JPACK_CONFIG`, then `./jpack.json`. Desk therefore removes `JPACK_CONFIG` from the environment of every named desk's runtime, and of the commands above, so the runtime reads `jpack.json` in the desk's own folder. This also changes existing named desks: none of them can mean to read another project's configuration. The startup project keeps an inherited `JPACK_CONFIG`, because there it is the owner's only way to choose one, and the launch logs it.

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
