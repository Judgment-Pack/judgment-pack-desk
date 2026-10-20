# Private Graph host registry

The saved-chat Go factory now supplies seven concrete Graph host tools through
Agent's `context.HostTool`, canonical phase gate and shared context accounting.
This is a private implementation foundation. The production executor is still
unregistered; existing browser chat and Graph authoring panes keep their current
engines. Policy, Dialect, test/source/attachment tools, client actions and their
workflows still require migration and certification.

`assistant_host_tools.go` holds one finite registry with declarations and
implementations together. Every tool validates exact argument names, required
values and bounds before work. Reads are explicitly `effect: read` and delegated
reader capabilities; only authoring guidance has reference context policy. No
mutable read is declared immutable. `propose_graph` is a parent Work proposal,
with no save, acquisition, execution, approval or Runtime grant. Plan and children
inherit Agent's existing intersections. Runtime's saved grant ceiling is unchanged.

The concrete producer reuses the rooted project reader, Graph declaration and
Runtime services. Configured ids select files; models supply no paths, URLs,
commands or write targets. Guidance obtains `author_graph` from Runtime with exact
current declared pack bytes (at most 64 pack ids and bounded context). A private
freshness precondition runs after Runtime serialization and before dispatch.
Checks retain Runtime's exact answer bytes inside the existing answer envelope.
Rehearsals use only configured saved Graphs, explicit input JSON on stdin and
`--rehearsal`; no missing facts are synthesized, and typed values are unchanged.

Every concrete call uses Agent's canonical `CallIDOf` and the held run/request
writer. Native callbacks additionally carry the existing durable callback
operation identity, whose intent binds the actual mapped tool offer and model
invocation. The private Codex bridge already replaces provider ids with opaque
host ids, so equal provider ids in different invocations remain distinct.
An ephemeral adapter scope marker checks the exact held CurrentRequest value and
Execution.RequestKey. The coordinator's copied root input passes; child inputs do
not. This metadata supplements canonical gates and recorded parent ToolCalls;
restored metadata, narrative and child reports confer no proposal custody.

Successful root Graph reads retain the current target, exact document digest and
exact declaration digest in a finite host receipt. Revision proposals require
that receipt and fresh matching declarations/files. Child reads remain useful
read-only results, but never establish the parent's revision basis. A reconstructed
registry reads the same retained custody; it cannot recapture changed or missing
basis data from a checkpoint narrative. Project exclusion spans only the bounded
proposal read/retain operation, with no private lock across model or Runtime work.

The only new domain-output variant is a Graph draft with immutable run, request,
canonical call and producer-operation association. Its host-generated identity,
exact content, current declaration path and optional read basis are persisted in
the same transaction as the result before success is returned. Exact retries use
the retained result; incomplete/uncertain operations never rerun. Storage/import
validation rejects missing, conflicting, foreign or duplicate associations.

Terminal projection binds these outputs into its prepared digest and appends the
existing GraphDraft snapshot shape while retaining prior drafts/source records.
Runs without domain outputs keep the earlier digest formula for prepared-receipt
compatibility. The existing prepare/write/readback/ack path, exact unrelated-chat
preservation and detached-draft migration remain authoritative. Proposal data
never writes a project Graph or authorizes the reviewed save path.

Focused fixtures cover actual Eino execution of all seven tools; actual private
CodexManager parent/child/parent calls with repeated provider ids; Work/Plan/read
intersections; stale/removed declarations and targets; missing/foreign call and
owner scope; exact payload and result replay; refusal before persistence success;
checkpoint reconstruction; tampered imports; and terminal write/ack recovery.
The standalone shared-reader probe reads produced snapshots and drafts retained
after conversation deletion through the existing browser readers. These fixtures
are bounded evidence, not a claim of full feature or provider certification.
