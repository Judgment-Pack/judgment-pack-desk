---
status: proposed
date: 2026-10-02
deciders: maintainer
---

# Turn the gates on by default

Issue #182. The maintainer has decided that Desk turns its gates on by default.
This record sets out the design, what each gate establishes and what it does
not, every place Desk calls the runtime, and the questions still open. It is
proposed: no code changes until the maintainer accepts it.

It was checked against Desk `main` at `975fd31`, which pins Runtime `v0.25.0`,
Runner `v0.4.0` and Gateway `v0.8.1`. Every runtime behaviour quoted here was
measured on the released `jpack` 0.25.0 for linux/amd64, with its checksum
verified.

## Context

The runtime, Runner and the gateway each offer a gate. Desk's default path
reaches none of them.

- A new desk's `jpack.json` is `{"configVersion":"3","packs":{}}`
  (`internal/desk/desks.go:191`). It has no reviewed-set lock (runtime
  ADR-0019), no `requireReviewed` (ADR-0044) and no audit trail (ADR-0018).
- `--runner-require-tested-releases` defaults to `false` (`main.go:53`). Runner
  then creates a job from a release whose saved tests never ran. It already
  refuses a release whose tests failed or did not complete.
- Nothing in Desk marks packs as reviewed. The README says so on purpose: "No
  re-lock button", and the desk "writes no lock".

Three facts about Desk shape the design.

1. **Desk itself makes no deciding run.** With the pinned runtime, every
   `experimental_evaluate` Desk sends is declared a rehearsal. The one exception
   is a runtime whose tool listing Desk could not read (section 5). A rehearsal
   consults no lock and writes no record (runtime ADR-0028, ADR-0041). So
   `requireReviewed` refuses nothing Desk does, and nothing Desk does writes to
   the audit trail.
   The project's gates hold the *other* callers: an agent given the project's
   `jpack mcp`, a script, a CI step.
2. **Jobs never read the project's configuration.** For each release, Runner
   writes its own `jpack.json`
   (`{"configVersion":"3","packs":{"target":{"path":"pack.json"}},"audit":{"dir":"audit"}}`),
   runs `packs lock` over it, and evaluates by `--pack-id target` under that
   lock (Runner `internal/runner/runtime.go`). So every Jobs record says
   `reviewed: true`, whatever the project's own lock says. The project's gates do
   not reach Jobs, and this record keeps it that way.
3. **Desk's Go starts only `jpack mcp`.** It hands the runtime's path to Runner
   and runs no other runtime command. `packs lock` and `packs verify` have no
   MCP tool in 0.25.0. The review step is the first place Desk's Go runs them.

## Decision drivers

- A new desk is gated from the start, and a deciding run is refused for a draft,
  not for every run.
- The owner's loop stays free: drafting, rehearsing and testing never need a
  lock.
- Every gate says, where the owner meets it, what it establishes and what it
  does not.
