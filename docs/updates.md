# Desk releases and updates

The update unit is one tested Desk bundle: Desk, Runtime, Runner, its source
worker, Gateway and adapters. Existing job releases keep their digest-pinned
Runtime and pack. Upgrading Desk does not reinterpret those jobs.

## One component lock

`internal/releaseplan/components.json` is the source of component versions,
repository identities, exact commits and channels. Go embeds it; the Gateway
builder, Jobs compatibility CI and release packager read it. Development pins
are held rather than replaced with an older published release. A stable Desk
release refuses development pins. Runner is pinned to its first stable release,
`v0.2.0`.

Gateway is pinned to `v0.8.0` and includes `adapter-render`, required by its
local source plan. Its Drive connection uses whole-Drive consent and Desk
search-and-select. Upgrade the catalog, selection UI and relay together with
the pin. Older Drive connections may report `reconnect-required` even while
status says connected; reconnect explicitly to grant the new scope.

Dependency updates use two bots with separate responsibilities:

- **Dependabot** (`.github/dependabot.yml`) checks Go modules, the `web` npm
  dependencies and GitHub Actions weekly. Related AI SDK, React and localization
  packages are grouped to keep compatible libraries together.
- **Renovate** (`.github/renovate.json`) only manages the three component release
  pins. It looks for published stable GitHub releases during its daily UTC
  maintenance window, resolves the underlying tag commit, and updates the
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
The hosted App controls run timing; the configured window is not a guaranteed
execution time. Configuration alone does not install the App. Without it,
component updates remain manual: run
`python3 scripts/component-releases.py propose`, inspect the diff, and open a PR.

Neither bot is configured to merge its own PRs. Review and compatibility CI are
required. CI verifies each non-development pin against its published release and
peeled version tag alongside companion builds, so an updated version with a stale
or missing commit cannot pass. Existing job releases remain unchanged.

CI uses the lock for actual Gateway lifecycle/PDF checks and Runner recovery and
input-profile checks, plus Go tests, frontend tests and localization checks.
A pushed `vX.Y.Z` tag runs that same CI before publishing. The packager requires a
clean tree and that exact tag, fetches the locked commits, builds a complete
bundle for Linux/amd64, macOS/arm64 and macOS/amd64, and runs each archive on
its native host before publication. The same packaging and smoke checks run on
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
