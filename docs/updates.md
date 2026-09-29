# Desk releases and updates

The update unit is one tested Desk bundle: Desk, Runtime, Runner, its source
worker, Gateway and adapters. Existing job releases keep their digest-pinned
Runtime and pack. Upgrading Desk does not reinterpret those jobs.

## One component lock

`internal/releaseplan/components.json` is the source of component versions,
repository identities, exact commits and channels. Go embeds it; the Gateway
builder, Jobs compatibility CI and release packager read it. Development pins
are held rather than replaced with an older published release. A stable Desk
release refuses development pins. Runner may remain on its explicitly reviewed
preview pin until a stable Runner release exists.

`component-updates.yml` checks daily and on manual dispatch, then proposes newer
stable pins in a PR. It never merges its own changes. Configure
`COMPONENT_UPDATE_TOKEN` as a bot token with repository contents and pull-request
write permissions. This lets ordinary PR CI run automatically; using the default
workflow token can require a human to approve the resulting PR workflows.
Automation is active after the workflow reaches the default branch and the bot
credential is configured. Repository rules and review requirements still apply.

CI uses the lock for actual Gateway lifecycle/PDF checks and Runner recovery and
input-profile checks, plus Go tests, frontend tests and localization checks.
A pushed `vX.Y.Z` tag runs that same CI before publishing. The packager requires a
clean tree and that exact tag, fetches the locked commits, builds a complete
bundle, and publishes `release-manifest.json` and `checksums.txt`. The manifest
records component identities, platform, state compatibility epoch and every
bundled file digest. Stable publishing is Linux/amd64 initially. The installer
reports unavailable assets on other platforms rather than selecting another CPU.

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