- Nothing in an existing project changes without the owner's confirmation.
- Nothing here makes runtime ADR-0047 (chained, signed and witnessed records;
  Desk #186) harder to build.

## Decision

### 1. New desks start under reviewed law, with deciding runs recorded

`POST /api/desks` writes this `jpack.json`:

```json
{"configVersion":"4","requireReviewed":true,"audit":{"dir":".desk-private/audit"},"packs":{}}
```

If the maintainer answers question 1 with yes, it writes configVersion `"5"` and
adds `"requireComparableFacts":true`.

It also:

- creates `.desk-private/audit`, owner-only, beside the folders it already
  creates;
- runs `jpack packs lock` in the new folder, after writing `jpack.json` and
  before the desk's manifest. The manifest is already written last, so a failed
  lock leaves no desk.

**The initial lock.** `packs lock` accepts a project with no packs. On 0.25.0 it
reports "0 declared document(s)" and writes a lock that holds only `lockVersion`
and the configuration's digest. `packs verify` then exits 0. This lock needs no
confirmation, because it reviews no pack. It pins only the configuration bytes
Desk has just written.

Without it, `requireReviewed` refuses every deciding run, because the project
has no lock. With it, a deciding run is refused only when it applies a draft.
Measured on 0.25.0, over MCP, in a desk laid out this way:

| State | Deciding run | Rehearsal | Test tools |
|---|---|---|---|
| No lock | refused: no reviewed-set lock | evaluated | run |
| Empty lock; a pack passed as text | refused: applies a draft | evaluated | run |
| A pack added by Desk, not yet locked; by id | refused: `config-drift` | evaluated | run |
| After review and lock; by id | evaluated, `reviewed: true`, one record | evaluated, no record | run |
| The pack edited after the lock; by id | refused: `document-drift` | evaluated | run |

**A change to `jpack.json` holds every pack.** Adding a pack, or changing any
description or hint, is `config-drift`. The runtime then refuses a deciding run
of *every* pack by id until the next lock, because the configuration says which
file a decision id names. The review step says so.

**Where the trail lives: `.desk-private/audit`, private, never committed.**

- Records hold the facts and evidence as evaluated (ADR-0018). They may be
  sensitive.
- `.desk-private/` is already in a new desk's `.gitignore` (`desks.go:193`). It
  is owner-only. Desk's watcher skips it, and Desk's file API refuses both reads
  and writes under it (`internal/desk/watch.go:24`, `wireRelativePath` in
  `internal/desk/files.go`). So Desk's own editor can neither show nor change a
  record.
- The runtime requires the directory to be inside the project. `packs validate`
  reports `audit-dir-inside-root` as passed for this path. On 0.25.0 the trail
  file was written 0600, in a 0700 directory.
- Runner already keeps a managed desk's Jobs state under `.desk-private/jobs`
  (`internal/desk/jobs.go:66`).

Rejected: `audit/`, committed. Facts would enter version-control history, and
removing them later means rewriting it. It would also work against ADR-0047. Git
checks out, merges and rebases a tracked file. Checking out an older commit
presents an older trail, and merging two branches' trails puts two lines at one
sequence. Under a chain, both read as tampering.

Rejected: outside the project, for example in Desk's own configuration
directory. The runtime refuses an audit directory outside the configuration's
directory.

The cost: the trail is in no backup. Desk's backups hold chat data only
(ADR-0003), so losing the desk's folder loses its records. See question 3.

**ADR-0047.** This choice does not make it harder.

- The chain's lock, the signature sidecar, stamping tokens and the bytes a
  repair keeps all live beside the trail. That works inside
  `.desk-private/audit`.
- The signing key must not go there. `.desk-private` is inside the project, so
  anything that can read the project can read it. Desk #186 decides where the
  key lives.
- Handing over a checkpoint, or downloading the trail, needs a Go route, because
  the file API refuses `.desk-private`. That route must pass the trail's exact
  bytes through and never re-encode them (ADR-0047, "Exact bytes, wherever a
  record travels").

### 2. A review step: "Review and lock"

**What it shows.** Desk's Go runs `jpack packs verify --format json` in the
project, the same way the relay starts `jpack mcp`: through the held directory
descriptor on Linux, and through the re-checked pathname elsewhere. It lists the
runtime's findings in plain words:

| Finding | Shown as |
|---|---|
| `config-drift` | The project file changed; every decision waits for a lock |
| `document-drift` | Changed since the last lock |
| `lock-entry-missing` | New, never locked |
| `locked-but-undeclared` | Removed from the project |
| `document-missing` | File missing |
| `path-mismatch` | Locked at another path |

Desk shows the runtime's answer. It computes no verdict of its own.

**The diff.** The lock holds digests, not bytes, so the runtime cannot say what
changed. Desk keeps its own copy of each file it locks, in
`.desk-private/reviewed/`, named by SHA-256. At the next review it shows a diff
only when the copy's digest equals the lock's entry. Otherwise it says it has no
earlier copy to compare. The copy helps the owner read; the lock is the record.

**The lock covers the whole set.** `packs lock` pins every declared pack and
graph, and the configuration. Desk cannot lock one pack. So the step lists every
difference, and one confirmation covers all of them and says how many.

**The bytes confirmed are the bytes locked.** The confirmation carries the
digests the owner was shown. Desk checks them against the files before it runs
`packs lock`, and against the lock it wrote afterwards. If a file changed in
between, Desk puts the previous lock's bytes back, writes nothing else, and
shows the step again.

**What it says.** "Locking records that you confirmed these exact files as this
project's reviewed set. It is not a second person's approval, and it records no
name." The lock carries no identity: only `lockVersion` and digests.

**Until then, a new or changed pack is a draft.** A deciding run that applies it
is refused. Rehearsals, the test tools and Jobs go on as before. Matrices and
rows are outside the lock (ADR-0019), so editing test cases never needs a
review.

**Where.** A "Review and lock" action on Packs, with each pack's finding beside
its name.

### 3. Tested releases by default

- `--runner-require-tested-releases` defaults to `true`. The owner turns it off
  with `--runner-require-tested-releases=false`. It applies to every desk of the
  installation.
- Runner `v0.4.0` refuses only a new job, with `release_untested` (HTTP 409).
  Jobs created earlier keep running.
- Desk tells the page whether the policy is on; today the page cannot know. The
  untested-release note (`web/src/jobs/ReleaseReadiness.tsx:19`) then says that
  this installation will refuse the job, and how to turn the policy off.

What it establishes: the release's saved tests ran and passed, against that
release's pack and runtime. What it does not establish: that the tests are
right, or that they cover the pack. A pack with no saved tests cannot become a
job until it has some.

### 4. Existing desks are offered the upgrade, never forced into it

The offer covers the project Desk was started on, and managed desks created
before this change. It appears once as a note on Packs that the owner can
dismiss, and stays available in Admin → Project. It lists each change, what it
writes, and why:

1. **`jpack.json`:** `configVersion` moves to `"4"` (or `"5"`, by question 1),
   with `requireReviewed` and the audit directory `.desk-private/audit`, created
   owner-only if it is missing. A project that already declares an audit
   directory keeps it. Every other
   member, and the members' order, is carried through, as
   `web/src/packs/jpackConfig.ts` already does.
2. **`.gitignore`:** add `.desk-private/` when the project is a Git work tree
   that does not already ignore it, so that records are not committed.
3. **The first "Review and lock"** of the project's current packs.

The rules:

- Nothing is written before the owner confirms.
- The configuration and the first lock go together. If the lock is not
  confirmed, or fails, Desk restores the configuration's previous bytes, because
  `requireReviewed` with no lock refuses every deciding run.
- A project that declines keeps every file byte for byte.
- A project that already keeps a lock, for example one its CI checks, is told
  that the new configuration is `config-drift`. Every deciding run by id is
  refused, and its CI's `packs verify` fails, until the new lock is committed.
- Desk asks the runtime which configuration versions it reads
  (`packs schema --format json`, `supportedConfigVersions`) and offers none it
  cannot read. `"4"` needs runtime 0.24.0 or later, and `"5"` needs 0.25.0.
  Measured: 0.24.0 refuses a `"5"` configuration outright, and that refusal
  stops every project tool, `list_packs` included.

### 5. Every place Desk invokes the runtime

At `975fd31`. A draft is a pack passed as text, or a declared pack whose bytes,
or whose configuration, differ from the lock.

Go:

| Where | How | What runs | Configuration | Standing under `requireReviewed` |
|---|---|---|---|---|
| `internal/desk/relay.go:147`, through `project_linux.go:72` (`project_other.go:16` elsewhere) | `jpack mcp`, one per WebSocket, in the project directory | every MCP tool; the page is the client and the relay passes frames untouched | the project's `jpack.json` | carries every web row below |
| `internal/desk/jobs.go:64`, boot line at `:130` | the Runner companion, given the runtime's path | Runner runs `spec validate`, `packs lock`, `packs list` and `experimental evaluate --pack-id target` | Runner's own per-release `jpack.json` (configVersion `"3"`) and lock | Jobs: Runner's own release lock |
| `internal/desk/builds.go:69` | reads build information from the file | nothing is executed | none | not an invocation |

Web, all through the relay:

| Where | Tool | Arguments | Standing |
|---|---|---|---|
| `web/src/mcp/queries.ts:446` (`useEvaluate`), used by `routes/PackEvaluate.tsx:49` and `packs/edit/TryItPane.tsx:83` | `experimental_evaluate` | `pack` or `pack_id`; `rehearsal: true` when the runtime advertises it (`queries.ts:430`) | Rehearsal. If the tool listing could not be read, the call goes without the flag and is a deciding run: refused for a draft, recorded for a reviewed pack. The refusal panel shows the runtime's message. |
| `web/src/packs/test-workspace/TestsWorkspace.tsx:479` (one case without an expectation) | `experimental_evaluate` | `pack` as text, `rehearsal: true` | rehearsal |
| `web/src/research/checkCandidate.ts:117` (with `validate` at `:102`) | `experimental_evaluate` | `pack` as text, `rehearsal: true` | rehearsal |
| `web/src/research/useResearchRun.ts:232` | `experimental_evaluate` | `rehearsal: true`, forced on the authoring run's own connection | rehearsal (confirmed) |
| `web/src/assistant/session.ts:481` (`gateTransport` in `assistant/toolGate.ts`) | the assistant's tools: `get_schema`, `list_examples`, `get_example`, `validate`, `experimental_evaluate` (`config/deskConfig.ts:133`) | `experimental_evaluate` is rewritten to `rehearsal: true` on the wire. Codex's tool calls come back through the page to the same gate (`internal/desk/agent_run.go`). | reads and rehearsal |
| `web/src/packs/test-workspace/TestsWorkspace.tsx:507` | `experimental_test_cases` | pack as text, and a matrix | test surface |
| `web/src/mcp/queries.ts:235`, `:281` | `experimental_test_packs`, `experimental_test_graphs` | | test surface |
| `web/src/mcp/queries.ts:111`, `:125`, `:196`, `:307`, `:350` | `list_packs`, `get_pack`, `validate`, `experimental_list_graphs`, `experimental_get_graph` | | read |
| `web/src/mcp/starters.ts:112`, `:154`, `:169` | `list_examples`, `get_example`, `get_schema` | | read |
| `web/src/research/expectations.ts:35`; `packs/test-workspace/proposals.ts:40`, `:73` | `experimental_validate_expectations`, `experimental_get_test_matrix_contract`, `experimental_validate_test_matrix` | | read; evaluates nothing |
| `web/src/mcp/prompts.ts:110` | the prompts `author_pack` and `test_pack` | | read |
| `web/src/jobs/JobsView.tsx:102`, `:162` | `get_pack`, then Runner's `previews` with the saved bytes | | Jobs. The release is made from the saved bytes, whatever the project's lock says (question 4). |

New with this record: the review step's Go route runs `packs verify`,
`packs lock` and `packs schema` (sections 2 and 4).

Under `requireComparableFacts` the standing changes, because it refuses
rehearsals too (ADR-0046, point 12). Every `experimental_evaluate` row above
would be refused when a fact has a type that no comparison in the pack can
match. The test-surface rows and Jobs would not.

### 6. What each gate establishes, and what it does not

**The reviewed-set lock, with `requireReviewed`.**

- It establishes that a recorded deciding run applied exactly the bytes last
  locked, and that its record names that lock (`reviewed: true` and
  `reviewedSet`). A deciding run of anything else is refused.
- It does not establish that the pack is right, that anyone but the owner looked
  at it, or who confirmed it. It is not a wall: whoever can edit the project can
  edit a pack and lock again (ADR-0019). A rehearsal or a test tool still
  answers under a draft.

**The audit trail.**

- It establishes that each completed deciding run against this project leaves a
  line with the pack's digest, the inputs and the disposition.
- It does not establish anything about rehearsals, tests or refusals, which
  write nothing. Nothing in Desk itself writes to it (see Context). A line is not
  signed or chained, its `at` is the operator's clock, and anyone who can write
  the project can change it. ADR-0047 is the design for that, and Desk #186
  tracks Desk's part. Neither is built here.

**`requireComparableFacts`, if chosen.**

- It establishes that no evaluation of this project reads a fact of a JSON type
  that the comparison reading it can never match.
- It does not establish that the facts are true, or present. An absent fact is
  not refused.

**Tested releases.** See section 3.

**Whom `requireReviewed` binds** (ADR-0044, point 5): a caller that neither
chooses which configuration a run reads, nor can edit that configuration or its
lock.

- **Desk's assistant** reaches only the five tools the ToolGate allows, over the
  `jpack mcp` that Desk started. It has no file tool, and Codex runs with no
  environment (ADR-0008). So it is a caller of the bound kind. But every
  evaluation it makes is a rehearsal, so the requirement never refuses it, and
  it never records.
- **The owner, in Desk,** is not bound, by design. Desk's editor can write
  `jpack.json` and `jpack.lock.json`.
- **An agent with the project folder** (file or shell tools, or a coding agent
  working in the folder) is not bound. It can edit a pack and run `packs lock`,
  or point `--config` at another file.
- **An agent given only a `jpack mcp` that someone else started** on this desk's
  configuration, with no file or shell tools, is bound.

**Giving an agent the project's tools without that access.** Start `jpack mcp`
for the agent with `JPACK_CONFIG` naming the desk's `jpack.json`, and give the
agent no file or shell tools. It then cannot choose the configuration or edit
it. This holds only as far as its client really withholds those tools, since the
server runs as the owner's user. Desk documents this now, in the README and the
in-app help (PR E). It does not build a Desk-hosted MCP endpoint for outside
agents in this line. That would be a new authenticated surface, and needs its
own design (question 5).

### Statements this record makes stale

PR E corrects these, except the last, which PR D corrects.

- README: "**No re-lock button**" and "the desk has no lock parser, writes no
  lock". Section 2 reverses both.
- README: "**Nothing about this pack's standing in the reviewed set appears
  here**". Section 2 reverses it, by quoting the runtime's own `packs verify`.
- README: "the Evaluation payload carries no lock member". Untrue since runtime
  0.24.0: a deciding run's payload carries `reviewed` (ADR-0044).
- README: "Phase 3 runs matrices from disk, which is where a lock's answer will
  appear regardless". The test surfaces never consult the lock (ADR-0019).
- `web/src/packs/jpackConfig.ts:49`: "`SupportedConfigVersions()` is
  `{"1","2","3"}`". The runtime now reads `"1"` to `"5"`. The code carries other
  members through, so it keeps working.

## Consequences

- Good: a new desk holds deciding runs to the owner's reviewed set from the
  start, and records them.
- Good: the review step turns a CLI step the README left to the owner into one
  confirmation that Desk can explain.
- Good: jobs come only from tested releases, unless the installation says
  otherwise.
- Bad: every edit to `jpack.json`, such as a new pack, holds every deciding run
  by id until the owner locks again.
- Bad: a pack with no saved tests cannot become a job until it has some.
- Bad: the trail is in no backup.
- Bad: Desk's Go now depends on the output of `packs verify`, `packs lock` and
  `packs schema`. Their JSON output is versioned (`outputVersion` `"2"`).
- Neutral: in Desk itself, `requireReviewed` refuses nothing and nothing is
  recorded. Turning the gates on mainly holds other callers. If question 1 is
  answered yes, Desk's own rehearsals can be refused for wrong-typed facts.

## Questions for the maintainer

1. **Should new desks also set `requireComparableFacts`, at configVersion
   `"5"`?**
   - What it refuses: an evaluation in which a present fact has a JSON type that
     some comparison in the pack can never match. For `equals`, `not-equals` and
     `in`, that is a type no operand has, `null` included. For an ordered
     comparison, it is anything but a decimal string. The check covers every
     comparison the pack states, not only those the evaluation reaches
     (ADR-0046).
   - It refuses rehearsals too. In Desk, that means the Evaluate page, Test
     draft, a single test case run without an expectation, and the assistant's
     checks and rehearsals. Desk shows the runtime's refusal,
     `JPS-FACTS-COMPARABLE-REQUIRED`, which names each pointer, its type and
     what the comparison can match, and never a value. The assistant receives it
     as a tool error it can correct. Measured on 0.25.0: `"amount": 120` against
     `greater-than "5000"` was refused, by id and as text, as a decision and as a
     rehearsal.
   - Saved test suites (`experimental_test_cases`, `experimental_test_packs`)
     and Jobs are not refused.
   - The costs: an owner cannot see in Test draft what the pack does with a
     wrong-typed fact, though a saved test case still can. A pack that compares
     one pointer with operands of different types can never pass for that
     pointer, and must use one `in` instead. The runtime floor becomes 0.25.0.
   - **Recommendation: yes.** The trap it closes gives a confident wrong answer:
     a detector written `equals true` answers its fallback when the fact arrives
     as `"true"`, `1` or `null`. The refusal names the fix. Apply the same answer
     to the upgrade offer, as its own item.
2. **Should the new tested-releases default also apply to existing installations
   when they update?** It is an installation flag, not a project file, so the
   offer in section 4 does not cover it. **Recommendation: yes.** Existing jobs
   keep running, the refusal names the way out, and the release notes say how
   to turn it off.
3. **The trail is in no backup and no version control. Accept that for now?**
   **Recommendation: yes,** with the review step saying so. Add a download of
   the trail's exact bytes with Desk #186's checkpoint hand-over, not in the
   chat backups of ADR-0003.
4. **Should "Review this release" in Jobs show whether the pack's saved bytes are
   in the project's reviewed set?** Today a release is made from a draft as
   readily as from a reviewed pack, and Runner's own lock marks every Jobs
   record `reviewed: true`. **Recommendation: show it, as the runtime's
   `packs verify` finding for that pack, and refuse nothing.** Refusing would
   change Jobs, which this record keeps as they are.
5. **Should Desk build an MCP endpoint for outside agents, served by Desk with
   the desk's configuration and no file access?** **Recommendation: not now.**
   Document the `jpack mcp` setup in section 6. Build the endpoint only for a
   concrete need, under its own ADR, because it is a new authenticated surface.

## Delivery, after acceptance

One PR each, in this order, each under Desk's review rules.

| PR | What |
|---|---|
| A | `--runner-require-tested-releases` on by default; the page learns the setting; the untested-release note names it |
| B | New desks gated: the configuration above, `.desk-private/audit` and the initial lock |
| C | "Review and lock": the Go route for `packs verify` and `packs lock`, the reviewed copies, and the page |
| D | The upgrade offer for existing desks |
| E | The README and in-app help: what is gated, what is recorded, what is not bound, and the stale statements above |

Not in this line: any part of runtime ADR-0047 or Desk #186, and the gateway's
write policy (Desk sends no writes through `/act`).

## More information

- Runtime, at `v0.25.0`: ADR-0018 (the audit trail), ADR-0019 (the reviewed-set
  lock), ADR-0028 and ADR-0041 (rehearsals), ADR-0044 (`requireReviewed`),
  ADR-0046 (`requireComparableFacts`), ADR-0047 (defensible records),
  `docs/building-with-packs.md`, and the `0.25.0` CHANGELOG entry.
- Runner, at `v0.4.0`: `internal/runner/runtime.go` (its own configuration and
  lock) and `internal/runner/store.go` (`release_untested`).
- Desk: [ADR-0001](0001-make-the-assistant-engine-a-slot.md) (the ToolGate),
  [ADR-0003](0003-private-chat-data-and-recovery.md) (private data and backups),
  [ADR-0008](0008-codex-subscription-agent.md) (Codex), and issues #182 and
  #186.
