---
status: accepted
date: 2026-10-03
deciders: maintainer
---

# Keep a desk's decision record defensible: key custody, checkpoint hand-over and stamping

**Amended 2026-10-05** (issue #217). No decision and no answer below changed.
The amendment records what has shipped since acceptance, and adds what the
Jobs record needs:
- status statements brought up to date, each with the release or PR that
  changed it: this introduction, "Runner and the gateway", the 0777 row of
  section 1's table, section 5 and "More information";
- `--limit 300` on the Jobs chain's `audit checkpoint --trail`, as section 2
  chose for the desk's trail (sections 5 and 8);
- `audit verify --trail <copy>` over the Jobs chain, in a Jobs record panel
  (sections 4, 5 and 8; issue #216);
- in the delivery table: PRs 1 and 2 marked shipped and 8a merged, PR 8
  split into 8a and 8b, and rows for Runner's signing key (issue #215) and the
  Jobs record panel.

Each amended passage says "Amended 2026-10-05".

Issue #186. Runtime ADR-0047 (accepted 2026-10-01) makes a decision record
defensible to someone who does not trust its operator. It names Desk's part:
key custody, checkpoint hand-over and stamping settings. This record decides
that part. The maintainer accepted it on 2026-10-03, with the answers recorded
under "The maintainer's answers". It sets out what Desk would build, what each piece establishes and
what it does not, every new runtime command Desk would run, and the questions
the maintainer needs to answer. Nothing here was built at acceptance.
*Amended 2026-10-05:* PRs 1 and 2 shipped in Desk v0.5.1 (#202; #204 and #205,
as 2a and 2b). PR 3a, key custody for the desks Desk makes, is #219; PR 8a
is #220. Neither is in a release yet, and rotation (3b) is not built.

It was checked against Desk `main` at `61a158c`, which then pinned Runtime
`v0.25.0`, Runner `v0.4.0` and Gateway `v0.8.1`
(`internal/releaseplan/components.json`). None of the runtime commands below
was then in a release. They were on runtime `main`, in the PRs that closed
issues #206 to #209 and #216: #211 to #215, and #217. The latest release then,
0.25.0, has none of them.

*Amended 2026-10-05, checked against Desk `main` at `072229f`:*
- #200 moved the pins to Runtime `v0.26.0`, Runner `v0.5.0` and Gateway
  `v0.9.0`, which Desk v0.5.0 and v0.5.1 carry.
- Runtime v0.26.0 ships #211 to #215 and #217, so every runtime command below
  is in a release.
- Runtime v0.27.0 adds a rule for the directories on a key's path (section 1;
  its CHANGELOG). Desk does not pin it yet.
- Line numbers in Desk's code are as of `61a158c`, except where an amendment
  gives another.

**How this was measured.** The released 0.25.0 cannot sign, so the runtime
behaviour quoted here was measured on runtime `main` at `581330e`. It was built
from source into a scratch folder, with a private build cache, for linux/amd64,
and reports its version as `0.0.0-dev`. The lab copied Desk's layout: a
configuration directory with an owner-only `secrets/`, and a desk folder under
its `desks/`. What an older runtime does was measured on the released `jpack`
0.25.0 for linux/amd64, with its checksum verified. No time-stamping authority
was asked: stamping was measured only against a refused connection and a
listener that never answers.

## Context

**Where a desk's records come from.** A desk keeps its trail in
`.desk-private/audit` (ADR-0009, section 1). Desk itself writes nothing there.
Every `experimental_evaluate` it sends is a rehearsal, except when the
runtime's tool listing could not be read (ADR-0009, section 5). The deciding
runs that write records come from other callers:
- an agent given the desk's `jpack mcp`, started as ADR-0009 section 6
  describes;
- a script;
- a person at a shell.

Jobs record in Runner's own per-attempt trails, never in the desk's.

**What runtime `main` adds.** Each item is measured below.
- **The chain** (#211): on by default wherever a project keeps a trail. That
  includes configVersion `"4"` and `"5"`: measured under `"5"`. Only
  `"chain": false` needs configVersion `"6"`. Once Desk pins such a runtime,
  every desk's trail is chained, with no change in Desk.
- **`jpack audit verify`, `checkpoint` and `repair`** (#212 and #213). This
  includes `checkpoint --since/--limit` for a deliverer, files of held
  checkpoints for `--expect`, and `--require-checkpoint-through`.
- **Record signatures in a sidecar, `signatures.jsonl`** (#214 and #217). A key
  is named by `audit.signingKey`, which needs configVersion `"6"`, or by the
  `JPACK_SIGNING_KEY` environment variable at any configVersion. The key is
  made by `audit key generate` and rotated by `audit key rotate`.
- **Stamping** (#215). `jpack audit stamp` asks an RFC 3161 authority, named by
  `audit.timestampAuthority` (configVersion `"6"`) or by `--tsa` at any
  configVersion. The token is kept in `stamps.jsonl`. `audit verify
  --tsa-roots` checks the stamps.

**How Desk starts the runtime today.**
- **The relay's `jpack mcp`** (`internal/desk/relay.go:153`, `runtimeEnv` at
  `internal/desk/runtime.go:84`):
  - for a desk Desk made, it gets Desk's environment without `JPACK_CONFIG`;
  - for the project Desk was started on, the whole environment.
- **The commands Desk runs to completion** (`runRuntime`,
  `internal/desk/runtime.go:148`):
  - Desk's environment without `JPACK_CONFIG`;
  - at most 20 seconds (`runtimeCommandTimeout`) and 64 KiB of standard output
    (`runtimeAnswerLimit`);
  - in the directory Desk holds, and its callers name `--config jpack.json`.
- **Runner** gets only `LANG` and `LC_ALL` (`internal/desk/jobs.go:138`).

So a `JPACK_SIGNING_KEY` set where Desk was started reaches every runtime Desk
starts, for every desk. It does not reach Runner.

**No `audit` command is an MCP tool.** Measured: `tools/list` on `main` names
17 tools, and none of them is an `audit` command. The runtime's guide says why:
a verification an agent runs on the trail of the server it is using shows
nothing to someone who does not trust that server's operator. So Desk's Go runs
these commands, as it runs `packs verify` and `packs lock` (ADR-0009, section
2).

**Exact bytes.** Desk's file API refuses `.desk-private`. So handing over a
checkpoint, or downloading the trail, needs a Go route, and that route must
pass the bytes through untouched (ADR-0009, section 1; runtime ADR-0047, "Exact
bytes, wherever a record travels").

**Runner and the gateway.**
- **Runner `main`** keeps an installation-level chain of runs, with export
  version 4 and `GET /v1/run-chain` (Runner #34, closing #30). It was
  unreleased: v0.4.0 has export version 3. *Amended 2026-10-05:* Runner v0.5.0
  ships it.
- **Runner #36**, open at acceptance, proposed that Runner check record
  signatures. *Amended 2026-10-05:* Runner #37 closed it, in v0.5.0. Runner
  gives each run's runtime an installation key, named by the boot line's
  `signingKey`, and `verify-run` checks the record's signature (export
  version 5).
- **Desk** asked Runner for export version 3 (`web/src/jobs/JobsView.tsx:303`)
  and refused any version but 2 and 3 (`internal/desk/jobs.go:334`).
  *Amended 2026-10-05:* since #220 it asks for version 5
  (`web/src/jobs/JobsView.tsx:339`) and forwards 2 to 5
  (`internal/desk/jobs.go:363`).
- **Gateway `main`** has `requireSignedRecord` (gateway #201, ADR-0012). It was
  unreleased: v0.8.1 predates it. *Amended 2026-10-05:* Gateway v0.9.0 ships
  it. Desk sends no writes through `/act`, so this record does not use it.

## Decision drivers

- Nothing Desk shows claims more than the runtime's own check establishes.
  Every panel says what it does not establish, in the runtime's words where the
  runtime has them.
- The operator's own copies are labelled as the operator's: Desk's record of
  hand-overs, Desk's verification, and the local gateway.
- No new network traffic, key or third party unless the owner configures it.
  The exception is a key for a new desk, if question 3 says so.
- Nothing on the decision path changes. Signing, hand-over and stamping never
  refuse or delay a decision. The runtime already works this way: "pending,
  never failed".
- Exact bytes from the runtime's file or output to the owner's saved file, with
  nothing parsed and re-encoded in between.
- With a runtime that cannot do a thing, Desk hides or disables it, and never
  pretends.

## Decision

### 1. Key custody

**Where the key lives.** One key per project, as an Ed25519 seed file at:

```
<Desk configuration directory>/secrets/signing/<project>.seed
```

- **The configuration directory** is `$XDG_CONFIG_HOME/jpack-desk`, or
  `~/.config/jpack-desk` (`configDirFor`, `internal/desk/assistant.go:119`).
- **`secrets/`** is the directory that already holds the assistant credential.
  Desk's custody validates it and holds it by descriptor
  (`internal/desk/custody.go`):
  - every component is a real directory, not a link;
  - no component is writable by group or other;
  - Desk's own directories are owned by the user and are 0700.
- **`signing/`** is made under it the same way, 0700.
- **`<project>`** is the desk's id, for a desk Desk made. For the project Desk
  was started on, it is the hex SHA-256 of the project's path, as Runner names
  that project's Jobs store.
- **The seed** is 0600, and stays the only name of its file.

One key per project, not one per installation:
- a copied key then signs for one trail only;
- a rotation is that project's alone;
- the process-wide hazard of `JPACK_SIGNING_KEY` (below) does not arise.

**What the runtime accepts, measured.** The lab project was a desk folder at
`<config>/desks/<id>`. Each key was named to the runtime, and `packs validate
--format json` was read for its `audit-signing-key` check.

| Key | Runtime `main` |
|---|---|
| `<config>/secrets/signing/<id>.seed`, 0600, directories 0700 | accepted; records signed |
| the same, mode 0400 | accepted |
| in `<config>/desks/`, beside the desk's folder | accepted |
| in a directory any user can write (0777) | **accepted**: the runtime does not check the key's directories. *Amended 2026-10-05:* runtime v0.27.0 refuses a key in or under a directory its group or others can write, unless that directory is sticky (runtime #222, closing #221; its CHANGELOG). Not measured here. Desk's custody refused it already. |
| inside the project (`.desk-private/in.seed`) | refused: "inside the project's directory" |
| through a linked directory, or the seed itself a link | refused: "goes through a symbolic link" |
| mode 0640 | refused: "can be read or written by its group or by other users" |
| a second hard link | refused: "more than one name" |
| a relative path | refused: "not absolute" |
| no file there | refused: "no file is there" |
| not 64 hexadecimal characters | refused |
| a valid key that is not the key in force | refused: "not the key in force in the signature sidecar" |

What a refused key does, measured:
- `packs validate` answers `invalid` and exits 1;
- `packs verify` and `packs lock` are unaffected: `valid`, exit 0;
- a deciding run is still recorded, unsigned, and exits 0. So a refused key
  never refuses a decision, and Desk's review step is unaffected.

The runtime refuses less than Desk's custody does. A key Desk keeps in
`secrets/signing/` therefore meets both rules.

Not measured:
- a bind mount, which the guide says the runtime cannot see;
- macOS, where a path through `/var` is a link and is refused;
- Windows, where the runtime refuses every key. Desk's custody keeps no key on
  a build that cannot establish who owns a directory (`custody_other.go`,
  `!unix`), so Desk offers no signing there, and says why.

**Creating it.** Desk runs `jpack audit key generate <path> --format json`.

- Measured: it writes the seed 0600 and prints `publicKey` and `keyId`.
- It never writes over anything: a second run at the same path exits 4.
- The runtime opens the path itself, so Desk hands it a path and not a
  descriptor. A descriptor path through `/proc/self/fd` would be a link, which
  the runtime refuses. The window this leaves is in a directory only this user
  can change.
- Desk keeps each public key in order, with the sequence it took over from, in
  `secrets/signing/<project>.keys.jsonl`. This is public material. It is the
  list of keys a verifier needs, and Desk shows it for the owner to hand over
  (section 2).

**Telling the runtime to use it.**

- **A desk Desk made: `audit.signingKey` in its `jpack.json`, at configVersion
  `"6"`.** Every runtime that reads that configuration then signs:
  - the agent's `jpack mcp`, started as ADR-0009 section 6 describes;
  - a script;
  - Desk's own relay and its audit commands, which name `--config jpack.json`.

  This matters because Desk is not the one that records. A key that only Desk's
  runtimes knew would sign almost nothing. The member is part of the
  configuration, so the lock covers it:
  - a new desk is written with it, and the initial lock pins it;
  - an existing desk gets it through the upgrade offer, with one confirmation
    and one lock (ADR-0009, section 4; question 2).

  Measured: with the member set, a deciding run over `jpack mcp` was recorded
  and signed, and a rehearsal wrote nothing.
- **The project Desk was started on: the owner's choice** (question 2). The
  member is reliable, but has costs for a project that is committed and checked
  in CI:
  - its absolute path discloses the home directory;
  - `packs validate` fails, exit 1, in any checkout without that key;
  - a runtime older than the floor refuses the project outright (section 6).

  The other way is `JPACK_SIGNING_KEY`, which works at any configVersion
  (measured under `"5"`). Desk sets it on the runtimes it starts for that
  project, and Desk's agent setup puts it in the `env` of the `jpack mcp`
  server entry. A caller started without it records unsigned. Nothing fails, so
  nothing tells it.
- **An inherited `JPACK_SIGNING_KEY` is removed** from every runtime Desk starts
  for a desk it made, as `JPACK_CONFIG` is (`runtimeEnv`, `runRuntime`).
  Measured:
  - the variable takes precedence over the configuration's key;
  - a key that is not the key in force signs nothing;
  - it also signs a project at configVersion `"5"` that names no key.

  On the project Desk was started on, an inherited value is kept, because it is
  the owner's. Desk shows it, and offers no key action there while it is set,
  because the runtime would act on that key and not Desk's.

**Rotating it.** Rotation is the owner's action, and never scheduled. Desk:

1. runs `audit key generate` for the next seed, beside the current one;
2. runs `audit key rotate --next <next> --config jpack.json`;
3. renames the next seed over the current one's name, in the same directory.

Measured:
- `jpack.json` never changes, so there is no `config-drift` and no new lock.
- Between steps 2 and 3, the old key is not in force: `packs validate` says so,
  and a record written then is unsigned.
- After step 3, records are signed with the next key. `audit verify` with both
  public keys in order reports every signature valid and one rotation. With the
  second key alone, it reports `signature-invalid` and `rotation-invalid`.

The rename removes the old seed's name. It does not erase its bytes from the
disk. Desk records the next public key after the previous one.

A lost key cannot be rotated away from. The trail and its sidecar must then be
moved aside together, and the next record starts a new trail (the guide,
"Rotation"). Desk shows the runtime's refusal and that way out. It does not
move a trail itself (question 9).

**Whom the key binds.** Following ADR-0009 section 6, and runtime ADR-0047
§2b:

- **Desk's assistant** has five tools behind the ToolGate and no file tool.
  Codex runs with no environment (ADR-0008). So it cannot read the key. It only
  rehearses, so it signs nothing either.
- **An agent given only the desk's `jpack mcp`**, with no file or shell tools,
  cannot read the key. Its deciding runs are signed by a key it cannot use. So
  a line it writes into the trail by any other path carries no valid signature.
  This is the case signing answers (ADR-0047, T1).
- **An agent with file or shell tools, running as the owner's user,** can read
  the seed. Mode 0600 keeps out other users, not this one. Against such an
  agent a signature binds nothing. Only these keep it apart:
  - another OS user;
  - a sandbox that denies reading Desk's configuration directory;
  - giving it MCP tools only.

  Desk cannot check which of these holds, and says so.
- **The operator** holds the key. A signature establishes nothing against the
  operator.

### 2. Checkpoint hand-over (C4)

**Holders.** The owner adds a holder: a label they type, such as
"Counterparty: procurement desk", and a channel. Desk keeps the holders and its
record of hand-overs in `.desk-private/handover/`. That folder is owner-only,
the file API refuses it, and it is in no backup, like the trail.

**What is handed over: exact bytes from a Go route.**
- `GET /api/audit/checkpoints?holder=<id>` runs `jpack audit checkpoint
  --config jpack.json --since <cursor> --limit 300` in the human form.
- That form's standard output is the checkpoint lines, each in its RFC 8785
  canonical form with a newline. Desk answers `application/jsonl` with those
  bytes as the runtime printed them.
- Measured: a line is about 170 bytes. So 300 lines stay under `runRuntime`'s
  64 KiB bound, where the default `--limit` of 1000 would not. Desk asks again
  after the last sequence until fewer than 300 come back, and joins the batches
  untouched, up to a bound of its own.
- The page saves the answer as it came, as `JobsView` saves Runner's export.
  The last line is the current checkpoint. It alone witnesses every record
  before it. The others let a holder pin each record, so that two checkpoints
  for one sequence prove a rewrite.

The JSON form is not used for the bytes. It carries the checkpoints inside
another document, and re-encoding them is what this design forbids.

**The deliverer's cursor.** Desk keeps one cursor per holder and per trail
identity: the last sequence handed over.
- A download is not a delivery. The cursor moves only when the owner confirms
  that the file went to the holder. Until then the same request gives the same
  bytes, because a checkpoint is a function of its record's bytes.
- If the checkpoints come back for another trail identity, Desk starts that
  holder at 0 for the new trail and says so. That happens when a trail was
  moved aside.

**What Desk shows.**
- For each holder:
  - the label and channel;
  - the trail identity, and the last sequence handed over;
  - when the owner confirmed it, by Desk's clock;
  - the SHA-256 of the bytes handed over;
  - how many records since are unwitnessed by that holder.
- Beside the list: "This is Desk's own record. You keep it, and you can change
  it, so it proves nothing to a holder or to anyone else. Only the holder's own
  copy counts."
- Desk also holds the trail to its own record, with `audit verify --expect`
  (section 4). That shows whether the trail still matches what was handed over,
  and says that the copy checked is the operator's.

**What is not a holder, and is not offered as one.**
- The local managed gateway. It is the operator's, and its key was generated
  in the operator's store (runtime ADR-0047, "The existing action receipts").
- The owner's own connected accounts, such as Drive, S3 or Notion through the
  gateway. The operator can change what is stored there.
- Any file in the project or in Desk's configuration directory.

**Channels** (question 4).
- **First: download or copy.** The owner sends the file by any channel the
  holder will keep: an e-mail attachment, a ticket, or a folder the holder
  controls.
- **Second, for a holder who runs one: an HTTPS endpoint.**
  - Desk POSTs the same bytes, `application/jsonl`, on a schedule.
  - Retries are idempotent by each line's SHA-256.
  - A 2xx answer moves the cursor, and Desk's record of that answer is still
    the operator's.
  - A credential for the endpoint, if any, is kept in Desk's custody, never in
    the project.
  - It is new outbound traffic from Desk, so it is built only for a concrete
    holder.

**Downloading the trail** (ADR-0009, question 3's answer, delivered with
this). `GET /api/audit/trail?file=evaluations|signatures|stamps`:
- opens the file through the project root, refusing links;
- takes the shared `flock` that the runtime's writer and verifier take on the
  trail, reads the size, and releases it;
- streams that many bytes untouched. Writers only append, so the prefix is
  whole.

The sidecar is written under the trail's lock, so it is read under it too. The
stamps file is read under the lock its own writer takes. Each file is saved
under the runtime's own name. A recipient checks a copied record with
`signatures.jsonl` beside it (the guide, "A copied record").

### 3. Stamping (C2)

**Configuring an authority.** These are per-desk settings, kept by Desk:
- the authority's address (http or https);
- the authority's root certificates (PEM), for verification;
- optional policy OIDs and revocation lists;
- an interval.

Desk passes the address as `--tsa`, and does not write `audit.timestampAuthority`
into `jpack.json` (question 5). Desk is the only scheduler, and `--tsa` works at
configVersion `"5"` (measured). So stamping needs no configVersion `"6"`, no
`config-drift` and no lock. A person stamping by hand passes `--tsa` too.

**No authority by default.** Choosing one is a trust decision. A stamp also
sends each checkpoint's digest to that party (runtime ADR-0047, "Privacy").

**How Desk schedules it.** Desk is the resident process the runtime lacks.
- At the owner's interval, for each desk with an authority, Desk runs `jpack
  audit stamp --config jpack.json --tsa <address> --timeout 15s --format json`.
- It runs only when the trail's head has moved past the last checkpoint
  stamped, and never more than one at a time per desk. The runtime is
  idempotent too: an `already-stamped` checkpoint asks nothing.
- It is off the decision path. While Desk is not running, nothing is stamped,
  and records stay pending.
- `--timeout 15s` keeps the runtime's own wait inside `runRuntime`'s 20-second
  bound. The runtime then ends itself, instead of being killed while it writes
  `stamps.jsonl`. Measured with a listener that never answers: `--timeout 3s`
  returned after 3.03 s.
- Measured failures, each of which writes nothing:
  - no authority: `JPS-AUDIT-STAMP-NO-AUTHORITY`, exit 3;
  - a refused or unanswered request: `JPS-AUDIT-STAMP-UNREACHABLE`, exit 4;
  - a trail that fails a check: `JPS-AUDIT-STAMP-REFUSED`.

  Desk shows the runtime's message, and tries again at the next interval.
- The request goes from the runtime's process, through Go's default HTTP
  client. So it uses the proxy settings in the environment Desk passes.

**Showing records still pending a stamp.** Desk runs `audit verify --tsa-roots
<roots>` (section 4) and reads `coverage.stamped.through`.
- Records after that sequence are pending.
- The runtime's lag between each covered record's `at` and its first stamp is
  shown as the runtime reports it.
- Without roots, the runtime answers "stamps not checked". Desk then shows that
  sentence and the sequence its last stamp run reported, labelled as the
  authority's answer to Desk's request, not as a trusted stamp.

### 4. Verification in Desk

**A decision-record panel** for each desk runs `jpack audit verify --config
jpack.json --format json`, with the inputs Desk holds:
- `--public-key` for each key in `<project>.keys.jsonl`, in order;
- `--expect` with Desk's hand-over record;
- `--tsa-roots`, `--tsa-policy` and `--tsa-crls` from the stamping settings.

It passes no `--require-…` flag. Those are a reader's demands, not the
operator's.

**What it shows:**
- the status;
- the segments and discontinuities;
- the coverage: legacy prefix, chained, unchained, uncovered and damaged lines;
  signed through; witnessed and unwitnessed; stamped through;
- each finding by name;
- the runtime's `establishes` and `doesNotEstablish` sentences, verbatim.

The runtime writes those sentences in English, and Desk shows them as it shows
the runtime's diagnostics.

**What it says:** "Desk ran this on your machine, over your trail, with keys and
checkpoints you keep. It shows what a holder would see. It is not evidence to
anyone who does not trust you. A holder runs the same command on a copy, with
what it holds."

**Repair.** When the report names `incomplete-last-line`, Desk offers `jpack
audit repair`, with a confirmation. The confirmation says:
- repair starts a new segment and keeps the damaged bytes;
- it never restores the lost line;
- until it is done, every deciding run is refused. Measured: exit 4, "the audit
  trail's last line is incomplete".

After a repair, `audit verify` reports `segmented`, exit 0. Desk never repairs
on its own (question 8).

**When it runs:** when the panel opens, and after a stamp, a hand-over or a
repair. Never on a timer.

Measured: a report with the runtime's cap of 100 findings was 11.5 KB, within
`runRuntime`'s bound. A larger one is refused by that bound rather than
truncated.

**A Jobs record panel.** *Amended 2026-10-05, issue #216.* Beside the
decision-record panel, for the desk's Runner (section 5), Desk takes a private
copy of the chain of runs Runner serves and runs
`jpack audit verify --trail <copy> --format json`, with the inputs Desk holds:
- `--public-key` for Runner's key (section 5), when Desk keeps one;
- `--expect` for each checkpoint of the chain that Desk handed over.

It runs on request only. It shows the status, the coverage, each finding by
name, and the runtime's `establishes` and `doesNotEstablish` sentences
verbatim, and offers the chain for download. A transfer that ends early is an
error, never a report over a shorter copy. What it says: "Desk ran this over
its own copy of the runner's chain of runs, with the keys and checkpoints it
keeps. It shows what a holder would see. It is not evidence to anyone who does
not trust this installation."

### 5. Runner

**Export version 4.** After Desk pins a Runner release that includes #34:
- the page asks for version 4. Runner answers 3, or 2, for a run with no entry
  in its chain, and the page labels each. *Amended 2026-10-05:* #220 (PR 8a),
  against Runner v0.5.0, asks for version 5, which adds the record's signature
  sidecar (Runner #37). Runner answers 4, 3 or 2 where the run lacks what a
  later version carries, and the page labels each.
- `GET /v1/run-chain` is passed through for download. Its content type is
  `application/jsonl`, not the `application/json` the proxy set at acceptance
  (`internal/desk/jobs.go:407` then). Its bytes are passed untouched.
  *Amended 2026-10-05:* #220 passes the route on with Runner's own
  `Content-Type` (`internal/desk/jobs.go:451`), up to 67,174,400 bytes
  (`runChainLimit`); every other route still gets `application/json`
  (`jobs.go:453`).
- **The Jobs chain is handed over through section 2.** Desk saves the served
  chain to a private temporary file and runs
  `jpack audit checkpoint --trail <file> --since <cursor> --limit 300`, and
  asks again as section 2 does. Measured: the runtime reads a chain in
  Runner's entry form and prints its checkpoints, outside any project. A
  holder may hold both the desk's trail and the Jobs chain, with a cursor for
  each. *Amended 2026-10-05:* `--limit 300` added, as section 2 chose. A
  checkpoint line is about 170 bytes, so at the default of 1000 lines the
  answer would pass `runRuntime`'s 64 KiB bound (`runtimeAnswerLimit`,
  `internal/desk/runtime.go:154`) once more than about 385 entries were not
  yet handed over.
- **The Jobs chain is verified in the Jobs record panel** (section 4).
  *Amended 2026-10-05, issue #216.* Desk saves the served chain to a private
  copy and runs `jpack audit verify --trail <copy> --format json`, with
  `--public-key` for Runner's key when Desk keeps one, and `--expect` for each
  checkpoint of the chain it handed over. The panel shows section 4's sentence
  on the operator's copy. Runner's CI holds the runtime's verifier to Runner's
  chain (`TestTheRuntimesVerifierReadsTheChainOfRuns`, with `--expect`). Not
  measured here: Runner's key signs each run's record, kept in its export as
  `run.auditSignatures`, and no sidecar sits beside the chain's copy, so what
  `--public-key` reports over that copy is for the panel's PR to measure.

**Signatures.** Runner #36 proposes an installation key that Runner passes to
each attempt's runtime. Today Desk passes Runner no key. It will pass none
until #36 settles how. When it does, the key is a separate one in Desk's
custody, never a project's.

*Amended 2026-10-05:* #36 is settled. Runner #37 closed it, in v0.5.0: the
boot line's `signingKey` names a seed by its absolute path, which Runner checks
at boot and passes as `JPACK_SIGNING_KEY` to operational evaluations only.
Desk's boot line still carries no key (`internal/desk/jobs.go:156`), so every
Jobs run is unsigned. The key, as decided above (issue #215):
- one seed for each desk's Runner, at `secrets/signing/runner/<desk id>.seed`
  in Desk's configuration directory, where `<desk id>` names the desk as
  section 1 names `<project>`. The folder `runner/` is a name no project's id
  can take;
- made with `audit key generate`, and held by the same custody checks as a
  project's key, through PR 3's helpers rather than a second implementation;
- passed to Runner as the boot line's `signingKey`; never a project's key, and
  never an inherited `JPACK_SIGNING_KEY`.

It is delivery row 11.

*Amended 2026-10-05:* #229 delivers row 11. For the project Desk was started
on, `<desk id>` is the name of its Runner's state directory, the hex SHA-256 of
the project's path. The key is made at a Runner's start where none is kept,
with a creation marker beside it, as a desk's key has, and is named on the boot
line only where no marker is left, the runtime reads it (`audit key public`)
and its list of public keys holds that one key. Otherwise Runner starts without
a key, its runs go on unsigned, and Desk says why; where Runner refuses the key
at boot, Desk starts it again at once without it. Desk never removes or makes
again a key it could not read or that was refused. Every decision on a Runner
key, and a start's sweep of the desks' keys, is taken under one exclusive lock
on the signing folder Desk holds (review round 1 of #229): a sweep never waits
for it, and any other start waits up to 10 seconds and then starts Runner
without a key, changing nothing.

### 6. The version floor

All of this needs a runtime release that includes #211 to #215 and #217. Measured
on 0.25.0:
- it reads configVersions `"1"` to `"5"`;
- it has no `audit` command: "unknown command", exit 3;
- it refuses a configVersion `"6"` project outright: `JPS-PROJECT-CONFIG-VERSION`,
  exit 2, for every command, `packs list` included.

**How Desk tells.** Desk already asks `packs schema --format json` for
`supportedConfigVersions`. It treats `"6"` as the sign, because the first
release that reads `"6"` has all of these commands. An `audit` command that
answers "unknown command" is treated as absent all the same.

**With an older runtime,** the decision-record panel shows one line: "This
runtime (jpack X) writes an unchained trail and has no audit commands. Chaining,
checkpoints, signing and stamping need jpack <floor> or later." Desk then:
- generates no key;
- offers no hand-over and no stamping;
- runs no verification;
- calls nothing "chained", "signed", "witnessed" or "stamped".

**Desk never writes configVersion `"6"` for a runtime that cannot read it**, as
ADR-0009 does for `"5"`. If the runtime is later replaced by an older one, a
desk at `"6"` stops answering every project tool. Desk shows the runtime's
refusal, which names the version.

**Mixed runtimes on one trail.** A record that an older runtime writes into a
chained trail is unchained and unsigned. Measured: it is `uncovered` until the
next chained record, which commits to it as an `unchained` block. The panel
shows these counts.

### 7. What each part does not establish

| Part | Establishes | Does not establish |
|---|---|---|
| The chain | the lines are consistent with one another | that the trail is complete; that its last line, or lines rewritten from some point with their links recomputed, are the ones first written |
| A held checkpoint | the records up to it are the ones that existed when it was handed over, against an operator who does not hold the holder's copy | anything after it; that the holder kept every checkpoint; when it was made or handed over |
| A signature | a holder of the key signed these exact bytes | anything against the operator, who holds the key; anything after the key is copied; anything against an agent that can read the key; that the trail is complete |
| A stamp | the checkpoint, and every line before it, existed by the authority's stated time, as far as that authority is independent of the operator | when any record was made: a stamp is an upper bound on existence; anything against an authority that colludes; revocation, where no supplied list speaks for it; anything after the last checkpoint stamped |
| A record's `at` | — | anything: it is the operator's clock, and the runtime reports the lag to a stamp for the reader to judge |
| The local gateway's receipts | that the operator's gateway saw those bytes | anything against the operator: it is the operator's, and no witness |
| Desk's record of hand-overs | — | anything to a holder: it is the operator's, and the operator can change it |
| Desk's verification | what the operator's own copies show | anything to someone who does not trust the operator |

### 8. Every new runtime command Desk would run

All run from Desk's Go through `runRuntime`. Each runs in the directory Desk
holds, with `--config jpack.json` named. `JPACK_CONFIG` is removed, and, for a
desk Desk made, `JPACK_SIGNING_KEY` too. Where Desk was started under a
`JPACK_CONFIG` that names another project, the commands are unavailable, as in
ADR-0009's review step.

None of them evaluates. So none consults the lock, and none is refused by
`requireReviewed` or `requireComparableFacts`. None changes `jpack.json`.

| Command | Writes | Network | Where Desk runs it | Section |
|---|---|---|---|---|
| `packs schema --format json` (already run) | nothing | no | before any audit action: is `"6"` read? | 6 |
| `packs validate --format json` | nothing | no | to show the `audit-signing-key` check, and why a key is refused | 1 |
| `audit key generate <seed> --format json` | a new seed, 0600, never over a file | no | key creation; the next key of a rotation | 1 |
| `audit key public <seed> --format json` | nothing | no | to show a key's public half again | 1 |
| `audit key rotate --next <seed> --format json` | a `key-rotation` line in `signatures.jsonl`, under the trail's lock | no | rotation, owner-initiated | 1 |
| `audit checkpoint --since N --limit 300` (human form) | nothing | no | hand-over: its standard output is the bytes handed over | 2 |
| `audit checkpoint --trail <file> --since N --limit 300` | nothing | no | the Jobs chain's checkpoints, from a private copy | 5 |
| `audit verify --trail <copy> --format json [--public-key …] [--expect …]` | nothing | no | the Jobs record panel, over a private copy of Runner's chain; exit 1 on any failed check, read whatever the exit | 4, 5 |
| `audit verify --format json [--public-key …] [--expect …] [--tsa-roots …]` | nothing | no | the panel; exit 1 on any failed check, read whatever the exit | 4 |
| `audit stamp --tsa <address> --timeout 15s --format json` | a line in `stamps.jsonl`, under its own lock | yes: the checkpoint's digest and a nonce, to the authority | the scheduler | 3 |
| `audit repair --format json` | a `discontinuity` record (signed where a key is in force); the damaged bytes kept as a line | no | after the owner confirms | 4 |

*Amended 2026-10-05:* `--limit 300` on the Jobs chain's checkpoints
(section 5), and the Jobs record panel's `audit verify --trail` (section 4;
issue #216).

Desk's Go also reads, never writes, the runtime's files: `evaluations.jsonl`,
`signatures.jsonl` and `stamps.jsonl`, for download (section 2).

## Consequences

- Good: a desk's trail becomes something its owner can hand to a counterparty
  or auditor, as a checkpoint they keep and a trail they can check.
- Good: records written by an agent limited to MCP tools are signed by a key it
  cannot use.
- Good: stamping runs where the runtime cannot, in Desk, off the decision path.
- Good: nothing here can refuse or delay a decision.
- Bad: a key is a secret Desk now keeps for each project. Its custody, rotation
  and loss are part of running a desk.
- Bad: against the most common agent setup, one with file tools running as the
  owner, a signature binds nothing. Desk can say so, but cannot change it.
- Bad: every guarantee rests on holders who actually keep what they are given.
  Desk cannot see whether they do.
- Bad: a desk at configVersion `"6"` is unreadable to runtime 0.25.0 and older.
- Bad: Desk's Go depends on the output of six more runtime commands, all under
  `outputVersion` `"2"`.
- Neutral: chaining arrives with the runtime pin alone, and needs nothing from
  Desk.

## The maintainer's answers

The maintainer answered on 2026-10-03 with "go", read as agreeing with every
recommendation below. Each answer is recorded after its question; the
maintainer may overrule any of them.

1. **Where does a project's signing key live by default?**
   **Recommendation:** `<Desk configuration directory>/secrets/signing/<project>.seed`,
   one key per project, under the custody Desk already applies to `secrets/`.
   That custody refuses a directory other users can write to, which the runtime
   does not check. The alternative of one installation key is worse: a single
   copy signs for every desk.
   **Answered: yes, as recommended.**

2. **How is the key named to the runtime?**
   **Recommendation:** for a desk Desk made, `audit.signingKey` in its
   `jpack.json` at configVersion `"6"`, so that every caller that reads the
   desk's configuration signs. For the project Desk was started on, offer the
   same through the upgrade step, listing its costs:
   - the home path in a committed file;
   - `packs validate` failing in CI;
   - runtimes before the floor refusing the project.

   Default it to `JPACK_SIGNING_KEY` on the runtimes Desk starts and in Desk's
   agent setup. In both cases, remove an inherited `JPACK_SIGNING_KEY` from the
   runtimes of desks Desk made.
   **Answered: yes, as recommended.**

3. **Is signing on by default for new desks?** It needs the new runtime.
   **Recommendation:** yes, where the runtime reads `"6"` and Desk's custody can
   keep a key. Elsewhere the desk is created unsigned, and the creation says so,
   as ADR-0009 does for `requireComparableFacts`. On ADR-0009's principle,
   defaults are on, and the cost to the owner's loop is nil. The panel says what
   a key does not bind.
   **Answered: yes.**

4. **Which hand-over channels ship first?**
   **Recommendation:** download or copy of the exact bytes, with an
   owner-labelled holder and a cursor that moves on the owner's confirmation.
   Build an HTTPS endpoint second, only for a holder who runs one. Never offer
   the local gateway, or the owner's own connected accounts, as holders.
   **Answered: download or copy first, as recommended.**

5. **Does Desk schedule stamping, and against which authority by default?**
   **Recommendation:** Desk schedules it once the owner configures an authority.
   There is no default authority. The setting lives in Desk, passed as `--tsa`,
   not in `jpack.json`, so that it needs no `"6"` and no new lock. The interval
   is the owner's, and stamps run only when records were added.
   **Answered: yes, with no default authority.**

6. **Does Desk run `audit verify` and show its report?**
   **Recommendation:** yes. It is a read-only panel, with the runtime's
   sentences verbatim, run with the keys, hand-over record and roots Desk holds,
   each labelled as the operator's own. It runs on opening and after an action,
   never on a timer.
   **Answered: yes, a read-only panel.**

7. **Does Desk adopt Runner's export version 4, and later its signatures?**
   **Recommendation:** adopt version 4 once Desk pins a Runner release with #34:
   - ask for 4, and label what Runner answers;
   - pass `run-chain` through as exact bytes;
   - hand over the Jobs chain's checkpoints through the same mechanism.

   Pass Runner no key until Runner #36 settles how. Then use a separate key, not
   a project's.
   **Answered: yes, as recommended.**

8. **Does Desk offer `audit repair`?**
   **Recommendation:** yes, when verification reports a torn last line, after a
   confirmation that says what repair does and does not do. Never automatically.
   **Answered: yes, behind a confirmation.**

9. **When a key is lost, does Desk move the trail aside to start a new one?**
   **Recommendation:** not in this line. Show the runtime's refusal and the
   guide's way out. Moving a trail aside discards the identity that holders'
   checkpoints name, and needs its own confirmation design.
   **Answered: not in this line.**

## Delivery, after acceptance

One PR each, in this order, each under Desk's review rules. Everything after
the first needs Desk to pin a runtime release that includes #211 to #215 and
#217, through the usual component-update PR.

*Amended 2026-10-05:* #200 pinned Runtime v0.26.0, which includes them. PR 8
is split into 8a and 8b. Rows 11 and 12 are added where they fall in this
order, and are numbered after 10 so that the PR numbers already cited keep
their meaning. The status column is as of 2026-10-05.

| PR | What | Needs | Status, 2026-10-05 |
|---|---|---|---|
| 1 | Remove an inherited `JPACK_SIGNING_KEY` from the runtimes of desks Desk made, and show one inherited on the startup desk | nothing; can ship now | shipped: #202, in v0.5.1 |
| 2 | The decision-record panel: capability check, `audit verify` with no held inputs, the older-runtime line; download of the trail, sidecar and stamps as exact bytes | the runtime pin | shipped: #204 (2a) and #205 (2b), in v0.5.1 |
| 3 | Key custody: generate per project; new desks at `"6"` with `audit.signingKey`; public keys shown; `packs validate`'s check shown; rotation | 2; questions 1–3 | 3a: #219, custody for the desks Desk makes, in no release yet; 3b, rotation, not built |
| 4 | The upgrade offer learns `"6"` and the signing key, as its own item | ADR-0009 PR D merged; 3 | |
| 5 | Hand-over by download or copy: holders, the checkpoints route, cursors, Desk's record; verification against that record | 2; question 4 | built: PR #241 |
| 6 | The repair offer | 2; question 8 | |
| 7 | Stamping: settings, scheduler, pending records; verification with roots | 2; question 5 | |
| 8a | Export version 5 and labelling, `run-chain` pass-through | nothing: #200 pinned Runner v0.5.0; question 7 | merged: #220, on `main`, in no release yet |
| 8b | The Jobs chain in hand-over: `audit checkpoint --trail <file> --since <cursor> --limit 300`, with a cursor of its own | 5 | |
| 11 | Runner signing key in Desk's custody, `secrets/signing/runner/<desk id>.seed` under the same custody checks, passed as the boot-line `signingKey` | 3; policy settled by question 7; Runner #36 closed by #37 in v0.5.0 | #229, in no release yet |
| 12 | The Jobs record panel beside the Decision record: `audit verify --trail <copy>` with the Runner key and the held checkpoints | 5 and 11 | |
| 9 | The README and in-app help: what each part establishes and does not, and the agent setup with the key | each of the above | |
| 10 | Hand-over to an HTTPS endpoint | a holder who runs one; question 4 | |

Not in this line:
- the gateway's `requireSignedRecord`, since Desk sends no writes through
  `/act`;
- a gateway witness run by another party (runtime ADR-0047, section 3);
- moving a trail aside (question 9).

## More information

- **Runtime `main` at `581330e`:**
  - ADR-0047 (defensible records);
  - `docs/building-with-packs.md`, sections "The chain", "Checking a trail,
    and handing over a checkpoint", "Handing every new checkpoint to a holder",
    "Stamping checkpoints with a time-stamping authority", "Signing the trail",
    "Record signatures, exactly" and "Repairing a torn trail";
  - issues #206 to #209 and #216, and PRs #211 to #215 and #217.
- **Runner `main`:** `docs/MAPPING-V2.md`, "The installation's chain of runs";
  #34 and #30; #36 (signatures, open at acceptance). *Amended 2026-10-05:*
  #34 and #37 are in Runner v0.5.0, and #37 closed #36; see also the same
  document's "Record signatures".
- **Gateway `main`:** ADR-0012 and #201 (`requireSignedRecord`).
- **Desk:**
  - [ADR-0003](0003-private-chat-data-and-recovery.md) (backups);
  - [ADR-0005](0005-managed-local-gateway.md) (the managed local gateway);
  - [ADR-0008](0008-codex-subscription-agent.md) (Codex);
  - [ADR-0009](0009-gates-on-by-default.md) (the gates, `.desk-private/audit`,
    the review step and the upgrade offer);
  - issues #182 and #186.
