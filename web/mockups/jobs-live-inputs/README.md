# Source-driven Jobs input mock

Static design proposal only. All cases, connections, records and extraction outputs are illustrative. No production code, provider acquisition or persisted job is changed by this mock.

Open `http://localhost:5173/mockups/jobs-live-inputs/index.html#inputs` while Desk's Vite server is running. The review bar switches between Job & cases, Facts & evidence, Trigger, and Preview & release. Source rows open the existing-style right details pane. Theme switching is available.

The design separates case selection, per-target source bindings, trigger timing, and sample preview. Production runs retrieve inputs afresh. Constants remain per-field choices. Case discovery and event enrichment are proposed Runner extensions, not claims about current integrations.

The facts mirror the fields in the supplied screenshot. Evidence requirements and connection names are illustrative because the full pack was not supplied for this mock. AI extraction remains marked as generated and requires explicit target permission. Retrieval errors are distinct from confirmed absence. Release checks are shown as not run to avoid implying validation of the real pack.

Screenshots are rendered from this HTML/CSS using Desk's local Inter font and matching neutral light/dark colors. Existing application behavior is unchanged.

## Source operation revision

The source pane now separates **Request**, **Output mapping**, and **Run policy**. Examples cover an MCP tool with arguments, an HTTP endpoint with method/path/query/headers/body, storage artifact selection, and AI instructions selected as either a versioned prompt or a skill bundle executed by a configured worker. These are design examples, not installed providers.

Direct views accept `?source=screening`, `?source=cases`, `?source=vault`, or `?source=extract` before `#inputs`. Add `&tab=output` for artifact identity/availability mappings or `&instructions=skill` for skill execution.

`{{case.id}}` in the mock is readable placeholder notation; implementation must use typed parameter bindings rather than string substitution. Actual artifacts must be joined to the correct case and stored with identity/version/digest. Evidence availability does not claim truth or authenticity. The HTTP/model configuration is proposed: Runner's current operation path accepts MCP only.

## Facts and evidence relationship

The facts table now shows **supporting material** and **derivation**, not just a connection name and a path. Source chips configure the retrieval operation. Derivation controls open the output mapping. The required-evidence section records separate availability gates from the pack.

Logical flow: source operation → acquired artifact or record → derived fact → pack evaluation. It is many-to-many: one record may support several facts, and a derivation may consume several records. An acquisition log can support a negative finding, but a retrieval failure cannot establish absence. Fixed inputs and manual declarations remain labelled as assertions. A retained model response records the derivation; it does not replace the underlying source material or independently prove its claim.

## Storage revision

See [STORAGE-DESIGN.md](STORAGE-DESIGN.md) for naming patterns, incremental checkpoint semantics, late arrivals, stable reads and ownership. The storage mock includes a scoped folder-browser dialog and matching-file preview with example statuses. The **Files** tab supports dated folders, timestamped filenames, glob, regex, and exact-file examples; **Incremental** configures first load, changed versions and reconciliation; **Output mapping** controls records and case identity. Switching the purpose to evidence lookup changes deduplication into per-case selection semantics.

Direct examples:
- `index.html?source=vault&pattern=folders#inputs`
- `index.html?source=vault&pattern=filename#inputs`
- `index.html?source=vault&pattern=regex#inputs`
- `index.html?source=vault&tab=policy#inputs`
- `index.html?source=vault&role=evidence&tab=policy#inputs`

## Integration-first configuration

Integration is the first field in each source pane, ahead of operation/file settings. **Add source** opens a shared integration picker; **Add integration** opens the permitted, supported Gateway catalog. The fixture includes Gateway v3 provider IDs (Amazon S3, Google Drive, Notion, Obsidian and Gmail). Generic HTTP/model provisioning is not offered as if it were already in that catalog.

An Amazon S3 example can be configured, added to the in-memory list, then selected for the proposed storage source editor. This preserves the job draft, updates its storage root and marks the selection/mapping for review. S3 is explicitly labelled interactive-only: unattended discovery is a proposal, not a current implementation claim. Other catalog entries can simulate account setup but cannot be selected as compatible Jobs operations without the missing job contract. Existing MCP, HTTP and model rows are illustrative configured profiles, not new catalog providers.

The production picker must combine permitted Gateway catalog entries, accessible configured connections and trusted Runner operation/background capabilities. Catalog visibility is not permission to connect or execute. Provider configuration and credentials remain owned by Gateway. Job releases freeze connection authority/operation configuration and mappings; credentials remain refreshable within that authority. Do not silently substitute another connection on deletion/revocation. No production code, accounts or credentials are changed by this preview.

Mock state is in-memory and resets on reload. Do not enter real credentials. Screenshot links: `integrations-config-dark.png`, `integrations-picker-dark.png`, `integrations-catalog-dark.png`, `integrations-setup-dark.png` (light variants also available).
