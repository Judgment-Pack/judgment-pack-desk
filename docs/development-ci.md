# Manifest-backed CI ownership

`dev.json` remains the command list for both local checks and CI. Local
`scripts/dev/check.sh` still runs the complete list. CI uses
`scripts/dev/ci-ownership.json` to select each job's checks, setup steps and build
prerequisites through that same runner. Add a new check to an owner explicitly;
`ci-ownership` fails for unassigned, removed, duplicate or unknown checks, unknown
owners and missing setup or ordered build dependencies. A new setup step also
needs an owner. Node checks require `npm-ci`; other dependencies are recorded in
the map. `development-build` prepares its own isolated worktree and dependencies.

Go owns formatting, Python and mutation-harness checks, component identities,
vet and the unit Go suite. Web owns its audits, typecheck, build, unit suite and
dialect read. Integration owns the real-companion, certification and shared
reader checks. It installs dependencies and builds its own embedded page, then
runs its assigned checks without repeating the Go or Web unit suites. Local
Gateway keeps both operating systems; development bundles keep all three native
platforms. Their expected variant sets are in the map. Workflow matrices may not
silently omit a variant: the final verifier requires each variant's receipt.

`check` always evaluates its direct dependencies, even after another job fails
or is canceled. Each successful owner uploads a receipt for each OS variant,
bound to the checked-out candidate SHA and the manifest, ownership, runner and
workflow source digests. The final job reads artifacts from its own workflow
run and requires the exact receipt set and successful dependency results. Matrix
job outputs are not used as per-platform evidence. Missing, failed, canceled or
unexpectedly skipped jobs fail the gate. A development lock requires development
bundles and skips stable release archives. A stable pull request requires release
archives and skips development bundles; other stable events skip both. These
are the only accepted dependency skips. Release archive receipts follow the
successful native archive matrix and assembled-platform check; the optional
receipt input defaults off for normal release publication.

The standalone policy-source workflow intentionally retains its producer/consumer
JSON, logs and observation digest artifacts, including manual dispatch; candidate
CI also requires this check through Integration. Harness certification retains
its adapter matrix and path-triggered diagnostics. Component freshness is an
explicit scheduled/manual report about later releases. Mutation rows retain the
PR changed-row workflow, with the full suite in the nightly workflow. Those two
manifest checks retain their existing optional environment policies and are
listed as external owners; this does not make any other candidate check optional.
DCO, native release packaging, provenance, security checks and mutation resource
caps retain their existing workflows and gates.

Run `python3 scripts/dev/ci-ownership_test.py` for the focused, offline ownership,
setup, workflow wiring and aggregation cases. The `ci-ownership` manifest check
also runs semantic omission probes: accepting an unassigned check, a failed job
or stale evidence must make a behavioral test fail.
