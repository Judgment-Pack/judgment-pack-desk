# Child storage observations

Desk #685 in v0.14 observes child storage and warns about low filesystem space.
Child growth is warned about, not prevented. Child admission/enforcement belongs
to #693. No observation changes Desk charges, reservations, limits, recovery,
permissions or a child's ability to run.

The authenticated, exact GET `/api/storage/status` adds nullable
`childObservation` to its version 1 response. The existing accounting snapshot is
unchanged. Older responses without this additive field decode as unavailable
child observations. Present child objects and root rows use closed decoding;
unknown fields, selectors, states and inconsistent nullable counters refuse.
No paths, connection IDs/names, credential contents, Git remotes or raw failures
are published. Counters remain decimal uint64 strings.

Selections come from current Server/component owners, never request paths:

- Runtime: the startup-only inherited `JPACK_CONFIG`/`JPACK_SIGNING_KEY` selection,
  otherwise that Desk's `jpack.json`; configured audit directory and selected
  signing-file metadata. Named desks ignore inherited selectors. Only bounded
  nonsecret configuration bytes are read. Signing files are only statted.
- Runner: the companion's original retained workspace directory, without calling
  endpoint, validating/launching an executable, or starting work.
- Gateway: original protected-store `local-gateway`, `gateway-connections`, and
  its actual selected input-handoffs path, without ensure/start/key reads.
- Native: existing legacy profile, all current registry-selected Codex connection
  profiles (registry bound 32), plus retained managers for removed connections.
  Read-only profile callbacks exclude Close and never call lazy get/openProfile,
  take a profile lease or initialize an account. At most33 distinct selections
  plus an explicit unknown overflow row are observed; child routes are not capped.
- Git: only the 16 registered active scratch roots created by the actual HTTPS
  catalog route before init/fetch. Registration failure/overflow stays unknown;
  cleanup waits for the observer's existing Server mutex. Local caller catalogs
  never register and shared temporary directories are never searched.

Original descriptions remain held during their observation. Absolute paths pin
at most 64 named ancestors and refuse symlinks/replacements. Named roots join
held identities before and after traversal. Project move/Close refuses new
publication. Runner/Gateway/native Close waits for their read-only callbacks;
Server Close retires the volatile cache. There is no new durable state or owner
registry. Git registration is a fixed transient descriptor set, not filesystem
inventory or recovery authority.

One refresh uses a cooperative 1 second child context within the existing 5 second
status request, 4096 traversal credits, 16 directory levels, 1 MiB accumulated entry
names, 32-entry read batches and at most 64 public rows. The fixed closed
encoding is below 16,640 bytes even with 64 maximum-width rows, within its
32,768-byte cache cap and the unchanged status response byte ceilings. Every attempted entry,
including errors and shared aliases, consumes credit. Configuration reads use at most 1 MiB for Runtime and the existing 4 MiB
configuration reader for each before/after registry selection (at most 9 MiB
input total; the registry decoder accepts at most 1 MiB). No
file data is read by the tree walk. Directory descriptions are closed during
unwind; retained ancestor and recursion depths keep additional descriptors below
256. Each native syscall remains synchronous: cancellation is checked between
bounded calls and does not abandon a borrowed description or promise a kernel
I/O interruption deadline. No refresh goroutine, retry or component call exists.

The existing cache mechanics coalesce refreshes, throttle to 5 seconds and mark
retained observations stale after 15 seconds or a failed refresh. Each observation
keeps its original timestamp. Child observations are cached per Server separately
from installation-wide Q projections, and remain available when Q/status
qualification is unavailable. Refresh failure preserves prior data as stale.
The shell reuses its single storage notice for child warnings and actual refusals, without conflating their messages. Warnings require no file/log/notification write and do not manufacture pressure
refusal occurrences.

File-length bytes are metadata observations, not allocated disk bytes. Shared
objects and aliased/nested roots are counted once within the bounded refresh;
shared rows are partial and have nullable counters. There is no aggregate that
adds them to Desk charges. Concurrent change, unsupported objects, symlinks,
failed reads or bounds prevent a complete measured row. Partial counts from
bounded or shared traversal are lower bounds. A detected file or directory change invalidates the complete root aggregate to null;
independently witnessed filesystem availability can remain. Unknown is null,
never substituted zero. Inactive means no current
configured/active owner selection, not zero historical storage.

Filesystem available bytes come from the existing held-directory statfs leaf
and its reviewed Linux Frsize/Bsize or Darwin Bsize conversion. They are separate
from file lengths and all quotas/promises. A positive available count below 1 GiB
produces a warning; zero is measured exhaustion. Undefined/denied counters stay
null, including the warning result. This is an advisory threshold, not a new
headroom allowance or guarantee that a future write can succeed. Filesystem
quota discovery and native qualification are not inferred from these counters.

Handwritten Go/API, native/profile lifecycle, strict decoder and UI vectors cover
selection, alias/nesting, replacement, bounds, partial/unknown/stale data,
warning-only semantics and unavailable Q. The existing browser fixture checks
actual authenticated child projection separately from handwritten display
warnings. Its first-use enrollment/API prerequisite, real native execution,
independent review and integrated browser/full release checks remain mandatory.
Source authoring is not execution or qualification evidence.
