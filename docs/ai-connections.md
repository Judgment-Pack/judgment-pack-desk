# AI connections and desk preferences

Connections > AI manages named, reusable AI connections on this computer. Add a
connection for each ChatGPT account, API provider, or separate API credential.
The connection name identifies it in Admin and in the composer's grouped model
menu. Rename, Set default, and Disable/Enable are available in the row menu.
Disabling is reversible and retains credentials. There is currently no connection
removal action in the UI.

Admin > Assistant controls the current desk. A desk may inherit the shared
connection/default-model choices or select its own enabled connections, default
connection, and model/reasoning preferences. The desk's connections narrow the
shared ones: only a connection that exists and is enabled on this computer can be
enabled in a desk. Its model lists do not narrow: shared connection model lists are
defaults, and a desk's explicit list for a connection replaces that default for the
desk, and may name any model the provider offers (the picker offers the account's
whole catalog). The relay and the run socket then admit only the models on
whichever list applies. A native account's advertised model catalog and reasoning
capabilities still apply.

The composer selects a connection and model together. Chats retain that selection
across changes to shared defaults. Switching a connection resets that chat's
reasoning override. Each new assistant reply records its bound connection ID,
connection name, and model in its checkpoint; these appear in reply details.
Existing replies keep unknown attribution. An older saved chat without a
connection ID requires an explicit selection before its next send.

## Storage and migration

- `<Desk config directory>/ai-connections.json` stores connection IDs, names,
  enabled flags, the shared default, and non-secret provider settings. The file
  is private to this computer and is not a project file.
- `jpack-assistant.json` in each project stores desk preferences. Version 2
  references connection IDs and contains no endpoints, API keys, or account
  credentials. Version 1 remains readable.
- API credentials use the existing private custody store. New connections have
  separate `secrets/assistant-<connection ID>` entries, bound to provider protocol
  and origin. The migrated API connection retains `secrets/assistant`.
- Additional ChatGPT accounts have independent `codex-<connection ID>` homes.
  The migrated account retains its existing home and authorization.

Before the first registry save, Desk derives named `legacy-api` and
`legacy-codex` connections from the existing machine `desk.json`. Reads do not
write a migration or require reconnecting. Both retained configurations survive;
the formerly active engine remains the shared default. Existing desk model
preferences continue to apply to their corresponding migrated connection.

The first connection write materializes the registry. Thereafter the registry is
the source of AI connection settings; the legacy assistant configuration remains
in `desk.json` for preservation and is not kept in sync. Other machine settings
continue to use that file. Do not edit its old assistant section to manage the new
connections.

Connection IDs refer to this computer's registry. Copying a project to another
computer does not copy credentials or recreate its connections. Missing or
disabled IDs require choosing an available connection in Assistant settings.

## Request isolation

Registry updates use a digest precondition and the existing atomic custody writer.
Concurrent edits fail explicitly instead of overwriting another connection.
Inference requests carry a connection ID and revision. The relay resolves that
connection's endpoint and credential, enforces the desk's connections and the model list that applies,
and removes internal selectors before contacting the provider. Changing the
shared default does not retarget an already bound request. Changed revisions,
removed connections, and disabled connections fail explicitly.

Each native account has an independent manager. API model discovery in Admin is
available before a connection is enabled in a desk; this does not grant inference
permission. Normal local session, origin, credential-binding, and tool-admission
checks remain in force.

## Verification

Tests cover preservation of legacy credentials, independent credentials for the
same model ID, separate desk choices, compare-and-swap conflicts (at the read and
again at the rename), changed and removed request targets, independent native
managers, discovery versus inference, and matching Go/TypeScript validation of
both profile versions. Each connection's key is held as the desk's own key is
(an owner-only file in the owner-only secrets folder, never in the project, the
log or an answer) and is read back and compared with what was written before a
save is reported; a registry write is likewise read back and held to its bytes,
and is refused before writing where its reader would refuse it. Page tests cover
adding and renaming connections, independent defaults, selection controls and
grouped composer choices. Provider inference is not sent during these checks.
