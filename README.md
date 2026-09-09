# judgment-pack-desk

A local web desk for a Judgment Pack project. `jpack-desk` is one Go binary: it
serves a single-page application and relays JSON-RPC between that page and a
`jpack mcp` subprocess running in your project directory.

The browser is the MCP client. The Go program is a chassis rather than a feature
server: **what it carries, it does not read.** JSON-RPC bytes cross the relay
untouched, so the desk can show anything the runtime's tools expose without a
matching change on the Go side.

It does have the few routes the browser cannot do for itself — the file API,
because the runtime has no write tools; the assistant's key, because a
credential must never be pasted into a project; and the **model relay**, a
per-feature *route* that carries the assistant's model traffic because the key
must never reach the page. None of them parses what it carries either: the relay
adds one header to a request and reads no request body and no model name. It
reads **one** kind of answer, and only to refuse it — a model listing, which is
the one relayed answer the desk *renders*, so that an endpoint cannot hand the
machine-held credential to the browser as a model id. See
[Where the assistant key lives](#where-the-assistant-key-lives).

## What it shows

**A project home.** `/` is what the project declares — its packs, and the two
rehearsals that can be run over them. The matrix and graph entries appear only
where the project has one: `list_packs` reports a matrix flag per pack, and the
configured graphs come from `experimental_list_graphs` where the runtime serves
it, or from running their matrices where it does not.

**A shell around all of it.** A header, a left rail, an Inspector, a Console and
a status strip — described under [Shell](#shell). The rail's first entry creates
a pack, and asks three things: a **name**, a **description**, and a
**template**. The name gives the id, the id gives the file name, and where that
file goes is `storage.packs` in `jpack-desk.json` — configuration, not a
question for whoever is creating a pack. The templates are the runtime's own
examples plus an empty pack derived from the runtime's own schema; the desk
ships none of its own, because a desk-authored skeleton would be the desk
asserting what a pack is.

The id is derived live and shown under the field: diacritics folded, every run
outside `a–z0–9` becoming one `-`. A name that cannot become the runtime's
`decisionId` is refused rather than repaired, and the refusal states the
alphabet — a name written in another script is made of letters, and telling its
author otherwise would be false. A Latin letter that neither decomposes nor has
a standard transliteration is **named in the refusal rather than deleted**:
`Łódź` is `lodz` and `Straße` is `strasse`, but `Azərbaycan` is refused saying
which letter it could not carry, because silently dropping one produces an id
nobody would recognise as their own name.

The empty pack is offered **only once `get_schema` has actually answered with a
skeleton that carries a `specVersion`** — not on the strength of the tool being
advertised, which is a claim about a tool rather than about a template, and a
file with no `specVersion` is not an incomplete pack but one nothing can read
as a pack at all. While either listing is still being asked the field says so
and selects nothing; a listing the runtime *refused* says that instead, in the
runtime's own words. Nothing is said about what checks will make of the result:
that is the runtime's verdict to report, on the page this opens.

**Or describe it.** Beside the template choice is a disclosure — *Describe it
instead* — where you type what the pack should decide in your own words and
press **Propose**. The desk runs the runtime's own `author_pack` prompt through
the assistant, and the dialog shows what it did — every tool call, every
guardrail, every failure — and then the proposal: the document summarised, the
unknowns it declared, the `validate` report and the rehearsal evaluation quoted
as the runtime wrote them, and the whole document behind *Show document*.
**Create** then writes that document exactly as it writes a template.

What it never does. **No file exists until Create is pressed** — the section
proposes and nothing else, which is ADR-0001's rule that the proposal is the
only sink. **The name field wins**: the id and the title come from what was
typed, whatever the proposal called itself, and the dialog says so in one line
where the two differ. **The document that is written is the desk's after
shaping** — applied to the frozen snapshot that was on screen, so what was
shown is what was written.

**A proposal belongs to the submission that produced it.** Pressing Propose
again withdraws the previous one at the press, and so does an `error` after a
proposal: an engine that proposes a document and then fails has said the work
does not stand. So a second Propose whose prompt is refused, or which is
stopped while the prompt is still being read, leaves nothing on offer — and
Create is *held* rather than quietly falling back to a template nobody chose.
Whatever the section is holding it for is in the button's own `title`: a run in
flight, a refused prompt, a run that ended without a document, a proposal that
could not be read as JSON data.

**The runtime says whether a proposal is a pack, before either write.** The
desk fills the four members the dialog asked about — the name, the id, the
version and the description — and leaves everything else exactly as the model
wrote it, `specVersion` included. Nothing is stripped and nothing is repaired
in silence: what the assistant proposed is what the runtime is asked about. The
exact bytes that would be written go to `validate`, Create is offered only
where the answer is `valid` for those bytes, and a refusal shows the check
strip's own sentence and **every** diagnostic the runtime returned, in the same
rendering the Checks panel uses — `code`, `codeStability`, `layer`, `severity`,
the message and the pointer, each as the runtime gave it and none of them
reworded. Where the answer sets `diagnosticsTruncated`, the runtime stopped at
its own limit and the desk says so in a line of its own naming that limit; the
sentence is the desk's, and the number in it is the desk's copy of the
runtime's cap rather than a figure the answer carries. A proposal is refused
outright where the connection serves no `validate` to ask.

**Losing the assistant ends the session**, it does not merely hide it: the key
leaving this machine or the endpoint leaving the file stops the run through the
run hook — one terminal event, one connection close — discards the proposal and
holds Create with a sentence saying so. **A route change is a dismissal**: this
dialog is mounted by the rail, above the route, so a Back or a Forward would
otherwise leave it standing over another page with its run alive; it closes on
one, exactly as Escape does. Closing ends the session and discards the proposal;
nothing about it is persisted. Without an endpoint and a key the section is one
line saying where those are configured, and the runtime's prompts still run in
any chat client you already use.

Two writes, in this order, and nothing is sent until everything that could
refuse has been asked. The pack is written with `PUT /api/file`,
`baseSha256: ""` and `createParents: true` — so a file already under that name
is refused rather than overwritten, and **the missing configured parent
directory** is created: `storage.packs.dir`, whatever it is set to and however
deeply it nests, not the literal `packs/`. Then
`jpack.json` is amended with one entry, written against the digest the read
that answered the id question returned, so a change made while the dialog was
open is refused rather than overwritten. Where the second write fails, the pack
file is on disk and nothing names it: the dialog says exactly that and stays on
screen to say it, because the file API has no delete verb and claiming an
unwind would be worse than the residue.

**A pack browser:**

- `/packs` lists the project's packs in a pane beside the document, and
  `/packs/:id` reads one — the whole document, in the order the file writes it,
  with every member the file leaves out stated as left out, and links from it to
  the what-if view and the test matrix. Described under
  [Pack view](#pack-view).

Conditions are rendered as an indented tree and never paraphrased into English.
A paraphrase of a policy condition would be a claim about what the policy
means, and only the document gets to make that claim.

**An evaluation and trace view:**

- `/packs/:id/evaluate` runs the pack over documents you supply, through the
  runtime's `experimental_evaluate` tool, and renders the payload it returns.

That view keeps three things apart, because the payload does:

- The **disposition** is the portable JPS Core §8.3 answer and the authoritative
  part of the payload. It gets the first panel and a frame of its own: kind,
  outcome id, the retained reason set, and the handoff state with what triggered
  it.
- The **handoff target** is shown *beside* the disposition and never inside it,
  because §8.3 keeps it outside one. It is what the pack configures. No delivery
  is observed, and the desk claims none.
- The **trace** is informative. It is rendered as the staged walk it is —
  applicability, then exceptions, then rules, in the payload's own order — with
  each entry's id, its condition verdict colour-coded across `true`, `false` and
  `unknown`, the effect or outcome where the entry carries one, and badges for
  `skipped`, `suppressed` and `onUnknown`. It decides nothing.

The envelope panel reports the facts about the run rather than about the answer:
the `experimental` flag, the specVersion the pack declares beside the
evaluatorSpecVersion of the contract applied to it, the packId and packVersion
read off the document that was evaluated, the bundled artifact digest, and
`conformanceClaimReference` — displayed as what it is, a locator for the file
that states the runtime's claim, and not a claim the payload itself makes.

Nothing is invented: a member the payload omits is absent from the view rather
than filled in, and a verdict is shown as the payload spells it. A refused
evaluation carries no disposition at all, so a refusal is reported as its §8.4
class and phase with the runtime's diagnostics, and never as a substitute
answer.

**A what-if loop.** `experimental_evaluate` takes the facts and evidence
documents as JSON text rather than as paths, so the loop needs nothing from the
chassis: edit the documents in the page, press **Re-evaluate**, and a *What
changed* table puts the previous disposition beside the current one — kind,
outcome id, reasons, handoff state, what triggered it, and the handoff target.
Unchanged members are listed too, so the diff never hides what held. The trace
is not diffed: a trace that moved while the disposition held is not a change in
the answer.

The two editors keep the tri-state the tool asks for. Leaving the evidence box
unchecked omits the key entirely, which is what "no evidence document at all"
means; a key present with an empty string would be a *supplied* empty document,
and is refused as malformed-input.

**A matrix and coverage view:**

- `/matrix` runs every matrix the project declares, through
  `experimental_test_packs`.
- `/packs/:id/matrix` runs one pack's.

Two things are on that page, and they answer different questions.

The **rows** say whether what a project wrote about its own packs still holds.
Each row shows the disposition it expects beside the one the evaluator produced
— the comparison is on RFC 8785 canonical bytes, so the view parses those bytes
to show `kind`, `outcomeId`, reasons and handoff, and reports the runtime's own
verdict rather than recomputing one. A row that expects a refusal carries no
disposition at all, and is shown as the §8.4 class and phase it names.

A row may additionally assert **where the decision goes** (ADR-0025), and that
assertion is kept visibly apart from the disposition, because §8.3 keeps the
target outside one. It is the case the view most needs to make legible: a pack
edit touching only `escalation.target.name` leaves every disposition byte
identical, so such a row fails with its expected and actual dispositions
matching exactly. The page says so in as many words rather than leaving a red
row to be puzzled over. The three states the report distinguishes are kept
distinct too — a named target, the literal `null` for "no target at all", and
`unavailable` where the report cannot state one — because "no target" is an
answer and "unavailable" is the absence of one.

The **coverage report** says how much of each pack those rows are about, and it
usually has more to say than the rows do: a matrix can pass everything it has
while stating nothing about most of what its pack can do. So the gaps lead and
the witnessed probes fold away underneath. Each gap carries the runtime's own
sentence naming what no row said, including the derived boundary probes
(ADR-0023) — the exact value at which a strict and a non-strict encoding of a
threshold would differ, which is the one input a matrix is most likely to lack.

None of it gates. A missing probe moves no status, and the page says so, because
a report that looked like a failing check would be read as one.

**Graph views:**

- `/graphs` runs every graph the project configures, through
  `experimental_test_graphs`; `/graphs/:id` runs one.

A graph composes packs: one node's outcome lands at a fact pointer the next
node's rules read, and its resolution state feeds that node's evidence. No JPS
version defines a graph, a composition, or a composite result — the format is
the runtime's own convention, and only each node's pack evaluation reaches the
shared evaluator. The page carries the payload's own label saying so.

Each graph is drawn as the **walk** it is, and what that diagram can claim
depends on what the connected runtime serves. The desk feature-detects both
graph tools by name at connect time and never reads a version string — following
`nextCursor` to the end of the listing, because a tool on a page nobody asked
for would read as a tool the runtime does not have. A listing that does not
answer leaves what the runtime can do **unknown rather than absent**: the
optional surfaces stay off, and a banner says the listing is what is missing,
because a page that quietly withdrew them would be claiming the runtime lacks
them.

**With `experimental_get_graph`** (ADR-0029) the desk fetches the graph
document itself and draws the composition: every node the document declares,
laid out in layers by the document's own edges, with **one real arrow per
declared edge** labelled with what that edge carries — the fact pointer it
writes, the evidence requirement it feeds, and the tri-state that requirement
takes when the upstream disposition is not an outcome. The node the document
declares as its `result` is marked, and the arrow from it to the composite
headline is the one further relationship the document states. Each node names
the pack it evaluates. Layering is longest-path over the declared edges; where
two nodes sit in one layer the tie is broken by the runtime's own evaluation
order, read off the coverage report, so nothing invents a sequence.

**The runtime's verdict on its own bytes is final.** `experimental_get_graph`
reports `status: valid` where its own strict decode succeeded and
`undecodable` where it did not, and that decode is stricter than a browser's:
duplicate member names alone are refused there and resolved last-wins by
`JSON.parse` here. So the desk reads nothing out of a document the runtime could
not — it falls back with the runtime's own sentence, verbatim, rather than
drawing a graph out of bytes the runtime had already refused.

Past that verdict, what the views draw from is checked here, member by member,
because `valid` means acceptable JSON with an object root and not a schema
verdict. A `nodes` member that is not a map, a missing `edges` array (the format
requires it even when empty, so absent is not none), an edge missing an
endpoint, a member of the wrong type — each declines the drawing and says which
member declined it, rather than being coerced into a shape the document never
stated. **A cycle or a self-loop declines it too**: the format requires the
edges to form a DAG, so every way of drawing one is a repair — and an edge
naming an endpoint the document does not declare is listed rather than drawn.

The document also shows what coverage alone could not: a node the coverage
report names no probe for is declared by the document and drawn, and the page
says coverage names no probe for it rather than showing it with a gap count of
zero. Why coverage names none is not something either payload states, so the
page does not say.

The two accounts on screen — the document, and the matrix run whose coverage and
rows it is joined to — come from two calls, and **ADR-0030 is what proves they
describe one file**. A graph matrix entry reports `graphSha256`, the digest of
the exact bytes that run decoded; `experimental_get_graph` reports the `sha256`
of the bytes it served. The desk compares the two.

Where they **agree**, the walk is drawn and the page says so in one line: one
revision, a binding of bytes and not a verdict on the revision. Where they
**disagree**, the graph file was edited between the two calls, so the two
answers are about two revisions and the desk does not join them at all — the
document walk is withdrawn, the coverage fallback stands in with a line naming
the divergence, and both queries are asked again so the next pair can re-bind.
That re-ask is **one cycle per pair**: the pairs a connection has asked about
are remembered whole, digests folded to one spelling, so a file still being
edited settles into the withdrawal instead of spinning, and one edited back and
forth between two revisions asks about each once rather than alternating
forever. Combining one revision's rows with another revision's arrows is the
thing this prevents, and it prevents it by not drawing rather than by choosing a
winner:
neither revision is called wrong, because which is right is not a question two
digests answer. Where the matrix entry states **no digest** — jpack 0.18.0 and
older, or an entry whose document did not load at all — there is nothing to
compare, so nothing is claimed in either direction and the older bounds below
stand exactly as they were.

Those bounds stay in every case, because the digest upgrades the join rather
than replacing what keys it: the document query is keyed by the connection, a
document is drawn only while the runtime still advertises the tool, and neither
is joined to the other while either call is in flight.

**Without it** — jpack 0.18.0 and older — the fallback is unchanged: the nodes
represented in the coverage report, on the evaluation-order axis the runtime
enumerated them along, ending in the composite headline. Coverage is then the
only account of the graph's shape that reaches this wire, and it can omit a node
the walk holds, so that diagram claims representation and not
completeness. **It draws no arrow between two nodes**, because the wire carries
the walk's node order and the edge indices coverage represents but not which
node feeds which — and in a graph with independent branches that arrow would be
false. Its edges are reported beside it as the indexed slots the payload
describes them as, each with the witness its resolved and unresolved branches
have. Nothing is reconstructed by parsing the English in a `detail` sentence,
which would make a contract out of prose.

A runtime that serves documents but could not decode this one falls back the
same way, with one line saying so: **serving is not validating**, and the
runtime returns a mid-edit document deliberately rather than going silent on it.

What holds either way: choosing a row colours the nodes with the comparisons
that row reported and nothing else; a node the selected row reports no
comparison for is shown exactly that way rather than as having passed; and the
row's own verdict — which covers the headline and every reported node comparison
together — is shown beside the diagram as the row's, never painted onto the
composite.

**What the project configures** is listed above the run where
`experimental_list_graphs` is served: the configured id beside the document's
own id and version, its declared format version and result node, its node and
edge counts, and the configuration's description. It costs one call that
evaluates nothing, so it lands before the matrix has finished — and it lists a
graph whose rows would not load, which a matrix run reports only as a failure.
Counts absent from a row are printed as not read rather than as `0`: the runtime
omits them, never zeroes them, exactly so a malformed document cannot look
honestly empty. A listing that *refused* is reported on the page with the
runtime's own message, and the section is left empty rather than showing what a
failed call could no longer confirm — the matrix still runs beside it, and still
reports the graphs whose rows loaded. Without that tool the page behaves as it
always did and finds the graphs by running their matrices.

Graph coverage is grouped per node, in the runtime's evaluation order, and
reported exactly as the pack coverage is.

**Traces of the compared nodes the walk evaluates, where you ask for them**
(ADR-0031). A control on the page — present only where the connected runtime
advertises the `include_traces` argument on its own graph matrix tool, and **off
by default** — re-runs the matrix asking for them. Off omits the argument
entirely, so the untraced call is unchanged; the two answers are kept in
separate cache entries, because a payload carrying no traces answers a different
question and must never be shown as the answer to this one. Each trace is drawn
by the same renderer the evaluation view uses, since it is the same artifact
under the same contract, and a comparison that *mismatched* shows its trace too
— that is the one worth reading. A trace of `[]` is said to carry no entries; an
absent member shows nothing, because absent means not asked or not evaluated,
and a comparison naming a node the graph does not declare has none even when
traces were asked for. The comparisons are listed lexicographically by node name
and each trace is the evaluator's own walk order: two different orders, said so
where they meet.

A traced request can fail, and traces riding inside the runtime's report budget
is one reason among several — a suite that fits without them can be over it with
them. The desk does not say which reason. A tool error arrives in one
unstructured shape whether the cause was the budget, an argument this runtime
rejects, a configuration it could not find, or a graph id it does not have, and
an error that is not a refusal at all covers a response the runtime *did*
produce and the desk could not read. So the page shows the runtime's own message
as the reason and adds only what it actually knows: that **this request asked
for traces**, that it **did not produce a usable answer**, and what clearing the
ask will do — return to the untraced answer where one is still in hand, or retry
the untraced request where none was ever received. The control stays on screen
through the failure. It never renders a failure as an absence of traces: the
question was not answered, so nothing is known about the answer.

**Where a composed decision is handed off** (ADR-0032). A rows document
declaring `graphMatrixVersion` `"2"` may assert the handoff target of the
composite and of each node it names, and the run reports each assertion as an
expected/actual pair — on the row, and on that node's comparison. The two
members are **one pair**: they appear together, exactly when a *well-formed*
assertion rode a run this walk *performed*, and a row whose assertion was itself
defective reports that defect in its detail and no pair at all. The desk reads
them through one accessor that applies that rule, and shows nothing where only
one half arrived. The composite's target is the result node's own — a named
target exactly when that disposition requested a handoff, and the literal `null`
otherwise.

Both pairs are drawn with the component the pack matrix uses, in the same three
states: a named target, the literal `null` for "no target at all", and
`unavailable`. That third state is reachable on the **row** alone, where a
refused run leaves nothing to state; a node comparison exists only because the
walk evaluated that node, so it reports a rendering or `null` and never
`unavailable`. The renderings are display values and are never compared here,
exactly as on the pack side — a capped rendering can differ from its own pair
past the cap — so the comparator decided on decoded targets, and the row's own
status is the only verdict shown.

A project that configures no graph is an answer rather than an error: the walk
reports `skipped` with no entries, the home page offers no graph entry, and the
graphs page says the project configures none.

**A shell around all of it.** Six regions — a header, a navigation rail, the
routes above, an Inspector, a Console and the status strip that is its
collapsed face — plus two pages of their own: `/admin`, which renders the desk's
configuration as two groups of cards in one shape under a status line, and
`/help`, which names what this runtime advertises and renders its own authoring
prompt as text. The shell **derives no verdict**:
no status colour in the rail, no rollup count, no "N failing" pill anywhere. A
red badge in a nav rail would be a gate the runtime never issued.

## Shell

**Six regions**, on a CSS grid of a **definite** viewport height —
`height: 100dvh` and not `min-height`, so the content row divides the viewport
instead of growing to fit a long page, `.desk-main` is the route's scroll
container, and the 28px strip stays on screen. The three pane sizes in the table are the
configured values, written onto the grid as `--rail-w`, `--inspector-w` and
`--console-h`; collapse writes one of two values into a second custom property
and never a third number.

A route publishes into the Inspector through `useInspectorPortal(node)`, which
portals into the element the slot hands it and **claims the slot while it is
there**. The context sits above `<main>`, so a route can reach it, and the pane
publishes its target through a callback ref — a drawer that starts closed
reports no target rather than a detached one, and a route that is told there is
nowhere to publish renders nothing.

The claim exists because a portal cannot tell React it happened: the pane used
to render its empty-state paragraph unconditionally, so the first route to
publish showed its panel *and* the empty state underneath it. A CSS `:empty`
sibling rule would have been shorter and is rejected — vitest runs with
`css: false`, so nothing in this repo could hold it and the mutation harness
could not discriminate it.

**A route may add a landmark inside `main`.** The pack route adds two: the
packs pane is a list of navigations, so `<nav aria-label="Packs">` is the
correct markup for it, and the document carries its own `<nav aria-label=
"Members">` outline. The six regions below are the *shell's*, each still exactly
one, and a test holds that with both of the route's own landmarks mounted.

| Region | Default | Collapse | Landmark |
|---|---|---|---|
| Header | Always visible, 48px | Never | `banner` |
| Left rail | Expanded, `panes.left.width` (248px) | → 56px icon rail; a drawer below 900px | `navigation`, named "Project" |
| Main | Always visible | Never | `main`, `id="main"`, the skip link's target |
| Inspector | Closed, `panes.inspector.width` (360px) | → 0px; a drawer below 1100px | `complementary`, named "Inspector" |
| Console | Collapsed to the 28px strip, `panes.console.height` (240px) | → the strip, never below it | `region`, named "Console" |
| Status strip | Always visible, 28px | Never | `contentinfo` |

**The measure is left-aligned at one gutter.** A route's content is not centred
in the main pane — `.desk-measure` is `margin: 0` with `padding:
var(--density-section) var(--density-gutter) 4rem` — so the content's left edge
is the rail's right border plus one gutter (2rem, 1.25rem compact, 1rem below
600px) and is the same on every route at every width, which is what a
persistent rail is for; a centred column beside a rail is aligned to neither.
**Each route names its width kind**, with `data-measure="form" | "wide" |
"full"` on its top-level element: `form` is `--measure-form` (44rem: Admin,
Help & About), `wide` is `--measure-wide` (72rem: the packs layout, a pack's
evaluate route, project home, author) and is the declared default, `full` is
`max-width: none` (matrix and coverage, graphs) — and a route that names none
fails `web/src/routes/measure.test.ts`, which derives the set of routes from
`App.tsx`, rather than quietly rendering at the default.

**The document never scrolls, because every scroller is a containing block.**
A scroll container clips and scrolls only the descendants whose containing
block lies inside it, so **every rule that authors a scrolling overflow
declares a position that positions in the same rule** — `relative` on sixteen
of the eighteen, and `fixed` on the two that were already out of flow, the
dialog's content and the shell's drawer, which the rail and the Inspector both
use. So do the frame and its four panes, under their own exact selector. A
rule that *merely clips* is not held — an `overflow: hidden` on an ellipsis
label or a popup clips text, and text has no containing block to be laid out
against.

Measured by `scripts/containment-check.sh` on a build of e2d1dee, in the
script's own configuration: Admin at 1400×800 measured a document 2355px tall
inside an 800px window, `scrollY` reached 1555 after a `scrollTo(0, 5000)`, and
the whole shell could be scrolled up out of the frame, because Radix renders a
1px hidden `<select>` beside every Select trigger that sits inside a `<form>`:
three of them, at y 1725, 2257 and 2354, absolutely positioned against the
*initial* containing block, neither scrolled with the main pane nor clipped by
the frame, and counted into the document's own overflow.

Two things check it, and they check different halves.
`web/src/ui/containingBlock.test.ts` **reads the source**. It holds the
declaring rules — each rule that authors a scrolling overflow, and the frame
and the four panes under their own exact selector. What it cannot hold is the
cascade: a rule that takes a pane's position
back by *any other* selector — an ancestor in front of it, an id, an attribute,
a nested `&`, a `:global`, an inline style — is a computed result and not a
sentence in a sheet, and three drafts that tried to emulate it were each
defeated by a construction nobody had thought of. Its docstring says exactly
that, rather than leaving it to be found. **`scripts/containment-check.sh`
measures the cascade**, in Chrome, against a built chassis: the computed
`position` of the frame and each pane, `scrollHeight` against `innerHeight`,
`scrollY` after a `scrollTo(0, 5000)`, and whether any absolutely positioned
element still resolves its `offsetParent` to `BODY`. Every route `App.tsx`
declares, at every width the sheets author a breakpoint for, four pane
configurations above 1099px and three at or below it — below that the Inspector
is a modal drawer whose overlay owns the pointer, so the console cannot be
toggled while it is open — 242 rows a build. It measured 0 of 242 contained at
e2d1dee and 242 of 242 after. CI supplies no runtime binary and no project, so
there is nothing for the chassis to serve; the gate is run by hand. Run before
every merge that touches a stylesheet. This is a convention; nothing automated
enforces it.

A collapsed pane is **removed from the accessibility tree**, not merely made
invisible: closed is the `hidden` attribute plus `[hidden] { display: none
!important }` in the shell sheet, so a viewer who has closed the Inspector
cannot tab into it.

**Below 900px the rail is an overlay drawer, opened from the header.** In drawer
form the rail draws no collapse toggle, so the opener has to live outside it —
a control inside a closed drawer opens nothing. It is the `Project navigation`
button at the left of the header, present only at that width, carrying
`aria-expanded` always and `aria-controls="desk-rail"` **only while the drawer
is open** — a closed `Dialog` unmounts its portal, so the id is not in the
document and naming it would offer assistive technology a broken relationship
rather than none. The same holds for the Inspector's toggle below 1100px. Both
drawers hand focus back to the header control that opened them, by reference:
neither has a `Dialog.Trigger` to restore to, because both openers are in the
header two grid cells away. The drawer carries the
`navigation` landmark with it, so the region table above holds at every width,
and it carries a visible close button — Escape and the overlay are not
affordances a viewer can see. **Every navigation inside it closes it**: the
drawer is modal, so a link that navigated and left it standing put the
destination behind an overlay.
Both drawers are **modal**: while one is open the page beneath it is
`aria-hidden`, which is what a modal is for and is why the landmark count is not
the same in that state.

**Shortcuts.** `Mod` is Ctrl or Cmd.

| Chord | Does |
|---|---|
| `Mod+B` | Collapse or expand the navigation rail |
| `Mod+Alt+I` | Open or close the Inspector |
| `Mod+Alt+J` | Open or close the Console |
| `Mod+S` | Save, while editing a pack |

Every **shell** chord is suppressed while focus is in an `input`, a `textarea`
or a `contenteditable` — which is exactly an editor — and every one has a
visible button, so a chord the browser claims costs a click and not a feature.

`Mod+S` is on the list and is deliberately **not** installed by
`installShortcuts`, and the two facts are one fact: save is the chord that has
to fire *inside* a text field, which is where that rule silences everything
else. The pack editor registers it on `document` for exactly as long as edit
mode is on screen — not on a subtree, because `document.body` is where focus
sits the moment the mode opens (the Edit button unmounts itself) and from a
subtree listener the chord neither saved nor called `preventDefault`, so the
browser's own "Save page as…" opened over unsaved work. The shell's rule stays
as written and the label says where this one applies.
**Every modifier a chord does not declare is rejected**: Ctrl+Shift+B is not
`Mod+B`, and Ctrl+Cmd+B is neither of the two spellings of `Mod` — an undeclared
chord is left to the browser unprevented rather than claimed and swallowed.
On macOS, Cmd+Alt+I and Cmd+Alt+J are the browser's own developer-tools chords
and Cmd+B is Firefox's bookmarks sidebar; the Ctrl spelling works everywhere.
`Mod+J` (Downloads) and `Alt+<digit>` (Firefox tab switching) are deliberately
not bound, and `F6` is left to the browser.

A pane is not a dialog, so `Escape` does not close one — with one exception,
stated rather than hidden: below 1100px the Inspector renders as a drawer, and a
drawer *is* a dialog, so Escape closes it there. Swapping to the drawer remounts
the subtree, so inspector-local state resets at that breakpoint.

**What is remembered is what somebody chose.** A layout that came from the
configuration file or from the built-in defaults is not written down: it is
re-derived on every load from inputs that are still there, and a record of it
would be preferred over the file on the next visit — which is how a `panes`
block becomes permanently inert on a browser that opened the desk once before
the file existed. The record appears the first time a pane is moved by hand.

**Where it is kept.** Which panes are open is per viewer and per project, in
`localStorage` under

```
jpack-desk:shell:v1:<projectKey>
```

where `projectKey` is **the project root the chassis pins at startup**,
percent-encoded whole. It is that root and not the runtime's `configPath`,
because a project with no `jpack.json` reports no config path — so every
configless project on one origin used to map to the literal `default` and share
a single record, two directories with one layout between them. The literal
`default` remains and now means exactly one thing: the file listing has not
answered yet, and **nothing is written under it**.

**The whole path, not a slug and a short hash.** It was a 64-character slug plus
eight hex digits of FNV-1a, and eight hex digits collide: two roots differing
only past the 81st character produced one key, so one project's reset removed
the other's record. Percent-encoding is injective — `%` is itself escaped, so
the encoding is prefix-free and decodable — which makes distinct roots distinct
keys by construction rather than with probability, and it carries no `:`, so no
root can be read as part of the key's own prefix. Records under the old keys are
never read again, which is what this store does with every record it cannot
use.

**The whole path means the bytes the chassis reported**, and it used to be
trimmed first — which made the claim false exactly where it matters: a POSIX
filesystem permits a trailing space, so `/srv/project` and `/srv/project ` are
two directories that shared one key, and one project's record was restored and
reset for the other. That is the collision the encoding replaced, reintroduced
one line above it. Whitespace still decides whether there is a project at all,
and that decision belongs to one predicate so the two cannot disagree.

One desk on one origin serves whichever project it was started against, and a
layout chosen for a three-pack project is not the one chosen for a forty-pack
one. Only the collapse flags and the console's channel are stored — no widths,
because nothing on this desk can yet change one, and a stored number no viewer
could have chosen would be a record of a choice nobody made — and **only for the
panes the viewer has actually moved**, one bit each. A record that carried all
three because one was toggled would be two built-in defaults outranking the
configuration file for ever. Every read and write is in `try/catch`: a private
window and a browser with site data blocked *throw* on the accessor rather than
answering null.

**Reset panes** is in the user menu, and it is in the shell rather than in Admin
because the record it clears is this browser's and this viewer's — the same
class of thing as the two settings links beside it, and about all three panes,
so it is not one pane's own header control. It clears exactly that one key:
`localStorage.clear()` would take every other key on this origin and every other
project's layout with it, and a reset that logged the viewer out of something
would be one that lied about its scope. **And only a record this shell wrote** —
the key is derived from a path the viewer never chose, on an origin this desk
shares with whatever else has been served from it, so a value that is not JSON,
is not an object, or carries another shell version is left exactly where it is.
The reset runs inside the provider that owns the record, so it cancels a write
already on its way, refuses to clear the provisional `default` key before the
chassis has said which project this is, and reads the key back afterwards — and
it reports **what happened**, in four sentences rather than one, because
"cleared", "this browser refused", "the project is not known yet" and "what is
there is not ours" are four different facts.
The menu stays open while it answers, since a menu that closed would take the
answer with it.

**The pane dimensions are not a setting on a settings page.** They are in the
schema, they are decoded, validated and applied, and Admin offers no control for
them: a page that edits the frame it is drawn in is a control looking for a
pane, and the write path went with the form. `panes` in `jpack-desk.json` is
read exactly as it always was.

Reduced motion is respected: `prefers-reduced-motion: reduce` sets every pane
transition to zero, and collapse is instant.

**Appearance is yours.** Theme and density are set from the **user menu**, as
two radio groups that apply the moment they are picked — there is no Save,
because a preference is not a file — and they are stored in this browser, under
one key per project, on exactly the grammar and discipline the pane record uses:

```
jpack-desk:appearance:v1:<projectKey>
```

The project file's `appearance` is the **default** for everyone who has not
chosen, and that is the whole of the change. It used to be a card on Admin that
wrote the member on a Save, so one person choosing dark chose it for everybody
who ever cloned the repository; the member is still in the schema, still decoded
and still applied, and it is now the value the menu names under the groups —
`Project default: <theme>, <density>` — so that "Use the project's default" is
not a leap in the dark. The ladder is preference, then the project file, then
the built-in default, computed in one place and applied in one place.

Every read and write is in `try/catch`, a record this desk did not write is left
where it is, and a stored value outside the decoder's own unions is a record
this desk did not write: never applied, never shown as chosen, and never
deleted. A record is this desk's when it
carries the version, no member this writer does not write, at least one that it
does, and a value in its own union for every member present — ownership is every
byte of the record and not the version number, because `localStorage` is one
namespace shared with everything this origin has ever served under a key derived
from a path the viewer never chose. So `{"v":1,"writer":"another-app",…}` and
`{"v":1,"theme":17}` are both somebody else's value: not applied, not deleted,
and named as such. The question is not "is this legible" but "could this desk
have written it", and one member this writer could not have written says the
writer was not this one, whatever the member beside it says. The write serializes only the
member the viewer actually chose, for the reason the pane record gives — a
`density` stored because the *theme* was picked would be a built-in value
silently outranking `jpack-desk.json` for ever. A choice is stamped with the key
it was made under, and a chassis that names a different project **discards** it
rather than hiding it: hidden, it came back when the tab returned to the first
project, ahead of that project's own record and over whatever another tab had
written there meanwhile. Clearing it says which of four
things happened, on the panes' reset's own terms.

**Nothing is applied that this desk has not established.** The record is
unreadable until the chassis names the project, and `appearance` is the schema's
until the file has been read — so until each of those answers, the root element
is left exactly as it was found and the menu shows nothing as chosen, with the
project-default line saying it has not been read yet. A member the viewer has
actually picked needs neither answer: it is the top of the ladder, so it applies
the moment it is picked. With a stored `dark` over a file that says `light`, one
load applies `dark`, once, and neither `system` nor `light` at any point.

**The record is this browser's, not this person's.** The identity slot has no
real user yet; when it does, the record can move server-side and follow somebody
between machines. Until then a second browser is a second preference, and the
menu does not pretend otherwise.

**What a theme does.** `appearance.theme` writes `data-theme` on the root
element — `light` and `dark` pin a palette, `system` removes the attribute and
leaves `prefers-color-scheme` to answer, so **under `system` the desk follows
the operating system's own setting and changes with it**. Both palettes are
authored: every colour token on `:root` has a dark value, in both of the blocks
that select dark — the media block and the attribute block — and they carry the
same value token for token, which a test holds, because they cannot be written
once and nothing else stops one being edited and the other forgotten. The
condition verdicts are re-authored rather than inverted: green, neutral and
violet are what `true`, `false` and `unknown` *mean*, so the dark values keep
the hue and change the lightness.

**The contrast is measured, not chosen by eye.** `web/src/ui/palette.test.ts`
computes the WCAG ratio for every text on every background it is painted on,
every semantic foreground on its own background, and the focus ring against
both grounds — 4.5:1, and 3:1 for the ring — from the token bytes themselves,
in **both** palettes. It caught the light one first: `--ink-faint` was `#86867d`
and reached 3.4:1 on the page, and it is darker now because of it. The borders
are deliberately not in that set and the test says so where the pairs are
listed: they separate regions rather than identify controls, and holding them
to 3:1 would be a dark palette re-authoring the light desk on its way past.

**One case still flashes, and it cannot be fixed with a script.** Under
`system` nothing does: the media block paints dark on the first paint, before
any JavaScript runs. A viewer who has explicitly chosen `dark` while their
computer is set to light gets one light frame, because the record that holds
that choice is keyed on the chassis' project root and the page does not know it
until the file listing answers — a pre-paint script would have to guess the
key, and a wrong guess applies one project's preference to another's desk.

**What compact does.** `appearance.density` writes `data-density` on the same
element — `compact` sets it, `comfortable` removes it, because comfortable is
the scale on bare `:root`. That scale is a list row's height, a control's
height, a table cell's two paddings, the gap between items in a list, the type
size of the surfaces that are dense to begin with, the two vertical rhythms a
flat page keeps, and the page measure's own gutter — the count is deliberately
not written here or in the sheet, because it said "six" through two chunks that
added three. Compact tightens every one of them, and everything that shrinks
reads one — the packs list, the inspector's rows and its diagnostics, the
assistant's event list and proposal, the disposition diff's cells, the Admin
cards' field rows, the console log, the pane heads, the `Button` and `Select`
heights, and the gutter the content starts at. Tokens rather than
per-component rules: a pane with a `[data-density]` selector of its own would
be one more answer to a question the scale already answers. A test
holds every compact value **strictly** smaller than its comfortable one, in the
same unit — a density that is offered, stored and applied while changing
nothing is exactly what this replaces. The one number that could not stay in
the sheet is the packs list's row height, because that list is windowed and
reserves its off-screen rows in JavaScript; `ROW_HEIGHT` carries it and the
same test holds it equal to `--density-row` in both blocks.

## Pack view

`/packs` is a layout: the project's packs on the left, the selected one beside
them. The pane survives every change to the selection, so a filter and a scroll
position are not lost by opening a pack.

**The document reads in the file's own order.** A pack that writes `rules`
before `outcomes` renders rules first, because that is the document someone
wrote and a page that re-sorted would be showing one nobody did. The order the
schema declares is used for exactly one thing: where to put the line for a
member the file does **not** declare.

**Every top-level member finds its own place, the five identity members
included.** They were drawn as one unit positioned at the earliest of the five,
so a document writing `decision` before `specVersion`, `id` and `version` had
those three moved in front of `decision` — the page re-sorting, in the one
place its own test was written not to look. Grouping them under a single
`Identity` outline entry is a **nav** decision and is made in the nav: five
near-identical entries would be a worse nav, and that is not a reason to move
anything on the page.

**An omitted optional member gets a line saying it is omitted**, spliced in at
the position the schema's order gives it — so `applicability`'s "not declared"
sits between the decision and the evidence requirements, and `/description` has
one too: while the identity members were one unit the unit was present because
`title` was, so nothing ever said the description was absent. A **required**
member that is missing gets no such line: its absence is a refusal rather than
an omission, the runtime issues one at that pointer, and a block there would
take that diagnostic off the strip — where every reader sees it — and put it
behind a selection nobody has made. Every outline entry is a link, omissions
included, because the omission block is a place on the page like any other.
This is the difference between "the pack does not narrow its own scope" and
"the page did not draw that part", and the view this replaced could not tell
them apart: its section wrapper returned nothing when it had nothing to
render.

Three things the old view dropped are here: `metadata.reviews`, the per-member
`extensions` objects (eight `$defs` may carry one, plus the root), and the
condition tree. Reviews are **rendered and never written**: this surface has no
reviewer identity, so a review it wrote would be signed by nobody.

The fixtures the whole of this is asserted against are documents `jpack spec
validate` **reads**, and a test holds every enum-valued member of each of them
against the spec's own closed lists — including each condition node's own `op`,
at every depth, which the walk used to descend through while checking only the
operators inside `fact` nodes. Two of the three are accepted outright;
`full.pack.json` is structurally accepted and then refused as `unsupported`
with exit 2, deliberately — it declares `example.review-window` as a *required*
extension, which is the case that exists to show a runtime refusing a document
it can read perfectly well. Saying all three were "accepted" was a claim about
an exit code none of them had in common. They were not: `full.pack.json` wrote an
evidence-requirement id into `escalation.triggers`, which is what made the wrong
reference model above look correct and froze it in a passing test.

### One address space

Every block carries its RFC 6901 pointer, and that one string is five things:

| Where | As |
|---|---|
| The renderer | `data-pointer="/rules/1"` |
| The DOM | the element's `id`, verbatim |
| A deep link | `#/rules/1` — scrolls to the block, focuses it, selects it |
| The Inspector | the `?at` search parameter |
| A diagnostic | `instancePath`, which is the same string from the runtime |

The escaping mirrors the runtime's own `carrier.Pointer` byte for byte: `~` to
`~0`, `/` to `~1`, and the document itself is the empty string. Three
consequences are written into `packs/pointers.ts` because all three are silent:

- An id containing `/` or `~` is legal HTML and is **not** a valid CSS
  selector, so **every lookup by pointer value uses `getElementById`** and
  never `querySelector`. Fixed selectors — `[data-pointer]`, `a`, the article
  itself — are enumerated with `querySelectorAll` in a few places, which
  interpolates nothing and cannot hit this; the rule is about pointer values,
  and stating it as "no `querySelector` anywhere" was a claim the code does not
  make.
- A fragment is percent-decoded before it is compared.
- **An address that is not an address names nothing.** RFC 6901 admits exactly
  two escapes, and `~2` or a bare trailing `~` is neither — those parsed as
  ordinary characters and named a member nobody wrote. An array index must be
  `0` or a digit string with no leading zero, and is bounds-checked; a member
  is looked up as an **own** property, so `/constructor` and `/toString` name
  nothing rather than selecting something no JSON document has. One evaluator
  decides all of it, because three of them disagreed.

Selection is held in the **route** and never in the pane: `RightPane` swaps its
wrapper at 1100px and remounts the subtree, so a selection the pane held would
be lost at that width. Selecting writes with `replace`, because choosing what to
inspect is not a navigation and must not fill the Back stack — from a block and
from an outline entry alike, and both carry the rest of the address through. One
element carries one pointer, and a test holds that no pointer appears twice.

Selecting also **opens the Inspector** where it is closed, which is the shell's
default in a fresh profile. It is a response to a gesture and not a seed: a
reader who picks a member has said what the pane is for, and without it the
panel filled behind a closed pane and the only thing that changed on screen was
the block's own border.

**The document is one tab stop**, not ninety-seven. A roving tab index puts
`tabIndex={0}` on exactly one block: the arrow keys move it in document order,
Home and End reach the ends, and Enter or Space selects. Otherwise the only
keyboard route into `?at` was the outline, which addresses the twelve member
units and nothing under them — no rule card, no condition operand, no review. No
`role` is claimed for a block: these are the document's own regions, nested
inside one another, and `role="button"` on a container holding more of them
would be a lie about both.

### The Inspector's three panels

- **Member** — the pointer, the member's own JSON subtree pretty-printed in a
  container that scrolls sideways, and the provenance beside it: the declared
  path, the byte count, the digest of the loaded document, and the line
  "matches the file the editor holds" **only when the two digests are equal**.
  `get_pack`'s `sha256` and the chassis' file read are two answers about one
  file, and only equality proves they describe one revision.
- **References** — what the member names and what names it, in both directions,
  computed from the document. Where an id resolves to nothing the line says "no
  declared outcome carries this id" and stops: `JPS-SEMANTIC-UNRESOLVED-OUTCOME`
  is the runtime's to issue, and the desk must not shadow it with a word of its
  own. A member that names **no id at all** produces no line: `escalation.
  triggers` is a closed enum of five reason words, not a list of ids, and
  resolving one printed a dangling-reference claim on every conformant pack.
- **Checks** — the diagnostics anchored at or under the pointer, each printing
  the runtime's own `code`, `layer`, `severity` and `codeStability`, with the
  pointer at the foot; then which bytes were checked. An empty set is **not** a
  clean bill and the panel does not dress it as one — and it does not say even
  that much while the check is still in flight, where the list was truncated, or
  where the check is stale, because in each of those the empty set is not an
  answer.

### The packs pane

240px in main's left: a filter over the pack id, a sort (name ascending and
descending, and nothing else — `list_packs` reports no date and no size, so any
other order would be one the desk invented), the rows with their versions, and
"Show all N" past the first screenful, offered only while the listing has
**currently** succeeded — a refetch error keeps the last good data, and a button
underneath a failure sentence offering to show all N of a listing the pane had
just said it could not read is an offer about nothing.

Rows are links, so tab order is native; the arrow keys step between them and
Home and End reach the ends of **the list**, not of the window. A destination
that is not rendered is scrolled into view and focused in the render that brings
it in: navigating by the rendered anchors clamped every key to the window, so
with 300 rows in a 400px viewport focus stopped at row 21 and ArrowDown from
there prevented the default and moved nothing. Filtering resets the scroll,
because a window computed from the old position can begin past the end of the
new list and render no rows at all.

A pack whose document the listing could not read is still listed, with `packId`
and `packVersion` sent as **empty strings** and the reason in `detail`. Such a
row carries the runtime's own sentence instead of a version: an empty version is
not a version, and a bare "v" asserted a member of a document nothing could
read.

Past a screenful the list is windowed — a fixed row height, an overscan, and no
new dependency. **A viewport that cannot be measured renders every row**, which
is the case in jsdom, where nothing is laid out and every measured height is
zero.

A refused listing shows the failure. "This project declares no packs" and "the
listing did not answer" are different statements and only one is about the
project — which is why the rail's Packs entry carries a count only where the
listing actually answered, and never a `0`.

The pane is a `<nav aria-label="Packs">`, because it is a list of navigations.
That is a **seventh** landmark on the page while this route is open, inside
`main`, and the document's own member outline is an **eighth** — three
navigations in all, each named, so a screen reader can tell them apart. The
shell's own six are unchanged, and a test mounts both of the route's landmarks
and holds that each of the six is still exactly one.

### Checks and layers

The check runs on load, over the file's bytes where they loaded and over the
served document where they did not — and the strip says which. The query is
keyed on the **bytes and the connection epoch**: identical bytes answer
differently on a runtime bundling different specification artifacts, so a report
cached across a reconnect would be a different binary's opinion of the same
file. `validate` is sent `{document}` and nothing else; omitting `through` is
what makes the runtime run its own default, which is the whole ladder.

The sentence is derived from the payload's own `layers` rows and quotes its
`status` word verbatim — **every row it was given, and the status, in one
shape whatever happened**. A failure used to be printed as the failing layer
alone: `invalid` with `[carrier passed, structural failed]` came out as
`structural — 1 diagnostic`, naming neither the verdict the runtime reached nor
the layer that ran. **A layer the payload does not list is one that did not
run**: the ladder short-circuits, so a carrier failure reports `[carrier
failed]` alone and a structural failure returns before the semantic layer. Two
`unsupported` shapes must not be confused and each has a test — a specification
version the runtime does not bundle reports one layer row and a `capability`
diagnostic whose layer appears in no row at all, while an unsupported required
extension reports all three layers passing.

The document is one of three views on a pack, and it carries the links to the
other two: **Try it** to `/packs/:id/evaluate`, and **Test matrix** where the
listing says the pack declares one. Both were reachable only from the view this
replaced.

Diagnostics anchor on an exact `instancePath` match, else on the nearest
**rendered** ancestor with the diagnostic's own pointer printed verbatim beside
it, else on the document strip — which **prints them**, under the layer
sentence, with the runtime's code, message and the pointer it named. A pack with
no `specVersion` is refused at `/specVersion` and nothing draws a required
member that is not there, so its diagnostic reaches the strip and nothing else;
counting it in the sentence and printing it nowhere would be a page that says a
member is wrong and never says which. That ancestor walk is what makes a *missing*
member reportable: the runtime reports one at the pointer including the absent
name, so `/rules/0/when` on a rule with no `when` lands on that rule's card. A
diagnostic computed against different bytes is **never re-anchored** — deleting
`rules[0]` moves every `/rules/N`, so a `/rules/0` diagnostic would land on a
rule that is not the rule it is about, which is worse than no diagnostic at all
because it looks like an answer. The report carries the exact bytes it checked
and they are compared with the bytes on screen; where they differ the check is
stale, the strip says so, and **no** diagnostic is anchored — not the ones that
still resolve, because nothing in a pointer says which of them would still be
right. The check runs over the file on disk where it loaded and over the served
document where it did not, and those are two artifacts: the digest warning is
about two answers from two sources and can be quiet while these bytes still
differ.

An empty document is not checked at all, and the strip says so in words: the
call is disabled, and a disabled query has no data for ever — which the strip
used to read as "Checking…" and print until the page was left.

Where `diagnosticsTruncated` is set the runtime stopped at its own limit of
100, and the panel says the list was cut rather than that nothing else was
found.

### Editing

`?edit` on the same route, and a search parameter rather than a path segment
for one reason: the dirty blocker's predicate is
`currentLocation.pathname !== nextLocation.pathname`, so a mode in the path
would ask "leave without saving?" every time a viewer switched back to Read,
and a predicate loose enough to allow that would stop asking on the exits it
exists for. The toggle is the same page — same mount, same scroll, same
selection, same buffer — and `?at` and `?edit` are both written with
`replace: true`, because how you are looking at a document is not a
navigation.

**The buffer is the document, and nothing stands behind it.** Both modes draw
`indexDocument(buffer).value` rather than the parsed pack `get_pack` served, so
a keystroke in the JSON view moves the reading document above it and a form edit
is in the bytes the moment it is made. A page over one revision while the form
writes into another is the digest-binding failure one component further in.

The served document is drawn **only before a file has been loaded at all** — the
read has not answered, or the listing names no path. Once the editor holds
bytes, those bytes are the page whatever they say: bytes that do not scan are
the JSON view with the position they stop at, bytes that scan into something
that is not an object are the JSON view too, and a member of the wrong shape
states itself at its own pointer with its bytes in it. Falling back to the
runtime's last good answer there would draw a document that is on no disk, over
a file that no longer holds it — and the Inspector beside it would list members
and references the file does not carry. The digest sentence still says when the
two sources disagree.

**And the buffer follows the address.** `/packs/:packId` is one element inside
the packs layout, so another pack is another *parameter* and nothing unmounts —
which is what lets the mode toggle keep the mount, the scroll and the buffer.
Everything the page holds about a file therefore has to follow the address
itself: the buffer is seeded once **per path**, and the last write's verdict and
any unwritten operands are dropped when the path moves. A watcher refetch
carries the same path and still does not rebase, which is the rule the base
depends on. The Inspector's provenance group says the same thing from the other
side: while the buffer is dirty it stops claiming the file matches what the
editor holds, and says the figures are the file on disk instead.

The toolbar is edit mode's: a reading page carrying a Check button and a Save
that can never be pressed is chrome about a mode nobody is in. The way *in* is
one control beside the two standing links, and it writes `?edit` with
`replace: true` for the same reason selecting a member does.

**Forms in place.** A member's card becomes its form where it stands, and it
keeps the block's pointer as its `data-pointer` and element id — so a
diagnostic still anchors *on the field*, a deep link still reaches it, and the
Inspector still selects it. `ui/Field` owns the label, the `aria-describedby`
and the `aria-invalid`; what the field adds is the runtime's own code and
message, printed under the control and named in the description, so a screen
reader reaching the input is told what the runtime said rather than that
something is invalid. Phase 2 covers the flat members — identity, decision,
outcomes, evidence requirements, sources, escalation — and rules and
exceptions. What a field *says* before the runtime answers is said in words: an
id's hint is "lowercase letters, digits and hyphens", not the pattern, because
the pattern is `shape.ts`'s and a regular expression is not something to read
aloud.

**A member the document does not carry is stated, not drawn.** A field whose
container is absent has no span to splice into — a missing object is a different
edit, and inventing one would write members nobody asked for — so a control for
it would take a keystroke and move no bytes. `source.locator`,
`source.citation`, `escalation.target` and a rule's `when` are each drawn as
"not declared" with an offer to write the schema's own required members, empty;
the fields appear once the object does. A condition removed from a `not` is the
same case, and it is offered a condition back rather than being described as a
node kind this desk does not know.

**Form | JSON is a third view of the same buffer**, kept in sync both ways over
a monospace textarea with a scroll-synced, `aria-hidden` line gutter and no
editor dependency. Bytes that do not scan keep JSON available, withhold Form,
and print where the scanner stopped as a line and a column. So does a document
this desk's scanner and `JSON.parse` read differently — a duplicated member
name is the case that exists in the wild, and a form that wrote through a
reading nobody else shares would edit a document nobody has.

**Every edit is a splice.** `documentText.ts` indexes the bytes once and each
write replaces exactly one value's span; every byte outside it survives,
including the ones the desk has no opinion about. Blanking a `nonEmptyString`
removes the member rather than writing `""`, and a member the document does not
carry yet is *inserted* — at the position the schema's own property order gives
it, in the layout a neighbour already uses. Dirty is a **byte** comparison, so
a whitespace-only change is unsaved. Undo is a capped stack of buffer
snapshots, one per committed action, with typing coalesced per field — so a
sentence typed into a description is one Undo and not nine — and it is a
toolbar **button** rather than a chord, because `Mod+Z` inside a text field is
that field's own undo and taking it away would trade per-character undo for
per-action undo without asking. Past the cap the oldest entry is dropped and
the control goes disabled rather than the stack lying about its depth. Discard
restores the base and clears the last save attempt's verdict.

**The condition builder** draws the schema's five node kinds — `literal`,
`all`/`any`, `not`, `fact`, `evidence-present` — recursing through `$ref`. Each
group is a `role="group"` named by its operator and its pointer, its controls
are real buttons, and a nested group collapses to "collapsed · N conditions". A
`fact` node's operand control switches on the operator: the four ordered
comparisons write a decimal **string**, `in` a list, `equals` and `not-equals`
any JSON at all. **It shapes and it never refuses** — an empty `in`, an
unquoted `5000` and an id nothing declares are all writable, and `validate`
names them at their pointers. Changing an operator keeps the author's operand
rather than retyping it. A node kind this desk has never seen is printed as its
JSON and offered no controls, exactly as the reading tree holds it. One
deliberate exception to "every keystroke reaches the buffer": the operand
controls that take arbitrary JSON hold what is typed until it parses, because
writing each intermediate keystroke would withhold form mode with a parse error
in the middle of a word. Nothing is refused and nothing is corrected — the
field says it is not written yet and names the bytes still on disk, the text is
held by the editing session so it survives the switch to the JSON view and
back, and the toolbar says how many fields are in that state beside the unsaved
dot. Changing a node's *kind* moves one word where the new kind needs no member
the old node lacks, which is what `all` → `any` is: re-serializing the subtree
for it would re-indent every nested condition and re-print the author's own
number literals.

**Rule order is §7-significant**, so it moves by keyboard and not by drag: two
buttons on each card and `Alt+ArrowUp` / `Alt+ArrowDown` inside it, through the
writer's `moveElement`. Focus follows the card to its new address and a live
region names the position it landed in. A move invalidates every `/rules/N`
pointer past it — `?at`, the Inspector's subtree and every anchored diagnostic
— so the check is marked stale and **nothing is re-anchored**.

**Checks run on idle and on demand.** The first bytes go at once; every later
change waits for a pause, and the toolbar's Check and the save path close the
gap. The report carries the bytes it checked, and where those are not the bytes
on screen no diagnostic is anchored at all — the strip says the check is behind
the buffer, and the panel says the same rather than listing what it found. In
the JSON view the strip prints every diagnostic with its own pointer, because
there are no blocks to distribute them to and a report visible only to whoever
has the Inspector open is a report the page is keeping to itself.

**Try it** runs the draft without saving it. `experimental_evaluate` takes
`pack` as JSON text **XOR** `pack_id`, so the source control sends one or the
other and never both: the tool's `required` list is `["facts"]` alone and the
handler enforces exactly-one-of by hand, so both and neither are each refused
on an argument mistake rather than on anything about the pack. A text pack
never reaches the reviewed set — `applied` is built only where a `pack_id` was
supplied, and the consult is gated on it — so a draft run is `lock.DraftRun`,
never refused for being unlocked and proving nothing about a recorded decision.
The audit writer, though, runs for **every** call including a text pack, and
only `rehearsal: true` suppresses the record: the declaration is sent wherever
the runtime advertises the argument, and where it is not, the pane says the run
would be recorded in a project declaring an audit directory and requires an
explicit second click. The evidence rows come from the **draft's** own
requirements, so one added in the editor appears and one deleted stops sending
a key. The disposition, the reasons and the trace are the existing evaluation
view, verbatim; the foot prints the payload's own `packId` and `packVersion` —
which is the pack document's `id`, a URI, and not the project's decision id.
A preflight refusal is rendered as the runtime's answer, class and phase and
diagnostics, with no disposition anywhere near it; mid-edit that is the
ordinary answer rather than an error. The result is stale the moment the buffer
moves, and says so, and the confirmation for an unadvertised rehearsal
remembers the bytes it confirmed rather than being a flag — the editor beside
the pane is most of what would be sent. The pane sits beside the editor where
the editor keeps 512px, and takes the Inspector's place where it does not. The
**workspace** is what is measured, not the editor column: the column is the
pane's flex sibling, so placing the pane shrinks the box the decision was read
from, and a predicate whose input depends on its own output has no fixed point
across a wide band of ordinary widths.

**Save** is `PUT /api/file {path, content, baseSha256}`, and the base moves
only on load, on an explicit reload, and on a successful save — never on a
watcher refetch, which would silently rebase onto bytes nobody saw and make the
next save overwrite them without the 409 that exists to prevent exactly that.
The read-back is compared to the **submitted** snapshot rather than to the live
buffer, so typing after a save cannot turn a true "verified" into a false "does
not match". **It is never gated on the check**: the chassis writes bytes and
the runtime judges them, in that order, and outstanding diagnostics stay on
screen through the save. A 409 shows both digests behind a `digests`
disclosure — printed short and carried whole, so a reader can compare one
against `sha256sum` — distinguishes `exists` from `stale` from the chassis' own
code, and offers Reload — which says that it discards — and *Overwrite anyway*,
which is never the primary control. On success `list_packs`, `get_pack` and the
`validate` queries are invalidated.

**A save still in flight when the author leaves the pack completes on disk, and
its read-back is dropped with the editor.** The per-save callbacks reach the
route through react-query's observer, and leaving the pack — or a reload landing
first — detaches it: the write happens, and nothing here is moved onto it.
Nothing is retained, because the file on disk is the truth and the next open
re-reads it; what the page owes the author is to say so, and it does — *This save
finished, and this page has no account of it*, with the read that would settle it
beside the sentence. The same line stands where an answer does arrive and the
buffer refuses it, which is a read-back for a file this buffer is no longer about.

**A read or a write that lands over an edit is refused.** Both take as long as
they take, and what comes back is a whole file. The ticket a reload carries names
the file, the incarnation of the buffer *and* the edit revision it was issued at
— a number every commit, undo, discard **and unwritten operand** moves, because
text typed into a field that is not JSON yet is work too (holding it counts once,
where it changes; releasing it does not, because the write that follows a release
is the edit) — so an edit made while
the read was in flight makes the answer stale and the buffer declines it, keeping
both the work and the undo entry that could take it back. The stale-file offer
stays on screen, which is the honest state: the file did move, and this buffer
has moved too; and the offer says what it would cost — *Reload, losing these
changes* — for unwritten text exactly as it does for unsaved bytes.

A **save** carries the same identity, and `landed` refuses a read-back for
another file: a PUT in flight across a navigation used to make one pack's bytes
another pack's base. An edit made *during* a save is not refused — that is the
case the save's own text comparison answers, by keeping the work and leaving it
dirty against the revision that landed. (The counters are JavaScript numbers, so
the claim is bounded and stated as such: a ticket cannot collide within 2^53
edits of one page session.)

**A refusal does not take the page away.** Save writes bytes the runtime may
then refuse to serve, so `get_pack` failing is a state this editor can produce —
and the way out of it is the editor. The refusal is printed above the bytes
rather than in place of them: the file API returns whatever is on disk, the path
comes from the listing where `get_pack` cannot name it, and the JSON view stands
in both modes (read-only until the mode is Edit). Bytes shaped like nothing the
desk expects — `rules` pasted as an object — are read as what they are rather
than taking the route down with the unsaved buffer inside it.

**The lock line.** Where `jpack.lock.json` is in the file listing, one
sentence: the project keeps a reviewed set, and updating it is the project's
own step. Where it is not, silence — not "this project keeps no reviewed set",
which would be a claim about a file that may simply not have been read. No tool
reports lock state, the Evaluation payload carries no lock member, and `packs
lock` is a CLI verb (ADR-0019), so the desk cannot know it and computes none of
it.

**Keyboard.** `Mod+S` saves. It is registered by the editor rather than through
the shell's `installShortcuts` — every shell chord is suppressed inside a text
field, which is exactly where save has to fire — and it is bound for as long as
edit mode is on screen rather than to a subtree: `document.body` is a reachable
resting place for focus, and it is where focus sits the moment edit mode opens,
because the Edit button unmounts itself. The chord is claimed there too, so the
browser's own "Save page as…" never opens over unsaved work. `Alt+ArrowUp` /
`Alt+ArrowDown` move a rule, from the card itself — which is the element the
move focuses, so the chord works twice in a row. Escape does not discard.

What phase 2 does **not** do: add or remove an entry in a list (an outcome, a
requirement, a source, a rule), write `metadata.reviews`, or edit a condition
node kind it has never seen. Each is a line in the JSON view.

### What this page will not say

No desk-computed verdict of any kind: no conformance claim, no lock state, no
health, no pass/fail chip. The runtime judges documents and this page quotes it.
**Nothing about this pack's standing in the reviewed set appears here** — not
whether it is in the set, not whether the entry is current, not whether an edit
would invalidate it. No tool reports any of that, so the desk cannot know it and
must not compute it. The one thing it does say is that the set **exists**, which
is the file listing's own answer and nobody's inference: where the listing
contains `jpack.lock.json`, one line says the project keeps a reviewed set and
that updating it is the project's own step.

The editor adds four more. **No English paraphrase of a condition**: `"5000"`
keeps its quotes and `greater-than` stays the document's word, in the reading
tree and in the builder alike, because "is greater than" is a second,
unversioned statement of the rule. **No re-lock button** — `packs lock` is a
CLI verb (ADR-0019) and the lock line says whose step it is. **No generated row
expectation**, and no claim to call `packs suggest`, which is CLI-only
(ADR-0024). And **no form that refuses a value**: what an author types is
written, and the runtime is what names it.

## Configuration

One optional file in the project root, read through the **existing** file API
like any other project file — no new endpoint, no new proxy entry, and no Go
change.

```json
{
  "deskConfigVersion": 1,
  "organization": { "name": "Acme Co.", "mark": null },
  "user": { "displayName": "local user" },
  "appearance": { "theme": "system", "density": "comfortable" },
  "panes": {
    "left":      { "mode": "expanded", "width": 248 },
    "inspector": { "open": false, "width": 360 },
    "console":   { "open": false, "height": 240 }
  },
  "storage": {
    "packs": {
      "kind": "filesystem",
      "dir": "packs",
      "idBase": "https://example.invalid/judgment-packs/"
    }
  }
}
```

Each pane dimension is bounded, and **zero is refused like anything else**: a
pane the file declares `open` at zero pixels is an open pane nobody can see
with a toggle that appears to do nothing, and an enormous one pushes the strip
out of a frame that does not scroll.

| key | minimum | maximum |
|---|---|---|
| `panes.left.width` | 160 | 640 |
| `panes.inspector.width` | 240 | 720 |
| `panes.console.height` | 80 | 720 |

A value outside its range is refused by name like every other problem, and the
whole file with it. Beside that the sheet caps each pane against the viewport
it is actually in — neither side column past `40vw`, so main keeps at least
20% with both open, and the console no further than leaves 120px of route
under the header and above the strip — because a size that is legal on a
monitor is still able to eat the frame on a phone.

**A cap is not the same as a configured value.** An accepted 720px Inspector
renders 440px at a 1100px window; the Inspector's *drawer* form is 320px unless
the file states a width. Nothing on a settings page reports that any more — the
Panes card is gone, and with it the reader that measured the frame by its ids —
but `useInspectorSlot().size` is still **measured** rather than configured, so a
route laying something out beside the pane is laying it out against the width
the pane actually has, updated as the window is dragged.

**On a viewport too short for the reserve, the routes give way and the console
does not.** An open console never renders below 80px — the smallest height the
schema accepts for one — even where the cap would otherwise reach zero, because
a pane of no height whose toggle still says it is open is a control that lies.
`.desk-main` scrolls; it is the one that can afford to lose the pixels. The
one thing that never gives way is the strip: where the viewport has less room
between the header and the strip than the floor asks for, the console takes all
of it and no more. At a 203px viewport an open console renders 80px with 47px
of route above it; at 109px it renders 33px, which is everything there is. A
*collapsed* console is still exactly 0px, and the strip is its collapsed face
as always.

Every key is optional except `deskConfigVersion`. `organization.name` is a
non-empty string or `null`; `null` is how a file asks for the desk's own name,
and `""` is refused by name rather than rendering a blank brand. `appearance` is
decoded and validated, and it is the **default** rather than the answer: what
this desk paints is the viewer's own preference where they have one, set from
the user menu and held in their browser. `theme` is applied as above and
`density` is applied as above. `organization.mark` is `null`,
an inline `<svg …>` string, or a `data:` URI of at most 65,536 bytes of UTF-8
(measured with `TextEncoder`, not in UTF-16 code units — the two disagree by up
to **three** to one on a mark carrying non-ASCII; three and not four, because a
four-byte astral character costs two UTF-16 units and is therefore only 2:1,
while the three-byte character that costs one unit is the worst case), carried in the JSON
itself and encoded to a `data:` URI in the browser — **never** injected as
markup, and never a file path (the file API refuses non-UTF-8, so it could not
carry a raster image, and no endpoint is being added for a logo). Absent an
organization name, the header reads `judgment‑pack desk` — never an invented
company, and never a name taken from a token claim.

**`storage.packs` is where a new pack goes**, and it is the whole reason the
Create-pack dialog has no path field: the name gives the id, and the id gives
the file name inside `dir`. Every member is optional and takes the default
above. `dir` is project-relative and slash-separated, and is refused here for
the lexical shape the file API would refuse anyway — so Admin names the key that
is wrong rather than the dialog failing later on a path nobody chose to look at.
**`dir` and `idBase` are refused more widely than that, for a control
character**: every code point from `U+0000` to `U+001F` and `U+007F`, at any
position, tested against the value as it was written rather than after any
trimming. That is the decoder's own rule, applied at Save — wider than the
chassis, which refuses `U+0000` in a path outright — because a name carrying
one is a name this desk could never write, and `new URL` will not catch it on
the prefix either: it percent-encodes a `U+0000`, silently *deletes* a tab, and
takes a `U+007F`. The refusals also cover the directories the chassis excludes
from its endpoints altogether (`.git`, `node_modules`, `dist`, `.venv`, `vendor`, and a staging
name): `"dir": "dist"` is a plausible thing to type, and a configuration that
decodes clean while making every create fail is worse than one refused where it
was written. The list is mirrored from `internal/desk/watch.go` and held to it
by a test that reads that file.

`idBase` must parse as a URI, because a pack document's `id` member is
`format: uri`, and it is **normalised at decode** to end in `/` (or left alone
where it ends in `#`), so a pack's id is a plain concatenation everywhere it is
used and Admin shows the prefix that will actually be written.

`kind` admits only `"filesystem"` today, and its refusal names the other two by
name: `"database"` and `"cloud storage"` are **not available yet**. Admin names
them in the decoder's own words, as text rather than as disabled controls, and
**nothing in the desk branches on this member** — a pack is created by writing a file,
always. The create UI never asks which kind is configured.

### The desk-level file

A second optional file, this one on the machine rather than in the project:

```
~/.config/jpack-desk/desk.json      # $XDG_CONFIG_HOME/jpack-desk/desk.json where that is set
```

It is read through its own read-only endpoint, `GET /api/desk-config`, under
the same session and origin guard as everything else — **not** through the file
API, and that is not an inconsistency. The chassis resolves every file-API path
through the project's pinned `os.Root`, which is exactly what stops it reading
anything outside the project; a file in `~/.config` is therefore not
addressable there and never will be. An absent file is answered `200` with
`present: false` and the path it would be at, because "there is none, and it
would be here" is an answer rather than a failure to answer, and Admin needs
the path in order to tell you where to write one.

The same answer carries **what this process was launched with**, in both
states: `project: {dir, file}` — the project root the chassis resolved and
pinned, symlinks already followed, and the project's own `jpack-desk.json`
inside it whether or not one is there — and `runtime: {bin}`. Admin prints all
three and composes none of them: a page joining a directory to a file name
would be asserting a path on a filesystem it cannot see, and would be wrong the
first time a project was reached through a symlink. Neither is in the
configuration schema at any depth, for the reason neither ever was: the chassis
executes the binary it was given.

It takes every key the project file takes, plus the two that may **only**
appear here:

```json
{
  "deskConfigVersion": 1,
  "identity": { "provider": null },
  "assistant": {
    "endpoint": {
      "url": "https://api.example.invalid/v1",
      "kind": "openai-compatible",
      "model": "a-model",
      "tools": [
        "get_schema", "list_examples", "get_example", "validate", "experimental_evaluate"
      ]
    },
    "engine": "vercel",
    "thinking": "off"
  }
}
```

**`project.file` says which project this machine's desk opens** when
`jpack-desk` is launched with no directory argument. It is an absolute path
naming a `jpack-desk.json`, and the desk opens the directory that file is in;
`null` or absent means it names none. It is desk-level only — which project a
machine opens by default is not a fact about any one project, and committing one
would push one operator's filesystem onto every clone.

```json
{
  "deskConfigVersion": 1,
  "project": { "file": "/home/someone/a-project/jpack-desk.json" }
}
```

**Writing it is an operator's, and Admin may only nominate the project it is
running in.** The two are different authorities and the difference is the whole
of this member's safety. The desk pins one project root at startup and serves
the file API through it; `project.file` chooses the root of the **next** launch.
So page code that could write any path could hand its successor a root outside
the authority the page itself had: `{"project":{"file":"/jpack-desk.json"}}`
would have pinned `/` on the next argument-less start, and the file API would
then have served the host. That is the key-retarget class, in a member instead
of a credential.

`PUT /api/desk-config` therefore accepts exactly two values for it from the
page: **this project's own file**, spelled as the chassis reports it in the same
answer, which nominates the project the desk is already serving and grants
nothing it does not already have; and **null**, which withdraws a default and so
takes authority away. Anything else is `422 desk-config-refused` naming
`project.file`, with nothing written. **Both are stated**: `{"project":{}}` is
refused too, because an omission is not a withdrawal — a client that meant
nothing by leaving the member out would otherwise clear whatever an operator
had written. And the accepted value is compared **exactly**, with no
normalisation: a padded or otherwise re-spelled path is a second rule about
which spellings mean the one value a page may write, and it is the re-spelling
that would get stored.

Admin's Project card is that rule as a shape rather than as a validation on top
of one: one button, **Use this project as the default** (or **Clear the
default** where it already is), and no field for a path. A different default is set by editing the desk-level file yourself,
which is custody-validated and is the operator's own authority.

**A configured default is validated on the host that is about to act on it**,
and refuses the launch where it fails. The decoder's rule is lexical, because it
is shared with a browser that has no filesystem to ask — a leading separator, or
a drive letter with one — and a spelling that is absolute on one platform is a
*relative path* on another: `C:\p\jpack-desk.json` is one path component to Go
on Unix, so `filepath.Dir` answers `.` and the desk would open whatever
directory it happened to be launched from. So the launch checks the value again,
against this machine. The whole refusal matrix, in the order it is applied:

| the configured `project.file` | refused because |
|---|---|
| is not absolute on this host | a path written for another platform is a relative one here, and would open whatever directory the desk was launched from |
| is directly in the filesystem root | this desk will not serve a project rooted there — checked before anything is asked of the filesystem, so it is refused for *where it is* rather than for not existing |
| does not resolve | a default that is not there is not a project; `EvalSymlinks` is what says so |
| does not resolve to a regular file | a directory, a socket or a device is not a configuration file |
| resolves to some other name | a link cannot point the name this desk reads at something else |
| resolves into the filesystem root | the root check again, on what the link actually reached |

A default that fails any of those **refuses the launch by name** — never a
silent fall back — because somebody who configured a default and got some other
project would have no way to see that the member they wrote was ignored.

**And what is validated is what is served.** The directory that passes is then
**pinned as a held descriptor**, and the identities of the directory and of the
`jpack-desk.json` that chose it are compared against that descriptor before
anything is served: validating a pathname and then resolving it again to open
is a window in which a rename can substitute another tree for the one that was
checked. It is the pattern the credential directory is already held to.

**Two consumers cannot go through `os.Root` at all**, and they are why that
sentence needs a second paragraph rather than a footnote. A subprocess's working
directory is set by the kernel with `chdir`, and an inotify watch is taken by
path; neither accepts a `*os.Root`. Both used to be given the resolved
*spelling*, so a rename-and-replace at that spelling left every new `jpack mcp`
judging one tree while the file API edited another.

- **On Linux, both follow the descriptor.** `/proc/self/fd/N` on this desk's own
  pinned directory resolves to the open file description rather than to a name,
  so it means that directory however it is called afterwards, or whether it is
  called anything at all. The watcher is given that path directly.
- **The runtime gets there through a shell trampoline**, so that nothing rests
  on an `os/exec` internal. The descriptor is passed in `ExtraFiles`, which is
  documented to make it descriptor **3** in the child, and the child runs
  `sh -c 'cd /proc/self/fd/3/. && exec 3<&- && exec "$0" "$@"' <jpack> mcp`: the
  `cd` resolves through the inherited open description at a number this desk
  knows rather than one it inferred, `exec 3<&-` closes the descriptor before
  the runtime is executed — so the runtime inherits a working directory and not
  a capability — and the final `exec` leaves no extra process in the tree. The
  one cost is a dependency on a POSIX `sh`, and a host without one is refused by
  name at the spawn rather than failing somewhere a reader cannot see.
- **On every other host it is check-then-use, and the desk says so rather than
  implying otherwise.** There is no portable way to hand a subprocess a working
  directory by descriptor, so the pathname is re-verified by identity
  immediately before each spawn and a moved project **refuses the relay** rather
  than starting a runtime somewhere else. The window between that check and the
  child's `chdir` is not closed by it. Only Linux is race-free here, and Linux
  is the only host on which this desk keeps a key at all.

**The paths `GET /api/desk-config` reports are informational.** `project.dir`,
`project.file` and `runtime.bin` are the spellings captured at launch and are
never re-read, so a rename cannot make the desk report something new — and the
nomination rule compares against that same captured spelling, so a pathname that
has stopped naming the pinned root cannot authorise anything either. What a page
could persist through it is at most the string this desk already told it. And a
principal who can rename the project directory out from under a running desk
already holds more than the page does: what this section promises is that the
desk's own halves do not come apart, not that such a principal is harmless.

**Precedence**: project file → desk-level file → built-in default, and for the
three pane flags one layer in front of all three — this browser's record of
what the viewer chose, for the panes they chose it for. The project's own file
wins because it is the more specific statement: the desk-level file is this
machine's answer for every project it opens, and a project that says something
different is saying it about itself. Two sections do not take part in that
order at all, because they exist in only one of the files: `identity` and
`assistant` are refused by name in a project file, so their only source is the
desk-level one. There are no shell flags on the command line.

**Each file is refused on its own.** A bad key in one does not refuse the
other, neither is repaired by the other being fine, and Admin reports each as
its own with its own path. The status strip's `configuration refused` cue
fires for either, because the argument the cue exists for — that a mistyped
key must not look exactly like having written no file — does not care which
file carried the typo.

**Any problem refuses the whole file**, and every refusal names its key.
Partial acceptance would let a typo'd key sit there doing nothing while its
siblings applied, which reads as a setting that does not work rather than a
spelling that is wrong. An unknown key — at the top level or nested — is refused
by name. `identity` in the *project* file is refused by name with its own
reason: a project is a shared checkout, and committing an issuer would push one
operator's directory onto every clone. **There is no `clientSecret` key in the
schema at all**, so one pasted in is refused by name rather than silently
persisted. A missing file is the defaults, with no banner and no error — a
*refused* file is the defaults too, and the two are told apart on the status
strip, which reads `configuration refused — see Admin` and links to the page
that names every problem. Without that cue a mistyped key looked exactly like
having written no file at all from every surface except `/admin`.

A third state sits beside those two and gets its own cue: a file that **could
not be read**. A 404 is an absent file and stays silent; a 413, a permission
refusal or a non-UTF-8 body is a configuration that exists and was not
honoured; and a socket that never answered establishes only that **absence was
not established** — it is not evidence that the file is there. The strip reads
`configuration could not be read — see Admin` for all of them, and Admin
separates the two provenances: where the chassis answered, its status is named
and its reason is quoted as the chassis'; where nothing answered, the reason
shown is the browser's own and is said to be. Reporting any of this as the
defaults, silently, is a desk describing itself as unconfigured when it is
merely unread.

On a phone the strip paints a short spelling of that cue — `config refused`,
`config unread` — because the full sentence is wider than a 320px strip has
left beside the console button, and a link that neither shrinks nor wraps
painted straight across it. The link's accessible name is the full sentence at
every width.

### Admin, as an overview and one open section

**A settings page carries only settings.** That sentence is the whole of this
shape, and three things follow from it. Every setting is editable in place.
Status is not a setting and lives elsewhere. And a per-viewer preference is not
an administrator's setting either.

**Under the heading is a status line, not a card.** One line — two on a narrow
shell — carrying the connection and the binary the chassis was launched with,
both of them the connection's or the chassis' own answer and neither composed on
the page. **The connection's verdict is its `status`, never the runtime it last
met**: `server` is retained across a reconnect, so every surface that read
"connected" off its presence said so while the socket was down and the banner
said the connection was lost. One producer — `connectionSays` — answers for the
status line, Help & About and the status strip; the runtime is named only where
the connection is actually up. It replaced a **Runtime** card whose four slots held nothing anybody
could edit; the card's own content is in **Help & About**, which is where a
reader goes to ask what they are connected to.

**Neither configuration file is on that line**, and the omission is the point:
each group below names its own, and naming a file twice is what the grouping
exists to stop. The line is what the desk is *running*, which is the one thing
no card is about.

**`/admin` is an overview**: two groups, one per file, each stating its file's
path and read status once, and under each a row per section carrying that
section's title, a one-line summary drawn from the decoded configuration, and
its own status only where it differs from its group's — the comparison being by
what the status *says* rather than by which state it is, because two refusals
naming two keys are not one status.

| Group | Header states | Rows |
|---|---|---|
| **This project** | `jpack-desk.json`'s path, its read status, and the default-project nomination | Organization, Storage |
| **This desk** | the desk-level file's path and its read status | Assistant, Identity provider |

**A row is a link to `/admin#<id>` and the fragment is the state**: the section
it names opens beside the list — a 14rem column of rows on a wide shell, the
list stacked above the section below the Inspector's own breakpoint with every
row the reader is not in collapsed to its title — `aria-current` marks the open
row, Escape or the **All settings** link at the top of the open section clears
the fragment, and a fragment naming no section is the overview. Those addresses
are the ones the rail's menu and the user menu have linked to since these were
headings; nothing else changed about them, and they scroll as they did.

**The summaries come from the decoded configuration and are composed nowhere
else**: the organization's name or `none`, `filesystem · <dir>`, `none` or
`<kind> · <model> · thinking <tier>`, `None` or the issuer. A row that fell back
to a name this page made up — the project it happens to be open on, the desk's
own brand — would be a value nobody wrote that a reader cannot tell from one
that is in the file.

**The page was a stack and is now a list, and that is the whole of the change.**
Four sections each with a heading, a status, a disclosure and a form, one under
another, meant a reader who came to change one thing scrolled past three they
did not — and the page grew by a screen with every setting the desk gains. What
a settings page is instead is an overview of what each setting currently *is*,
and the one section that was asked for.

**A section's Status is its read state *and* its own write.** The group's status
is the file's read state, so a section that only ever reported its read state
showed nothing at all while it was writing, while a refusal stood against it, or
while the file had moved underneath it — three things the section knows and the
group does not. The form publishes what its write is doing up to the card it is
inside, on the pattern the Inspector slot already uses: the form owns the draft
and the save, the `save` node is handed to the card as a prop, and neither can
reach into the other. A write in the air says so first, then a file that moved,
then any other refusal in whoever's words refused it; a save that **landed**
publishes nothing, because the file was read back and the read state is the
truth again. A member the *other* file supplied is not one the
group's header speaks for: it is given no group, and states its own Location and
Status exactly as it did before there were groups. The heading levels follow the
document — a group is an `h2` and its cards are `h3` — so the outline is the one
on the screen.

**The file itself is in the right pane.** The bytes are context rather than a
setting — nobody edits a file's text here — so Admin claims the Inspector slot
the way the pack routes do, through `useInspectorPortal`, and releases it when
the route leaves, which is what puts the next route's panel back. The panel names
the file and the member (`jpack-desk.json › storage`, `desk.json › identity`, or
the project file alone on the overview), states the **Location** the chassis
reported and the **Digest** the read carried, says the file's **Status** in the
same closed vocabulary the sections use, and quotes the member's **own bytes** —
never a re-serialisation of the decode, and where those bytes cannot be
established, one line saying so rather than the decoded value in their place.
Below 1100px the pane is the existing drawer, opened from the existing control;
nothing here opens it on the reader's behalf, because a restored layout is not a
page's to override.

**The forms stay in the main column**, and that is a measurement rather than a
preference: the pane is 360px, a label column alone is 9rem, and below 1100px
the pane is a drawer over the very page it would be editing. Each open section
is what it was — its **fields**, and a **Save** where it has a write path and
nothing where it does not. A **Location** is still the path from the chassis,
never composed on the page and never stood in for; where the chassis has not
answered it says *the desk has not said*, because the relative name this page
reads the file by is a file-API address rather than an established location on a
filesystem. A **Status** is still one line from a closed set: `read`, `not
present — defaults in use`, `refused: <key>: <the decoder's own reason>`, `not
read — <who said so>: <their reason>`.

**Nothing on the page is a box.** Hierarchy is type, spacing and hairlines: one
rule between groups, one above each row, one type scale, and every title in
sentence case — a frame is drawn only around an object, which on Admin is the
code block in the right pane and an alert. **One primary button per
section, and it is the Save that writes the form**; the default-project
nomination sits on the row it changes as a secondary, and a Reload inside a
stale panel keeps its own because an alert has one action. Three tests hold it
— the sheets carry no `background` and no four-sided `border` on a container,
`styles.css` resets `fieldset`, and every button under the article comes out
carrying `Button`'s class.

**Panes is gone from Admin, and the reset went to the shell.** Its three pane
dimensions were a settings page editing the frame it is drawn in; its reset
cleared a record in this browser's own storage, which is a per-viewer
convenience rather than a deployment's configuration. `Reset panes` is now in
the user menu — see [Shell](#shell) — and the `panes` member is still in the
schema, still decoded, still validated and still applied. The write path left
with the form: `CARD_POINTERS` names the **2** members a card may write —
`/organization` and `/storage` — and a test drives every Save on the page and
compares what came off the wire to it. Appearance left the same way and for its
own reason; see [Shell](#shell). That count is read out of the source by a test,
so this sentence cannot drift from the list the way it did when the list was
three. It is a declaration and **not a type constraint**, deliberately:
narrowing the save hook to it would make routing a Save through `/panes` a
compile error, and the only mutation left would break the expectation the test
compares against — a comparison against itself, which proves nothing about the
write path. The guarantee is behavioural, and the rows that hold it point a real
Admin Save at `/panes` and at `/appearance`.

**`storage.packs.kind` is a value, not a control.** The union has one member, so
a `Select` there would look like a choice, read like one to every enumeration of
what a reader can change, and offer none. The card states what the file says,
with the decoder's own sentence about the two kinds that are not available yet
under it; the refusal of any other kind is unchanged. One code comment says what
brings the control back, and it is a second kind existing.

There is no narration. A test sweeps every text node the page writes and fails
on one over 140 characters, exempting quoted material — a path, a decoder's own
refusal, a member of the file as it is written. **The sweep runs over the states
the configuration cannot express, too**: a connection opening, being retried and
failed; a tool listing that did not answer; a save in the air with a refusal
beside it; a stale write with its digests disclosed. And over the panes' reset
in the user menu, which is a portal outside the page, for each of its four
answers — the two standing sentences in that menu are over the bound on purpose
and the sweep is asserted against exactly them, so a third one fails. The standing disclaimer, the
deployment-state list, the warning notes and every paste block are gone: a real
problem is a section's Status line, and the Copy buttons went with the blocks
because the Location line says where the file is and the pane shows what is in
it.

**A refused file's bytes are never rendered.** The decoder refuses a whole file
for one credential-shaped member, and the point of refusing it is that the desk
will not act on it — so a pane that quoted it anyway would put the member the
refusal is about, and on the overview the whole document around it, into the DOM
of the surface reporting the refusal. On a refusal, and on a read that produced
no file, the pane shows its Status line and no bytes at all. **The rule has one
spelling**: `showsContent` is exported from the card and imported by the pane,
because two spellings of one rule are invisible to a harness that breaks one of
them — which is exactly how a second, redundant gate inside the old disclosure
survived as a mutation nothing could catch.

**Two things write the desk-level file, and each writes one member of it.** The
project group's header nominates this project as the default (or withdraws one),
the Assistant form writes `assistant`, and neither sends the other's — a member
absent from the request is carried across untouched. That header's other slots
are about the *project's* file, so the one line under its control names the file
it actually writes, from the chassis' own answer.

**Storage and Organization each Save one member of `jpack-desk.json` through
the file API**, by splicing that member's own bytes
and decoding the whole file before any of it is sent — so every other member
keeps its bytes, order and whitespace, and a value this desk would then refuse
to read never reaches the disk. **The write states the digest its read carried
and asks for no override**: a file that moved underneath the card is a `409`
with nothing written, and Reload reads it again while keeping every value that
was typed. The bytes and that digest are one revision, **held** rather than
read live — the chassis invalidates every query when it sees this file change,
and a card that followed would rebase onto bytes nobody saw and overwrite them
with no refusal at all — so it moves on an arrival while nothing is unsaved, on
Reload, and on a save that landed, and nowhere else. A card writes only the
fields that differ from what the file supplies, so a member nobody touched stays
undeclared — and where a value comes from the desk-level file,
which this page does not write, the card says so and offers no Save.

**Two things are written, and each is exactly as wide as its reason.** The key
is one, below. The other is the desk-level file, over `PUT /api/desk-config`,
under the same session and origin guard as everything else. Its body is
`{assistant?, project?, ifMatch}`: **a member that is present is replaced and a
member that is absent is untouched**, which is what lets two Admin cards write
two members of one file without either sending the other's. A body naming
neither is a `400` — a conditional commit that would change nothing is a request
with no meaning, and answering it `200` would report a write that did not
happen. **`ifMatch` is required and never defaulted**: it is a pointer, so an
omitted member and the empty sentinel are two different requests. They were one,
and where `desk.json` is absent the actual digest is the empty string too — so a
body carrying no `ifMatch` compared equal and created the file, which is a write
with no precondition from the route whose whole argument is that the commit is
conditional. `""` is a claim about the disk and has to be made. It exists because choosing a model and a thinking tier is something an
author does while working, and the alternative is telling them to edit a file
in `~/.config` by hand between attempts. Four things bound it:

- **The request names no file.** There is no path in the body: the chassis
  writes the one file on that machine, through the same pinned custody
  directory the key is written through — validated once at startup, and every
  operation through the descriptor rather than a pathname.
- **It is a conditional commit.** The page sends `ifMatch`, the digest of the
  bytes it last read — `GET /api/desk-config` now answers that digest beside
  the content, and the empty string means "there is no file". A file that moved
  underneath the page is `409 desk-config-changed`, with both digests, and
  **nothing is written**. There is no `override`, unlike the file API: this file
  names the endpoint a credential is presented to, and "write anyway" is not a
  choice a page should be able to make about it.
- **The chassis composes the bytes and decodes them before any of them reach
  the disk.** Not the object the page sent — the *file* this desk would store —
  through the whole-file decoder the browser shares. A key-shaped member, an
  unknown kind, a missing `tools`: each is `422 desk-config-refused` carrying
  the decoder's own problems key by key, with nothing written. So a page cannot
  store a configuration Admin would then report as refused, and the credential
  scan applies to a write exactly as it applies to a file somebody typed.
- **Every other member survives.** `identity`, `deskConfigVersion` and anything
  else present are carried across **by their own bytes, in their own order** —
  copied out of the file verbatim, whitespace included, and never re-serialised
  or reflowed, so `1e2` does not silently become `100` and a member's place and
  shape in a file somebody wrote stay theirs. Only the members this route was
  **asked about** — `assistant`, `project`, or both — are rendered, and each is
  replaced where the file already has it and appended where it does not. A file
  with a **duplicate top-level
  member** is refused rather than composed over (`422`, naming the member):
  `encoding/json` keeps the last value and a reader in another language may
  keep the first, so a rewrite would silently choose one. The file is written
  owner-only (`0600`) by staging, `fsync`, and rename inside the same
  directory; a `0644` file is still *read*, because a checkout or an editor
  leaves one, but this desk publishes its own writes at the mode it chose.
- **The bytes are checked as bytes.** The request body must be UTF-8 and
  exactly one JSON object with nothing behind it, and the composed file must be
  UTF-8 and within the same bound every read applies — each refused before
  anything is staged. Go's JSON decoder replaces an invalid byte inside a
  string while decoding and `json.RawMessage` keeps the original, so without
  the first of those a `0xff` in a model name decoded clean and would have been
  written into a file every later read then refuses. A file this route writes
  and this desk cannot read is worse than a write refused.
- **The digest is compared twice**: once against the bytes this transaction
  read, and again after the new bytes are staged and immediately before the
  rename that publishes them. The second is what covers an ordinary editor,
  which takes none of this desk's locks — without it a write that landed
  between the two was overwritten and the route reported success. **The
  residual is the rename itself**: a writer whose own write lands between that
  second check and the rename still loses, and no compare-and-swap on a POSIX
  rename exists to close it.

The answer carries the new digest and the **decoded** slot — read back off the
disk rather than echoed, defaults applied — so the page can verify what landed
and has the digest its next write needs.

**Admin › Assistant is what calls it.** The section is a form — the wire
protocol, the endpoint, the five tool grants, the model, the engine and the
tier — and Save is this route. Three things bound what the page may get wrong,
and each is held by a test that reads the request on the wire: the digest it
sends is the one its read carried, and a read that produced none disables Save
and says why; the object is composed by **naming its members**, never by
spreading the form's draft, so a member nobody declared cannot reach this file;
and a refusal is rendered in the decoder's own sentences, against the field
each problem's key path names. A 409 keeps every value that was typed and
offers Reload, which reads the file again so the next Save states a digest that
is true — there is no overwrite, because this route offers none.

**The key is the other**, on Admin › Assistant, and the exception is exactly as
wide as its reason. A key must never be pasted into a
project file — a project is a shared checkout, and a key committed to one is a
key published to every clone — so it cannot go through the file API, which
writes only inside the project, and it is not in the configuration schema at
any depth. It gets its own endpoint instead. Everything else in the desk-level
file — `identity` and every section the project file also takes — stays a value
you write in a file yourself; the two exceptions above are a key, which cannot
live in a file at all, and the `assistant` object, which a chassis route
rewrites in place under the four bounds listed there.

That is a claim about **Admin**, and it is deliberately not the broader one it
used to make. `jpack-desk.json` is an ordinary project file — the desk reads it
through the same `GET /api/file` every other file goes through, and now writes
one member of it through the same `PUT /api/file` — so the generic Author
editor lists it and can write it exactly like any project file. Saying "nothing
is ever PUT to a configuration file" was a sentence this repository's own file
API refutes. What is true is narrower and is the whole of it: **a configuration
surface writes one member of one file per Save, states the digest it read, and
decodes the bytes before it sends them** — the chassis composing and decoding
for the desk-level file, and the page splicing and decoding for the project's
own. The editor that will write either of them whole is the one that treats a
file as bytes and forms no opinion about what they mean. `runtime.jpackBin` and
`project.dir` are not in the schema at all: the chassis executes the binary it
was given, so a config-supplied path would be a way to run code on this machine
by editing a file.

**Identity is display, never a gate.** `identity.provider` is one nullable
field — null, or an object. There is no `kind`, no vendor string and no third
shape, and that absence is what stops an issuer someone else operates from
acquiring anything an issuer you run yourself lacks. Configuring one changes
what the header shows and nothing about who may reach the desk.

**A key is refused by name wherever it is written.** There is no
`clientSecret` and no `apiKey` in this schema at any depth, and a member whose
name looks like a credential — `key`, `secret`, `token`, `password`,
`credential`, `bearer`, `authorization`, in any casing — refuses the whole file
with a sentence saying that keys are never stored in configuration and where
the one key this desk holds goes instead. That is deliberately not the "unknown
key" refusal every other misspelling gets: whoever pasted a key into a
configuration file has made a mistake about *where keys live*, and a refusal
that only says the spelling is wrong invites them to go looking for the right
spelling.

### The assistant slot

`assistant.endpoint` is one nullable field — null, or an object — on the same
pattern as `identity.provider` and for the same reason. Beside it are two
settings that say **how** an assistant runs rather than whether there is one:
`assistant.engine` and `assistant.thinking`. There are three **deployment
states** and they are not three shapes:

| | |
| --- | --- |
| **None** | The default. `endpoint` is null, no key is asked for, and nothing renders an assistant. The runtime's authoring prompts still run in any chat client you already use. |
| **Bring your own** | An OpenAI-compatible, Anthropic or Gemini endpoint you already have. The desk stores the endpoint, keeps the key on this machine, and has no relationship with whoever issued it. |
| **Supplied** | An endpoint someone else operates for you. Configured in exactly the four fields above — **an ordinary endpoint, the same code path**, nothing it can do that yours cannot. |

There is no `vendor`, no `operator`, no `mode` and no third shape, because the
last two rows are the same object with a different URL in it. The one member
that does branch is `kind`, and it names the endpoint's **wire protocol**
rather than who runs it: each protocol puts the credential in a different
header and the call on a different path, so no single request could satisfy
them. There are three:

| `kind` | credential header | what the probe asks for |
| --- | --- | --- |
| `openai-compatible` | `Authorization: Bearer <key>` | `GET <base>/models` |
| `anthropic` | `x-api-key: <key>` | `POST <base>/v1/messages`, one output token |
| `gemini` | `x-goog-api-key: <key>` | `GET <base>/v1beta/models?pageSize=1` |

`gemini` is Google's **native** Gemini API and deliberately not that vendor's
OpenAI-compatibility layer, because three things exist only on the native wire
and the assistant needs all three: thought parts, thought signatures carried
back across tool turns, and an explicit thinking budget. The base is whatever
the endpoint documents — `https://generativelanguage.googleapis.com` for the
service Google runs — and its reference is
[the models list](https://ai.google.dev/api/models#method:-models.list) and
[generating content](https://ai.google.dev/api/generate-content). That API also
accepts its key as a `?key=` query parameter, and **this desk never uses it**:
a credential in a URL is a credential in a log, a `Referer` and a proxy's
access record, which is the same rule that refuses userinfo in a configured
URL. Every row of that table is a header.

Nothing in the desk reads the host, compares it to a list, or behaves
differently for one endpoint than another — which an enforcement test holds in
place by enumerating every host comparison in the source and requiring each to
be a loopback name. A proxy or a self-hosted endpoint speaking any of those
three wires is that `kind`, at its own URL.

`url` must be an `https:` URL, or an `http:` one on `localhost` or
`127.0.0.1` — a rule about transport, because a bearer credential sent in clear
text over a network is a credential given away, and one about transport only.
It may **not** carry a user, a password or a fragment: a key is never written
into configuration, and that includes into a URL. It **may** carry a query
string, because some gateways route on one — and that query string is never
logged. **The query is held to a rule of its own**, in both decoders, because
`PUT /api/desk-config` makes the file page-writable and the configured query is
the one part of a relayed request that then travels upstream byte for byte on
every later call. Three refusals, each named against `assistant.endpoint.url`:
a **credential-shaped name** by the same reading a member name gets (`key`,
`apiKey`, `api_key`, `access_token`, `secret`, `password`, … and `auth`, which
that reading does not otherwise catch); a **name the relay reserves** (`alt`,
which the relay may add itself, and `pageToken`, which would page a listing
this desk documents as first-page-only) — reserved on *every* kind, because a
per-kind rule would make a URL legal until somebody changed `kind` beside it;
and a **semicolon anywhere in it**, which is the relay's own rule verbatim. The
reserved names are compared **without regard to case** — `?ALT=sse` would
otherwise be accepted and the relay would add its own pair beside it, which is
two copies of one name to an upstream that folds case — and **every pair's name
and value must decode to valid UTF-8**, because `%FF` is one byte and no error
to Go's decoder and an exception to the browser's, and a configuration the
browser refuses must not be one this desk sends a key on. An ordinary
`?route=eu&api-version=2024-10-21` is accepted and unchanged, and so is any
percent escape both sides read the same way. An escaped path is carried through exactly as configured: `%2F` stays
one segment, because re-encoding it into a separator would send the credential
to a different resource than the one written down.
It is the base the endpoint documents for its own protocol: for
`openai-compatible` the base carrying `/models` and `/chat/completions`, which
usually ends in `/v1`; for `anthropic` the base carrying `/v1/messages`; for
`gemini` the base carrying `/v1beta/models`, which is the origin alone. The
desk appends the path its protocol prescribes and never guesses a version
segment.

`tools` is required, is validated against a closed list — `get_schema`,
`list_examples`, `get_example`, `validate`, `experimental_evaluate` — and
refuses anything else by name. It is required rather than defaulted because a
defaulted tool list is a capability granted by a file that never mentioned it;
`[]` is accepted and means an assistant that may call nothing. Every one of the
five is a **read**: four questions put to the runtime and a rehearsal, which
consults no reviewed set and decides no outcome. `list_examples` is on the list
because the runtime's own `author_pack` prompt tells the model to call it — a
list without it grants a capability the prompt then asks for and cannot have.
The list is mirrored in
`internal/desk/assistant.go` and held to it by a test that reads that file,
because both sides refuse by it.

`engine` names the loop that runs the assistant, and `thinking` the depth it
runs the model's reasoning at:

```json
{ "assistant": { "endpoint": { }, "engine": "vercel", "thinking": "off" } }
```

Both are optional, both default — `vercel` and `off` — and both are allowed
with `endpoint: null`, because they describe how an assistant would run and a
desk that has configured none may still have an opinion about that. Each is one
string from a closed list, on the identity slot's precedent: no discriminator,
no vendor, and a value outside the list refused **by name** (`assistant.engine`,
`assistant.thinking`) rather than ignored — a setting that appears to grant
something is a grant to whoever wrote it.

`engine` admits `vercel`, the default, and `builtin`, a fallback that adds
nothing to what this desk already ships. The slot exists so that the desk's
promises — propose-only, rehearsal-only, the tool allow-list, key custody — are
held *below* whatever runs the loop, and so that an engine ships only once it
has passed the desk's own conformance session
([ADR-0001](docs/adr/0001-make-the-assistant-engine-a-slot.md)). `thinking`
admits `off`, `on` and `ultra`; the two states it cannot express — a model that
always thinks, and an endpoint that offers no thinking at all — are the desk's
to report when it meets them rather than settings anyone selects.

Both are acted on now: the page loads the named engine's chunk and runs it —
and **this build certifies both ids**, so the tab's status line names the engine
the file asked for and nothing is ever substituted — and a tier other than `off`
puts real parameters on every request and runs the refutation pass below.

**What the slot has shipped so far**, in ADR-0001's own order: the slot and the
key custody; the Assistant tab, with propose and accept-into-draft; the engine
slot itself, with `vercel` and `builtin` both certified against the conformance
session; **Describe it** in the Create dialog, which runs the same session with
no draft and hands what comes back to Create rather than to a diff; the thinking
tier with its refutation pass; the native Gemini wire on **both** engines —
function declarations, thought summaries streamed into the tab as reasoning,
thought signatures carried back verbatim across tool turns, and the tier mapped
to Gemini's own thinking configuration — certified by the same session that
certifies the other two families; and the **Admin form** below, which chooses
the endpoint, the model and the tier and writes them.

**Admin › Assistant is a form**, and the paste block that stood here is gone —
it existed because the page could not write the file, and a second way to do
one thing where the second is hand-editing a file this desk also rewrites is
worse than either alone.

| the form asks for | and it is |
| --- | --- |
| **Wire protocol** | one of the three, by name. Choosing one offers the base that protocol's own reference documents — `https://api.openai.com/v1`, `https://api.anthropic.com`, `https://generativelanguage.googleapis.com` — as a **default in an editable field**, replaced by typing over it. Nothing reads those back, compares an endpoint to them, or treats an endpoint at one of them differently: the enforcement guard admits the three literals only as values of one table in one module, and the sharper guard beside it — every host comparison in the page's source is a loopback name — is untouched. |
| **Endpoint** | the base, held to the transport rule and the configured-query rule **by the decoder's own function**, so a URL those rules refuse is shown refused in the sentence the file's reader would write and is not sent. |
| **Tools it may call** | the five, as five checkboxes. All on for a desk that has configured nothing, because `[]` is a real choice — an assistant that may call nothing — and a form opening on it would have a blank field making it. |
| **Model** | typed, with **List models** beside it. |
| **Engine** | `vercel` or `builtin`, with the two things the SDK-backed one cannot do named beside it: it shows the model a tool schema narrowed where the SDK declares one narrower (the tab says `narrowed` when it does, and on the Gemini wire `list_examples` arrives with no parameters at all), and it cannot carry an empty signed thought part back across a tool turn on that wire. Both are measured in this repository's own suite, and both are why an author might choose `builtin` for a Gemini endpoint. |
| **Thinking** | `off`, `on` or `ultra`, with **what that tier puts on this protocol's wire read off `thinking.ts` itself** rather than restated beside it — so the line changes when the table does. On the Gemini rows it also says that the two budgets are this desk's choice inside a documented field. |

**Remove endpoint** writes the other state the slot has: `endpoint: null`,
through the same conditional commit and with the same digest. Until it existed
the form could not reach it — clearing the boxes sends an object the decoder
refuses — so a desk that had configured an endpoint could get back to **None**
only through the generic file editor, while this page described None as one of
three deployment states. It confirms in one line first, and the line is about
the key rather than the endpoint: the key stays on this machine, still entered
for the endpoint being removed, and this desk will not present it anywhere.
`engine` and `thinking` survive, because they say *how* an assistant would run
and not whether there is one.

**Save** writes the `assistant` object over `PUT /api/desk-config` and nothing
else in the file moves. The status line on the Assistant tab and **Describe
it** name the new model and tier at once, and **from the write's own answer**:
it carries the slot the chassis read back off the disk and the digest the next
write states, both of which are set into the cached configuration before the
re-read is asked for. Leaving them to that second read meant a write which
landed while the read hung left every one of those surfaces describing the
endpoint that had just been replaced, under a form saying "Saved". The re-read
still happens, for the parts a write cannot speak about — the project's own
file, every other section's badge — and the key is re-read with it, because a
write can move the binding in either direction. A read that *answers* and
refuses is newer information about the same file than the write's answer, and
this desk says nothing about a file it could not read rather than describing
one from memory: Admin reports it and Save is refused until it can be read.
**And "nothing" is its own state rather than the defaults.** The slot every
consumer reads has a third value — `unavailable` — because falling through to
the built-in defaults told every surface that *no assistant is configured*,
which is an absence this page had not established about a file it could not
open. **All three surfaces read it**: the tab and Describe it say what happened
and offer no control, and **Admin** — where a reader goes to find out why —
says the configuration could not be read, claims no absence, and shows its
fields disabled, because they are the built-in defaults there and typing into
them would compose a write over a file nobody has seen. Nothing latches: a
later read that works puts the configured form back. The key row is unaffected
throughout, because it reads the chassis' own `configuredOrigin`,
`configuredKind` and `bound` and never the page's copy of the configuration —
a row that read the page first said "save an endpoint first" while a perfectly
good key read beside it named the endpoint.
The test drives the real provider over a stubbed file rather than a fixture,
because a fixture would hold the mechanism constant and prove nothing about
it.

**List models** reads the endpoint's own listing through the relay by naming a
path suffix — `models`, `v1/models`, `v1beta/models` — and fills a picker. Two
things gate it and one of them is new: the stored key must be bound to the
endpoint that is *saved*, because the relay refuses a credential entered for
another destination before opening a socket; and the form on screen must **be**
that endpoint. The family and the suffix used to come off the editable draft
while the gate came off the file, so choosing Gemini without saving sent
`v1beta/models` to a still-saved OpenAI-compatible endpoint — a request the page
composed for one destination and the desk sent to another. The endpoint it asks
about is captured at the click, and the rows are **dropped from state** the
moment the form says a different host or protocol: a picker left standing after
that is a list of models from somewhere else, and rows merely *hidden* came
back when the URL was typed away and back again — an arbitrarily stale listing
with no request behind it. **The field beside it never goes away**: the listing is
first-page-only, an endpoint may refuse to list at all, and a gateway may route
on a name of its own — a picker that was the only way to choose would make
every one of those unconfigurable. **What is saved is the id and never the
label**, which differ on two of the three protocols — and **an id is an option
only if the configuration decoder would take it**, asked of that decoder rather
than re-stated here: a copy of the rule is how a whitespace-only id came to be
offered, saved cleanly into the field, and produced a 422 on the next Save.
Typing the same value still gets the decoder's own sentence against the field,
because the chassis is what decides. A refusal is its status
and one word from the probe's own closed vocabulary, and the body is not read;
an answer that is not JSON gets a fixed sentence, because `JSON.parse` quotes
the text it failed on and that text is the body.

**The key row says which endpoint the key is for**, in five states: not read
yet; **no endpoint**, where the entry field is not offered at all because
storing a key requires one to bind it to; **none stored**, with the field
labelled for the host it would be entered for; **stored and bound**, with
Replace and Remove and no masked box standing beside a working key; and
**stored for somewhere else**, naming both hosts, because a reader has to be
able to see which of the two moved. A write answering `keyRebindRequired`
moves the row at that instant rather than waiting for the key read.

The **Check reachability** button still reports the desk's own probe. The key
and the endpoint stay separate: removing the endpoint does not remove the key
from this machine, so the page says `none — no endpoint configured` and lets
the key line say whether one is still kept here.

**What the assistant is, and is not**, in the sentence the page carries: it
proposes edits to the draft; you accept them; the runtime checks them. It never
saves a file and never decides an outcome.

### The Assistant tab

On a pack's route — reading or `?edit` — the right pane carries two tabs,
**Inspector** and **Assistant**. The Assistant tab renders only where an
endpoint is configured *and* a key is stored on this machine; otherwise it says
in one line where that is configured, because a control that would refuse is
worse than a sentence that explains.

Type what the pack should decide and press **Run**. The desk fetches the
runtime's own `author_pack` prompt over `prompts/get` with what you typed as its
`policy` argument, hands it to the engine, and shows what happens: each tool
call by name, each answer's `isError` and byte count, each guardrail in the
desk's warning colour, and at the end the **proposal** — the diff below, the
document as read-only JSON, the unknowns the assistant declared, and beneath
them the runtime's own checks **quoted whole**: the `validate` report and the
rehearsal evaluation, as the runtime wrote them. A summary of a verdict is a
second verdict, so there is none.

`Escape` stops a session; leaving the route stops it too. A session has two
phases — the desk reading the runtime's prompt, then the engine running — and
Stop ends either. Every session emits exactly one `end`, whichever way it
finishes. Pressing Run again with the text unchanged is a second run, because a
model is not a pure function. Nothing about a session is persisted — coming back
is a new one.

**The session is given the draft it is editing.** The first user message carries
the runtime's prompt and then the bytes in the editor, verbatim and fenced,
under one fixed sentence: *This is the draft being edited; propose the whole
document.* On the reading route the saved document plays the same part. So what
comes back is a whole document, which is what the diff needs, and the tab says
whether it is *an update to the draft it was given* or *a new document* — from
what this desk sent, never from a `kind` the model wrote.

**A run captures its baseline, and the proposal belongs to it.** What the run
records where it starts is the whole of what the proposal is about: which file,
which incarnation of the buffer, and the exact bytes it sent. The diff is
computed against those bytes rather than against the live buffer, so what is on
screen is what Accept would apply; and Accept is offered only while the page
still holds them. An author who kept typing while the model was thinking is told
so — *The draft changed since this proposal was made — run again to propose
against it* — rather than having their sentences replaced by a document the
model never saw. Undo back to those bytes puts the proposal back on offer.

**Fix** runs the runtime's `fix_pack` prompt beside Run, with the diagnostics the
check on this page already produced as its `diagnostics` argument — **the bytes
of the `diagnostics` member, cut out of the runtime's own answer** by the same
scanner the editor splices with. Not a message list, not a count, not a severity
filter, and not a re-serialization of a parse: a re-serialization is this desk's
whitespace and this desk's escaping on a refusal it did not write. Same engine,
same gate, same proposal path, and the draft in the first message as above. It is
offered only where the check reports something to fix and the runtime advertises
the prompt, and the tab says which prompt is running.

**The assistant opens its own MCP connection**, and that costs one more
`jpack mcp` process while the tab is running. The reason is the ToolGate below:
the desk's one client serves the page's own calls — `list_packs`, `get_pack`,
Try it — every one of which is outside the assistant's allow-list, so a gate on
that transport would break the desk. A second socket, gated at the wire, is what
makes "no page code path can bypass it" a structural claim rather than a habit.
The connection lives exactly as long as the session and is closed on stop, on
unmount and on navigation.

### The proposal as a diff, and Accept into draft

The proposal is drawn as **what accepting it would do to the draft**, member by
member: each top-level member added, removed or changed, with the draft's text
and the proposal's beside it, and each array member compared *element by
element* — matched by `id` where the elements carry one, by position where they
do not. Members that did not move collapse to one line with a count.

The comparison is **computed here and never quoted**. The contract's proposal
event carries a document and its unknowns and nothing else; there is no account
of its own work for this desk to repeat, and a model's account of what it
changed is not evidence about a document.

**The proposal is canonicalized once, where the event arrives**, in the run hook
— `JSON.parse(JSON.stringify(x))` — and **frozen all the way down**; the diff,
the rendering, the writability check and the writer read that one snapshot and
never round-trip it again. The reasoning is the ToolGate's: an engine may put any
value on `document`, and a getter or a `toJSON` can answer one thing while the
diff is looking, another while the pane renders and a third while the writer
serializes — three readings are three documents, and the one a person accepted
would be none of them. The freeze closes the same gap one layer out: the snapshot
travels to the pane on the run's event list, and anything holding that event
could otherwise reach into it between the memoised diff and the accept. A
document that cannot be read as JSON data at all — a cycle, a throwing getter, a
value that is not an object — becomes an `error` on the stream and no proposal:
there is nothing to show and nothing to write. That there is exactly one
canonicalization site is swept for in `assistant/enforcement.test.ts`, because a
second round trip added back "for safety" is a second reading.

The caption names the draft it actually compared: on a page whose bytes have
moved since the run began it says *the draft this proposal was given*, because
"the draft on this page" is by then a different document.

Each row is identified by its **kind and its pointer**, not by the pointer alone.
A proposal that replaces one rule with a rule of another id produces two rows
about position 0 — the rule that left, on the draft's pointer, and the one that
arrived, on the proposal's — and one key for both is a warning from React and two
rows a reader cannot tell apart.

An id that names two elements of one array matches nothing, and a keyed element
is never paired positionally: a new rule at index 0 must not be reported as an
edit of whichever rule happened to sit there. Where the draft cannot be read at
all — no bytes, bytes that are not JSON, or bytes this desk and `JSON.parse`
disagree about (a duplicated member) — the diff says so and the whole proposal
is new.

**Accept into draft** applies it through the same span-preserving writer a form
edit uses (`packs/edit/writes.ts`), in exactly one `write` on the editing
session:

- one undo entry, so **Undo takes the whole accept back in one step**;
- every byte the proposal did not move survives — the author's own indentation,
  spacing and member order included — because each change is a splice at a
  pointer and an untouched member is not written at all. ADR-0019 makes a human
  read the diff of a save, and an accept that re-serialized the file would hand
  that human every line of it;
- **nothing is saved.** The on-idle check runs again over the new bytes, the
  toolbar's dirty count moves, the navigation guard covers an accepted-but-
  unsaved draft exactly as it covers typing, and Save is still yours to press;
- a draft with nothing to splice into — bytes that do not scan, or a duplicated
  member — is replaced whole, which is the one case where there are no spans to
  preserve.

Accept is enabled only on `?edit`, with a proposal, once the run has ended, and
only while the page still holds the baseline the run captured. It is disabled
with the reason in its `title` while a run is in flight, while a save or a reload
is in flight, once accepted, once rejected, and where the draft has moved; on the
reading route it is not drawn at all and one line stands in its place — *Open Edit
to accept.* **"Accepted" is a comparison, not a memory**: the pane holds the bytes
the accept produced and says the proposal is in the draft exactly while the draft
is those bytes, so Undo puts it back on offer instead of leaving a control
disabled under a sentence that has stopped being true.

The save gate is asked **again at the instant of the click**: the route claims a
save synchronously and react-query reports it a render later, so a control that
consulted only the rendered value could write into a buffer whose save is already
in the air. A reload is the other direction — the buffer refuses a read that
lands over an edit made while it was in flight (`BufferIdentity.revision`), and
the pane declines to start one it knows is about to be argued with.
**Reject** drops the proposal and keeps the event stream. There is no partial
accept: per-member checkboxes are a later refinement, and this chunk deliberately
does not ship half of one.

### The thinking tier

`assistant.thinking` is `off`, `on` or `ultra`. Set to anything but `off` it
makes whichever engine runs **ask the model to think at that depth**, stream the
model's reasoning into the tab as it arrives, carry thinking blocks back
verbatim across every tool turn, and run the refutation pass below before a
proposal is shown.

**The tier maps to provider parameters in one desk-owned table**, per endpoint
family, and the engine receives the normalized result — it never chooses a
parameter of its own. The table is `web/src/assistant/thinking.ts` and it is the
whole of it:

| family | tier | what goes on the wire |
| --- | --- | --- |
| `openai-compatible` | `on` | `reasoning_effort: "high"` |
| `openai-compatible` | `ultra` | `reasoning_effort: "xhigh"` |
| `anthropic` | `on` | `thinking: {"type":"adaptive"}` with `output_config: {"effort":"high"}` |
| `anthropic` | `ultra` | the same, with `"effort":"xhigh"` |
| `anthropic`, after a 400 | `on` / `ultra` | `thinking: {"type":"enabled","budget_tokens":8000}` / `16000` |
| `gemini` | `on` | `generationConfig.thinkingConfig: {"includeThoughts":true,"thinkingBudget":8192}` |
| `gemini` | `ultra` | the same, with `"thinkingBudget":24576` |
| `gemini`, after a 400 | `on` / `ultra` | `{"includeThoughts":true,"thinkingLevel":"medium"}` / `"high"` |
| `gemini` | `off` | `{"thinkingBudget":0}`, or `{"thinkingLevel":"minimal"}` after a 400 |
| the other two | `off` | **nothing at all** |

`off` is expressed by **omission on two families**, and that is not a shortcut:
Anthropic rejects `{"type":"disabled"}` on the models that always think, and
several OpenAI-compatible endpoints answer 400 to `reasoning_effort: "none"`, so
*send nothing* is the only spelling of off those two accept.

**On the native Gemini wire the opposite is true, and off is a member.** A model
that carries a `thinkingConfig` at all reasons by default there, so sending
nothing asks for thinking by accident — which makes `thinkingBudget: 0` the only
honest spelling of off on that family. The asymmetry is stated rather than
smoothed over, because it is the reason the table is per family at all, and
because it gives *this model always thinks* its first real subject: see the
state table below.

The **budget numbers on the Gemini rows are this desk's choice inside a
documented field, not a range quoted from anywhere.**
`generationConfig.thinkingConfig.thinkingBudget` is an integer token allowance
in the API reference; the allowed range is per model and the reference states no
range that holds across the family, so 8192 and 24576 are simply an ordinary
working depth and a deep one. A model whose range excludes one of them answers
400 naming the member, and the desk falls back to the level spelling and then
degrades — the same path a model with no budget field at all takes.

The depth on the Anthropic family lives in a **sibling** member on current
models and inside the thinking member on 4.5-era ones, and on the Gemini family
it is a token budget on one model generation and a `thinkingLevel` out of
`minimal | low | medium | high` on the next. So each of those two families has
two spellings, the desk tries one and, on a 400 that names a member it actually
sent, **falls back once** to the other. A fallback says nothing in the tab: the
desk asked in the other spelling and the session still thinks.

**Five states, three of them selectable.** The last two are the desk's to report
when it meets them, on every engine, and are deliberately not values anybody can
put in a file:

| state | how the desk reaches it |
| --- | --- |
| `off` | the file said so, or said nothing |
| `on` / `ultra` | the file said so and the endpoint did it |
| **this model always thinks** — from absence | the tier is `off`, the desk asked for no thinking, and reasoning came back anyway on two consecutive answering turns |
| **this model always thinks** — from a refusal | the tier is `off` and the endpoint answered 400 to the member that turns thinking off, **at every spelling the desk knows**. Gemini only, because it is the only family where off is a member at all |
| **unavailable for this endpoint** | a 400 naming a member the desk sent, at every spelling it knows; or two consecutive answering turns carried no reasoning at all; or a thinking signature came back truncated |

The refusal road to *always thinks* runs the dialect fallback **first**, and that
is not a formality: a 400 naming `thinkingBudget` is equally *this model cannot
be turned off* and *this model spells it `thinkingLevel`*, and only trying the
other spelling tells them apart. Reached that way the member comes off — there is
nothing left to ask. Reached from absence nothing is withdrawn: the file said
off, nothing refused it, and every later request goes on saying so.

**The degrade happens once, visibly, and the session completes.** The refused
member is never sent again — the requests carrying a tier parameter are a prefix
of the run, at most one per spelling — one line appears in the tab, and the
scenario runs to its proposal. Whether a 400 is *about the tier* is decided
against a **closed list** of the providers' own documented refusals plus the
member names this desk actually sent; an endpoint's body is matched against that
list and never quoted past it, so a refusal about a document cannot be read as a
refusal of the parameter.

The tab's status line names the tier the file asked for and the state the
session reached: `builtin · a-model · thinking on · unavailable for this
endpoint`. Each reasoning passage is one line in the stream, collapsed with its
character count, and opens on a click. **Reasoning text never reaches the
runtime** — it is for the person reading the tab, and the runtime is asked about
documents.

**Thinking blocks come back complete and unmodified**, which is both signing
wires' own rule — Anthropic's and Google's — and the reason the built-in engine
echoes the model's turn exactly as it received it rather than rebuilding it: a
`redacted_thinking` block survives because nothing filters by block type, a
Gemini `thoughtSignature` survives because the part it sits on is the part that
goes back, and an Anthropic signature split across two `signature_delta` events
is concatenated rather than the last fragment kept.

**On the Gemini wire that means a signed part is never joined to anything**, and
that is Google's own rule rather than a precaution: a signed part is not merged
with an unsigned one and two signed parts are not combined, because a signature
certifies the exact bytes it came with. The pieces of an *unsigned* summary are
still joined — that is what a streamed continuation is — and the joining a
*reader* wants happens separately, in the passage the tab shows, so the wire
keeps the part count, the order, the text and the signatures exactly as they
arrived. A signature rides on one of two parts here: a thought summary that
still has its text, or the **first `functionCall` part** of a turn, which is
where function calling puts it and where later parallel calls do not. The
`vercel` engine cannot make its SDK reassemble one (`vercel/ai#19663`, still
present at `ai@7.0.93` and measured by this repository's own suite), so the desk
**detects** the truncation instead: fragments are ledgered as they arrive — under
`anthropic.signature` on one wire and `google.thoughtSignature` on the other,
from one table — each outgoing body is compared with them, a block whose
signature came back as a fragment is removed rather than sent, and the session
degrades once with the reason. That test is written to go red if the SDK is ever
fixed in silence.

The scripted endpoint refuses a continuation that dropped, truncated or
misplaced a signed part, so every Gemini thinking leg **gates** on the replay
rather than merely reporting it — at all three placements.

### Gemini's schema subset, and what the model is shown

`tools[].functionDeclarations[].parameters` on the native Gemini wire is an
**OpenAPI subset**, not JSON Schema, and an endpoint answers 400 to keywords an
ordinary schema carries. Every one of the runtime's own five declares
`additionalProperties: false`, so on this family the choice is between removing
something and not running at all.

**The ruling: a closed, documented removal list, on this family only.** The
keywords are `$schema`, `$id`, `additionalProperties`, `const`, `examples` and
`patternProperties` — `web/src/assistant/geminiSchema.ts`, applied at every
depth, and applied to nothing else. A property whose *name* happens to be one of
them is left alone, because under `properties` the keys are the author's words
rather than JSON Schema's. Nothing is added, nothing is re-typed, and no value
is changed.

**What that costs, and it is stated rather than glossed:** on this family the
model is shown **the runtime's contract minus exactly those keywords**. Without
`additionalProperties: false` a member nobody declared looks acceptable; without
`const` a fixed value looks free. It changes nothing about what is *enforced* —
the ToolGate rewrites and refuses on the wire, and the runtime validates every
call it receives — so the worst case is a model proposing a call the runtime
then refuses, which is a turn spent rather than a guarantee lost.

`oneOf` is deliberately **not** on the list. Some models refuse it and some do
not, which makes it exactly the case the list must not grow to cover: a union
removed reads as *anything at all*, so a contract that said "one of these three"
would be shown as unconstrained. It travels — and **a keyword the list does not
name is reported and never stripped**: a 400 whose message names a keyword this
desk actually sent becomes an `error` event naming it, with the closed list
quoted, rather than the desk widening its idea of the runtime's contract on
being refused. Both engines carry that rule and both have a conformance leg for
it.

**The two engines do not show the model the same contract, and the desk says
which.** `@ai-sdk/google` does not send the schema it is given: it rebuilds it
through its own converter, which copies an allow-list of keywords and drops the
rest. So on the `vercel` engine `pattern`, `maximum`, `uniqueItems`, the
conditionals and the annotations below never reach the model either, and a tool
whose schema declares an object with no properties is declared with **no
`parameters` member at all**. That is the SDK's behaviour and not this desk's,
and it is below the one seam this adapter has — but it is exactly the
cross-engine contradiction the closed list exists to prevent, so it is declared
rather than discovered:

| engine | what the model is not shown, on `gemini` |
| --- | --- |
| `builtin` | `$schema`, `$id`, `additionalProperties`, `const`, `examples`, `patternProperties` — the desk's list, and nothing else |
| `vercel` | all of those, **plus** `$comment`, `$defs`, `$ref`, `contains`, `default`, `dependentRequired`, `deprecated`, `else`, `exclusiveMaximum`, `exclusiveMinimum`, `if`, `maxLength`, `maximum`, `minimum`, `multipleOf`, `not`, `nullable`, `pattern`, `prefixItems`, `propertyNames`, `readOnly`, `then`, `title`, `uniqueItems`, `writeOnly` — and a tool whose schema declares an object with no properties is declared with no `parameters` at all |
| either, on the other two families | nothing: those wires take JSON Schema as written |

Three things hold that table honest, and one boundary is stated rather than
glossed.

**It is derived, not copied — over the recorded runtime's own vocabulary.** A
conformance leg sends a fixture whose keyword union is pinned, by a test that
computes both unions, to **every keyword the recorded runtime 0.19.0 emits in
its tool and pack schemas** — the five `inputSchema`s it served on `tools/list`,
and the pack schema its own `get_schema` answered, which is in the conformance
fixture with its bytes, its sha256 and its provenance beside it. What the leg
reads back is what the installed provider actually did with each of them, and it
is asserted equal to this table. An SDK that starts or stops dropping one of
*those* keywords is a red test.

**The rest of the `vercel` row is outside that lock**, and that is the boundary:
`maximum`, `multipleOf`, `contains` and the other keywords the recorded runtime
does not emit were measured the same way against a wider synthetic schema, and
they are true — but a runtime that never emits them gives this desk no way to
notice if the provider stopped dropping one. The first version of this section
said "every keyword the runtime could emit", which was a claim about a schema
somebody made up rather than about the runtime's own.

**The wire is asserted whole**: every leg requires the schema that arrived to be
**deep-equal** to what this table says arrives — not that three keywords are
present and six absent, which is what the first version checked and is a claim
about a handful of words.

**And the author is told**: a run opens with one line per tool that lost
something **beyond the desk's own list**, naming the tool and the keywords. Over
the runtime's own five that is nothing at all on `builtin` — its removals *are*
the ruling — and exactly one line on `vercel`, for the tool whose schema that
provider drops whole. A notice about `additionalProperties` would be the desk
warning about the rule it wrote down.

Two things the SDK does are **rewrites** rather than removals: it inlines a
`$ref` (dropping the `$defs` it resolved, so the constraint survives and the two
keywords do not) and it infers a `type` for a bare `enum`. The first is declared
above, because from a keyword's point of view those two names do not reach the
model; the deep-equality assertion therefore runs over the runtime's own five,
which carry no reference, and over the vocabulary fixture only for the engine
that rewrites nothing.

**One more thing that provider does, and it is not about schemas.** It surfaces
a thought part only when its text is non-empty, so an **empty signed thought** —
which the wire emits when a summary was not streamed — never reaches the desk:
it cannot be ledgered, cannot be replayed, and cannot be counted as reasoning.
Against an endpoint that emits one and enforces the wire's rule that signed
parts come back, a `vercel` session is refused and ends with the endpoint's
status; *this model always thinks* cannot be inferred from one there either. The
built-in engine has neither limit, because it reads the wire itself. Both halves
are conformance legs, written to go red the day the provider starts carrying
them.

**The Gemini API reference these rules were written against**, read on
**2026-09-06**:
[generating content](https://ai.google.dev/api/generate-content) (the
`:generateContent` and `:streamGenerateContent?alt=sse` methods, `contents[]` of
`{role, parts[]}`, the `text` / `functionCall` / `functionResponse` part
variants, `thought` and `thoughtSignature` on a part, `systemInstruction`,
`generationConfig.thinkingConfig` with `thinkingBudget`, `includeThoughts` and
`thinkingLevel`, `candidates[].content.parts[]`, `finishReason`,
`usageMetadata.thoughtsTokenCount`, and the `{error: {code, message, status}}`
envelope); [thinking](https://ai.google.dev/gemini-api/docs/thinking) and its
[signatures section](https://ai.google.dev/gemini-api/docs/thinking#signatures)
(the `thinking_level` values `minimal`, `low`, `medium`, `high`, each model
advertising a subset, and the rule that a client "MUST always resend all
`thought` blocks exactly as they were received from the model");
[function calling](https://ai.google.dev/gemini-api/docs/function-calling) ("only
a subset of the OpenAPI schema is supported"); and
[the models list](https://ai.google.dev/api/models#method:-models.list) for the
probe. What that page **does not** state, and this desk therefore does not
claim, is a `thinkingBudget` range that holds across the family — see the note
under the tier table.

### The refutation pass

With the tier on, the assistant does not show you a proposal it has not tried to
break. After the main loop has produced a document and **before** the proposal
event is emitted, the engine runs a second, adversarial session over it — a
second loop in `builtin`, a second `streamText` in `vercel` — instructed by the
runtime's own `test_pack` prompt, the document fenced beneath it, and one fixed
sentence from this desk saying to try to refute it with the runtime's tools and
to state no verdict of its own.

It runs **inside the same ToolGate**, on the same `callTool`, over the same five
tools, under the same run gate: its `experimental_evaluate` is rewritten to a
rehearsal before the frame leaves the page exactly as the main loop's is, it can
no more reach `write_file`, and pressing Stop during the pass ends it where it
would have ended the loop above. The desk hands an engine one capability, so
there is no ungated route for a critic to be given.

Three rules, all of them desk code:

- **the verdict is computed from the runtime's own results, never from the
  model's prose.** A check is a `validate` or a rehearsal `experimental_evaluate`
  the critic actually caused, read from the answers that came back through the
  gate; the proposal is refuted exactly where one of them did not come back as
  the runtime's own word for "this went through". A critic whose sentence says
  *REFUTATION: this pack is broken* over a `validate` the runtime called `valid`
  produces **not refuted**, and one whose sentence says *none found* over an
  `invalid` produces **refuted** — both directions are conformance legs;
- **a non-empty list of checks before "not refuted" is rendered.** A critic that
  talked and asked the runtime nothing produces *the critic ran no runtime
  check*, and the proposal is shown **without** a refutation line rather than
  with a clean bill of health nobody measured;
- **the checks are quoted and the prose is labelled.** The report shows each
  check as the runtime's own `status`, and the critic's own words beneath, as
  the model's words.

The settled status is a **table per tool** and not the single word `valid`: a
rehearsal evaluation answers `"status": "evaluated"`, so one word over both
tools would have reported every session ever run as refuted. **A status is the
only thing that makes a check**: an answer the runtime returned with `isError`
and no `status` in it is the runtime declining to *answer*, not declining the
document, and it is no check at all — and a call the desk's own gate refused
never reached the runtime, so it is a `guardrail` line and can never become one.
A verdict is a thing the runtime said.

A refuted proposal is **still a proposal**: the document is shown, the diff is
drawn, and Accept is offered. Refutation is information, not failure, and the
desk's one clean-run selector treats it as such.

**The ruling this carries, which the maintainer has not made.** On a *degraded*
endpoint — one with no thinking at all — the pass **still runs**. Its value is
the runtime's checks over the proposed document, not the model's thinking, and a
`validate` costs one call; the tab's line says the endpoint has no thinking, and
the checks appear as they always do. The alternative — skip the pass wherever
the endpoint degraded — is `REFUTE_ON_A_DEGRADED_ENDPOINT` in
`web/src/assistant/thinking.ts` set to `false`, one boolean and nothing else,
with a conformance leg that follows it either way. ADR-0001 lists this among the
questions the bake-off could not close, and it is open until the maintainer
rules.

### What is measured, and what is modelled

The conformance session runs both engines over all three wire formats, each
answered as a stream and as one whole object, at tier `off`, `on` and `ultra`,
against an endpoint with thinking, one with none, one that takes only the other
Anthropic spelling, one that takes only the other Gemini spelling, one that
splits its signatures, one that reasons whatever it is asked, one that refuses
to be turned off, and one that refuses a schema keyword. What that **measures**
is what this desk puts on the wire and what it does with what comes back: the
tier parameter on every request including the critic's, the signatures carried
back byte-equal and well formed, the schema the model was actually shown, the
degrade happening once, the refused member never re-sent, the critic's evaluate
arriving at the runtime rehearsed, and the verdict following the runtime rather
than the prose. The runtime's answers in it are a real `jpack mcp`'s, recorded.

What is **modelled** is the endpoint. The scripted model reproduces the
documented wire shapes of all three protocols; it is not evidence about how any
vendor's endpoint actually responds, and the reasoning text and signatures in it
are deterministic strings rather than a model's. On the Gemini family that
extends to two things worth naming: **which** models accept which thinking
spelling, and what a real `thinkingBudget` range is, are modelled by the
fixture's own refusals rather than measured — the desk's behaviour when it meets
each is what the legs establish. Two smaller things are modelled
too, and are named here rather than left to be discovered: the critic's copy of
the runtime's `test_pack` prompt is a stand-in string in CI (the recorded
runtime carries `tools/list` and `tools/call` and no `prompts/get`), and that
prompt is read **without** its `pack` argument on the page as well, because the
document the critic will be given does not exist when a session's prompts are
read — the document is handed to the critic verbatim instead.

### The ToolGate

`web/src/assistant/toolGate.ts` wraps the assistant transport's `send`, and
every outbound `tools/call` frame passes through it:

- a name outside the session's allow-list is **refused** — the frame never
  leaves the page, the caller's promise rejects with a `GateViolation` naming
  the tool, and a `guardrail` line appears in the tab. The engine turns the
  rejection into a result the model is told about, because a call dropped in
  silence is a turn the model spends re-asking for it;
- `experimental_evaluate` is **rewritten** to carry `rehearsal: true` whatever
  the arguments said, with a `guardrail` line saying what was there — the
  `rehearsal` member's previous value, and never the pack text;
- everything else passes untouched, including `initialize`, `tools/list` and
  `prompts/get`. The gate is about what the assistant may *do*.

The allow-list is `assistant.endpoint.tools` **intersected with the five**, and
the order is allow-list first: an `experimental_evaluate` the file never granted
is refused rather than politely corrected on its way out.

**The frame is canonicalized to bytes before anything is decided, and what
leaves is the canonical frame.** That is the shape of the whole check, and it is
the lesson the chassis' relay learned over four review rounds: a classification
made about a mutable object is a classification the object can change out from
under you. A `method` getter can answer `undefined` while the gate is looking
and `tools/call` while `JSON.stringify` is; a response-shaped object with a
`toJSON` can serialize as a request; an enumerable `toJSON` on a call's
arguments is invoked at serialization and drops whatever the gate had just
written. So the object is serialized once, the bytes are parsed back to plain
data — which has no getters, no `toJSON`, no functions, no symbol keys and no
prototype left — and every rule is applied to that. The transport is handed the
plain data and never the caller's object.

What is checked, in order:

1. the frame survives a JSON round trip at all (a cycle does not, and is a
   refusal rather than an exception thrown at the socket) and is a plain object;
2. **every frame is checked against the SDK's own message schema**
   (`JSONRPCMessageSchema`) — a request, a notification, a response or an error,
   each whole. That replaced a hand-written set of shape rules, and the reason
   is that the hand-written ones were looser than the sentence describing them:
   `{"id": null, "result": 7}`, an `error` that is a string, and a fractional id
   all satisfied "an id and exactly one of `result` and `error`", and not one of
   them is a JSON-RPC message. It is the same schema the client validates
   *inbound* frames against, so what this desk will send is what its own SDK
   will accept. A JSON-RPC batch is refused here too: an array is not a message,
   and "anything that is not `tools/call` is traffic I have no opinion about"
   let a batch carrying an allowed call beside a `write_file` out whole;
3. a method that is not `tools/call` but is a spelling of it to some other
   reader — `Tools/Call`, ` tools/call ` — is refused, on the chassis' own
   reasoning about its query: a frame two readers disagree about is one this
   desk will not send;
4. for `tools/call`, the tool is on the session's allow-list — checked against
   the name the frame **serializes into**, not the one it claims — and
   `experimental_evaluate` gets an own `rehearsal: true` written last onto the
   canonical arguments.

One whole conversation through the gate — the handshake, the notification after
it, a listing, a prompt, a tool call, and the client's automatic answer to the
server's own `ping` — is in the suite, because tightening frame rules is the
kind of change that breaks a connection while every refusal test stays green.

### What these guards do not claim

They are structural guards against **mistakes, SDK-internal paths and
uncertified adapters**, and certification is what the desk trusts. They are not
a sandbox. An engine runs in the page's own realm, and same-realm code that
patches a prototype or replaces a global is outside what any page-side guard can
contain — a module that redefines `Response.prototype.body`, or captures `fetch`
before the desk does, is not a threat the ToolGate or the model capability claim
to hold. What they do hold is that an engine which behaves itself cannot reach a
tool nobody granted, cannot send an unrehearsed evaluation, and is never handed
this chassis' credential; and that an engine which does not behave itself fails
the conformance session rather than shipping.

The engine is handed a `callTool` bound through this gate and **nothing else** —
no client, no transport, no `fetch`, and **no URL** — and the session's member
set is asserted whole in `assistant/enforcement.test.ts`, because the guarantee
is that there is nothing else there.

### Why the engine gets a capability and not a base URL

ADR-0001's contract sketch writes `model: { family, baseUrl, model }`, with
`baseUrl` "the chassis relay". This desk deviates. The reason it was first
written down was that the relay authenticated with **this chassis' session token
in the query**, so a `baseUrl` an engine could read was this desk's own
credential in the engine's hands, and an adapter holding it could open
`/ws?token=…` itself with `globalThis.WebSocket` and drive a third MCP
connection the ToolGate is not on.

**That reading is out of date since the launch exchange, and the deviation
stands on better ground.** No address is a credential any more: the session is a
bearer id this page holds and puts on each request itself, and the relay URL
carries nothing. Page code that spelled `/ws` would still have to find the id to
open it — which it can, from `sessionStorage`, exactly as it could once read the
token there.
Withholding the URL was never the load-bearing part in any case — the token
lived in `sessionStorage`, which is same-origin readable, so an adapter that
wanted it could always have read it. What the capability actually buys is that
**the desk decides what an engine may reach**: one mount point, a validated path
suffix, no header outside the relay's own allow-list. What stops an engine
opening a second, ungated connection is that every leg runs with `fetch`,
`WebSocket`, `XMLHttpRequest` and `EventSource` replaced by throwing sentinels —
a guard over behaviour, which is where that guarantee has always really been.

So `model` is `{ family, model, call }`. `call(suffix, request)` is bound by the
desk: it builds the address itself, admits only a path suffix that passes the
relay's own segment rule, carries only the headers on the chassis' outbound
allow-list, and captures `fetch` when the session is bound rather than reading
it at call time. The engine chooses a suffix — `chat/completions`,
`v1/messages` — and nothing else.

**The answer is a facade this desk builds**, not the one `fetch` produced: a
browser `Response` carries the requested URL on `.url`, and returning it handed
the engine this desk's own routing — and, before the launch exchange, the token
in it. It is defence in depth rather than the thing holding the gate, and it is
kept because an engine that is handed a capability should not be handed an
address by the back door. What comes back is a constructed `Response` —
empty `url`, the status and reason phrase, a filtered header copy, and **a body
stream of this desk's own**, piped through an identity transform: a `Response`
built from a `ReadableStream` keeps that very object, so a stream somebody
decorated was reachable as `facade.body.leak`.

The failure path goes the same way: a browser's `TypeError` for a failed fetch
quotes the URL, so the error is replaced with a fixed sentence. An **abort**
reaches the engine as a fresh `AbortError` with a fixed sentence and no `cause`
— the classification travels because a loop has to tell "stopped" from "failed",
and nothing else does, because a rejection named `AbortError` can carry the URL
in its own message and again in its cause.

The suffix must be a **primitive string** before anything else happens. The
TypeScript signature said `string` and the type is not what runs: a string-like
object can answer an innocuous `length` and `split()` while the validator is
looking and a different `toString()` when the URL is built, which would turn
this capability into an authenticated POST to another same-origin chassis route.

That capture of `fetch` is what makes the guarantee structural instead of
inspected: the conformance session replaces `fetch`, `WebSocket`,
`XMLHttpRequest` and `EventSource` with throwing sentinels **from before the
engine's chunk is imported until after everything it scheduled has run**, on
every leg. The sentinels record as well as throwing, because a reach from inside
a `setTimeout` throws into nobody's `catch`.

The barrier at the end **tracks handles rather than waiting**. It was a fixed
200ms, and a fixed wait is a delay an engine can out-wait — 201ms, an interval,
a timer that schedules another timer. Every scheduling primitive a page has is
wrapped for the sealed window, **each with its canceller**: `setTimeout`,
`setInterval`, `queueMicrotask`, `setImmediate`, `requestAnimationFrame` and
`requestIdleCallback`, with `clearTimeout`, `clearInterval`, `clearImmediate`,
`cancelAnimationFrame` and `cancelIdleCallback` beside them. Each call is
recorded *and* scheduled for real, so an engine that legitimately needs a timer
still makes progress, and whatever has not fired when the run ends is fired,
repeatedly, until nothing is left. What remains when the bound is reached is a
**failure**, not a pass. A handle the engine itself cancelled is never fired on
its behalf — that is what the cancellers are wrapped for — because running one
would report a reach the engine had already decided not to make; a schedule that
returns no handle at all, as `queueMicrotask` does, is filed under none, so a
`clearTimeout(undefined)` cannot cancel it by accident.

**`requestIdleCallback` is installed where the environment has none**, and
removed again afterwards. jsdom does not have it, so an engine that wrote
`globalThis.requestIdleCallback?.(() => fetch(…))` did nothing at all during
certification and reached the network in Chrome, after the seal would have
lifted: a primitive the *browser* has and the *harness* does not is a hole in a
guard whose whole claim is that everything an engine scheduled has already run.

**The drain runs what an engine left behind in the order a browser would have.**
One entry at a time, soonest first, with its clock advanced to that entry's due
time — never before it. Firing every pending handle at once ran them in the order
they were *scheduled* rather than the order they were *due*, so a sixty-second
idle callback ran before the one-second timer that was going to cancel it, and,
being run, was told a deadline it had not reached had been reached.

**Realistic means the deadline, not only the callback.** The shim takes the
`IdleRequestOptions` it is given, hands the callback a budget that is positive
at its first read and decreasing from the moment it starts — the browser's own
rule — and **honours the timeout it was asked for**: the callback is not run
before that deadline, and `didTimeout` is computed **when the callback runs**, from
whether the deadline was actually reached. Firing every positive timeout after a millisecond and calling that a timeout
credited an engine with work it would have cancelled long first; a callback with
no timeout is offered an idle slot on the next turn, and reports `false`. Held
under controlled time, deadline by deadline. A shim that answered
zero and false to everything would run the ordinary idle pattern
(`if (deadline.timeRemaining() > 0) work()`), watch it decline to do anything,
and certify a clean leg while a browser gave it a real budget and let it reach.
The hostile fixture writes both shapes, one guarded on the budget and one on
`didTimeout`.

**A certified engine leaves no live interval when its iterator ends.** An
interval is never run by the drain at all: running a few ticks and calling it
drained is a bound an engine can hide a reach behind, and an interval nobody
clears is one no bounded drain can exhaust. It is reported by name and the leg
fails for it. Promise reactions are flushed rather than tracked — the drain
turns the microtask queue over between rounds, which is how a `.then` chain an
engine left behind is caught while the seal is still up — and what that does not
cover is a reaction chained off something that resolves *after* the drain: a
fetch to a real host, a socket, a `MessageChannel`. That bound is real and is
stated here rather than papered over.

The cleanup is nested so that the drain, the timer wrappers, the sentinels and
both connection closes all come off whatever throws: a deferred callback that
threw used to leave a leg's globals installed for every leg after it. The desk's capability still works; an engine that reaches for a
global fails the leg by name (`K1a`). The string-enumeration guard that used to
forbid a handful of spellings under `engines/` is gone: it said of itself that a
novel spelling walks past it, and `new globalThis["Web"+"Socket"]` is that
spelling. When the `vercel` adapter lands, this capability is what is passed as
the provider's `fetch` option, so the shape survives the next chunk.

### The engine slot, as it stands

`web/src/assistant/engine.ts` is ADR-0001's contract. `assistant/engines/` is
the registry: one lazily loaded chunk per certified engine, so a **release**
carries every certified chunk and a **session** downloads one. There is **one
table**: the certified list is derived from the loaders rather than written
beside them, and the two sets are asserted equal at the type level, so an id
cannot become loadable without being put in front of the conformance session and
an id the decoder declares cannot be left without an adapter. Both directions are
compile errors.

**A run is a gate, and it is closed before it is aborted.** `openRun` gives each
run a gate the consumer's `return()`, its `throw()`, the session's own signal and
the run's natural end all close — once, synchronously — and closing it marks it
**closed first**, then aborts, then releases. The order is the point: a signal
says "stop soon", and a closed gate says "nothing more from this run reaches
anybody". When the thing an engine was awaiting wins its race with the abort by
a microtask, the loop resumes holding a value; aborting cannot stop it delivering
that, and a flag the delivery path reads can. A session already aborted when
`start` is called closes the gate before a provider is built or a request is
made: no event, no model work, nothing.

**`withAbort(() => work(), signal)` bounds each await on the world**, and takes a
thunk so a closed run starts no work at all — evaluating the argument *is* the
request. It is used at each model request, each `session.callTool`, each read of
the AI SDK's stream and of its result promise, and at the relay's and the
providers' request and body reads; `sseEvents` takes the run's signal so a
stalled stream cannot outlive its run. **Two waits are not wrapped, and are
bounded differently:** the channel take and the channel's wake wait are released
by `abandon()`, which the gate calls as it closes — a wrapped race there would be
a second way to end the same wait. Aborting the awaited thing alone was never
enough: a model request honours a signal and a `tools/call` over a socket does
not, and a cleanup queued behind an await on something it was meant to end waits
for ever.

The runtime is reached through a guard that reads the run's signal **before it
dispatches**, so no `tools/call` arrives after the consumer has left. A cancelled
run says nothing at all, on either engine, not even `end`: the terminal event
belongs to a run that finished, and the page's own terminal accounting is the run
hook's.

**An engine's outer shape is a hand-written iterator, not an async generator.**
A generator serves `next()`, `return()` and `throw()` from one queue, so a
`return()` arriving while a `next()` is pending is not run until that `next()`
settles — and a run waiting on a model request that ends only when it is aborted
could never be stopped by the consumer that owned it, because the abort was
inside the `return()` queued behind the very `next()` it would have released.
Both promises hung for ever, on both engines, measured. So `return()` cancels
**first**, synchronously, before it awaits anything: the pending `next()` settles
`{ done: true }`, and only then does `return()`. Each engine holds an abort of
its own, chained to the session's, and gives its model requests that one.

**One channel, one consumer, and no terminal event out of a `finally`.** The
adapter's events reach the contract through an ordered channel whose `push`
resolves only once the consumer has taken the event — which is what puts the
desk's own guardrail line between the `tool_call` that provoked it and the
`tool_result` that followed. A second reader is refused by name before it takes
anything, because the in-flight slot is a single slot and two readers would
overwrite each other's. And `end` travels that channel like every other event
rather than being yielded from a `finally`: a `finally` that yields makes a
consumer's first `return()` resolve `{ value, done: false }` with the generator
still suspended, and `for await`'s own closing discards the value, so the
terminal event is never delivered at all. A consumer that stops listening is owed
no terminal event; what it is owed is a closed iterator.

`assistant/engines/contract.ts` holds what belongs to the contract rather than to
either loop — the twenty-turn bound, the desk's own sentence to the model, the
one reading of a proposal (exactly one fenced block, never the prose beside it),
the rule that an answer is read by **what came back** rather than by what was
asked for, and the served schema an engine may show the model. Both engines
import it, because a rule written twice is a rule two readers can disagree about.

**A tool the runtime served without an `inputSchema` is refused**, on either
engine: the session ends with one `error` naming the tool, before a request is
made. K2 says the model is shown the contract the runtime enforces *or it is
shown nothing*, and a permissive `{"type":"object"}` written by the desk is this
desk telling the model that anything is acceptable for a tool whose real contract
it does not know. The five a real `jpack mcp` serves all carry one.

| engine | what runs the loop | added download (gzip) | what it guards | what it does not do yet |
| --- | --- | --- | --- | --- |
| `vercel` **(default)** | Vercel AI SDK v7 — `ai` 7.0.93, `@ai-sdk/openai-compatible` 3.0.44, `@ai-sdk/anthropic` 4.0.49, all pinned exactly | **96.0 KiB** for the lazy chunk, plus 2.0 KiB shared with the other engine and 1.2 KiB the main chunk grows by | the rehearsal hook named as a key of the SDK's own options type, so an upstream rename is a compile error rather than a guard that fails open; the desk's gate handed the call **as the model made it**; a placeholder origin the adapter never resolves, and a query refused at both layers; the SDK's own retries off; the truncated thinking signature it carries back (`vercel/ai#19663`), detected and degraded rather than sent | reassemble a split signature: it detects the truncation instead, and the session degrades once with the reason |
| `builtin` | the bake-off's control loop, by hand — two SSE parsers, both wire formats | 3.2 KiB, and no new dependency at all | the same promises, held one level below it in the ToolGate and the model capability, which is where they are held for **every** engine; and the assistant turn echoed as received, so thinking blocks, redacted blocks and split signatures survive by construction | nothing the default does — it is the fallback that adds nothing to the supply chain |

`builtin` is the port of the bake-off's control loop — a hand-written turn loop
over an explicit messages array, both wire formats, no new dependency —
restricted to the contract: it takes the runtime's prompt and the runtime's own
tool definitions, speaks to the chassis relay **with no credential of its own**,
reads a stream or a whole answer by what came back rather than by what it asked
for, ends on one fenced JSON block, bounds itself at twenty model turns, and
emits `end` exactly once.

`vercel` is the same contract on `streamText`. Its adapter is a translation and
never a second opinion, and four things about the SDK are the adapter's business
rather than the desk's:

- **the rehearsal hook is `experimental_`.** `streamText`'s options carry a rest
  parameter, so a misspelled `experimental_refineToolInput` is accepted in
  silence and the rewrite is simply never applied — measured at **zero** compile
  errors on a rename. It is named once, as a key of the SDK's own options type,
  so the day `ai` drops the option the adapter stops compiling.
- **the desk's gate is handed the call the model made.** The hook rewrites what
  the SDK carries — which is what the model is shown on its next turn — and the
  ToolGate rewrites what leaves the page. A gate handed a call somebody already
  fixed reports nothing, and the guardrail line in the tab is the only place a
  person learns that the rehearsal flag was forced.
- **the SDK reads the answer it asked for.** `streamText` picks its response
  handler when it picks to stream, so an endpoint that answers whole to a request
  that asked to stream yields no events at all: `AI_InvalidResponseDataError` on
  one path and `AI_NoOutputGeneratedError` on the other, both on the shipped
  release. The adapter re-frames a whole answer into the events the same protocol
  defines. That is the only wire knowledge in it, and two of the four conformance
  legs exercise it.
- **the SDK retries a 409 twice, with a backoff** — and 409 is what the desk's
  own relay answers when no key is stored on this machine. Three requests and six
  seconds for a refusal a person has to go and fix. Retries are off, so both
  engines make one request per turn.
- **the refusal path leaks a rejection nobody can catch, and one of them is
  still there.** `streamText`'s result exposes its output as promise-valued
  members, and reading one mints a promise that rejects when the call fails;
  read and left unclaimed, it reaches the page as an unhandled
  `AI_NoOutputGeneratedError`. Those are claimed at the cause — every
  promise-valued member, the moment the result exists, enumerated from the
  object rather than from a list the next release would date.

  **One more is not reachable from this side, and this desk says so rather than
  hiding it.** In a real browser, an endpoint that answers 400 leaves exactly one
  `AI_NoOutputGeneratedError` on the page, constructed inside the SDK's own
  transform `flush` and never handled late. Three things were tried and each was
  measured on the live drive: claiming the result's promises again after the
  stream is consumed (still leaks); claiming the result's whole object graph
  recursively, own properties and prototype getters, to depth four (still
  leaks — so the rejecting promise is reachable from the result at *no* depth);
  and reproducing it under Node with the same loop shape, where
  `process.on('unhandledRejection')` sees nothing at all, which is why neither
  jsdom nor the conformance session can observe it. It reproduces at tier `off`
  against an endpoint that refuses every request, so it is the SDK's refusal
  path rather than anything the tier added. The closest upstream report is
  [`vercel/ai#8084`](https://github.com/vercel/ai/issues/8084) — *"Unable to
  catch NoOutputGeneratedError"* — closed against 5.0.x; this is the same class
  on 7.0.93 and no open issue matches it.

  What the desk owns is that **the console is not where a person finds out**: the
  run puts the status and the endpoint's own sentence on its own stream, which
  the engine's suite asserts, so the rejection is noise beside a failure the tab
  has already reported. The fix is **not** a page listener: one of those is keyed
  on an error *name* and would suppress every rejection on the page carrying it,
  an unrelated operation's included, for as long as a run was open.

It reports what the model said about its own reasoning as the contract's
`reasoning` events, **whatever the tier is**: the tier is what this desk asks
for, and a model that always thinks reasons anyway. The tab renders one line per
passage rather than one per delta.

Either engine reaches a model only through `session.model.call`, naming a path
suffix; the address, the token and the header allow-list are the desk's. The
SDK's providers are built against `https://relay.invalid`, a placeholder origin
nothing ever resolves, and the `fetch` they are given reduces the absolute URL
the SDK composed to that suffix — refusing any other address, any query and any
fragment, and stripping every header outside the protocol's own, including the
placeholder key `createAnthropic` throws without. Both wire formats put `stream`
in the body, so no engine ever needs a query — which is as well, because the
relay refuses one.

### The conformance session

`web/src/assistant/conformance/` is the bake-off's scenario carried into the
repository and run in CI — keyless, deterministic, no network, no runtime
binary. **It runs over the registry**, not over one engine: every id in
`CERTIFIED_ENGINES` is put through all six legs and every check below, so
certifying an adapter is adding its id to one list and a further engine is one
PR — the adapter, its conformance run, and its row in the table above. Where the
two engines' wire shapes differ, the scripted model is held to what each **wire
format** defines rather than to either engine's spelling; a leg that needed an
engine-specific branch in the fixture would be a finding rather than a fix.

- `scenario.json` is the experiment's own fixture, whose DRAFT_V1, DRAFT_V2 and
  FACTS were proved against the runtime before it was written.
- `runtime.json` is a recording of a real `jpack mcp`: `tools/list` filtered to
  the five, exactly as served with the runtime's own schemas, and one
  `tools/call` answer per step T1–T6, taken from judgment-pack-runtime v0.19.0
  in a project copy that declared an audit trail. No audit record was written by
  any of them. It is **produced by a checked-in recorder** rather than by hand:

  ```sh
  # regenerate, or byte-compare what is committed against a runtime
  JPACK_COMMIT=<full commit> npm --prefix web run conformance:record -- ./bin/jpack /path/to/project
  JPACK_COMMIT=<full commit> npm --prefix web run conformance:verify -- ./bin/jpack /path/to/project
  ```

  The fixture carries the binary's SHA-256 and the runtime's full commit, and
  `verify` fails on a byte of drift — which is what makes "recorded" a claim
  somebody can check rather than a word in a comment. The project directory must
  declare an audit trail, so a recording pass that wrote one would leave the
  evidence behind. `sourceCommit` is stated by whoever runs the recorder and is
  the one member there that is a claim rather than a measurement; the fixture
  says so itself.
- `scriptedModel.ts` is the fixture's step logic in TypeScript, installed as a
  `fetch` stub that records every request; it decides the next step from the
  results present in the request's own messages, so a run that mishandled a
  message array gets a different script.
- `scriptedServer.ts` replays the recording on an in-memory transport pair, and
  matches a call by its **arguments**: an `experimental_evaluate` arriving
  without `rehearsal: true` is a failure, and so is a `write_file` arriving at
  all.
- `certification/` holds four engines the desk would never certify: one touches
  the network as its module loads; one schedules four reaches for after its run
  ends cleanly — soon, five minutes out, chained behind another timer, and on a
  promise chain with no timer at all — and leaves an interval ticking; one
  clears the interval it starts and cancels a timer it schedules, so it passes
  the interval rule, is never credited with the reach it cancelled, and still
  fails on the timeout it meant **and on a reach from a microtask it wrote a
  `clearTimeout(undefined)` beside**; and one throws from a callback nobody is
  awaiting, so the cleanup can be shown to run anyway. Each must fail its leg,
  and does. **A conformance session that only ever runs conformant engines
  proves nothing about the session** — and a rule that fails everything proves
  as little as one that fails nothing, which is what the third is for.

**Three bounds this session does not hold**, stated because a guard whose limits
are not written down gets read as a proof. A reaction chained off something that
resolves *after* the drain — a fetch to a real host, a socket, a `MessageChannel`
— is outside it. So is an engine that stops reading a stream: a reader left
attached schedules nothing, and no drain can see it. And so is an unhandled
rejection: these legs run in jsdom under Node, where a rejection nobody claims
goes to Node's own handler and never becomes a `window` event. So the adapter's
own suite measures that where it *is* visible —
`process.on('unhandledRejection')`, over the desk's own 409 refusal — rather than
by dispatching the event a browser would have sent, which would only have
measured its own dispatch.

Each leg also records **what the engine actually sent** — the step, how many
results its own messages carried back, the route, the body's own top-level
members, the tools offered and the header names — as a test annotation, so a
reviewer can read the wire rather than only the assertions about it.

Six legs — OpenAI-compatible, Anthropic and Gemini, each answered as a stream
and as one whole object, because an endpoint may ignore what the request asked
for —
and the checks are the experiment's own, plus one this desk added: **K1a** the
engine touches no network global at all — at load, during its run, or from a
timer it left behind — held by sealing `fetch`, `WebSocket`, `XMLHttpRequest`
and `EventSource` from before its chunk is imported until after deferred work
has been drained; **K1** no
credential in any request and nothing called but the relay; **K2** the five
tools out of `tools/list`, with no schema literal in any engine source; **K3a**
the rewrite, measured at the scripted server rather than at the page; **K3b**
`write_file` never arriving; **K3c** the proposal equal to DRAFT_V2; T8's
unknowns; and the event stream's exact order with one `end`.

Beside the shared matrix, the thinking half runs the tier at `on` and `ultra`,
the degrade, the dialect fallback on each family that has one, the refutation
pass in both verdicts, and — on the Gemini family — more: the schema the model
was shown, asserted **deep-equal** to what the table above says it is shown, over
the runtime's own five and over a probe carrying every keyword; that table's
`vercel` row **derived** from the probe's own wire rather than copied from
anywhere; the one line per tool the run opens with where something was lost; a
keyword the removal list does **not** name, refused by the endpoint and reported
by the desk with nothing stripped; the budget-to-level dialect fallback; the
thought signatures replayed across every tool turn at each of Gemini's three
documented placements — on the summary, on a single call, and on the first of a
parallel pair — **gating**, because the endpoint refuses a continuation that
dropped one; and *this model always thinks* by both roads, the two-turn
inference and the immediate refusal.

## Requirements

- Go 1.25 or newer (`go.mod` declares it; CI reads that file)
- Node 22 or newer (`web/package.json` declares `engines.node >= 22`)
- A `jpack` binary — the [judgment-pack runtime](https://github.com/Judgment-Pack/judgment-pack-runtime)

TypeScript and Vite are pinned to exact versions rather than caret ranges. They
are the two tools whose output this repository ships — a transpile and a bundle
— so a fresh `npm ci` producing a different build than the last one would be a
change nobody made.

## Production mode

One binary with the SPA embedded:

```sh
npm --prefix web ci
npm --prefix web run build     # emits web/dist, which go:embed picks up
go build -o bin/jpack-desk .

./bin/jpack-desk --jpack /path/to/jpack /path/to/project
```

It prints the URL to open. That URL is the **launch exchange**, not the desk:

```
judgment-pack desk
  project: /path/to/project
  runtime: /path/to/jpack
  open:    http://127.0.0.1:8791/launch?secret=1f3c…
```

Opening it once trades the secret for a **sixty-second, single-use handoff**
cookie and redirects to `/`, so what ends up in the address bar is `/` and the
secret is on no later request. The page then exchanges that handoff for a session
id it holds itself.

**The launch URL stays in browser history**, like any URL that was visited, and
the secret in it stays valid for the life of the process — reusable by design, so
that reopening the desk is not restarting it. What that history entry is worth is
one more launch, to somebody already at this machine's browser. See
[Security model](#security-model).

**The project is chosen in three steps, in this order**: the argument, then
`project.file` in this machine's desk-level file — read through the same
custody-validated store every other read of that file goes through, and
validated against this host before it is honoured — then the current directory,
exactly as it always was. A configured default this host cannot open (not
absolute here, not resolving to a regular `jpack-desk.json`, or in the
filesystem root) **refuses the launch and names the member**, rather than
falling through: a person who configured one and silently got some other project
would have no way to see that what they wrote was ignored. A desk-level file
that is refused as a whole names no project, and the launch says which problem.

`--jpack` defaults to `jpack` on `PATH`, and `--port` defaults to `8791`.

## Development mode

Two processes: the chassis for the relay, Vite for hot reload.

```sh
# terminal 1 — chassis with a fixed launch secret so the URL is stable across
# restarts (flags come before the project directory: Go stops parsing flags at
# the first positional argument)
go run . --dev-token dev --port 8791 --jpack /path/to/jpack /path/to/project

# terminal 2 — Vite dev server, proxying /launch, /ws and /api to the chassis
npm --prefix web run dev
```

Then open <http://localhost:5173/launch?secret=dev> **once**. The chassis answers
`303` to `/`; the browser lands on Vite's own `/` holding the handoff cookie —
set for the dev origin, and **named for the chassis' port**, which is the port
the desk was told it is on rather than the one the browser is talking to — and
the page immediately spends it at `POST /api/session` for a session id it keeps
in `sessionStorage`. Reload and navigate freely from there: the id is per tab and
survives a reload. Open the launch URL again after restarting the chassis, which
forgets every id it minted.

`--dev-token` names a **fixed launch secret**, and passing it is what
additionally permits the Vite dev server's origin — without it the chassis
refuses the proxied upgrade, because the browser's `Origin` is the dev server's
and never matches the host it reaches the chassis under. Vite proxies `/launch`,
`/ws` and `/api` to `127.0.0.1:8791` (override with `JPACK_DESK_CHASSIS`); without
the `/launch` entry the dev origin acquires no handoff, the page has nothing to
exchange, and the other two answer `401`.

To check a running chassis end to end with the desk's own client code. The
origin and the secret are separate arguments, because a credential does not ride
on a URL — the client presents it as `Authorization: Bearer`:

```sh
npm --prefix web run smoke -- http://127.0.0.1:8791 --secret dev
JPACK_DESK_SECRET=dev npm --prefix web run smoke -- http://127.0.0.1:8791
```

## Security model

The desk drives a runtime that reads your project, and — since the authoring
surface — writes files in it. Two capabilities are gated, and gated the same
way: **`/ws`**, the relay, and **`/api/*`**, the file API. Static assets are
not; they are the page, and the page can do nothing without one of the two.

- **Loopback only.** The listener binds `127.0.0.1`. Nothing off the machine
  can reach it.
- **A launch secret, traded once for a one-shot handoff.** A random 192-bit
  secret is generated at startup and printed as
  `http://127.0.0.1:<port>/launch?secret=…`. `GET /launch` compares it in
  constant time and sets **`jpack-desk-launch-<port>`**: a fresh 192-bit value,
  `HttpOnly; SameSite=Strict; Path=/; Max-Age=60`, single use, and worth exactly
  one call to `POST /api/session`. It mints **no session** — a launch that did
  would answer with a standing credential in a cookie jar, which is the
  arrangement this replaces. (The secret's length is observable, which does not
  matter: the format is fixed and public, and the value is the secret.)

  Then `303 See Other` to `/#`. The empty fragment is written out on purpose: a
  `Location` carrying none inherits the *request's* (RFC 9110 §10.2.2), so
  `/launch?secret=S#S` would land on `/#S` with the secret sitting in
  `location.hash`; the page takes the bare `#` off the address bar once, on
  load. **Nothing that looks like a launch is ever answered with the page**:
  every path at or under `launch` in any case, and every request whose query
  carries a `secret` pair under any case, is a `404` rather than the single-page
  fallback — which would have handed back the page with the secret still on the
  URL.

  A wrong or absent secret answers `403` with one line and **sets nothing**; the
  two are not distinguished, because a caller that could tell them apart would
  have an oracle for the shape of the secret. The secret stays valid for the
  life of the process: single-use *there* would make every closed tab a restart
  of the desk, and what is single use is the handoff, which is the thing that is
  ambient.
- **The session is a bearer the page holds, and nothing ambient authorizes
  anything.** `POST /api/session` spends the handoff and answers a 192-bit
  session id. The page keeps it in `sessionStorage` under
  `jpack-desk-session:<host:port>` — per tab, per origin **including the port** —
  and puts it on every request itself: `Authorization: Bearer <id>` on a
  `fetch`, and the `jpack-desk-session.<id>` subprotocol offer on the WebSocket
  upgrade, which is the only place a browser lets a page put anything on a
  handshake. It is never on a URL. The chassis answers the upgrade by selecting
  the plain `jpack-desk`, so the id is offered and never echoed back.

  **Why not a cookie.** A cookie is ambient by construction, and it has no port:
  one set for `127.0.0.1` is sent to every port on that host, so every other
  local service receives it — and a script that captures one replays it, because
  the rules that stop a *page* forging `Sec-Fetch-Site` or `Origin` are
  forbidden-header rules, and they bind browsers and nothing else. That was
  measured on this desk rather than reasoned about: a cookie captured at the
  exchange and replayed from `curl` with a forged header read the desk. Nothing
  is guarded here now — the ambient credential is gone.

  The store is keyed by an HMAC of each id under a key minted in this process,
  so "is this id live" is not a hash-table probe over bytes a caller chose. It
  holds at most 64 sessions, evicted **least recently used**: a lookup refreshes
  recency, so the tab somebody is actually looking at is the last to go.
- **The one thing that is still ambient, and the residual it leaves.** The
  handoff is a cookie, for the sixty seconds between the launch and the page's
  first request. It opens exactly one route and is spent by it.

  A script that captures it inside that window and forges `Sec-Fetch-Site:
  same-origin` **can take the session before the page does**. Nothing written
  here changes that: forbidden-header rules bind browsers, not scripts. What the
  shape does is make the theft visible and bounded — the handoff is single use,
  so the page's own `POST` then fails and the desk says "No session — open the
  URL that jpack-desk printed at startup" rather than working while somebody
  else is also inside. Sixty seconds, one use, and a failure the person sees.
- **Sign-out exists; expiry does not.** `DELETE /api/session` forgets a session
  and the id then names nothing. A session nobody ends lives until the desk stops
  or the bound above evicts it: there is no timeout, and closing a tab leaves the
  record behind until then.
- **Or the launch secret as a header, for a script.** `Authorization: Bearer
  <launch secret>`, compared in constant time, on `POST /api/session` to mint a
  session or on any gated route directly. That is how the smoke client, the
  acceptance run, the containment gate and every test in `internal/desk`
  authorize — none of them has a cookie jar. A session id presented there is
  looked up in the store instead, so the two never stand in for each other.
- **No session material on any query, anywhere.** The `?token=` parameter this
  chassis authenticated with is **removed**, not deprecated: it authorizes
  nothing, on any route, on any method, and neither does a session id put there.
  A credential on a query is a credential in an address bar, a `Referer`, a
  proxy log, a browser's history and `Response.url` inside the page — and no
  amount of care at the places a URL is forwarded to fixes that; see the model
  relay's query rule below for three ways it leaked out of one of them. One test
  greps the **built bundle** for `?token=`, `secret=` and `/ws?`.
- **An origin check, and it is the CSRF defence — where there is one to make.**
  A request whose `Origin` is not the origin the page was served from is refused
  — **scheme and host both**, and an `Origin` carrying a path, query, fragment
  or userinfo is refused outright rather than matched on its host. It applies to
  every gated request.

  On those routes it is now **defence in depth**: the session is a bearer this
  page holds, so a cross-site page has nothing to send in the first place. A
  request with no `Origin` is accepted there, because a script legitimately
  sends none and a same-origin `GET` sends none either, and neither is
  authorized by anything ambient.

  On the **exchange** it is not defence in depth. That is the one route an
  ambient credential opens, so it carries the Origin guard *and* requires
  `Sec-Fetch-Site: same-origin`, which a browser will not let a page forge.
- **`GET /api/session`** answers `{subject, issuer}` for the bearer the request
  carries, and `401` for a request that carries none. Today the exchange is the
  only thing that mints a session and it writes `local user` and a null issuer.
  Nothing in the page renders it; the record exists so that an identity provider
  has somewhere to write a subject it authenticated.
- **Browser support.** Fetch metadata is required on **one** route, the
  exchange, so what a browser must do is send `Sec-Fetch-Site` on a same-origin
  `POST` — Chrome, Firefox and Safari 16.4+ all do, and nothing else on this
  desk reads it. The WebSocket handshake carries no fetch metadata in Chrome and
  does carry it in Firefox; that was worth measuring and is worth recording, and
  the gate depends on neither, because the upgrade is authorized by the
  subprotocol offer the page makes.

**A cross-origin write is refused twice, and neither layer is load-bearing
alone.** A page on another site cannot send the file API's `PUT` from a browser
at all: the JSON content type makes it a non-simple request, so the browser must
preflight, and the chassis grants no CORS permission whatsoever — that layer is
the browser's. The second is ours: a `PUT` that arrives with a foreign `Origin`
anyway is refused by the same guard, which is what a non-browser client meets.
Both are asserted by tests, so neither can be dropped on the assumption that the
other suffices.

**What these layers are for, and what they are not.** The containment machinery
defends against *confused* requests — a page on another site, a bad path, an
encoded traversal — and against this desk's own bugs; the write protocol defends
honest editors working at the same time. None of it defends against a hostile
local process that already owns the filesystem: such a process can race any
component addressed by name, and every part of this arrangement has one — the
watcher watches by pathname, the runtime is started with a pathname working
directory, and the runtime reads the project by pathname too. Holding a
descriptor makes the file API's own resolution unraceable and stops the two
halves of the desk drifting onto different trees; it does not make the machine
someone else's problem. That boundary is stated here rather than implied by the
absence of a caveat.

It writes to the project only through the file API, only inside the project
root, and only where a request carried a session and an acceptable origin. The
runtime subprocess inherits the project directory as its working directory and
is killed when the socket that started it closes. Identity is display only; the
change that would falsify that is wiring an identity provider — discovery,
JWKS, a redirect — and the PR that does it must amend this paragraph in the
same commit.

### Where the assistant key lives

The paragraph above used to open "the chassis holds no credential and opens no
outbound connection". It now holds exactly one credential and makes exactly one
kind of outbound request, and this section is what that sentence was replaced
with rather than quietly edited around.

**The key is on this machine, in one file, owner-only — and it is stored
together with the destination it was entered for.**

```
~/.config/jpack-desk/secrets/assistant     mode 0600, in a directory of mode 0700
```

```json
{ "assistantKeyVersion": 1, "origin": "https://gw.example", "kind": "gemini", "key": "…" }
```

**The binding is the point of that record.** A key that travelled wherever the
configuration happened to point would be a key page code could redirect by
writing one member of a file — and this desk has a route that writes that
member. So the probe and the relay present the key only where the configured
endpoint's scheme, host and `kind` still equal the ones stored beside it, and
refuse with `assistant-key-unbound` otherwise, sending nothing. Changing the
path or the query keeps the binding; changing the host, the scheme or the wire
protocol breaks it, and the repair is to enter the key again. Storing one
therefore requires an endpoint to bind it to, and a key file **without
`assistantKeyVersion`** — the format this replaces, a bare key — is refused
rather than read: a credential with no binding is the state the record exists
to end, and the sentence names the one action that repairs it.

**Admin's key row reads that binding rather than waiting to meet it.** It says
which host and which protocol the stored key was entered for, offers no entry
field at all where no endpoint is configured — there would be nothing to bind
to — and, where the two disagree, names *both* destinations so a reader can see
which of the pair moved. Neither half is a secret: both are in the file the
page already reads.

**The verdict is this desk's, and the page computes none of its own.**
`GET /api/assistant/key` answers `configuredOrigin` — the origin of the
endpoint the file names now, as *this* desk computes it — and `bound`, which is
the relay's own predicate rather than a second reading of it. The page tried to
work it out and got it wrong in a way nothing on the page could have caught:
the browser's `URL` drops an explicit `:443` where Go's `url.Parse` keeps it, so
a key stored for a host and a configuration naming the same host with its
default port written out read as *stored and bound* while the relay answered
`assistant-key-unbound` and sent nothing. **Two implementations of one rule is
one too many, and the one that decides has to be the one that presents the
credential.**

`XDG_CONFIG_HOME` is honoured where it is set to an absolute path; a relative
one is ignored, as the specification says. The write is staged in the same
directory and renamed over the target, so a reader during a replace sees the
old key or the new one and never half of one.

**That directory is validated once, at startup, and then held as a
descriptor** — the same treatment the project gets, and for a sharper reason.
A pathname is not custody: `os.ReadFile`, `MkdirAll`, `Chmod` and `Rename` all
follow symbolic links and none of them looks at who owns what it lands in, so
in a configuration tree another local user can write to, a preplanted
`secrets/assistant` symlink is enough to make this desk read a file of the
attacker's choosing as the key — and present it to an endpoint the same
attacker named in a preplanted `desk.json`. So:

- **Every component of the path is checked before anything is opened**: a real
  directory rather than a symlink, and not writable by group or others. A
  *sticky* world-writable ancestor is admitted — `/tmp` is the case, and the
  sticky bit is exactly the rule that only an entry's owner may replace it.
- **The desk's own two directories must be owned by the user running the
  desk**, and must not be writable by anyone else. Their ancestors may belong
  to root as well, because `/` and `/home` do on every ordinary system.
  A `0755` directory of ours — what a umask of 022 produces — is narrowed to
  **exactly** `0700`, because nobody else could have written into it. Exactly:
  the setuid, setgid and sticky bits are taken off with the rest, so `02700`
  becomes `0700` rather than being left as it was. None of the three grants
  anyone else access, and none is a reason to refuse a desk — but a sentence
  that names one mode while the code accepts another is a small untruth, and
  the same `chmod` was being issued anyway. One that **is** writable by group
  or others is **refused**, not narrowed: tightening it closes the future and
  can do nothing about what was already planted while it stood open, and what
  may have been planted is a `desk.json` naming an endpoint this desk would
  then present its key to.
- **`desk.json` is held to the key's rules but one.** It must be owned by this
  user, a regular file, not writable by anyone else, and the descriptor opened
  is compared to the entry inspected — because writing that file is choosing
  where a credential goes. It **may** be world-readable: it holds no secret,
  and refusing `0644` would refuse what an editor or a checkout leaves.
- **The validated directory is then pinned**, and every read, write, rename and
  mode change goes through that descriptor with no-follow semantics. The key
  file itself must be a regular file no one else can read; a symlink at its
  name is refused rather than followed.

**A desk that cannot establish that keeps no key, and says so.** The rest of it
runs: packs, rehearsals and graphs are unaffected. Admin › Assistant renders
the refusal, which names the directory that has to be repaired, and every
assistant endpoint answers `409 assistant-unusable-store` with the same
sentence.

**The residual is the file API's residual.** None of this defends against a
hostile local process running *as this user*: it already owns these files and
can replace them whenever it likes. What validation closes is the window in
which *another* user's writable directory redirects an open. That boundary is
stated here rather than implied by the absence of a caveat.

**It is in no project.** Not in `jpack-desk.json`, not in the desk-level
`desk.json`, not in any file the file API can reach — and the configuration
schema has no member it could be written to, at any depth, with a member named
like a credential refusing the whole file by name.

**It never comes back.** No endpoint returns it. `GET /api/assistant/key`
answers `{present, fingerprint}`, where the fingerprint is four characters from
each end and is **empty** for a key shorter than twelve — a key of eight
characters would be disclosed in full by a four-and-four fingerprint, and a
redaction that redacts nothing is worse than none. The page therefore never
holds the key it does not type, and the masked field is never populated from
anything: there is no value to populate it with.

**It is never logged, and neither is a URL that might carry one.** The desk
records that a key was stored and that a probe was made; it records neither the
key nor any fragment of it, and a test asserts the log contains the event and
not the value, so an empty log cannot pass for a clean one. The probe line
carries the endpoint's **scheme and host only**. A configured URL may carry a
query string — some gateways route on one — and a query string is a place
people put credentials, deliberately or by pasting a presigned link, so it is
never written down. Userinfo and a fragment are refused outright when the file
is read: a key is never written into configuration, and that includes into a
URL.

**The probe is made by the desk, not the page**, because the key must not reach
the page in order to be presented to an endpoint:

| | |
| --- | --- |
| `GET /api/desk-config` | the desk-level file's bytes, or that there is none and where it would be |
| `GET /api/assistant/key` | whether a key is stored, and its fingerprint |
| `PUT /api/assistant/key` | store one — non-empty, at most 4 KiB, no control character |
| `DELETE /api/assistant/key` | remove it |
| `POST /api/assistant/probe` | reach the configured endpoint once and report what came back |
| `ANY /api/assistant/relay/v1/…` | carry one model request to the configured endpoint, with the key attached here — and, for the one shape it renders, read the answer before forwarding it |

The probe's four answer `409` with `assistant-unusable-store` where this machine
has no directory safe to keep a credential in, `assistant-unconfigured` where
the desk-level file names no endpoint or was refused, and `assistant-no-key`
where nothing is stored. Each of those is a different repair, which is why they
are three codes and not one.

All six are under the same two checks as `/ws` and the file API, through the
same shared guard, and a request refused by the guard stores nothing — asserted
by its own test, because a handler that stored and then refused would pass
every status assertion.

**The probe request carries no destination.** If it took a URL from its body,
anything holding a session on this desk could point it — and the key it holds
— at a host of its choosing; the destination comes from the file on this
machine instead, so a request body cannot move it. It sends the smallest
request the configured protocol defines — a model listing for
`openai-compatible`, a messages call bounded to one output token for
`anthropic` — waits at most ten seconds, and **follows no redirect**: Go strips
`Authorization` across hosts and knows nothing about `x-api-key`, so a followed
redirect could walk that credential to a host nobody configured.

**A control character in a key is refused wherever it sits**, including at
either edge. The key is presented in a request header and a newline in one is
header injection; only *ordinary* whitespace is trimmed, and only after the
refusal has been decided, so a leading newline is no longer quietly repaired
into an acceptable key.

**A configuration the desk refuses authorises nothing.** The chassis decodes
the whole desk-level file under the same contract the browser decodes it under
— every unknown key refused by name at every level, a credential-shaped member
refused wherever it appears, all four endpoint members required, one URL rule —
and any problem anywhere in that file refuses the probe outright, with **no
outbound request at all**. Before this, the chassis read only
`assistant.endpoint`: a file carrying a stray `apiKey`, a missing `tools` or a
whitespace model showed "configuration refused" on Admin while the probe
happily sent the stored key to the endpoint it named. The two decoders are held
together by fixtures both read — `web/src/config/fixtures/desk-config/`, with
one `expected.json` naming each file's verdict and the keys it must refuse — so
a rule changed on one side and not the other fails on both. There is an accept
and a refuse fixture for **every member either decoder validates**, and each
refused one is probed with a key stored while a counting transport asserts that
nothing at all left the process.

The answer is `{reachable, status, latencyMs, diagnostic}`, where `reachable`
means the endpoint answered *successfully* — a 401 is a host that is there and
a credential it will not take, and calling that reachable would report a desk
that cannot make one call as ready. **`diagnostic` is one word from a closed
list** — `unauthorized`, `forbidden`, `not-found`, `timeout`, `tls`, `refused`,
`dns`, `unexpected-status` — and **never text the endpoint wrote**. The
endpoint's own sentence used to be quoted with the key substituted out of it,
which is a categorical promise held by one string replacement: a body under the
endpoint's control can carry a *derived* representation of the credential —
base64, percent-encoded, JSON-escaped, hex, or half of it — that no
substitution reliably detects. So the body is read to the end and discarded —
to the end, bounded by the same ten-second deadline rather than by a byte
count, because a drain that stops early leaves the connection unusable. The cost
is real and accepted: debugging a misconfigured gateway now means reading that
gateway's own logs. A probe that reaches nothing still answers `200`; the
refusals are the states in which the question cannot be asked at all — no
usable place to keep a key, no endpoint configured (a refused file included),
and no key stored — and each says which.

### The model relay

**The page runs the assistant's loop and the page never holds the key.** Those
two sentences are only compatible if something between them carries the
credential, and it has to be the desk: the key is on this machine and is
returned by no endpoint. So a page-side engine points its provider client at

```
baseURL = <this desk's origin>/api/assistant/relay/v1
```

and sends its model traffic with **no credential at all**. The relay strips
whatever the page did send, attaches the configured key on the configured wire
protocol, and forwards everything else verbatim. It is also what makes the
arrangement possible in a browser: an ordinary bring-your-own endpoint answers
no CORS, so a page calling one directly could not read the answer.

- **The page can choose a destination; the key travels only to the destination
  it was entered for.** This sentence used to be "the destination cannot come
  from the page", and `PUT /api/desk-config` made it false: code holding the
  a session could write an endpoint of its own — same-origin, so the origin
  guard never applied — and then probe or relay and receive the machine-held
  key there.

  The answer is not to withdraw the write. It is that **the key is bound**.
  Storing one records the scheme, host and `kind` of the endpoint configured at
  that instant, beside the key and in the same file; the probe and the relay
  present it only where both still match, and refuse with
  `assistant-key-unbound` (409) otherwise, with **nothing sent**. A path or a
  query may change — that is the endpoint's own routing, and an author edits
  one without changing who is at the other end — but a host, a scheme or a wire
  protocol may not. A configuration write that moves any of those leaves the
  stored key in place and unusable and says so, `keyRebindRequired: true`, so
  the repair is a person entering the key again — which page code cannot do,
  because no endpoint returns the key, nothing in the chassis sends it to the
  browser, and the store endpoint takes a value the page must already hold.
  Storing a key therefore **requires an endpoint to bind it to**, and
  `GET /api/assistant/key` reports the binding beside the fingerprint so a form
  can say which host the key is for.

  The rest is unchanged: the destination is still `configuredEndpoint`, the
  same whole-file decode the probe uses, so a `desk.json` the browser refuses
  authorises no relayed request either. The page chooses a **path suffix** and
  one query pair on one protocol, and nothing else: not the host, not the path
  around it, and not the rest of the query.
- **The suffix is held to a closed class**: one or more segments of
  `[A-Za-z0-9._-]`, no dot segment, no empty segment, at most 256 bytes, and
  **no percent sign** — so the escaped and unescaped readings of an accepted
  suffix are the same string and `%2e%2e%2f` is not a dot segment in a costume.
  Anything else is refused with `assistant-relay-path` and nothing leaves this
  process. The suffix is appended to the configured URL's **escaped** path, so
  `%2F` in a configured base stays one segment.
- **One exception to that class, and it is a shape rather than a character.**
  The **final** segment may be `<name>:<method>` where the method is one of
  `generateContent`, `streamGenerateContent` or `countTokens` — the way the
  native Gemini wire addresses a method, as in
  `v1beta/models/gemini-2.5-pro:streamGenerateContent`. A colon anywhere else —
  including in a segment that is not the last, which the first version of this
  rule accepted and forwarded — a second colon, an empty name, an escaped
  colon, or a method outside those three is refused exactly as before. A method
  is a verb applied to the resource the path names, so there is nothing after
  it. The list is closed because the part after
  the colon is a **verb**: an open one would let whoever holds the session
  session ask the configured endpoint to *do* something nobody wrote down, with
  the stored credential attached, and adding a method is a reviewed change to
  that list. The rule is about the **path** and is not gated on `kind` — the
  kind decides the credential, and a relay that read one to decide the other
  would be two rules where there is one — though in practice only the gemini
  wire writes such a path.
- **The `v1` in that address belongs to this route, not to any endpoint**, which
  is how one mount point serves both protocols: an OpenAI-compatible client
  appends `/chat/completions` and an Anthropic one appends `/v1/messages`, and
  each lands unchanged after the configured base — exactly where the probe
  sends its own request.
- **It forwards the protocol headers, and only those.** The request travels
  with an **allow-list**: `Accept`, `Accept-Encoding`, `Accept-Language`,
  `Content-Type`, `Content-Length`, `User-Agent`, `Anthropic-Version`,
  `Anthropic-Beta`, `OpenAI-Beta`, `OpenAI-Organization`, `OpenAI-Project`, and
  the `X-Stainless-*` family both vendors' generated SDKs attach. Everything
  else is dropped. This was a denylist of credential names and the denylist was
  the defect: "every inbound credential is stripped" cannot be held by a list
  somebody thought of — `X-Auth-Token`, `X-Amz-Security-Token`,
  `Ocp-Apim-Subscription-Key` and whatever a gateway invents next all walked
  through it. With an allow-list the claim is structural, and `Cookie`,
  `Origin` and `Referer` fall out without being named. A page that needs a
  header this list does not carry is a reviewed change to the list.
- **Nothing of the page's query is forwarded, ever.** A relayed request carries
  **no parameter of its own**: every raw pair — any name, any case, any
  encoding, an empty name included, `token` included — is refused with
  `assistant-relay-path` and nothing sent. A literal `;` is refused with it.

  This was a *filter* first, and the filter leaked this desk's session token
  three times, three different ways, to three reviewers: `?%74oken=…` (the
  guard read names with `url.Query`, which percent-decodes, and a raw compare
  did not); `?x=1;token=…&token=…` (Go rejects a pair containing `;`, so the
  guard saw one parameter where a server that still splits on `;` sees two);
  and `?Token=…&token=…` (this desk compared case-sensitively, and ASP.NET
  Core's query parser folds case). Each fix was a better comparison, and each
  time the next parser disagreed somewhere else. **The class existed because
  the query carried a secret at all** — no comparison this desk can write is the
  comparison every parser downstream makes — so it is not filtered, it is
  refused, and refusing is the one rule every parser agrees on because there is
  nothing left for them to disagree about.

  **The class is now gone at its root, and this refusal is what keeps it gone.**
  Since the launch exchange no secret rides on any query anywhere in this
  chassis, so a `token=…` pair on a relayed request is not a credential being
  handled carefully — it is a page sending something nothing asks for, and it is
  refused like any other pair. It was the one exception this rule used to carry,
  and it is not one any more.
- **One pair is the exception, on one kind, byte for byte.** The native Gemini
  wire asks for a server-sent-event stream with a query parameter and has
  nowhere else to put it — it is not a header, and the configured URL cannot
  carry it because the same endpoint serves the unary call too. So for a
  configured `gemini` endpoint the page may send exactly `alt=sse`, and it is
  now the **whole** of what a relayed query may be: the literal seven bytes, at
  most once. (This paragraph said "nine bytes" and `alt=sse` is seven; the count
  was wrong from the first draft and nothing depended on it.) `alt=json`,
  `ALT=sse`,
  `%61lt=sse`, `alt=sse&alt=sse`, `alt=sse&x=1` and `alt=sse;x=1` are each
  refused with `assistant-relay-path` and nothing sent, and the pair is refused
  entirely on the other two kinds, which carry streaming in the request body
  and need none. This is a **closed exception and not a loosening**: byte
  equality against one fixed literal is the one comparison that has no second
  reading, which is precisely what the refusal above exists to guarantee.
  What the request said is settled before anything is read off this machine;
  whether the configured kind admits it is settled as soon as the kind is
  known, before the key is opened, and nothing outbound happens either way.
- **One query reaches the endpoint: the configured one, then that pair** — the
  endpoint's own routing, out of the file on this machine, carried across byte
  for byte and first, with `alt=sse` after it where the page sent one. So **the
  page chooses a path suffix, and one pair on one protocol**, and nothing else.
- **Method and body verbatim**, bounded at 8 MiB — a whole schema, several
  examples and a draft ride in one request — **refused with `too-large`, never
  truncated**. The whole body is read before a byte of it is dispatched, so an
  over-size body of undeclared length reaches the endpoint not at all rather
  than eight mebibytes at a time; the cost is bounded twice, at 8 MiB a request
  and four requests in flight, so at most 32 MiB of request bodies are held at
  once.
- **Streamed, not buffered.** The answer is flushed as it arrives; a test proves
  the page has the first SSE event in hand *before the endpoint has written the
  second*, which a relay that buffered would fail while still delivering both.
- **Bounded in time rather than in bytes**, because a model answer has no length
  worth guessing: **ten minutes** for one whole relayed request, **two minutes
  before the first byte of the answer**, and **two minutes between two writes**
  after it. The first of those two was missing and is the reason this sentence
  now names three numbers: the idle bound was installed once the transport had
  a response, so an endpoint that accepted a request and then sent nothing at
  all — not a header, not a byte — was held by the ten-minute bound instead,
  and four of them exhausted every relay slot for ten minutes. A stream that
  stalls, and a connection that never answers, are both cut rather than left
  holding the page. Writes **to the page** are bounded by the
  same pair — a page that authenticates and then stops reading would otherwise
  hold its slot for ever, since neither deadline ends a write to a client that
  is not listening and this desk's server has no `WriteTimeout` on purpose
  (`/ws` is a socket it holds open for a session). **Every write is capped at
  the overall deadline**; past it exactly one write is allowed, bounded at five
  seconds, so the refusal still lands rather than the page being dropped
  mid-connection. The whole bound is therefore the overall deadline plus five
  seconds, and no idle bound can extend a request past it.
- **At most four relayed requests in flight**, and past that the answer is an
  immediate `assistant-relay-busy` — a bound, deliberately not a queue, because
  a queue reports a wait as latency.
- **No redirect is followed**, for the reason the probe does not follow one: Go
  strips `Authorization` across hosts and knows nothing about `x-api-key`.
- **The answer is forwarded whole** — status, headers and body — minus the
  hop-by-hop headers, minus the credential headers below, and minus
  `Set-Cookie`: the page and this chassis share an origin, so a cookie from the
  endpoint would be stored against the desk and sent back to the desk's own
  endpoints. **No informational response and no trailer is forwarded at all**,
  because both reach the page down paths no filter on this route sees — a 1xx
  through the proxy's own client trace before the answer is inspected, and a
  trailer copied after the body — and nothing either protocol needs arrives in
  either.
- **The model listing goes over it too, and is first-page-only.** Each
  protocol's listing is an ordinary relayed `GET` — `<relay>/models` for an
  `openai-compatible` base ending in `/v1`, `<relay>/v1/models` for
  `anthropic`, `<relay>/v1beta/models` for `gemini` — carrying that protocol's
  credential header and none of the page's. **A page cannot ask for a second
  page**: Gemini's listing pages with `pageToken`, and nothing of the page's
  query is forwarded, so `pageToken=…` is refused with `assistant-relay-path`
  and nothing sent. **Later pages are not supported at all**, and that is the
  whole of it: `pageToken` is refused from the configured URL as well — a
  configured page token is a fixed cursor nobody re-reads, which is not
  pagination — so an endpoint with more models than one page holds shows the
  first page and no more. Supporting the rest would need a mechanism that
  passes a cursor safely, and this release does not have one. Nothing on the
  chassis is added for the listing; it is the relay.

  **The page's half is Admin's List models**, and it goes over the same
  capability an engine gets rather than round it: it names the suffix, and
  `bindModelCall` builds the address and holds the suffix to the rule above. `ModelRequest` carries a `method` for it — a
  closed `'GET' | 'POST'` pair, because the relay forwards the method verbatim
  and an open member would let whoever holds a capability ask the endpoint to
  *do* something nobody wrote down with the machine-held credential attached.
  The three families' answers differ — Gemini's `models[].name` is
  `models/<id>` with a `displayName` and the methods each model supports,
  OpenAI-compatible's is `data[].id`, Anthropic's is `data[].id` with a
  `display_name` — and each is read apart rather than guessed at. Gemini's rows
  are filtered to models whose own declaration includes `generateContent`,
  because that listing carries embedding models an assistant cannot run on.
- **Nothing else.** No retry (a retried model request is a second charge on
  somebody's account for an answer nobody saw), no caching, no request
  rewriting, no model-name inspection. A refusal carries `assistant-relay-*`
  and, for an endpoint that never answered, one word from the probe's closed
  diagnostic vocabulary — never anything the endpoint wrote.
- **The log line is the origin and the status**: no path, no suffix, no header,
  no body, no key.

**An endpoint can hand the key back, and this is the bound on that.** The
credential the desk sends is the endpoint's own, so an endpoint that echoes what
it was sent — a debug gateway, a misconfigured proxy, a hostile one — would
otherwise put the machine-held key straight into the page, which is the single
thing this route exists to prevent. So the **answer's headers are filtered too**:
`Authorization`, `Proxy-Authorization`, `WWW-Authenticate`, `Proxy-Authenticate`,
`X-Api-Key`, `Api-Key`, `X-Goog-Api-Key` and `Set-Cookie` by name, and any header
at all — under a name nobody listed — carrying the configured key in its value.
That last rule has a length in it: a key of **twelve bytes or more** is looked
for anywhere in a value, so `X-Echo: Bearer <key>` goes too; below twelve it is
exact equality only, because a short key is a substring of ordinary text and a
filter that deletes the answer to protect a credential is a worse answer than
the credential. Twelve is the same length below which this desk will not show a
key's fingerprint either.

**One relayed answer is read, and it is the model listing.** The rule above —
the body is carried and never inspected — is a rule about **model traffic**,
which an engine consumes in code. A *listing* is different in kind: it is a set
of strings this desk **renders**, into a picker on Admin, into the page's own
state, and into a field a person can copy from. So an endpoint that reflects
its own credential as a model `id` or a `display_name` would hand the
machine-held key to the browser through the one route whose entire purpose is
that it never gets there — and no filter the *page* could write would help,
because the page has never held the key and could not recognise one.

So for a **listing-shaped** relayed request — a `GET` at the suffix the
configured `kind`'s listing is at, decided from the file on this machine and
never from anything the page said — the relay reads the answer's body **whole,
before a byte of it is forwarded**, bounded at **1 MiB**, and:

- if any JSON string in it **equals** the configured key, or **contains** it
  where the key is twelve bytes or longer, the answer is
  `502 assistant-listing-refused` — *"the endpoint put the credential in its
  model listing; this desk will not list it"* — and **nothing of the body
  travels**, not the id, not the endpoint's own words around it;
- a body **past the bound** answers the same code: a listing this desk cannot
  read to the end is one it cannot say anything about, and forwarding the part
  it did read would be the truncation every other bound here refuses. **The
  read is bounded in time by the same idle deadline every other answer is** —
  it buffers rather than streams, and the wrapper that bounds the rest was once
  applied only after it, so an endpoint that sent one byte here and stalled
  held a relay slot until the overall deadline;
- otherwise the bytes are forwarded verbatim, with the length re-declared from
  what was actually read.

**A listing is forwarded only if it is exactly one JSON value**, decoded end to
end, with nothing behind it. Anything else — plain text, an empty answer, a
truncated document, a second value behind the first, a redirect, malformed JSON
whose escaped credential sits in the half a decoder never reaches — is refused
with the same code and none of it travels. This is the repair for a rule that
read the other way round: a decode error used to fall back to comparing the raw
bytes, so a body the scan *could not read* was forwarded whenever the literal
key bytes happened to be absent, and a scan that cannot read a body cannot
clear it. It costs nothing real — the three protocols' listings are JSON
documents, and an endpoint answering something else at its own listing path has
not answered a listing.

"Exactly one JSON value" is the decoder's own reading and not a shape list: a
bare string, a number, `null` and `true` are each one value and are carried,
and numbers are read as the digits they were written as, so a valid document
carrying `1e1000` is not refused for what Go can hold a float in.

The scan is **exact-and-contains**, on the same twelve-byte floor the answer
headers use, and it compares the **decoded** strings — member names and values,
at any depth, and a number's own digits, because a key of digits is a key —
since a key written into JSON with escapes is one string to a decoder and
different bytes on the wire. **A derived representation — base64,
percent-encoded, hex, half of it — is not detectable by any comparison**, which
is the ruling chunk 1 already took for the probe, and it is stated here rather
than implied away. A listing is asked for
uncompressed so that what is scanned is what was sent. Every status is scanned,
not only a success: a 401's body can carry the credential it rejected as easily
as a 200's can carry it as a model id, and a rule with a status in it is a rule
with a hole in it. The suffix table is
[mirrored on the page](web/src/assistant/modelListing.ts) and held equal to the
chassis' by a test that reads the Go declaration.

**The page does not read a refused listing's body at all — not even its
`code`.** The relay forwards a 4xx verbatim, so the body on a failed listing is
sometimes the desk's envelope and sometimes the endpoint's, and no header tells
them apart. What Admin says is the status and one sentence from a closed list;
on `502` that sentence names both readings without claiming either, and the
desk's own log is where the distinction lives.

**Every other body is not filtered, and that is a decision rather than an oversight.**
The relay parses none of the traffic it carries; a streamed answer cannot be
scrubbed as it passes; and the probe's own ruling already applies — a *derived*
representation of a credential (base64, percent-encoded, hex, half of it) is not
detectable by any substitution, so a filter over bodies would be a categorical
promise held by a `strings.Replace`. An endpoint that writes the key into a *completion*
therefore hands it to the page. **The residual is stated rather than papered
over: the key is the endpoint's own credential, it is presented only to the
endpoint the desk-level file names, and it is good only at the endpoint that
already holds it.** What this route guarantees is that the desk never volunteers
it — not that an endpoint cannot give away a secret it was given. The listing is
the one exception above, and it is an exception because the desk renders it.

The assistant calls it, and nothing else does
([ADR-0001](docs/adr/0001-make-the-assistant-engine-a-slot.md)). The **desk**
builds each relayed address — carrying no parameter but the one closed literal
the rule above admits — and hands the engine a capability rather than a URL, so
no engine chooses a query or an address. See
[Why the engine gets a capability and not a base URL](#why-the-engine-gets-a-capability-and-not-a-base-url).

**The page mirrors this rule, and the mirror is held equal to it by a test that
reads the Go source.** The desk's capability refuses a suffix the relay would
refuse *before the request leaves the page*, because a refusal that only
happened on the far side is a refusal after the fact. It carries the same closed
method list, the same one query pair on the same one kind, and the same segment
class — and three enforcement tests read `relayPathMethods`, `relayStreamPair`
and `relayExtraQueryPair` out of `internal/desk/modelrelay.go` and require the
two to agree in **both** directions. A method on the page's list and not the
chassis' is a call the engine believes it made and the relay refused; one on the
chassis' and not the page's is a capability the desk grants and the page cannot
reach. The wire protocol the pair is admitted on comes from the configured
endpoint where the session is bound, never from an engine: an engine names a
suffix and nothing else.

## Authoring (issue #14, phase 1)

The runtime has **no write tools**, and that is a decision rather than a gap:
ADR-0006 makes it a stateless oracle, so the authoring lifecycle belongs to the
client. The desk is the client, so **the desk owns writes** — through the
chassis, never through the relay, which stays a verbatim pipe.

What phase 1 is, exactly: a chassis file API, and an editor shell at `/author`
that lists the project's files, edits one as text, and saves it. That is all.
There are **no schema forms and no validation wiring** — those are phase 2, and
matrix and rows editing is phase 3. See
[issue #14](https://github.com/Judgment-Pack/judgment-pack-desk/issues/14) for
the whole shape.

**Nothing in the desk judges a document.** The file API moves bytes; it does not
read `jpack.json`, does not care whether a path is a pack, and forms no opinion
about what any file means. Every verdict stays the runtime's, asked for through
the tools every other view already uses, and rendered as the runtime states it.
That includes refusals, with one qualification worth stating precisely rather
than promising: **whether a reviewed-set lock (ADR-0019) refuses depends on what
the desk asked for.** The only evaluation surface here is the what-if view, and
against a runtime that accepts the rehearsal argument it declares one — a
rehearsal consults no reviewed set by design (ADR-0028), so a locked pack does
*not* produce a refusal there. Against an older runtime with no rehearsal
argument, the same view makes an ordinary evaluation and a lock refusal appears
verbatim. Either way the desk never offers to update a lock and never routes
around one. See "The lock, and what phase 1 does not do" below.

### The file API

Three endpoints, proxied alongside `/launch` and `/ws` by the dev server, under
the same two checks as `/ws` — the session first, then the Origin — through one
shared guard, because a new endpoint is a new place to forget one:

| | |
| --- | --- |
| `GET /api/files` | the project's regular files, with sizes and digests |
| `GET /api/file?path=…` | one file's bytes |
| `PUT /api/file` | replace one file's bytes, optionally creating its parents |

**Containment is a held directory descriptor, not a path check.** The chassis
opens the project directory once with `os.Root` when it starts and closes it
with the server; every list, read, stat, staging write and rename goes through
that handle. (On the desktop platforms this desk targets that handle is a real
directory descriptor. Go documents `os.Root` as falling back to pathname
resolution where the syscalls do not exist — Plan 9 and js/wasm — and the
guarantee is correspondingly weaker there; the desk is not built for either.) This matters for two reasons a string check cannot address:

- A pathname that is validated and then opened is checked against one
  filesystem and opened against another. Replace an approved ancestor directory
  with a symlink in between and the open follows it — no amount of resolving
  beforehand prevents that. `os.Root` resolves each component against the held
  descriptor, so the thing checked is the thing opened. There are tests that
  perform exactly this swap, on `GET`, on `PUT`, and on the listing.
- Pinning it *once* is the other half. Re-resolving the project path per request
  would let the authority itself be retargeted — rename the directory, or
  repoint the symlink it was reached through, and later requests would adopt a
  different tree without racing anything. A test repoints a symlinked root after
  startup and asserts the desk keeps serving the tree it was given.

*One version-dependent trap, named because it bit:* the `fs.DirEntry` that
`Root.FS()` yields resolves `Info()` by **pathname** on Go 1.25 and by
descriptor on 1.26, and on a filesystem that does not report entry types in the
directory block it lstats by pathname to classify at all. So the listing does
not use `Root.FS()` — depending on which toolchain is underneath to hold a
containment property is not a property.

The listing walks directory descriptor by directory descriptor: it opens each
directory through the pinned root, reads names in bounded batches, and
classifies each child with `Root.Lstat` — `Lstat`, so a symlink is seen as one
rather than followed.

It is also **bounded**, because a tree can be adversarial without anyone being
hostile: a bind mount or a directory hard link makes a tree contain itself with
no symlink in it, and a depth cap alone does not save you — two aliases per level
and the work doubles. Each opened directory's identity is compared with the
directories open above it (`os.SameFile`), and there is a total entry budget.
The watcher's traversal carries the same bounds.

Anything the walk could not read — an unreadable subtree, a repeated ancestor, a
budget reached — is reported in a `partial` member and named in the note. A
thinned answer that still returned a bare `200` would be indistinguishable from
a smaller project, so the editor renders `partial` prominently and, when it is
present, never says the project is empty.

A lexical check runs first and is tested on its own. It refuses **escaping**
`..` (an interior `a/../b` normalises and is fine), absolute paths, drive and
UNC forms, NUL, and **backslash anywhere** — on Windows that is
a separator, so `..\secret` is a traversal slash-only cleaning does not see, and
refusing it everywhere removes the platform difference from the argument rather
than reasoning about it. The two layers are independent rather than nested — `os.Root` refuses escapes
this one never sees, and this one refuses spellings (a colon, a backslash) that
Unix `os.Root` would happily treat as a filename. Each is kept because a change
to the other should not silently become the only thing standing.

*Windows caveat, stated because it cannot be tested here:* reserved device names
and trailing-dot or trailing-space aliases are refused by the filesystem layer
rather than by the lexical one, and this project's CI runs Linux only.

**What is readable and writable.** Any path inside the root **except**: the
directories the watcher also ignores — `.git`, `node_modules`, `dist`, `.venv`,
`vendor` — and this desk's own `.jpack-desk-*` staging files. Those are refused
by the *endpoints*, not merely omitted from the listing, and a path with such a
component is refused on `GET` and `PUT` alike. **Symlinks are not documents
here**: a path any component of which is a symlink is refused by both verbs,
because a read follows a link while a save renames over it — one name, two
objects, and an editor showing you one while the save replaced the other. Those exclusions
are reported in the listing's `excluded` member rather than left to be inferred.
Non-regular files are excluded too: a symlink is not listed and not readable, a
FIFO or device is refused on open (with `O_NONBLOCK`, so a FIFO cannot hang the
handler before the check runs). A file too large to read is **listed with an
empty digest** rather than hidden — it is really there — and refused by the read
endpoint. That empty digest means exactly one thing; a file the listing could not
read for any *other* reason is named in `partial` instead, so "too large" is
never said about a permission error. Reads and writes are refused alike for every one of these: an API that
reads and writes a path by different rules is one nobody can reason about.

Otherwise it is the user's own files on the user's own machine, and this API is
their hand, not a policy layer. It does not consult `jpack.json` and forms no
opinion about what any file is.

**Writing requires an existing directory, unless the write asks otherwise.** A
`PUT` whose parent directory is not there answers `404` naming it, rather than
reporting a containment failure — a missing directory is not an escape. The
conflict check runs first, so a write that also carries a stale `baseSha256`
gets its `409` before that `404`: the `404` is what a believed-new or
overriding write receives.

**`createParents` is the opt-in.** A request carrying `"createParents": true`
has the missing directories of its path made before the file is written; with
the member absent or false nothing is created and the `404` above is what comes
back. It exists because the desk decides a new pack's location from
configuration, so the Create-pack dialog can name `packs/…` in a project that
has no `packs/` yet.

Containment is not extended by it — a fourth verb is added to the handle that
already carries the other three. `Root.MkdirAll` resolves each component against
the same pinned `*os.Root`, so the thing checked is the thing created. It runs
**after** the lexical check (so a directory this API would refuse to write a
file into is one it will not create), **after** the symlink refusal — whose walk
stops at the first component that does not exist, which is exactly where
`MkdirAll` begins, so a symlinked parent is refused before one directory exists
— and **after** the stale-digest check inside the write lock, so a write refused
as stale creates nothing. A parent that is a regular file is refused with its
own code and its own sentence: opening through it is `ENOTDIR`, which is a
naming problem a rename fixes and **not** a containment failure. It used to be
answered as "path is not inside the project", which sent whoever read it
hunting a security problem that is not there.

**What the symlink walk promises, and what it does not.** Containment does not
rest on it: every operation past it goes through the pinned `os.Root`, so a
component swapped for a link pointing *out* of the project — at any moment,
including after the walk has passed — is refused by the root itself. There is a
test that performs exactly that swap after the walk and asserts nothing left the
project.

What the walk adds is the desk's own stricter rule, that it edits no path
passing through a link even one that stays inside the project. That rule is
checked in the walk and enforced nowhere else, so it is time-of-check to
time-of-use against a local writer racing it: an **inward** symlink introduced
between the walk and the write is followed, and the bytes land at the link's
target — still inside the project, still nameable by the listing, and not where
the caller asked. That residual is asserted by a test rather than papered over,
and it is scoped rather than closed: the threat model is a writer who already
has write access to the project directory and does not need to race anything to
move a file the desk just wrote.

**Bounded by what the listing can report, whether or not anything is created.**
A write past the walk's depth limit is refused with a `400`, and the check runs
**before** the "the parent is already there" fast path — it used to sit inside
the create-the-parent branch, so a write into 65 directories that already
existed succeeded. `GET /api/files` gives up at that depth and reports the tree
as partial, so a write allowed past it lands a file this API's own listing can
never name, and there is no delete verb to take it back. An existing parent does
not make that file findable. `storage.packs.dir` is refused at decode for the
same bound, so the configuration names the problem rather than the dialog
failing on the write.

**There is no unwind.** If the directories are created and the write then fails,
an empty directory is left behind. Removing it would need a delete verb this API
does not have, applied to a directory another process may have populated in the
interval; an empty directory is inert and the next attempt uses it.

**Modes come from the umask, and this API has no opinion of its own.** A created
directory is `0o777` masked by the umask and a created *file* is `0o666` masked
by the same umask — the ordinary convention in the user's own project, which is
committed to their repository and read by their other tools. A file the desk
brought into existence used to keep the staging file's `0o600`, so one request
made a world-readable directory holding an owner-only document; the staging file
is now created `0o666` and the kernel applies the umask. A file that already
exists still keeps its own mode across a save, so a document somebody narrowed
on purpose is not opened up by editing it.

**Atomic replace, scoped honestly.** The bytes are staged in the target's own
directory through the same pinned root — rename is atomic only within a
filesystem — the mode is set, the data is flushed, the file is closed, the
rename happens, and then a directory sync is **attempted** — best effort, and a
failure there is not reported, because it cannot undo a write that has already
landed. The staging file is written through the descriptor it was created with
and never reopened by name.

- *On Unix*, `rename(2)` replaces the directory entry atomically, so a concurrent
  reader sees the old file or the new one and never a truncated one. Only the
  POSIX rwx bits are carried across; owner, group, ACLs, extended attributes and
  the inode identity are not, because a replace is a new file by construction.
- *On other platforms*, Go promises no such atomicity and neither does this. What
  they get is `os.Root`'s rename semantics and nothing stronger claimed.
- *This is not crash durability.* Data before the rename and the directory after
  it is the usual recipe, and power loss, a lying disk cache, or a filesystem
  with its own ordering can still lose the write.

A crash between staging and rename leaves a staging file. It is excluded from the
listing and from the watcher, cannot be read or written through the API, and the
server removes stale ones at startup — **unconditionally**, and only files
bearing this desk's reserved prefix and suffix. That startup sweep skips the
excluded directories, which is consistent because nothing may be written into
them through the API either.

**The write answers with a read-back** taken off the disk after the rename rather
than echoing the request, so the client can verify what actually landed. The
editor compares it to the bytes it *submitted* — captured with the request, not
read from the live buffer — and says "saved, and verified" only when they match.

**Concurrency: a conditional commit, and one honest residual.** A write carries
`baseSha256`, the digest of the bytes the editor loaded. The current-bytes read,
the comparison, the rename and the read-back all happen under **one server-wide
write mutex**, so the check and the commit are one decision: two writes from the same base
produce exactly one `200` and one `409`, never two `200`s where the second
silently discards the first. The `409` carries both digests and `exists`, so the
client can say what it had, what is there, and whether the file was changed or
deleted. `override` writes anyway — the user's deliberate choice, never a
default.

That serialization covers writers **through this API**. It cannot cover an
editor, a `git checkout`, or any other process; nothing in a single chassis can.
For those the file watcher is the mitigation and not a guarantee: it notices the
change and the page says the file moved underneath the edit, but a write that
lands between this API's read and its rename is not serialized with. The `409`
is what makes the common case honest; the watcher is what makes the uncommon one
visible.

**The editor never rebases an open edit.** The watcher makes the desk invalidate
every query on a file change, so the bytes an edit is measured against are held
by the editor and replaced only when the user acts — an initial load, an explicit
reload, a successful save. A base taken from the live query would silently move
to bytes the user never saw, and `Save` would overwrite them with no `409` at
all. A file deleted underneath an open edit keeps the editor and the buffer, and
says so.

### The lock, and what phase 1 does not do

A project can carry a reviewed-set lock (runtime ADR-0019). Phase 1 **neither
interprets nor regenerates** one: the desk has no lock parser, writes no lock,
and offers nothing that would update one.

Two consequences worth stating plainly rather than implying otherwise:

- **Whether a lock refusal surfaces depends on the connected runtime.** The
  only evaluation surface in the desk is the what-if view. Against a runtime
  that advertises the rehearsal argument the desk declares one, and a rehearsal
  consults no reviewed set by design (ADR-0028) — so editing a locked pack and
  rehearsing it will *not* produce a lock refusal there. Against an older
  runtime with no such argument the same view makes an ordinary evaluation, and
  a lock refusal appears verbatim. Phase 3 runs matrices from disk, which is
  where a lock's answer will appear regardless of that distinction.
- **The editor can edit the lock file.** `jpack.lock.json` is a file in the
  project, and this API has no list of files that are special. Editing it is
  possible, it is the user's own file, and it is stated here as a fact rather
  than presented as a feature — the runtime remains the only thing that decides
  what a lock means.

## How the relay works

- One WebSocket connection spawns one `jpack mcp` subprocess with `cwd` set to
  the project directory.
- Bytes cross verbatim: one JSON-RPC message per WebSocket text frame on the
  browser side, newline-delimited JSON on the stdio side.
- Closing the socket kills the subprocess.
- One message originates in the chassis rather than the runtime: a file watcher
  over the project tree sends a `desk/fileChanged` JSON-RPC notification
  carrying the changed path, and the page invalidates its caches on it.
- A dropped socket is reconnected by the page rather than reported and left. The
  delay doubles from 500 ms up to a 15 s cap, with jitter, and each attempt
  builds a fresh MCP client — one that has closed already negotiated with a
  server that is gone. A successful reconnect invalidates every query, because
  whatever the project did while the socket was down arrived as
  `desk/fileChanged` notifications nobody heard.

## Layout

```
main.go              flags, embedded assets, HTTP server
internal/desk/
  server.go          routing, SPA fallback, session and origin checks
  session.go         the launch handoff, the exchange, the session store
  files.go           the file API: containment, atomic save, stale-write refusal
  assistant.go       the desk-level file, the key this machine keeps, the probe
  custody.go         the credential directory: validated once, then pinned
  deskfile.go        the desk-level file decoded under the browser's contract
  relay.go           WebSocket ↔ `jpack mcp` subprocess
  modelrelay.go      the model relay: the page's traffic, this machine's key
  watch.go           project-tree file watching
scripts/acceptance.sh  the two-run acceptance proof
web/                 Vite + React + TypeScript SPA
  src/mcp/           the MCP client: transport, connection, queries, the
                     advertised-capability reader, the canonical-string,
                     probe-name and graph-document readers, and the ledger of
                     divergent digest pairs already asked about
  src/files/         the chassis file API: the client, its query hooks, and
                     the save discipline both editors hold — the base that
                     moves only where the viewer acts, the reload that is a
                     direct read, and the read-back compared to what was sent
  src/routes/        project home, the packs layout and its two children
                     (the "select a pack" page and the pack document),
                     evaluation, matrix, graphs, the authoring shell, the
                     Admin page and Help & About — Admin being two groups of
                     cards in one shape, one group per configuration file, of
                     which two write the desk-level one: the project group's
                     default project and the Assistant form's endpoint, model
                     and tier
  src/components/    evaluation, coverage, row and graph-walk views, plus the
                     trace and handoff-target renderers both the pack and graph
                     surfaces share
  src/shell/         the six regions, the pane state and its per-project
                     record, this viewer's own appearance and the ladder that
                     resolves it, the published shortcut list, the dirty guards
                     both editors use — a `beforeunload` listener and a router
                     blocker whose predicate is the pathname alone — the icon
                     set, the console's
                     ring buffer, the fragment-scrolling hook the section menus
                     need, and the Create-pack dialog — which asks for a name,
                     a description and a template, offers Describe it beside
                     them, and decides the file's location from configuration
  src/ui/            the styled primitives: Button, Field, Input, TextArea,
                     Select, Tabs, Dialog and Alert, plus the editor's own —
                     SegmentedControl, Toolbar, CodeArea, SuggestInput and
                     AlertPanel — one CSS module each (see Styling)
  src/packs/         the pack surface: the RFC 6901 pointer module, the
                     span-preserving document writer, the validate reader, the
                     cross-reference reader, the packs pane and its windowing
                     hook, the scroll-spy — and what a new pack is called and
                     where it goes (the slug rule, the one shaping a template
                     and a proposal both take, and the jpack.json amendment)
    __fixtures__/    the documents every case here is asserted against, each
                     one the runtime accepts, held to the spec's own enums by
                     a test
    document/        the reading document: the member order the schema
                     declares, the block wrapper that carries every pointer,
                     the roving tab stop and its key handling, the
                     omitted-member line, the unparaphrased condition tree and
                     one component per member kind
    inspector/       the three panels: Member, References, Checks
    edit/            edit mode: the `?edit` helpers, the editing context, the
                     shapes mirrored from the schema, the one place a form edit
                     becomes text, the buffer with its undo and its discard,
                     the toolbar, the JSON view, the pointer-addressed field
                     wrapper and the field kinds, the condition builder and its
                     text operations, the rule and exception forms with their
                     keyboard reordering, the check on idle, the what-if pane,
                     the stale-write alert and the lock line
  src/admin/         the card every Admin section renders through, the member
                     slicer that quotes a file rather than re-serialising it,
                     the narration sweep both this page and the assistant form
                     are held to, and the one control that nominates this
                     project as the default
  src/config/        the schema both configuration files share, its strict
                     decoder, the two queries that read them, the precedence
                     between them, and the theme attribute — written from
                     `src/shell`, where the viewer's own preference is resolved
                     against the file's default
    fixtures/        the desk-configuration fixtures and their one verdict
                     file, read by this decoder and by the chassis' — two
                     implementations of one contract, held together
  src/identity/      the identity slot: one nullable field, and the header
                     control that renders it
  src/assistant/     the assistant slot: one nullable field and two settings
                     about how it runs, the four chassis calls, the Admin
                     section — a form over the endpoint, the tool grants, the
                     model, the engine and the tier, with the key row beside it
                     reading the desk's binding and the endpoint's own model
                     listing read through the relay — the tab, the event list
                     and proposal report both surfaces render, the ToolGate,
                     and engines/ — one lazily loaded chunk per certified
                     engine behind one contract
  scripts/smoke.ts   the desk's own client, driven outside a browser
```

## Styling

Four rules and one test that holds all four
(`web/src/ui/convention.test.ts`, which reads the source because vitest runs
with `css: false` and a component whose stylesheet was deleted renders exactly
like one whose stylesheet is intact). A second test —
`web/src/ui/palette.test.ts` — reads the two *global* sheets on the same terms,
and holds the palettes and the density scale that live in them. A third —
`web/src/ui/containingBlock.test.ts` — reads every `.css` under `web/src`, by
extension and not by name, and holds one pair: a rule that authors a scrolling
overflow also positions itself, so a scroll container is a containing block.
The frame is in the swept set for its deliberate clip; a rule that merely clips
is not. It is a sweep and not a list of names, so the scroller nobody has
written yet is held by it too. It holds the declaring rules only; whether a
later rule takes a position back is measured by
`scripts/containment-check.sh`. It reads source and says so, and the two
halves are named in its docstring.

**Three of them run over every `*.module.css` under `web/src`**, and one — the
component/module pairing — stays scoped to `src/ui`. The split is the point. The
pairing is a claim about how the *primitives* are built. Colour, radius and
bare-selector are not: a hex in `packs/PacksPane.module.css` is exactly the
second palette a hex in `ui/Button.module.css` would be, and a bare `li { … }`
there is just as global. Until this widening those three simply did not reach a
module written anywhere else.

- **The tokens in `styles.css` are the only source of colour and radius.** A
  module spells no colour of its own — no hex, no `rgb()`, no `hsl()`, and no
  named colour either — and no radius of its own: `--radius` and `--radius-sm`,
  never `4px`. A second palette is one the theme attribute does not reach; a
  literal radius is a second answer to a question the tokens already answer. The
  modal scrim is `--overlay` for the same reason.

  **And the rule reaches the two global sheets as well as the modules.** It did
  not, and `shell.css` was spelling three colours of its own — `#fff` on the
  Create button, the drawer scrim, and the menu's shadow — while `styles.css`
  spelled `#fbfbf9` in two rules outside its own token blocks. Every one of
  them was a colour the theme attribute could not reach, which is a defect a
  light-only desk had no way of displaying. `web/src/ui/palette.test.ts` asks
  the stricter question a global sheet needs: not "did this declaration take
  its colour from a token" but "does this sheet *spell* a colour anywhere" —
  in any property, including in a custom property of its own — with the token
  blocks of `styles.css` cut out first, because inside them a literal is the
  palette. `border: 1px solid transparent` is left alone, which is why it is a
  different rule and not the modules' one widened.

  The colour half is checked by **parsing** each sheet's declarations rather
  than matching property names, and the difference is not academic: the rule
  this replaced matched `border`/`outline` declarations and then skipped them,
  because it asked whether the property name contained "background" or "color".
  `border: 1px solid red`, `outline: 2px solid red` and a named colour in a
  `box-shadow` all passed the rule that exists to catch them. Every shorthand
  that admits a `<color>` is checked now, every `-color` longhand with it, and
  the named colours are checked as names — and the parser is itself proven
  against deliberately broken fixtures, because a rule that only ever sees clean
  sheets proves nothing about the rule.
- **A component under `src/ui/` owns its own `X.module.css`, and no other sheet
  styles it.** One component, one module, no orphan of either.
- **`shell.css` owns the five regions' layout and nothing a component renders.**
  No module touches `--rail-current`, `--inspector-current`, `--console-current`
  or `grid-template-areas`. A route that needs a region's measurement reads a
  token: `--main-room` is the height between the header and the strip less
  whatever the console is taking, which is what the packs pane needs to be
  sticky rather than a second scroll region. It is defined beside the geometry
  it is made of, with its `dvh` value behind `@supports` for the reason the
  sheet already states — a custom property is not validated at parse time, so a
  second declaration would win and fail later at substitution.
- **No component under `src/ui/` carries an inline style.** An inline style
  beats every sheet without `!important` and cannot be themed, which makes it
  the one way a component can quietly opt out of the tokens. Four files outside
  `src/ui/` do carry one, and each writes a value only the running page knows:
  `shell/AppShell.tsx` and `shell/RightPane.tsx` set custom properties the
  sheets then read (`--rail-current`, `--drawer-w`),
  `components/GraphWalkDiagram.tsx` sets a max-width measured from the
  container, and `packs/PacksPane.tsx` sets the heights of the two spacers a
  windowed list reserves for the rows it is not rendering. Naming them is what
  keeps the rule above true rather than approximately true.

Only class selectors appear at a module's top level. A bare element selector
inside a CSS module is **not** hashed — it is global — so one `button { … }`
there would restyle every button in the desk.

Modules need no cascade layer of their own. They are unlayered author rules, so
they beat every `@layer shell` rule by construction, and their class names are
hashed at build time so they cannot collide with the ~60 names `styles.css`
already owns. That is why there is no import-order rule in `main.tsx` to
remember and no layer to keep in sync.

The Create-pack dialog and the pack surface are built from these primitives
today — `Button`, `Field`, `Input`, `TextArea`, `Select`, `Tabs`, `Dialog` and
`Alert`. The other views keep the sheets they have; migrating them is its own
piece of work.

## Tests

```sh
go test ./...
```

The relay's end-to-end tests need a runtime and a project, and skip without
them:

```sh
go build -C /path/to/judgment-pack-runtime -o "$PWD/bin/jpack" ./cmd/jpack
JPACK_PROJECT=/path/to/project go test ./...
```

`JPACK_BIN` overrides the runtime binary; otherwise `./bin/jpack` is used when
present.

The component tests run under vitest against fixture payloads shaped like the
wire's own:

```sh
npm --prefix web test
```

They are where a rendering rule fails a test rather than surviving as a habit —
that a verdict the runtime did not state is never painted, that a served graph
document is read and never repaired, and that each fallback says exactly as much
as it should about why it is one.

The editor's own suites are named after the claim each holds.
`packs/documentText.test.ts` and `packs/edit/writes.test.ts` hold the splices:
every byte outside a touched span identical, a blanked `nonEmptyString` removed
rather than emptied, and a member that is not there yet written at the position
the schema gives it — behind **a neighbour's own leading run, reused verbatim**.
That run is copied and never invented, and it is never used to reformat a member
that was already there: an insertion changes the bytes it inserts and nothing
else, which is why a document indented with tabs stays indented with tabs and a
document nobody indented stays on one line.
`packs/edit/shape.test.ts` holds the mirrored schema against the fixtures'
own values. `packs/edit/useDocumentBuffer.test.ts` holds dirty as a byte
comparison, one undo entry per action, and a discard that clears the last
attempt's verdict. `packs/edit/conditionOps.test.ts` and
`ConditionBuilder.test.tsx` hold the builder: the operand control switching on
the operator, an ordered comparison emitting a *string*, an empty `in` and an
unquoted number both writable, and an unrecognised kind printed rather than
edited. `packs/edit/EditView.test.tsx` holds the one buffer — a JSON keystroke
moving the reading document, a form edit moving the bytes, form mode withheld
over a document the two readings disagree about, a diagnostic reaching its
field by `aria-describedby`, and a rule reorder that marks the check stale
rather than re-anchoring it. `packs/edit/navigate.test.tsx` holds the buffer
following the address on the route that does not remount — the second pack's
members drawn, the second pack's bytes, digest and path sent, and nothing of a
discarded edit left on the pack that follows it. `packs/edit/forms.test.tsx`
holds what a form does about a member that is not there and about text it has
not written. `packs/edit/resilience.test.tsx` holds the states this editor can
itself produce: a list pasted as an object, a pack the runtime will not serve,
`Mod+S` from `document.body`, the move chord fired twice from one card, the
what-if placement measured off the frame, and the Inspector's provenance while
the buffer is dirty. `packs/edit/save.test.tsx` holds the save: not
gated on the check, 409 with both digests whole and a non-primary Overwrite,
`Mod+S` inside the field the shell suppresses in, and a `?edit` toggle that
never prompts. `packs/edit/TryItPane.test.tsx` holds `pack` XOR `pack_id`, the
rehearsal declaration exactly where advertised, the explicit click where it is
not, a confirmation that does not outlive the bytes it confirmed, and a refusal
rendered with no disposition. `packs/edit/lockLine.test.tsx`
holds the one sentence, and that no lock, conformance, health or pass/fail word
appears anywhere on the page. `files/useFileEditing.test.ts` and
`shell/useDirtyGuard.test.tsx` hold the discipline lifted out of the authoring
view — `routes/AuthorView.test.tsx` staying green unedited is the proof the
extraction preserved its behaviour.

The assistant's suites are named the same way. `assistant/toolGate.test.ts`
holds the gate at a **recording transport**: what left the page, never what the
gate believes it did. `assistant/conformance/conformance.test.ts` is the desk's
conformance session, described above, and is the one that certifies an engine.
`assistant/engines/builtin/engine.test.ts` holds the handful of properties that
session only exercises incidentally — the request's headers and URL, the turn
bound, `end` exactly once, and a proposal taken from the fenced block rather
than from the prose beside it. `assistant/engines/vercel/engine.test.ts` holds
the ones that are about **that SDK** rather than about the contract: the address
discipline on the `fetch` its providers are given, the placeholder credential
that never leaves it, the two layers that put `rehearsal: true` on an evaluate
and which of them the desk's gate must see, the whole-answer re-framing, the
retry count, and — for the one rejection this desk cannot claim — that **the
author is told anyway**, on the run's own stream, with the status and the
endpoint's own sentence in it. There is no `unhandledrejection` listener, by
ruling: one is keyed on an error *name* and would suppress every rejection
carrying it. `assistant/AssistantPane.test.tsx` drives the
page's **real** transport against the recorded runtime through a stand-in
`WebSocket`, so the socket, the gate and the SDK client above it are the
production ones.

`scripts/needle-check.sh .` says whether every mutation needle still matches its
file, exactly once. A row whose needle has drifted reports `MUTATION DID NOT
APPLY` and is silently dead, and a full pass takes long enough that nobody finds
out until a review does — which is how a re-indentation in one PR left a row
from an earlier one broken. It applies nothing and runs no suite, and it is not
a substitute for running the rows.

`scripts/containment-check.sh` measures, in a real browser, the half of "every
scroll container is a containing block" that no reader of source can hold.
`web/src/ui/containingBlock.test.ts` holds the *declaring rules* and says in its
own docstring that it stops there: an override that reaches a pane by any other
selector — an ancestor in front of it, an id, an attribute, a nested `&`, a
`:global`, an inline style — is a computed cascade, not a sentence in a sheet.
This loads a built chassis in Chrome and reads what the cascade produced, over
every route `App.tsx` declares — the list is asserted against that file at run
time, so a route added later fails the gate until it is sampled — at every
width the sheets author a breakpoint for: `{1400, 640}`, plus `N − 1` for each
`max-width: N` and `N` for each `min-width: N` read out of an `@media` prelude
under `web/src`, each at height 800. Four pane configurations above 1099px and
three at or below it, because below that the Inspector is a modal drawer whose
overlay owns the pointer, so the console cannot be toggled while it is open:
242 rows a build. The intended row count is computed before any sampling and
checked against the rows afterwards, and each configuration is observed on the
page it claims to configure.
A row is contained only if `document.scrollingElement.scrollHeight` equals
`innerHeight`, `.desk` is exactly `innerHeight` tall, `scrollY` is 0 after
`window.scrollTo(0, 5000)`, the computed `position` of `.desk`, `.desk-rail`,
`.desk-main`, `.desk-inspector` and `.desk-console` is `relative` wherever the
route renders them, no absolutely positioned element resolves its
`offsetParent` to `BODY`, and no page or console error was raised while it was
sampled. It prints a table and exits non-zero on any row that fails.

```sh
npm --prefix web ci && npm --prefix web run build
go build -o /tmp/jpack-desk .
JPACK_BIN=/path/to/jpack scripts/containment-check.sh /tmp/jpack-desk /path/to/project 8765
```

The project must list at least one pack and one graph: four of the eleven routes
are a pack's and one is a graph's, and the gate exits 2 rather than sample fewer.
`judgment-pack-demo/projects/enterprise-demo` is one it runs on; a project with
packs and no graphs is not. `node scripts/containment-check.mjs --plan` prints what
a run would sample — the preludes read and the preludes refused, the widths, the
routes, the intended row count — without a browser.

It copies the project rather than driving the one it was handed, uses a
throwaway `XDG_CONFIG_HOME`, and kills what it starts by PID. A project that
lists no pack, or no graph, exits 2 saying so: four of the routes are a pack
and one is a graph, and neither id can be spelt without the project. `PLAYWRIGHT_CHROME`
names a Chrome executable; without it, `playwright-core` — a devDependency of
`web/`, which downloads no browser — is asked for the installed one. CI supplies
no runtime binary and no project, so there is nothing for the chassis to serve;
the gate is run by hand. Run before every merge that touches a stylesheet. This
is a convention; nothing automated enforces it.

CI runs `gofmt`, `go vet` and `go test` on one job and `npm ci`, `tsc`, the
component tests and `vite build` on another. It supplies neither a runtime binary
nor a project, so the end-to-end tests skip themselves there and what runs is the
coverage that needs nothing external.

### The acceptance proof

Two real evaluations through the relay, against the same pack: one with the
project's full facts, one with a load-bearing fact removed. The first should
resolve to an outcome; the second should escalate. Then the rows the project
declares about itself — every pack matrix, and every configured graph matrix.

```sh
go build -C /path/to/judgment-pack-runtime -o "$PWD/bin/jpack" ./cmd/jpack
JPACK_PROJECT=/path/to/judgment-pack-quickstart scripts/acceptance.sh
```

It builds the chassis, copies the project to a temporary directory — a completed
evaluation appends a record in a project that declares an audit directory, and
an acceptance run must not write into the tree it was pointed at — reads the
launch URL off the `open:` line of the chassis' startup output, splits it into an
origin and a launch secret, and drives the desk's client three times. The secret
travels to the client as `JPACK_DESK_SECRET` rather than on a URL, and the client
presents it as `Authorization: Bearer`. `MUTATE` is the jq expression that removes the fact,
and defaults to the quickstart pack's `/request/completeness`. `PACK` selects
the decision id where the project's first is not the one `FACTS` suits.

The matrix runs need none of that care — a row is a rehearsal and writes
nothing — but they run against the same copy anyway, so one run means one
project. `EXPECT_MATRIX_STATUS` defaults to `passed`; `EXPECT_GRAPH_STATUS` is
checked only when set, because a project that configures no graph correctly
reports `skipped`.

Setting `GRAPH_DOCUMENT` to a configured graph id adds the graph-serving pair to
that third run, and `GRAPH_FILE` names that graph's document relative to the
project so the served text is compared against the file byte for byte. Both are
unset by default: a runtime that predates ADR-0029 advertises neither tool, and
the step refuses rather than passing quietly. `GRAPH_FILE` on its own is refused
before anything runs, because it names the half of a check that only happens
when `GRAPH_DOCUMENT` says which graph to fetch — and an acceptance run must not
report green for a check that never ran.

The same client runs on its own against a chassis you already have open:

The origin and the launch secret are separate arguments: the secret is a
credential, and a credential does not ride on a URL. `--secret <secret>` or
`JPACK_DESK_SECRET` supplies it, and the client sends it as
`Authorization: Bearer` — on the `/ws` upgrade too, which is how a script
authorizes where a browser would have a cookie.

```sh
export JPACK_DESK_SECRET='the value after ?secret= on the printed URL'

npm --prefix web run smoke -- http://127.0.0.1:8791 \
  --facts /path/to/full-facts.json --evidence /path/to/evidence.json

# the two calls the matrix and graph views make
npm --prefix web run smoke -- http://127.0.0.1:8791 --matrix --graphs

# the graph-serving pair the walk diagram draws its edges from (ADR-0029):
# the inventory, then one document by its configured id, checked byte for byte
# against its own metadata and against the file on disk
npm --prefix web run smoke -- http://127.0.0.1:8791 \
  --graph-document vendor-onboarding-flow \
  --graph-file /path/to/project/graphs/vendor-onboarding.graph.json
```

`--graph-document` fails rather than skipping where the connected runtime does
not advertise those tools: asking for the step is asking for the check. Against
a runtime that has neither, the fallback is what the other flags already
exercise.

**The graph leg runs without being asked for**, and there the same absence is a
skip rather than a failure — naming no graph asks what this runtime can tell you
about its graphs, and "nothing" is an answer a runtime is entitled to give. Where
both tools are advertised it lists the configured graphs and, **where the
project configures one**, fetches its document, checks the served text against
its own `bytes` and `sha256`, and then checks the **binding** (ADR-0030): the
`graphSha256` the matrix run decoded against the digest served beside the
document. Equal proves the two calls describe one revision of one file, which is
what lets the graphs page join a served document to a matrix run at all; unequal
proves the file was edited between them and fails the drive. Where the entry
carries no digest, the leg reports that absence and the entry's own detail if it
has one, and does not fail — it says what it observed, not why.

Which graph it binds is printed with it. A graph that declares rows and whose
inventory row decoded is preferred, because only such a graph yields an entry
with a digest to compare; a project whose first graph carries a decode detail
would otherwise end the leg unbound while a later one could have bound. Failing
that it takes any graph declaring rows, then the first configured graph.

Both tools are required, and the skip line names whichever is missing rather
than assuming both are:

```
capabilities      rehearsal=true list_graphs=false get_graph=false include_traces=false
graph binding skipped  this runtime advertises no experimental_list_graphs and no
experimental_get_graph; both are needed to choose a graph and fetch its document
without being told which one (ADR-0029, which jpack 0.18.0 predates)
```

That drive still ends `OK`. No version is read anywhere: what a runtime can do
is what it advertises, and jpack 0.18.0 is named as a known example rather than
as the diagnosis.

The whole graph surface is **one matrix run per drive** — `--graphs` prints the
suite and the binding reads its entry out of that same run, and the suite's own
status is checked only *after* the binding has been stated. Two runs would be
two reads of an editable file, which is the condition the digest exists to
detect; and exiting on a mismatch first would leave a red run with no statement
of which revision it read.

The binding decision reads no matrix status, no row and no coverage probe.
Digest equality is byte arithmetic over bytes the runtime handed over; what a
run *concluded* is the runtime's to say, and the leg says only which bytes it
concluded it about.

## Upstream gaps

The desk consumes the runtime's public wire and nothing else. Where the wire
cannot express something the desk wants, the gap is recorded here rather than
worked around in the chassis — a chassis that parsed or supplemented the traffic
would stop being one.

- **Resolved: rehearsal mode on `experimental_evaluate`** (was: every completed
  call appended one audit record in a project declaring an audit directory, so
  a five-variant what-if session left five records saying the project decided
  five times). Filed as runtime issue #124 and closed by ADR-0028 in jpack
  0.18.0: a call declaring `"rehearsal": true` runs identically, appends no
  record, consults no reviewed set, and carries the label in its payload. The
  desk declares it on every what-if run when the connected runtime's own tool
  schema advertises the argument, and says in the page which of the two worlds
  the runtime is; against an older runtime the original consequence note
  returns, and the acceptance script still evaluates a copy.

- **Resolved: the graph document and the graph inventory** (was: no member of
  any payload carried the graph document — not the pack each node names, not the
  edges' endpoints, not the fact pointer or evidence id an edge carries, not
  which node is the declared `result` — so the desk could draw the nodes on the
  coverage report's order axis and no edge between them; and nothing listed the
  graphs a project configures short of running every one of their matrices).
  Filed as runtime issue #126 and closed by ADR-0029: `experimental_get_graph`
  serves one configured graph document by its configured id, byte for byte,
  beside its identity, digest and size, and `experimental_list_graphs` resolves
  the whole configured inventory for one call that evaluates nothing. The desk
  feature-detects both by name in `tools/list` and never reads a version string.
  With the fetch it draws the real edges from the served document; with the
  listing it says what the project configures before the matrix has run. Against
  a runtime with neither, the coverage-derived walk and its "no arrow is drawn"
  note return exactly as they were, and the home page finds the graphs by
  running their matrices. A document the runtime serves but could not decode
  falls back the same way with one line saying why — serving is not validating,
  so that document arrives as a successful call whose text is not a graph.

- **Resolved: binding a served graph document to the matrix run drawn beside it**
  (was: the walk joined two answers from two calls — the document
  `experimental_get_graph` served, and the coverage and rows
  `experimental_test_graphs` reported — by node name and by edge index, with no
  member of either payload saying the two described the same bytes;
  `experimental_get_graph` reported the document's `sha256` and a graph matrix
  entry reported none, so the desk bounded the window with connection-epoch
  keying and in-flight gating and invented no binding of its own, which bounds
  staleness without ever proving sameness). Filed as runtime issue #132 and
  closed by ADR-0030 in jpack 0.19.0: a graph matrix entry carries
  `graphSha256`, bare hex, the digest of the exact bytes that run decoded,
  present exactly when the document loaded. The desk compares it to the `sha256`
  served beside the document, and the comparison decides the join and nothing
  else — it never derives, revises or overrides a verdict the runtime reached
  about either revision. Equal, the walk is drawn as before and the page states
  the provenance in one line. Unequal, the graph file was edited between the two
  calls, so the joined walk is withdrawn, a line names the divergence, and both
  queries are invalidated so the next pair of answers can re-bind — **one
  refetch cycle per normalized pair**, each cycle being the two requests, and
  every pair a connection has already asked about is remembered for the life of
  that connection. So a file still mid-edit reads as a standing withdrawal
  rather than spinning the page, and a file edited back and forth between two
  revisions settles after asking about each of them once instead of
  ping-ponging. Absent — jpack 0.18.0 and older, or an entry
  whose document did not load, since a rows failure *after* a successful load
  keeps the digest — nothing is compared and nothing is claimed either way, and
  the epoch-bounded behaviour stands exactly as it was. The connection-epoch key
  stays in all three cases: it is what keys cache identity to one connection,
  and the digest upgrades the join from a bounded window to proven sameness
  rather than replacing it.

- **Resolved: node traces on the graph matrix** (was: ADR-0027 pinned the trace
  contract and bound it to each node evaluation inside a graph run, and the
  runtime's `GraphNodeEvaluation` carried that node's `trace` — but the wire
  dropped it, reporting per node only `node`, `status`, `expected` and `actual`,
  so the desk could show what a node concluded and not how it got there, though
  the runtime had computed it). Filed as runtime issue #127 and closed by
  ADR-0031 in jpack 0.19.0: `experimental_test_graphs` accepts an optional
  boolean `include_traces`, and asked, each reported comparison whose node the
  walk evaluated carries that evaluation's own `trace` under ADR-0027's
  contract. The desk detects the argument in that tool's own advertised schema —
  the same way it detects `rehearsal` on `experimental_evaluate`, and never by
  the tool's name, which predates the argument — and offers **an opt-in control,
  off by default**. Off omits the key entirely, so the untraced call is the one
  this desk has always made, byte for byte; the traced and untraced answers are
  separate cache entries, because a payload with no traces is an answer to a
  different question and must never stand in for one that was asked.
  **Traces are charged against the runtime's report budget**, so a suite that
  fits without them can be over it with them — one reason a traced request can
  fail, and not one the desk claims. A tool error is one unstructured shape
  whatever caused it, and a non-refusal error covers a response the runtime did
  produce and the desk could not read, so the page shows the runtime's own
  message as the reason and adds only what it knows: that this request asked for
  traces, that it did not produce a usable answer, and whether clearing the ask
  returns to an untraced answer still in hand or retries a request never
  answered. Never "these nodes have no traces", which would be a claim about an
  answer nobody received. The control stays on screen through the failure,
  including on a runtime with no inventory to render beside it, so the ask that
  failed is always reversible.
  Each node's trace is drawn by **the same renderer the evaluation view uses**,
  because it is the same artifact under the same contract; a mismatching
  comparison shows its trace too, which is the one most worth reading. `[]` is a
  trace with no entries and is said to be empty; an absent member is not asked,
  or not evaluated, and shows nothing. **Two orders are kept apart**: the
  comparisons are listed lexicographically by node name, the report's order,
  while each trace inside one is the evaluator's walk order — the page says so
  where they meet, and neither is read off the other.

- **Resolved: handoff-target assertions on graph rows** (was: ADR-0025 added
  `expectedHandoffTarget` to pack matrix rows and deferred the graph surface
  explicitly, so a graph row compared composite and per-node dispositions only
  and a change to where a composed decision is handed off left every graph row
  green). Filed as runtime issue #128 and closed by ADR-0032 in jpack 0.19.0:
  a rows document declaring `graphMatrixVersion` `"2"` may assert
  `expectedHandoffTarget` for the composite and `expectedNodeHandoffTargets` for
  the nodes it names, and the run reports each as an
  `expectedHandoffTarget`/`actualHandoffTarget` pair — on the row for the
  composite, on the named node's comparison for the rest. The two members are
  one pair: they appear **together**, exactly when a *well-formed* assertion
  rode a run this walk *performed*, and a row whose assertion was itself
  defective — undecodable, or naming a node the graph does not declare —
  reports that defect in its detail and carries no pair. The desk reads them
  through one accessor that applies the rule, so half a pair renders nothing.
  Both carriers exist because a headline-only assertion stays blind upstream,
  where an escalation target on a node three hops back is as editable as the
  composite's and changes nothing any headline can see. The desk renders both
  pairs with the **pack surface's own component**, on the same vocabulary: a
  capped rendering, the literal `null` for "no target at all", and
  `unavailable` where a refused run leaves no target to state. Those last two
  stay distinct, because one is an answer and the other is the absence of one —
  and `unavailable` is reachable on the row alone, never on a node comparison,
  which exists only because the walk evaluated that node. As on the pack side the renderings
  are **display values and are never compared here** — a capped rendering can
  differ from its own pair past the cap — so no mark on a pair is this client's:
  the row's status is the runtime's verdict and the only one shown.

## License

Apache-2.0. See [LICENSE](LICENSE).
