# 0015: The storage line is off in v0.14.0

Status: **accepted, 2026-11-08**, for Desk v0.14.0. The storage line is planned
for v0.15.0 ([Desk #698](https://github.com/Judgment-Pack/judgment-pack-desk/issues/698),
[Desk #685](https://github.com/Judgment-Pack/judgment-pack-desk/issues/685)).

## Context

The v0.14.0 candidate carries the storage line: first-use enrollment, the
owner record and catalog, census, custody, admission and limits, native
qualification, and storage-aware transfers and moves. It changes how a Desk
starts (one Desk per installation, the first project's seal, a read-only
start for what storage cannot account for) and it was still failing tests
for many separate reasons. No published release has shipped any of it.

Almost every storage-aware path already treated a server without an
enrollment as the v0.13.2 path. Startup was the one place that always
enrolled.

## Decision

Ship v0.14.0 with the storage line **off**. Storage behaves as in v0.13.2:
the same chat data paths and files, no accounting, limits, custody refusals,
admission or read-only start, several Desks per installation, and v0.13.2's
project, Desk home and chat data moves and backups.

- **The switch.** `New` (internal/desk/server.go) asks `storageLineEnabled`
  (internal/desk/storage_line.go). Off, it opens the private store as v0.13.2
  did (`openLegacyInfrastructure`: the shared home lease, the pending home
  migration check, the account and service folders) and never runs the
  storage infrastructure start, enrollment, the admission handoff or the
  read-recovery writer. A desk Desk made inherits its parent's choice.
- **Turning it on.** `JPACK_DESK_STORAGE_LINE=on` in the environment, or
  `Config.StorageLine` from a trusted host. It is never a project or browser
  setting. On is for development and the CI lane, not for use.
- **What the UI shows off.** `/api/storage/status` answers
  `{"version":1,"line":"off","servedAt":…}`. Admin → Storage & backups hides
  storage accounting and the limits editor and keeps chat data; the status
  strip has no storage notice; retained-read inspection and the finish-move
  choice are not offered. The storage policy and retention routes refuse.
- **Data already set up with the line on** (development builds only) opens
  with it off; its chats read and save. Desk home moves stay refused there,
  because its owner record does not move with a home.
- **Tests.** A test that needs the line calls `requireStorageLine(t)`
  (directly or through the storage helpers such as `ownerActualConfig`), and
  skips unless `JPACK_DESK_STORAGE_LINE=on`.
- **CI.** The required Go job runs internal/desk's shards with the line off.
  The job `go-test-desk-storage-line` runs the same shards with it on; it is
  `continue-on-error` and the job named Go does not need it, so it blocks
  nothing. internal/storagebudget has no switch and stays in the blocking
  Go checks. The local gateway check (`scripts/local-gateway-check.py`, also
  run on the release archives) checks two Desks on one installation with the
  line off and the one-Desk refusal with it on.
- **v0.13.2's tests.** Tests rewritten for the line keep their v0.13.2
  behaviour with the line off, as `…StorageLineOff` variants or as a branch
  on `storageLineOn()`.

## Consequences

The storage code stays compiled, tested and reachable with the line on, so
v0.15.0 turns it on rather than merging it again. Until then its failures are
visible in the storage lane and do not hold a release. The cost is a second
set of shards on each run and one more startup path to keep: v0.13.2's
legacy start must stay correct while it is the default.
