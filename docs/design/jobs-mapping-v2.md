# Mapping v2 in Jobs

Jobs → Create job → Input source → **Mapped sources** supports an ordered mapping
of caller case data, local JSON files, selected Google Drive JSON files and
MCP operations. The mapping and trusted profile digests are frozen in the release.
V1 local-file and Drive jobs keep their original picker and mapping workflow.

The first editor is intentionally advanced: named sources are listed compactly,
and **Edit mapping** exposes the JSON request templates, projections, derivation
rules, typed parameters, target admissions and deliberate omissions. Adding a
source moves remaining case projections to that source as a starting point;
review its actual response paths and request arguments before reading. Model
values require explicit `generated` admission for each affected target. Profile
classifications cannot be edited here. Sources returned through a model-backed
MCP tool are supported; HTTP/model API adapters are not yet available.

**Read sources and preview** is the only acquisition trigger. Desk asks Runner
for the next request, fetches it through the configured gateway, then submits the
receipt back for verification before asking for another request. One source may
use only derived values from earlier sources. Cancellation, a changed mapping,
case input, file selection or gateway/profile configuration invalidates the
preview. The browser does not expand templates or run derivation rules.

After a successful input preview, **Check release** checks the actual pack,
saved behavioral tests, mapping coverage and sample result. Creation requires
review of that release. A new operational run starts with empty case input and
new file selections, using the fixed mapping. A retry retains its exact payload
and idempotency key; it never silently fetches new source data.

Completed runs show retained per-target lineage and offer **Download verification
record**. History does not re-fetch sources or apply today's freshness limits to
historical inputs. Use Runner's `verify-run` with independently trusted public
profiles and the release digest to check an export offline. Downloads contain
private case, request and response data; the ordinary view keeps raw proof behind
technical disclosure. Local storage remains the artifact store.

The download asks Runner for verification export version 5, saves the answer as
it comes, and names the file for the version Runner answered
(`<run>-verification-v<N>.json`). Runner answers an earlier version where the run
lacks what a later one carries, with HTTP 200 either way:

| Version | What it carries | Runner answers it to a request for 5 when |
| --- | --- | --- |
| 5 | the audit record's exact bytes, the run's chain entry and its checkpoint, and the record's signature sidecar | the run has all of them |
| 4 | the record's exact bytes, the chain entry and its checkpoint; unsigned | the record has no signature sidecar |
| 3 | the record's exact bytes | the run has no chain entry, as one recorded before Runner `v0.5.0` chained its runs |
| 2 | no exact bytes of the record | the run holds none, as one recorded before Runner `v0.4.0` |

The page says which version it saved and what that version carries. For a
version-4 or version-5 file it names the run's chain entry by the sequence the
entry's own line gives ("chain entry 42, not checked"); for a version-2 or
version-3 file it says the export has no chain entry. Desk checks nothing in the
file: not the entry against the record, its checkpoint or the chain, and not the
signatures. `verify-run` reports the record bytes' SHA-256 as `recordDigest`.
Comparing it with a gateway receipt's `decision.recordDigest` is the reader's
step: `verify-run` does not make it. Desk forwards `version` on this route only,
as exactly one `2`, `3`, `4` or `5`, and refuses any other value itself. It
reads an export, and the run it is made from, up to Runner's `MaxExportSize`
(about 18.7 MiB), which is what `verify-run` reads; every other Runner answer
stays within 16 MiB, except the chain of runs.

**Download the runner's chain of runs**, under Jobs | Runs, saves
`run-chain.jsonl`: Runner's `GET /v1/run-chain`, every entry's line in sequence
order, exactly as Runner sent it, which `verify-run --chain` reads. Desk passes no
query to Runner and keeps Runner's `application/jsonl`. Runner sets no bound on
the chain; Desk's is 64 MiB (65,536 entries at the 1024-byte longest line Runner
writes, and some 220,000 at the 300 bytes an entry takes in practice). Desk reads
the whole answer before it sends any of it, so a chain past the bound, or a
transfer Runner aborts, is an error and the page saves nothing; neither is ever
passed on as a shorter chain. Nothing in the file is checked here.

## Installation trust

Install the matching Runner binary and pass an installation-owned JSON file:

```sh
jpack-desk --runner /absolute/path/jpack-runner \
  --runner-input-profiles /absolute/path/trusted-input-profiles.json \
  /absolute/path/project
```

The file is an array of Runner `InputProfile` objects, limited to 48 KiB. Each
profile pins an ID, public key, authority, source, acquisition class, adapter
name/version/digest, endpoint and (for MCP) allowed tools. Use the actual deployed
gateway identity and reviewed adapter metadata. Do not derive class authority
from a project, mapping, or unverified response. Runner rejects invalid/conflicting
profiles at boot. Restart Desk after changing the file; changed pins require a
new release. Public profiles can be inspected through authenticated
`GET /api/operations/input-profiles`; no browser write API exists.

Desk only acquires profiles matching its configured gateway's key and authority.
Drive selections additionally require the managed local gateway and enabled
document processing, so picker grants cannot be sent to a remote gateway.
Drive mappings generated by the editor bind `fileId` and `grant` through explicit
string case parameters; the picker fills those fields. All acquired sources in
one preparation share a session. The gateway session is sealed for cleanup,
but Runner currently verifies receipts, not session completeness.

Cases, request parameters and projected claims use the gateway's bounded safe
integer JSON domain. Original files and signed responses retain their exact
numeric text; do not replace retained response bytes by a parsed-and-reserialized
JavaScript object. The Jobs wire encoder preserves them. Runner is authoritative
for schema, proof, freshness, derivation and class admission.

## Boundaries and validation

This is an explicit, browser-driven acquisition workflow. Accepted evaluation
runs remain durable when the browser closes. Unattended acquisition, schedules,
runner-issued sealed preparation sessions and HTTP catalog operations are future
work. A receipt proves which source answered which request; it does not prove
that the answer is true. Missing/unknown claims are retained as such.

Checks cover v1 regression, sequential verified-prefix planning, denied generated
admission, wrong subject/arguments/signatures, changed relay/profile pins,
cancellation/late replies, frozen mappings, lossless wire bytes, and real isolated
Desk-to-Runner boot/planning. No real provider credentials are required by tests.

The Jobs companion CI check builds Runner `68e01c4` and Runtime `cbb0d91`, then
runs actual boot, recovery, profile and planning tests through Desk's authenticated
proxy. These are compatibility pins for the integration test, not automatic
installation or download settings.
