# Integration-first job inputs

Desk starts new jobs with **Integrations and case inputs**. Manual/API input remains available. Existing releases retain their input contract, including legacy single-file mappings.

**Add source** opens a shared integration chooser. Local JSON files and installed, compatible Runner profiles are selectable. A profile must match the configured Gateway authority and signer. MCP profiles need an allowed tool; the selected Drive file contract requires a record profile, managed local processing, and a connected account. Other source shapes remain unavailable until Runner has the corresponding execution contract.

**Add integration** opens the existing Gateway Connections surface without navigating away from the job. Only supported, permitted providers appear in this contextual catalog. Provider configuration and consent use the same implementation as connection settings. Blocked or unavailable entries cannot be used to create a job connection. After a successful connection, **Return to job** restores the chooser and refreshes installed profiles. It does not attach documents or select a source automatically.

An integration being connected does not authorize arbitrary operations or grant scheduled access. The chooser shows the installed background capability separately; Runner still validates the standing connection when a trigger is enabled. No new trusted profile is minted from browser-supplied metadata, and catalog discovery never grants execution permission. Profiles remain installation-owned. A catalog provider without a compatible installed Jobs profile is labelled unavailable for Jobs even when its connection succeeds.

Every source editor starts with **Integration**. For simple mappings, changing it preserves the target assignments but resets the request and authority pins. Acquired inputs and prior previews are invalidated. Advanced derivations or downstream source dependencies require explicit mapping review instead of automatic replacement. Existing source selection and unsaved case inputs survive setting up an unrelated integration. Credentials are never copied into a mapping or release.

The chooser controller and pending source configuration live outside the movable details portal, so desktop/mobile transitions retain both. Released mappings remain immutable.

Gateway owns provider discovery, setup, credentials and source execution. Runner owns trusted profiles, mapping verification, standing access and durable runs. Desk composes those contracts into the editor. Generic HTTP/model job execution, S3 scheduled discovery, and incremental file checkpoints shown in the static design remain separate backend work; this UI does not advertise them as implemented.

Validation covers catalog restrictions, signer mismatch, unsupported execution shapes, explicit return without attachment, request reset on source replacement, protected advanced mappings, stale preview invalidation and responsive portal state retention.
