# Executable Exchange input imports

Desk retains the catalog release/tag/commit and exact entry files. Runtime validates the Dialect and establishes canonical identity. Runner's offline `verify-preparation` command replays each fixture at its retained historical time, with fixture profiles and an independently pinned preparation-file digest. The command opens no job store, acquires nothing and does not evaluate a pack. Its report explicitly leaves run binding unchecked.

The importer checks publication identity before filtering availability. A failing fixture cannot hide a conflicting artifact pair elsewhere in the release. Historical entries are digest checked in their own format; named format-3 references also retain their required artifact identity. Upgrade notes use prior manifest entry digests, never Dialect digests. Earlier releases are pinned and bounded to 32 releases and 32 MiB retained metadata. Unsupported or malformed held history is refused.

Configuring an imported input Dialect checks the exact definition against the chosen saved pack through Runtime. Declared pack identity plus exact-byte digest or supported version comparator constraints are enforced again before job preview. The immutable imported definition and local binding remain separate. Fixture profiles never become installation authority.

Gateway catalog v4 exposes adapter identity and answer/search/page capabilities. Empty capability declarations grant no research contract. The importer still supports earlier catalogs; only v4 yields research-capability discovery.

These checks do not authorize acquisition, model requests, publication, job execution or writes. Research source execution still requires retained response files at this milestone. Fresh research, local profile provisioning and the draft workflow interpreter are subsequent delivery steps.

Validation includes real Runtime/Runner imports of integer and named/unbound fixtures, independent Spec parity in Runner, wrong pack ranges/identities, changed history, conflicting unavailable entries, changed fixture bytes, and failed-check UI behavior. Two isolated imports are not a claim of live two-installation research or completed workflow conformance.
