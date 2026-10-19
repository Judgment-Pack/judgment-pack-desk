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

An already Policy-enrolled workspace can explicitly enable local executor
ownership as a separate operator action:

```sh
jpack-desk policy-authority enroll-executor --root /absolute/independent/policy-authority --installation INSTALLATION_ID /absolute/project
```

Stop all older or unconfigured Desk, Runner and Runtime processes first. The
machine-use lease excludes configured Desk processes; it cannot prove that an
older process or one launched without these flags has stopped. Executor
enrollment requires the existing exact anchored Policy ledger and retained
history to be current. It preserves those bytes and does not initialize or
repair missing Policy state.

This explicit action upgrades the machine registry to its closed version 2 and
retains an executor intent before creating
`executors/<machineWorkspace>/<machineWorkspace>.lease` beneath the independent
authority root. It creates that private directory and empty stable file once,
syncs their ancestry, and retains their separate device and inode identities in
the registry. Ordinary startup creates none of them. A retained pending or partial
enrollment stays held and cannot be replayed or repaired by repeating
the command. Status reports `executorState` separately from Policy `state`;
`enrolled` describes retained ownership configuration, never current run
permission. A final write error may leave an observable stable enrollment, but
does not turn the failed command into a successful delivery.

Version 1 storage-only workspaces retain their existing behavior. They are not
automatically upgraded. Older Desk binaries refuse the version 2 registry.
Orphan executor directories refuse the profile, including when a restored
registry removes executor metadata. Missing or replaced enrolled custody holds
Jobs rather than creating a new lease or falling back to an unowned Runner.

Every owned Jobs start first runs the configured Runner's closed version 2
capability probe with EOF input, bounded output and a timeout. Probe and boot
execute the same private executable snapshot, with the configured source's
identity and digest pinned for this Desk lifetime. Boot explicitly requires
`executor-ownership/1` and carries both independent machine and Runner workspace
identities. Signing-key refusal can retry only with the same ownership demand
and a fresh paired snapshot. Ownership/probe/setup failure and an unexpected
owned Runner exit hold Jobs for this Desk lifetime; background endpoint retries
cannot restart it. Investigate before an explicit operator restart. A stopped
Runner does not establish that a Runtime child stopped: inherited external
ownership remains the actual exclusion boundary until those children exit.

Snapshot custody validates and pins the temporary parent before creating its
fresh private directory. Legitimate OS temporary-path aliases remain supported;
unsafe shared writable ancestry refuses before creation. Cleanup uses the held
directory and its retained identity, removes only the snapshot file and matching
empty directory, and never recursively follows a substituted temporary leaf.

Ownership is supported only where Runner can retain and inherit the external
lease. It does not implement governance admission, exact resource registration,
executor epochs, durable logical attempts, withdrawal enforcement or
`commitStart`. Existing jobs remain ungoverned. The shared wire fixture is
[executor-ownership-boot.json](design/executor-ownership-boot.json).

The required integration profile uses an isolated source-built ownership-aware
Runner and Runtime, without changing published component pins:

```sh
JPACK_EXECUTOR_RUNNER_TEST_BIN=/absolute/candidate/jpack-runner \
JPACK_BIN=/absolute/candidate/jpack \
go test ./internal/desk -run '^TestJobsExecutorRealEnrolledDeskRunnerRuntime$' -count=1
```

Supplying an incompatible Runner fails this profile. The fixture enrolls only
temporary projects and proves a real saved check and job run, copied-store
exclusion, held unexpected exit and explicit isolated restart. Runner's process
death fixtures separately prove that actual Runtime children retain ownership.
