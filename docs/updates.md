# Desk releases and updates

The update unit is one tested Desk bundle: Desk, Runtime, Runner, its source
worker, Gateway and adapters. Existing job releases keep their digest-pinned
Runtime and pack. Upgrading Desk does not reinterpret those jobs.

## One component lock

`internal/releaseplan/components.json` is the source of component versions,
repository identities, exact commits and channels. Go embeds it; the Gateway
builder, Jobs compatibility CI and release packager read it. Development pins
are held rather than replaced with an older published release. A stable Desk
release refuses development pins. Runner is pinned to `v0.6.0`, including calculator profiles, bindings,
calculated input lineage, verification export versions 3 to 5, the installation's chain
of runs, and a journal of each job's activity. It also exports a run whose job has no
mapping v2, as version 3 or later with `"inputs":"not-mapped"`, where earlier Runners
refused one. Each job's Activity tab reads the job's journal through Desk's Jobs route, which
forwards `GET /v1/jobs/{job}/events` and its cursor; Runner's store-wide journal is not
forwarded. Desk's verification download asks for version 5 and names the file for the version
Runner answered, which is earlier where the run lacks what a later version carries. Desk
passes `GET /v1/run-chain` through as Runner's exact bytes, up to 67,174,400 bytes (about
64 MiB), or fails the download. Each desk's Runner is given a signing key of its own,
which Desk keeps in its custody and names on Runner's boot line (row 11 of
[ADR-0010](adr/0010-defensible-decision-records.md)'s delivery table, #229), so where
Runner and the runtime accept the key, each run's record is signed and its version-5
export carries the signature sidecar. Desk does not yet rotate that key, check a chain
entry (row 12, the Jobs record panel) or hand the chain over (row 8b).

