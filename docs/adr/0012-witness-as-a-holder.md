---
status: accepted, 2026-10-12
date: 2026-10-12
deciders: maintainer
---

# A witness as a holder of checkpoints

Issue #233, row 5 of the gateway's ADR-0013 delivery
(Judgment-Pack/judgment-pack-gateway#199). ADR-0010 §2 left its HTTPS
channel to a holder unbuilt until a holder runs an endpoint (delivery row
10), and left "a gateway witness run by another party" out of its line. The
gateway's ADR-0013 designs that witness, and Gateway v0.11.0 serves it. This
record decides how Desk configures a witness, what it sends and when, what
moves its cursor, what it keeps, and how the decision record reads the
witness's chain. It is proposed: the maintainer accepts or amends it by
answering the questions at the end. PR 6 (#234) builds it.

It was checked against Desk `main` at `ad9296f`, which pins Runtime
`v0.30.0`, Runner `v0.8.0` and Gateway `v0.11.0` (#372); the gateway's
`SPEC.md` at `v0.11.0`; its ADR-0013 as amended 2026-10-12 (#239), on gateway
`main` at `845cf18`; the runtime's guide at `v0.30.0`; and the
specification's RFC 0017 (Draft) at `cd956d9`. Nothing was measured: no
witness and no runtime command was run for this record.

## Context

**What Desk has** (ADR-0010 §2 and §4): holders, with a cursor per holder
and per trail identity, in `.desk-private/handover/`; an HTTPS channel, not
built, whose cursor "a 2xx answer" would move; and a decision-record panel
that runs `audit verify` and "passes no `--require-…` flag".

**What the gateway serves** (v0.11.0, `SPEC.md` §6 "Witness endpoints", and
§8). `POST /witness/checkpoints` takes checkpoint lines "as `jpack audit
checkpoint --since` prints them", at most 1 MiB, under a bearer token that
names `witness.audience`, not the engine's, and "opens the witness and
nothing else" ("Submission tokens"). "Only the last line is signed": the
earlier ones are compared for conflicts "and otherwise discarded". Two
reads, a trail's head and its statements, are open to anyone who names it.
The witness's operator registers a trail, by default, before anything is
signed for it ("Registration"); each submitter is bounded to 60 submissions
a minute and 100 trails by default ("Rate"). A refusal is `{"error": <a
sentence>, "reason": <a word>}`.

**What the runtime reads** (runtime #228, since v0.28.0): `audit verify
--witness-key` with `--witness`, `--witness-head`, `--witness-resume`,
`--witness-save` and `--require-countersigned-through`; "the runtime fetches
nothing". A chain is read from index 0 or the reader's own continuation,
never late; current with a head, historical without.

**ADR-0013's answers that bear on Desk** (2026-10-05): question 5, "for a
witness, only a verified `checkpoint` statement for the last line sent moves
the cursor"; question 7, "Only the last line of a submission, its cost
stated to the owner." RFC 0017 proposes the statement as specification text
and changes no byte the runtime reads; nothing here waits on it.

## Decision drivers

- ADR-0010's, unchanged, the runtime's words and exact bytes among them.
- The witness's key is the owner's, pinned, never taken from the witness it
  checks (`SPEC.md` §5). The witness speaks in its own words; Desk's say
  only that it could not reach it. No statement is removed (the archive
  rule, ADR-0010, amended 2026-10-08).

## Decision

### 1. Configuring a witness

A witness is a holder (ADR-0010 §2) whose channel is `witness`, added per
desk (question 1), with:
- **its URL**: `https`, or `http` to a loopback address only, for a witness
  run on the owner's machine. Desk appends `SPEC.md` §6's paths to it.
- **its public key**: 64 hexadecimal characters, from the witness's operator
  out of band, never from its `/publickey`. The runtime holds it to its
  public-key rule (`CheckPublicKey`) before Desk keeps it: Desk runs `audit
  verify --witness-key <key>` once and reads only a refusal,
  `JPS-AUDIT-WITNESS-KEY-INVALID` with its reason, shown verbatim. Desk
  refuses its own managed local gateway's key, the operator's (ADR-0010 §2).
  A pinned key is never changed in place: a witness under a new key is a new
  witness (ADR-0013 §4, rule 3), added as a new holder.
- **the submitter's token**, issued by the witness's operator for
  `witness.audience`, not the engine's (question 2). The owner pastes it
  once; Desk keeps it in its custody at
  `secrets/witness/<name>/<holder id>.token`, 0600, `<name>` naming the desk
  as its signing key is named. Desk never shows it again, writes it to no
  log, answer, report or project file, and passes it to no process. It sends
  it only as `Authorization: Bearer` on a submission to the configured
  origin, never on a read, and follows no redirect. The witness judges the
  token, and its `401` is shown.

Before anything is kept, behind a confirmation in the stamping settings'
pattern, the page states: choosing a witness is a trust decision; each
submission sends it the trail identity, sequences and record digests, never
a record's contents; its reads are open, so whoever holds one of the trail's
checkpoints can follow the trail there (ADR-0013, question 3); only the last
line of a submission is countersigned (section 2); whoever holds the token
can poison the trail there for good (section 5); and the witness's operator
registers the trail first, for which Desk shows the trail identity.

Desk calls the submission and the two reads, never the operator's
registration or retirement. Removing a witness removes its token and
settings, stops its submissions and keeps its statements (section 3).

### 2. What Desk sends, and when

**The bytes.** A submission is the lines `audit checkpoint --config
jpack.json --since <cursor> --limit 300` prints, joined untouched through the
trail's last chained record, as ADR-0010 §2 joins them: `application/jsonl`,
each line canonical and newline-ended. Its last line is the head's
checkpoint, the one the witness signs. Over the witness's 1 MiB, Desk sends
the whole lines that end with the head's, and leaves the earlier ones unsent:
the statement for the head covers them through the trail's own chain.

**What only the last line costs** (ADR-0013, question 7). With Desk's cursor
where the witness's chain is, no earlier line of a submission is at a
sequence the witness holds, so none is compared: each submission is checked
by its last line alone. The chain grows one statement a submission, not one
a record, and lines sent before the last leave no evidence that they were
sent. The page and the panel say so (question 5).

**When** (question 3). Desk does not see decisions, which other callers
write (ADR-0010, Context). It submits on the owner's interval, 5 to 1440
minutes, 60 unless the owner says otherwise, in a loop like the stamping
scheduler's: one attempt per interval, only where the trail's head has moved
past the cursor, one at a time per desk; **Send now** takes the same turn.
Each request has 15 seconds. Nothing on the decision path waits, and while
Desk is not running records stay unwitnessed.

**The witness's answers.** Each refusal is shown with its `reason` and its
sentence as the witness gave them. Only the first row can move the cursor:

| Answer | What Desk does |
|---|---|
| `200`, one statement | keeps it; section 3 decides whether the cursor moves |
| `409` `conflict`, two statements | keeps both; the panel's next reading holds the trail to them |
| `409` `below-head`, the head | keeps it; sends again only once the trail's head is past that head's latest checkpoint |
| `409` `retired`, the retirement | keeps it; stops submitting for the trail, and says that the trail's operator goes on under a new trail, registered anew, which Desk does not make (ADR-0010, question 9) |
| `401` `unauthenticated`; `403` `not-registered`, `not-a-submitter` | tries again at the next interval; the owner gives a new token, or asks for a registration |
| `429` `rate`, `trails`; `503` `stopped`, `busy`; any other refusal | tries again at the next interval, never before `Retry-After` |
| no answer, or none of the witness's form | "Desk could not reach the witness", with the HTTP status where there was one; tries again at the next interval |

### 3. What moves the cursor, and what Desk keeps

**The cursor** (ADR-0013, question 5) is the last sequence the witness
countersigned for this desk, per witness and trail identity, in the holder's
`record.json` (ADR-0010 §2), with the statement's index. It moves only on a
`200` whose body is one `checkpoint` statement whose checkpoint is the last
line sent, by trail, sequence and record digest, compared by Desk's Go, and
that verifies under the pinned key by the runtime (question 4): section 4's
reading, with that statement as the head, reports the `witness` section
`read`, with no `witness-*` finding, its latest checkpoint statement at that
index and sequence. Statements between Desk's last and this one are fetched
first. Nothing else moves it. The same checkpoint sent again gets the same
statement, so a lost answer is recovered by the next submission. Checkpoints
of another trail identity mean the trail was moved aside: Desk starts the
cursor at 0 for the new trail, which the witness's operator registers anew.

**What Desk keeps**, in the holder's folder, owner-only, refused by the file
API and in no backup (ADR-0010 §2): `witness.json`, the URL and the pinned
key; `statements-<trail>.jsonl`, every statement the witness answered or
served, exactly as received, each once, appended and never rewritten;
`head-<trail>.jsonl`, the head last fetched, replaced only once its
statement is in the statements file; `continuation-<trail>.json`, saved by
the runtime after Desk's own last reading with no finding; and the witness's
last answer to a submission. Signed by the witness, the statements are
evidence in anyone's hands, Desk's included (ADR-0013 §1): one the served
chain lacks or contradicts shows that the witness forgot, withholds or
signed two chains.

**Never removed** (the archive rule). Desk removes no statement on its own.
A file it cannot use, unreadable, out of its form, or of a chain the runtime
finds equivocating, is moved aside in the holder's folder, named on the
decision record with Desk's sentence on why, and kept until the owner
removes it. A witness removed, or under a new key, leaves its statements as
they are, read as historical.

### 4. How the decision record reads the chain

When the panel opens, and after a submission, a stamp, a hand-over or a
repair, never on a timer (ADR-0010 §4), Desk fetches the trail's head with
no token, then the statements after its continuation's index, or from 0, up
to the head's, 1000 at a time, appending each new one to the statements
file, at most the runtime's bound of 110,000 statements or 64 MiB a reading;
the rest is read in steps. With the panel's other inputs, it runs `audit
verify --config jpack.json --format json --witness-key <pinned key>
--witness <statements after the continuation> --witness-head <fetched head>
[--witness-resume <continuation>] --witness-save <continuation>`, the
statements through a private copy of the lines after the continuation's
index, since the runtime refuses the others (`statement-before-continuation`).
Where the witness could not be reached or refused the read, Desk runs the
same without `--witness-head`: a historical reading of what it keeps.
`--require-countersigned-through` is a reader's demand (ADR-0010 §4), passed
only for one check the owner asks for, never kept (question 6).

**What the panel shows** is the runtime's report and nothing more:
`coverage.countersigned` beside `witnessed`; the report's `witness` section,
where the reading began (`index-0` or `continued`) and how it ended
(`current` at the head's index, or `historical`), the latest checkpoint
statement's index, sequence and `witnessedAt`, the conflicts' sequences,
`retired`, and whether a continuation was saved; each finding by name; and
the runtime's `establishes` and `doesNotEstablish` sentences, verbatim.

**When the witness did not answer as asked**, beside that report: with no
answer of its form, "Desk could not reach the witness", and the runtime's
historical sentence says the rest; otherwise the witness's `reason` and
sentence, verbatim, for a `404` `unknown-trail`, a conflict or a retirement
alike. ADR-0010 §4's sentence on Desk's own verification stays.

### 5. What a witness establishes, and what it does not

In ADR-0013's words (§3, §4, §5 and question 10; `SPEC.md` §6 and §8.8), as
rows of ADR-0010 §7's table:

| Part | Establishes | Does not establish |
|---|---|---|
| A `checkpoint` statement, verified under the pinned key | the key's holder was given this checkpoint and signed it as statement `index` of its chain for the trail, and states that it did so at `witnessedAt` | that the checkpoint names a real record; that it extends the one before; that the submitter was the trail's operator; that `witnessedAt` is true; anything against a witness that colludes, or whose key is stolen |
| The same, held against the trail | lines 1 to its sequence are the lines that existed when the witness signed, if the witness is independent of the operator | anything after that sequence; that this is the project's only trail; that the records are true, or that every decision was recorded |
| The chain read to a head Desk fetched | Desk holds every statement that head commits to, as the witness signed them: none missing, out of order or altered | that the head is still the witness's after the fetch; that the witness signed no second chain for another audience; that it will serve these tomorrow |
| A `conflict` statement | a submitter the witness allowed offered another record for a sequence the witness held | which of the two is the trail's; who that submitter was |
| A `retirement` statement | the witness accepts no more statements for the trail, and the chain ends there | why; whether the trail's operator went on under another trail, as it does after a retirement, registered anew |
| The submitter's token | to the witness, an issuer and a subject; it opens the witness and nothing else | that its holder is the trail's operator: a credential is enough to poison a trail for good, by a checkpoint signed at a sequence no honest one will exceed, which revoking the token does not unsign; recovery is retirement, not erasure |
| Desk's cursor and its record of submissions | — | anything: they are the operator's |

### 6. Every command and request Desk would add

From Desk's Go; the commands through `runRuntime`, as ADR-0010 §8 says.

| Command or request | Writes | Network | Where |
|---|---|---|---|
| `audit verify --format json --witness-key <key>` | nothing | no | pinning a key; only its refusal is read |
| `POST <url>/witness/checkpoints`, bearer token, the bytes of `audit checkpoint --since` | the answer, kept by Desk | yes: the trail identity, sequences and record digests | the interval, Send now |
| `GET <url>/witness/trails/{trail}/head` and `/statements` | the statements and head, kept by Desk | yes: the trail identity | the panel; an acknowledgement |
| `audit verify --format json … --witness-key --witness --witness-head [--witness-resume] --witness-save [--require-countersigned-through]` | the continuation | no | the panel; an acknowledgement |

## Consequences

- Good: a reader can ask someone other than the owner how far a desk's trail
  was witnessed, and hold an older or rewritten copy to the answer; and what
  Desk keeps is signed, so a witness that forgets can be shown to.
- Bad: Desk keeps a credential that can poison a trail at the witness for
  good, and sends new traffic, on the owner's word; whoever holds one of the
  trail's checkpoints can follow it there; the statements file grows about
  610 bytes a submission, never pruned.
- Neutral: one witness per desk; two need what ADR-0013 lists to revisit.

## Questions for the maintainer

*Answered 2026-10-12 by the maintainer: all ten as recommended. The recommendations below are the decisions.*

1. **Per desk or per installation?** **Recommendation:** per desk, as
   holders and stamping settings are, one witness each in this line.
2. **Where does the token live, and how is it given?** **Recommendation:**
   pasted once, kept in Desk's custody (section 1); one per desk where the
   witness's operator issues one: a thief of one token can poison every trail
   registered to its subject.
3. **When does Desk submit?** **Recommendation:** on the owner's interval,
   60 minutes by default, where the head has moved, and on Send now.
4. **What moves the cursor, and who checks the statement?** (ADR-0013,
   question 5) **Recommendation:** a `200` `checkpoint` statement for the
   last line sent, compared by Desk's Go and verified under the pinned key
   by the runtime, so Desk keeps no second copy of the key rule. No `409`.
5. **What does Desk send, given that only the last line is signed?**
   (ADR-0013, question 7) **Recommendation:** the lines since the cursor,
   ending with the head's, the cost stated on the page and the panel.
   Declined: one line a submission, which grows the chain a statement a record.
6. **Does the panel pass `--require-countersigned-through`?**
   **Recommendation:** only for one check the owner asks for, never by default.
7. **What does Desk keep, and does it read in steps?** **Recommendation:**
   every statement, the last head and its continuation, never removed;
   each reading resumes from Desk's own continuation.
8. **What does the panel say when the witness does not answer as asked?**
   **Recommendation:** the witness's words verbatim, the runtime's report,
   and in Desk's only "Desk could not reach the witness".
9. **Is Runner's chain of runs handed to the witness too?**
   **Recommendation:** not in PR 6: it has its own identity and
   registration, and is read through a private copy (ADR-0010 §5).
10. **Does PR 6 also build ADR-0010's plain HTTPS holder, acknowledged by a
    2xx?** **Recommendation:** no; it waits for a holder who runs one.

## Delivery, after acceptance

| PR | What | Needs | Status, 2026-10-12 |
|---|---|---|---|
| 5 | This record (#233) | gateway #211, #212 and #213 released (v0.11.0) and runtime #228 (v0.28.0); Desk's pins on them (#372); ADR-0013's answers to questions 5 and 7 | accepted 2026-10-12, merged: #379 |
| 6 | #234, as ADR-0010's delivery row 10: the witness holder and its confirmation; the token in custody; submissions on the interval and Send now; the cursor on a verified statement; the statements, head and continuation kept; the panel's fetch and `audit verify --witness`; the README and in-app help; tests against a witness run from the gateway's reference on loopback, and with it stopped | 5 accepted, with questions 1 to 10 answered; a custody and network-seam review, one cross-vendor round | in review: the PR for #234 |

Not in this line: Runner's chain of runs at a witness (question 9); a
second witness per desk; moving a trail aside (ADR-0010, question 9).

## More information

- **Gateway `v0.11.0`:** `SPEC.md` §5, §6 ("Witness endpoints", with
  "Submission tokens", "Registration", "Administration" and "Rate") and §8;
  ADR-0013 as amended 2026-10-12 (#239), §1, §3 to §6, questions 3, 5, 7
  and 10, and its delivery table; #240 (unreleased) changes nothing here.
- **Runtime `v0.30.0`:** `docs/building-with-packs.md`, "Handing every new
  checkpoint to a holder" and "Reading a witness's statements"; CHANGELOG
  0.28.0 (runtime #228). **Specification:** RFC 0017 (Draft, spec #132).
- **Desk:** [ADR-0010](0010-defensible-decision-records.md), §2 to §4, §7,
  §8, questions 4 and 9, delivery rows 5, 8b, 10 and 12;
  `internal/desk/handover.go`, `stamping.go`, `custody.go`; #233 and #234.
