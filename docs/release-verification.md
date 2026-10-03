# Verify a Desk release bundle

The component lock selects a published version and exact source commit. The
release packager downloads the platform archive from that component's repository,
checks its SHA-256 against `checksums.txt`, and requires a GitHub build attestation
signed by that repository's `.github/workflows/release.yml` at the locked tag and
commit. It refuses self-hosted attestations. A missing asset, failed checksum or
failed attestation stops packaging; it never substitutes a locally built binary.

Runtime, Runner, the source worker, Gateway and the seven adapters used by Desk
are copied unchanged, with their published license notices. Extra upstream
programs that Desk does not use are not installed. Desk and the embedded web UI
are built from the tagged Desk source. The development `build-bundle.py` and
`dev-components.py` workflows still build source and are not release packaging.

`component-artifacts.json` records the upstream archive identities and copied
file hashes. Desk's release manifest covers that file as well as every bundled
program. The archive smoke check downloads and verifies the upstream archives
again and compares their bytes with the completed bundle before starting it.
This catches a later packaging step replacing `jpack` with a same-version rebuild.

## Reproduce the packaging checks

Use a clean, disposable clone at the release tag, with the Go version required
by `go.mod`, Node 22, Python 3.12 and a current GitHub CLI supporting attestation
verification. Authenticate `gh` for public release/attestation reads; CI uses its
read-only `GH_TOKEN`. No personal component-update token is required.

```sh
npm --prefix web ci
python3 scripts/component-releases.py verify
python3 scripts/package-release_test.py
python3 scripts/published-components_test.py
python3 scripts/desk-update_test.py
python3 scripts/assemble-release_test.py

# For a PR smoke test only, create this tag in the disposable clone. Never push it.
git tag v0.0.0 HEAD
# Keep output outside the checkout so the clean-tree check remains meaningful.
release_output=$(mktemp -d)
python3 scripts/package-release.py 0.0.0 --output "$release_output"
python3 scripts/check-release.py "$release_output" 0.0.0
```

For a real release use its existing tag and version instead of `v0.0.0`.
`.github/workflows/release-build.yml` runs these packaging/smoke operations on
Linux/amd64, macOS/amd64 and macOS/arm64, then `assemble-release.py` verifies the
complete platform set and combines checksums. One platform's local check does
not establish native execution on another platform.

## Match the Runtime used by a job record

For the currently locked Linux/amd64 Runtime, download and authenticate it:

```sh
gh release download v0.26.0 --repo Judgment-Pack/judgment-pack-runtime \
  --pattern judgment-pack_0.26.0_linux_amd64.tar.gz --pattern checksums.txt
sha256sum --check --ignore-missing checksums.txt
gh attestation verify judgment-pack_0.26.0_linux_amd64.tar.gz \
  --repo Judgment-Pack/judgment-pack-runtime \
  --signer-workflow Judgment-Pack/judgment-pack-runtime/.github/workflows/release.yml \
  --source-ref refs/tags/v0.26.0 \
  --source-digest 1d38cda68e50b1bb0b9a40878f5c6ee8966c5f45 \
  --deny-self-hosted-runners
```

Extract `jpack` into an empty directory only after verification. Compare its
SHA-256 with the executable digest frozen by the job release, then supply that
executable to Runner's `verify-run --runtime`. Select the version and platform
named by the record, not today's latest release. See Runner's verification
instructions for the record/store arguments. The program's reported version
alone does not identify its bytes.

Older Desk bundles built their own components. Records from those bundles, source
builds or explicit Runtime overrides still require the original binary; the
published Runtime at the same version may have another digest. Retain original
executables and records. This packaging change does not migrate them.
