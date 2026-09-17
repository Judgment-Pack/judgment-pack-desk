# Private chat storage and recovery verification

Date: 2026-09-17. Base: Desk PR #100, `858d512`.

## Implemented

- Storage & data separates project pack settings, personal chat data, and backups.
- Dedicated private data root with explicit migration for existing profiles.
- Cross-process write lock, active relay protection, copy verification, atomic
  location switch, retained originals and restart recovery.
- Explicit preview/confirmation for linking history after a project folder move.
- Bounded manual ZIP backup and restore with manifests, checksums, entry limits,
  and rejection of unsafe paths, files, duplicates and inconsistent links.
- Shared neutral settings sections, fields, file input, buttons and Radix dialogs.

## Verification

- Linux: full Go suite and go vet pass; focused recovery tests also cover the
  shared staging helper and real model/gateway requests during relocation.
- Web: 147 test files; 3,415 pass and one intentional skip. Typecheck/build pass.
- Existing mutation matrix: 927 source needles remain valid; its guard tests pass.
- Chromium: `scripts/storage-check.mjs` passes five behavior groups covering
  cancel/focus, relocation, backups, corrupt/valid restore, project relinking and
  1440/1024/640/390px widths in light/dark themes.
- Chromium chat regression: nine groups pass with deterministic model responses,
  including checkpoint restoration, attachments, cancellation and narrow layouts.
- Go tests exercise stale revisions, two Desk instances, an actual child-process
  lock contender, unsafe destinations, credential exclusion, restart, cancellation,
  injected ENOSPC, pointer/path substitution, invalid ZIP directories and recovery
  when project-binding metadata is broken.

The macOS ARM64 application cross-compiles. Cross-compiling its test suite fails
on the existing `runtimeShellName` test reference. Windows test compilation also
fails on existing `syscall.Stat_t` and `syscall.Mkfifo` references. Both failures
were reproduced against untouched base `858d512`; this change does not claim
Windows support or native macOS test execution.

## Boundaries

No real user history was moved or restored during these tests. Fixture profiles,
files and backups live under temporary test roots. No paid model or provider call
was made. Current history limits and runtime/receipt verification remain in place.

PDF original/page storage and personal Drive connections remain dependent on
accepted gateway contracts and implementations. Organization sharing, automatic
retention and durable scheduled jobs are not implemented by this PR. The proposed
integration boundary is documented in ADR 0004.
