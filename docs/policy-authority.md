# Machine Policy enrollment

This opt-in local profile binds Desk's current Policy ledger to an independently
held machine record. It supports one OS owner and a trusted launcher that supplies
the same authority root and expected installation identity on every configured
launch. It detects a portable ledger restored behind a retained newer machine
record. It does not protect against a hostile process with the same OS owner,
replacement or rollback of the whole machine, or a launcher that omits the trust
configuration. Earlier copies made before enrollment markers existed cannot be
identified as enrolled copies.

The operator chooses an absolute, clean authority path outside Desk home,
configuration, projects, chats, Runner stores, portable backups and migration
roots. Keep its custody separate from those stores. No default root, automatic
initialization, enrollment, repair or reset is provided. Paths and installation
identities must come from the operator or trusted launcher, never browser input,
model output or project content.

Before initializing or changing enrollment, stop all Desk processes that can open
the workspace, including older binaries and launches without authority flags.
These commands are local operations: they finish without starting a web listener,
Runtime subprocess or serving adapter. This guide does not restart any running
Desk or change the local development server on port 5173.

Create the machine installation explicitly:

```sh
jpack-desk policy-authority init --root /absolute/independent/policy-authority
```

The JSON result contains the generated `installationId`. Retain that exact
identity in the trusted launch configuration. `init` accepts no installation
identity or project argument and refuses to replace existing authority state.

Inspect the installation and, optionally, a specific workspace:

```sh
jpack-desk policy-authority status --root /absolute/independent/policy-authority --installation INSTALLATION_ID
jpack-desk policy-authority status --root /absolute/independent/policy-authority --installation INSTALLATION_ID /absolute/project
```

The bounded JSON result reports version, installation identity, state, and where
applicable workspace identity, ledger digest and anchor generation. It does not
include authority paths or raw Policy content. Status performs no repair and
does not establish permission to replay a previous enrollment or Policy action.

Establish the initial workspace mapping explicitly before enrolling. Classify the
existing workspace as legacy:

```sh
jpack-desk policy-authority classify-legacy --root /absolute/independent/policy-authority --installation INSTALLATION_ID /absolute/project
```

This classification permits the known legacy workspace to use its existing
unbound Policy behavior in this configured installation. It is also the required
first step for enrollment. It does not enroll the workspace or authorize a reset
of enrolled state. A workspace intended to remain unprotected stops here.

Enroll the classified workspace only when it already has a valid Policy ledger
whose exact current contents the operator intends to retain:

```sh
jpack-desk policy-authority enroll --root /absolute/independent/policy-authority --installation INSTALLATION_ID /absolute/project
```

Enrollment records the workspace binding and current ledger high water. It does
not create a replacement ledger when one is missing, adopt another installation,
or overwrite a prior or uncertain enrollment. Stop on any failure. Inspect the
retained state; do not repeat or retry an uncertain enrollment automatically.
A subsequent workspace status can report `current` when the retained ledger
matches the machine high water. A pending state remains held even when its
ledger bytes match a recorded old or new digest.

Missing or corrupt machine registry state, unknown workspaces, copied bindings,
and pending enrollment or migration refuse startup instead of falling back to
legacy operation. An enrolled workspace with a valid machine binding can still
open when its anchor or Policy ledger is missing, but current Policy access
remains held. Historical recovery stays read-only and is available only when
the required ledger and referenced history blobs remain present and valid.

Launch Desk with both machine flags, keeping all flags before the optional
project directory:

```sh
jpack-desk --policy-authority-root /absolute/independent/policy-authority --policy-authority-installation INSTALLATION_ID /absolute/project
```

Supplying only one machine flag is an error. Omitting both retains legacy mode
for unenrolled workspaces; a retained portable enrollment marker refuses an
unconfigured launch. Keep older and unconfigured processes stopped: the marker
does not retrofit enforcement into older binaries or protect pre-marker copies.
Configured launches refuse pending Desk-home migration and skip automatic home
recovery. Desk-home moves, project moves and new-Desk creation are blocked in
this initial profile; it provides no recovery, reset or migration command.

This feature applies to the current Policy repository and its provenance
checks. Historical reads retain their separate read-only contract. It does not
grant execution authority, prove prior operation delivery, govern every Jobs
execution path, or publish a Runtime release.
