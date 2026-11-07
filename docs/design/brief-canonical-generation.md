# Brief generation on the canonical run service

The Generate or Regenerate action first reserves the existing brief subject.
The reservation freezes the source snapshot and selected model. A brief run owns
that subject and reservation token; it has no conversation owner.
Brief owner IDs retain the existing 1,024 UTF8 byte subject bound. Other canonical
owner kinds keep their 200 byte storage bound. Subject prefixes remain closed,
and case identity resolves uniquely from saved suites and case records.

`POST /api/assistant-briefs/reference` accepts exactly `subject` and `token`.
It returns a reference-only canonical start and, when present, the original run.
The server computes the revision from the stored snapshot bytes. JSON escaping
or Unicode round trips through the browser cannot change that revision.

`POST /api/assistant-runs` accepts purpose `brief` with that subject, revision
and token. Its admission ID is exactly `brief-` followed by the token. Connection
and connection-revision headers select the explicitly chosen current connection.
The server binds the authenticated session, policy, project, current profile,
connection revision and reserved model. Every physical dispatch resolves those
bindings and the saved source again. Definition and saved case snapshots are
server reads; job and run snapshots come from Runner's existing brief endpoint.
Saved document selections enter the existing verified document boundary, including
partial-source consent. Browser-extracted document prose is not verification.

The published Agent brief purpose and context boundary own execution and
request accounting. The executor supplies no Runtime, domain tools, child
executor, critic or delegation wrapper. Its API allowance is the published
purpose allowance; this is not a one-request workflow contract. Native execution
retains provider-managed accounting. Generation does not grant acquisition,
evaluation, pack-write, approval or decision authority.

`POST /api/assistant-runs/{run}/brief` accepts exactly `token`. It reads the
complete canonical proposal and validates the existing four text sections and
length bounds. It never accepts browser-authored sections as canonical output.
Before saving, the server rechecks the reservation, expiry, source, model,
session, policy, profile and connection. Selected document bytes are verified
outside the private-data lock and compared again inside the final transaction.
The saved revision is read back. A repeated acknowledgement first reconciles the
exact already-saved token, snapshot, model and text, including after source changes.
The model-free `GET` on the same run/brief route verifies this exact saved
receipt after a lost acknowledgement; finding only a matching token is insufficient.

Navigation detaches the status reader. Reopening discovers the same admission
and may finish its authorized commit; it never regenerates automatically. A
lost start acknowledgement triggers only a lookup of the same admission.
Interrupted output and operations stay in the canonical journal. Explicit Stop
cancels the canonical run and releases its own reservation. Existing legacy
brief APIs and saved history remain compatible.

The concrete brief executor is a factory input. Production executor activation,
the full surface inventory and release validation remain separate gates.