**Back up every desk's Jobs store before updating to a Desk that pins Runner
`v0.6.0`.** Its first start migrates each store to Runner's schema `"2"`, and Runner
`v0.5.0`, which earlier Desks bundle, then refuses that store. The updater does not copy
the store, and a rollback selects the earlier Desk, not the earlier data: after a
rollback, Jobs cannot open a migrated store until a copy from before the update is put
back. Stop Desk, copy each Jobs workspace folder, then update ([Runner's State and
backups](https://github.com/Judgment-Pack/judgment-pack-runner/blob/v0.6.0/README.md#state-and-backups)).
Desk's release stays at state epoch 1 even so: the change is Runner's store, not Desk's
own state, and this paragraph is the explicit step it would otherwise lack.

Runtime is pinned to `v0.27.1`, which chains a project's audit trail over its exact bytes
by default, and signs it where a key is set (runtime ADR-0047). Its `audit verify` report
also says, among what it does not establish, that the trail records decisions and not
refused or failed attempts (runtime ADR-0048); the decision-record panel shows that
sentence as the runtime writes it. Since v0.5.1, Desk's
decision record in Admin → Project shows the runtime's `audit verify` of a desk's trail,
run without public keys, held checkpoints or time-stamping roots, and downloads the
trail, its signatures and its stamps as exact bytes; the runtimes of desks Desk made no
longer inherit `JPACK_SIGNING_KEY`
([ADR-0010](adr/0010-defensible-decision-records.md), PRs 1 and 2: #202, #204, #205).
Desks Desk makes keep a signing key and start signed at configVersion `"6"` (PR 3a,
#219), and the owner can rotate a desk's key from its decision record, which finishes or
undoes at the next start a rotation a stop cut short (PR 3b). The project Desk was started
on is offered a key as an item of its upgrade that is never chosen for it (PR 4); the owner
can hand checkpoints to holders by download (PR 5), repair a trail whose last line is
incomplete (PR 6) and set a time-stamping authority for Desk to stamp with (PR 7). The README
section "A defensible decision record" says what each part establishes and does not.

Gateway is pinned to `v0.10.0` and includes `adapter-render`, required by its
local source plan, and the two OCR workers `ocr-tesseract` and `ocr-cloud`, which
the document processors run beside the companion
([document processing](document-processing.md)). Desk now asks for the plan with
its connections directory, so the plan follows the processor and search timeouts
set there; with neither set it is `v0.9.1`'s, byte for byte. It refuses to start where another user could replace an adapter it
launches, or a link to its seed or credentials; the bundle's adapters, owned by the
user who runs Desk, pass. Its Drive connection uses whole-Drive consent and Desk
search-and-select. Upgrade the catalog, selection UI and relay together with
the pin. Older Drive connections may report `reconnect-required` even while
status says connected; reconnect explicitly to grant the new scope.

Dependency updates use two bots with separate responsibilities:

- **Dependabot** (`.github/dependabot.yml`) checks Go modules, the `web` npm
  dependencies and GitHub Actions weekly. Related AI SDK, React and localization
  packages are grouped to keep compatible libraries together.
- **Renovate** (`.github/renovate.json`) only manages the three component release
  pins. On each hosted run it looks for published stable GitHub releases,
  resolves the underlying tag commit, and updates the
  version, revision and channel together in one grouped PR. A reviewed preview
  can advance to a stable release; further previews and development pins are
  excluded. Same-version digest changes are not proposed automatically.

Both configurations must reach the default branch. GitHub hosts Dependabot;
public dependencies need no additional credential. For component updates, install
or enable the [Renovate GitHub App](https://github.com/apps/renovate/installations/new)
on **Judgment-Pack/judgment-pack-desk**. Select only that repository if other
repositories should remain unaffected. The App installation grants Renovate its
own access; no personal token or `COMPONENT_UPDATE_TOKEN` secret is needed. This
replaces the custom token-based workflow shipped in v0.2.0.

Renovate is limited to the custom component file so it does not duplicate
Dependabot PRs. Its component dashboard shows pending updates and lookup errors.
The hosted App controls run timing; the repository sets no time window. In the
Mend Renovate app, the repository must be in **Interactive** mode for Renovate to
open PRs; **Silent** mode only scans. Configuration alone does not install or
enable the App. Without it, component updates remain manual: run
`python3 scripts/component-releases.py propose`, inspect the diff, and open a PR.

The **Component release freshness** workflow runs
`python3 scripts/component-releases.py status` twice daily and on manual
dispatch. It compares each published pin with its repository's latest stable
release and writes a table to the workflow summary. The run fails while a newer
stable release is unadopted or a lookup fails; a failed lookup is never reported
as current. Development pins are held and not looked up. The check is read-only:
it does not edit the lock, push, open PRs or gate a release, and it uses only the
workflow's read-only token. It makes a stalled bot, or an update PR left
unreviewed, visible. Run the same command locally for the same report.

Neither bot is configured to merge its own PRs. Review and compatibility CI are
required. CI verifies each non-development pin against its published release and
peeled version tag alongside companion builds, so an updated version with a stale
or missing commit cannot pass. Existing job releases remain unchanged.

CI uses the lock for actual Gateway lifecycle/PDF checks and Runner recovery and
input-profile checks, plus Go tests, frontend tests and localization checks.
A pushed `vX.Y.Z` tag runs that same CI before publishing. The packager requires a
clean tree and that exact tag. It downloads the published Runtime, Runner,
source worker, Gateway and adapter programs for Linux/amd64, macOS/arm64 and
macOS/amd64. Each component archive must match its published `checksums.txt`
and pass `gh attestation verify` against its repository, release workflow, tag
and exact locked commit, on a GitHub-hosted runner. There is no rebuild fallback.
Only Desk and its embedded web UI are built here. Each completed archive is
checked again against the published component bytes and run on its native host
before publication. The same packaging and smoke checks run on
pull requests with a disposable, local-only tag; those artifacts are not releases.
The release waits for all three platform checks before publishing once.

Each archive has `release-manifest_<os>_<arch>.json` beside it and an internal
`release-manifest.json`. A combined `checksums.txt` covers all archives and
manifests. The historical public `release-manifest.json` remains an alias of the
Linux manifest. Manifests record component identities, platform, state compatibility
epoch and every bundled file digest. The installer selects the current OS and
CPU, reports a missing archive instead of falling back, and checks the platform
on staging, activation, rollback and every launch.

macOS archives are not Developer ID signed or notarized; see the
[macOS opening instructions](../README.md#release-platforms). Native smoke tests
do not exercise Gatekeeper's downloaded-file dialogs. Windows has no published
archive and remains untested; managed installation refuses non-POSIX hosts.

## Install and run a managed release

Python 3.8+ is required on Linux or macOS. Obtain `desk-update.py` from a reviewed
Desk release or this repository, then run:

```sh
python3 scripts/desk-update.py install
python3 ~/.local/share/jpack-desk-install/desk-update.py run -- /path/to/project
```

The default installation directory follows `XDG_DATA_HOME`, falling back to
`~/.local/share/jpack-desk-install`. Use `--root /absolute/path` before the action
for another owner-only installation directory. An existing unrecognized directory
is refused. The installer leaves the source checkout and Desk data untouched.
Run through this launcher to use managed updates; invoking an extracted binary
directly does not enable installation controls.

**Help & About → Updates** shows the running build, latest stable release and last
check. **Prepare update** verifies and stages the bundle. **Cancel prepared
update** removes the pending selection; retained releases remain available.
**Automatically update on launch** checks and prepares the latest stable bundle
before the next start. It is off by default. Managed Desk also checks daily while
running, without a browser. Download or network failures leave the current
installation usable and are not reported as “up to date.”

**Component versions**, under Updates, compares the companions Desk started with
against the commits pinned by the lock built into this Desk. It does not say
whether newer component releases exist. Runtime, Runner and source worker
identities are the Go build stamps Desk read from the selected executables when
it started, without running them; a file replaced on disk afterwards is not
reflected until Desk restarts. The source worker is the one installed beside
Runner, not a worker service started separately, and a FIFO or other non-regular
file in a companion's place has no identity. The Gateway identity is the revision
its verified bundle manifest records when the local Gateway starts, shown only
while it runs. Missing metadata stays **Unknown**, as does a Gateway admitted by
an operator manifest digest (`JPACK_DESK_GATEWAY_MANIFEST_SHA256`), whose
recorded revision Desk did not check. A match is **Matches release**, or
**Matches pinned commit** for a development pin, which has no published release.
A modified build or another commit is **Different build**, shown by commit and
never as matching, and the section then asks for a restart through the
development launcher or a reinstall of the complete bundle.

The launcher holds an installation lock while Desk runs. It switches the current
release only before starting the next process, then explicitly launches that
bundle's Runtime and Runner. There is no browser-triggered forced restart and no
silent replacement of a running source worker. Stop Desk normally after saving
work, then start through the launcher to apply a prepared version. Credentials,
Gateway signing identity, chat history and Jobs storage remain in their existing
locations.

## Verification and recovery

Downloads use the fixed Judgment-Pack Desk release repository over HTTPS. The
archive checksum must match published checksums and GitHub's asset digest when
available. Extraction rejects traversal, duplicate paths, links and special
files, bounds compressed/expanded size and entry count, and verifies every file
against the release manifest. Those files are checked again before execution.
This trusts the repository's release publishing authority; checksum verification
is not an independent publisher signature.

New packaged bundles include `component-artifacts.json`: each upstream archive
name and SHA-256, its signer workflow and pinned source identity, and the hashes
of the programs and license notices copied from it. The packager authenticates
these archives with GitHub build attestations; the end-user updater still trusts
Desk's publishing authority as described above.

A job release created with a packaged Desk Runtime freezes the **published
Runtime executable's digest for that OS and architecture**. For `verify-run`,
obtain that exact Runtime tag and platform archive from
[Runtime releases](https://github.com/Judgment-Pack/judgment-pack-runtime/releases),
verify its checksum and attestation, and compare the extracted `jpack` SHA-256
with the record's frozen Runtime digest before replay. A matching version string
alone is insufficient. `component-artifacts.json` and `release-manifest.json`
identify the bytes Desk shipped. Different platforms can have different digests.

This does not rewrite old job releases. Records made with earlier Desk bundles,
a development build or an explicit Runtime override still need their original
executable. Retain it with those records; upgrading Desk cannot make a different
binary satisfy an old digest.

See [release verification](release-verification.md) for the packaging checks and
an example of checking the published Runtime archive.


The verified bundle also refreshes the installation launcher on activation.
Launcher changes within state epoch 1 must remain compatible with retained
releases. Current and previous directories are retained. After stopping Desk, select the
previous verified release with:

```sh
python3 ~/.local/share/jpack-desk-install/desk-update.py rollback
python3 ~/.local/share/jpack-desk-install/desk-update.py run -- /path/to/project
```

Rollback disables automatic installation so the next launch keeps the selected
version. This rolls back executable selection, not user data. Automatic updates accept
only state epoch 1, whose releases must preserve storage compatibility. Any
incompatible storage migration needs an explicit backup/migration workflow and
a new epoch; it is refused by this updater. Maintain ordinary workspace and Jobs
backups separately. Failed startup does not automatically run an older binary
against potentially changed data.

Development builds display **Local development build** and cannot stage updates
or enable automatic installation. Commit, review, merge and publish local changes
before expecting them to appear in the stable channel. No updater can retrieve an
unpublished local change from GitHub.

## Development start and restart

The VS Code `desk: start` and `desk: restart` tasks (`scripts/desk-dev.py`)
synchronize Runtime, Runner, its source worker, Gateway and adapters from the
same lock before building Desk (`scripts/dev-components.py`). They no longer use
`jpack` from `PATH` or companions left in `bin`.

On a cache miss, each locked commit is fetched into a temporary checkout and
checked against its version tag. Sibling repositories are never pulled, reset or
built. Runtime, Runner and the source worker must carry the locked commit, with no
local modification, in their Go build stamp; Gateway and adapters are built from
the locked commit's archive. These builds ignore `go.work` and any `GOFLAGS`
saved with `go env -w`. Each completed set records its lock, build-recipe
digests, host platform and file hashes under `bin/dev-components`. Every reuse
rechecks that record and the Go build stamps of Runtime, Runner and the source
worker. A matching set works offline. The record guards against accidental
change; it is no defence against someone who can write to the checkout, who
could as well change the scripts. A changed lock or recipe, a missing, non-executable or unlisted file, or
a checksum mismatch requires a fresh build into a new directory; if that fails,
startup stops before the running Desk is stopped. The first start after a lock
change needs network access, Git and Go, and takes several minutes.

Each start builds Desk into its own `bin/dev-launches` directory beside a copy of
the verified set. The copy is verified again and shares no file with the cache,
so nothing done to the cache changes what a running Desk, Gateway or Runner
executes. The default Runtime and Runner paths are passed explicitly.

Starts never delete anything, so each leaves its launch directory, more than
100 MB, behind, and a failed build leaves its marked `.building-*` staging
directory. Remove old ones explicitly with Desk stopped:
`python3 scripts/desk-dev.py prune`, or the **desk: prune** task. It refuses
while servers this launcher started are running, keeps the launch the last start
used, removes only directories the launcher marked as launches in this
checkout's `bin/dev-launches`, and never follows a link: `bin`,
`bin/dev-launches` and each launch are opened without following one, and
everything is removed through those descriptors. It trusts `bin/dev-launches` as
yours, written only by this launcher; it cannot see a Desk or companion started
some other way from an old launch directory, so stop any such process first.

Sets in `bin/dev-components` are kept, one per lock and recipe, for switching
branches; an unused one can be deleted at any time, since launches hold copies.

`JPACK_DESK_JPACK` remains an explicit Runtime override and is reported as outside
the lock. An inherited `JPACK_DESK_GATEWAY_MANIFEST_SHA256` is cleared so the
synchronized Gateway is checked against the locked revision.
`desk-dev.py status` reports the versions the running backend was launched with
and flags a changed lock as requiring a restart.

`start` stays idempotent while Desk is already running; use `restart` after
updating the checkout. The launcher does not pull or merge source changes. A
restart is an explicit interruption; scheduled work should use a managed
installation and its normal maintenance process. Existing job releases and user
data are never rewritten by synchronization.
