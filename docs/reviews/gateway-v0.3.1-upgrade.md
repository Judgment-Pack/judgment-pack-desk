# Gateway v0.3.1 local Desk upgrade

Desk's gateway pin and all seven gateway companion executables were updated from
`8361c0b1b1e6c913d4990d529a425c53e49fdcd4` to release `v0.3.1`, commit
`1ab277d127ba6ed60a4ede2c970742b04121d247`. The release was fetched into an isolated
checkout; the existing gateway working tree was not changed.

## Compatibility

- The upstream attachment-v1 opening-timeout fixture is preserved unchanged in
  `web/src/documents/__fixtures__/failed-timeout-encrypted.json`.
- Timeout and encryption failures remain distinct. No text from an unopened PDF
  becomes usable. Contradictory encryption states are refused.
- The companion manifest now includes the release label. An explicit different
  revision has no release label. Exact revision approval and executable checksum
  verification remain in force.
- Help & About (Runtime details) displays the local gateway's release and commit
  captured at launch. External/unavailable gateways are not labelled with the
  bundled version. A later file replacement does not relabel a running process.

## Verification

- Desk document tests: 199 passed.
- Runtime-details and local-gateway configuration tests: 21 passed.
- Full Desk Go suite passed; gateway lifecycle tests also passed under `-race`.
- Bundle packaging tests: 3 passed.
- Gateway attachment, document and PDF tests passed on the release checkout.
- Built gateway conformance: 30 canon vectors, 41 store vectors, 0 disagreements.
- Production UI build and staged Desk backend build passed.
- Isolated installed-bundle check passed: PDF extraction and signing, source
  status and refusals, two Desk instances, disabled processing, graceful/crash
  cleanup, stable signing identity, and external/invalid configuration handling.
- Chromium checks passed on port 8790 and localhost:5173 using ordinary local
  sign-in: gateway release and exact commit visible, no page errors. The initial
  development-page check timed out; a fresh normal browser load and the final
  explicit version/commit assertions passed without an application code change.

## Local installation

The tested bundle was installed and Desk restarted with its existing launch
settings. Its explicit manifest approval was updated to the tested bundle's
checksum. The running gateway executable matches the manifest. The project,
configuration digest, signing identity, runtime binary and runner binary were
preserved; the runner reports healthy. No Jobs were present before restart.

The runtime remains a local development build; this update does not replace it
with the older v0.22.0 release or relabel it as a release. No commit was made.
