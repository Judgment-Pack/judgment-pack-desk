# judgment-pack-desk

A local web desk for a Judgment Pack project. `jpack-desk` is one Go binary: it
serves a single-page application and relays JSON-RPC between that page and a
`jpack mcp` subprocess running in your project directory.

The browser is the MCP client. The Go program is a chassis, not a feature
server — it has no per-feature endpoints and parses none of the traffic it
carries, so the desk can show anything the runtime's tools expose without a
matching change on the Go side.

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
configuration read-only, and `/help`, which names what this runtime advertises
and renders its own authoring prompt as text. The shell **derives no verdict**:
no status colour in the rail, no rollup count, no "N failing" pill anywhere. A
red badge in a nav rail would be a gate the runtime never issued.

## Shell

**Six regions**, on a CSS grid of a **definite** viewport height —
`height: 100dvh` and not `min-height`, so the content row divides the viewport
instead of growing to fit a long page, `.desk-main` is the one scroll container,
and the 28px strip stays on screen. The three pane sizes in the table are the
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
else. It is registered by the pack editor on its own subtree, so the shell's
rule stays as written and the label says where this one applies.
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

where `projectKey` is a slug of **the project root the chassis pins at
startup** plus an FNV-1a hash of the whole of it. It is that root and not the
runtime's `configPath`, because a project with no `jpack.json` reports no config
path — so every configless project on one origin used to map to the literal
`default` and share a single record, two directories with one layout between
them. The literal `default` remains and now means exactly one thing: the file
listing has not answered yet, and **nothing is written under it**.

One desk on one origin serves whichever project it was started against, and a
layout chosen for a three-pack project is not the one chosen for a forty-pack
one. Only the collapse flags and the console's channel are stored — no widths,
because nothing on this desk can yet change one, and a stored number no viewer
could have chosen would be a record of a choice nobody made — and **only for the
panes the viewer has actually moved**, one bit each. A record that carried all
three because one was toggled would be two built-in defaults outranking the
configuration file for ever. Every read and write is in `try/catch`: a private
window and a browser with site data blocked *throw* on the accessor rather than
answering null. **Admin › Panes** clears exactly that one key, cancelling any
write already on its way, and reports what happened rather than assuming.

Reduced motion is respected: `prefers-reduced-motion: reduce` sets every pane
transition to zero, and collapse is instant.

**Theme.** `appearance.theme` writes `data-theme` on the root element —
`light` and `dark` pin a palette, `system` removes the attribute and leaves
`prefers-color-scheme` to answer. What it selects today is a palette whose
values are the light ones: this phase ships the plumbing — the two selectors in
`styles.css` and the attribute — and none of the dark values, because the three
condition verdict colours carry meaning, cannot be mechanically inverted, and a
desk that re-authored its neutrals around them would be half dark. So choosing
dark changes the attribute and no colour. `appearance.density` is recorded and
validated and is read by nothing yet.

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

**The buffer is the document.** Both modes draw `indexDocument(buffer).value`
rather than the parsed pack `get_pack` served, so a keystroke in the JSON view
moves the reading document above it and a form edit is in the bytes the moment
it is made. A page over one revision while the form writes into another is the
digest-binding failure one component further in; the served document is the
fallback only where the file did not load or its bytes do not scan, and the
digest sentence still says when the two sources disagree.

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
Nothing about the reviewed set appears here at all — no tool reports it, so the
desk cannot know it and must not compute it.

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

**A cap is not the same as a configured value, and Admin says both.** An
accepted 720px Inspector renders 440px at a 1100px window; the Inspector's
*drawer* form is 320px unless the file states a width. Admin › Panes prints
the configured number, the range it had to be inside, the cap that applies, and
the **measured** width or height of each pane that is on screen — and says
"not mounted at this width" or "collapsed" rather than reporting a number
nothing has. `useInspectorSlot().size` is measured on the same terms, so a
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
decoded and validated; `theme` is applied as above and `density` is not read
yet, which Admin › Appearance also says. `organization.mark` is `null`,
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
the same lexical shape the file API would refuse anyway — so Admin names the
key that is wrong rather than the dialog failing later on a path nobody chose to
look at. That includes the directories the chassis excludes from its endpoints
altogether (`.git`, `node_modules`, `dist`, `.venv`, `vendor`, and a staging
name): `"dir": "dist"` is a plausible thing to type, and a configuration that
decodes clean while making every create fail is worse than one refused where it
was written. The list is mirrored from `internal/desk/watch.go` and held to it
by a test that reads that file.

`idBase` must parse as a URI, because a pack document's `id` member is
`format: uri`, and it is **normalised at decode** to end in `/` (or left alone
where it ends in `#`), so a pack's id is a plain concatenation everywhere it is
used and Admin shows the prefix that will actually be written.

`kind` admits only `"filesystem"` today, and its refusal names the other two by
name: `"database"` and `"cloud storage"` are **not available yet**. Admin lists
them as coming soon, as text rather than as disabled controls, and **nothing in
the desk branches on this member** — a pack is created by writing a file,
always. The create UI never asks which kind is configured.

**Precedence, as phase A resolves it**: project file → built-in default, and
for the three pane flags one layer in front of both — this browser's record of
what the viewer chose, for the panes they chose it for. There are no shell
flags on the command line; the desk-level `desk.json` is not read; and a layer
that does not exist is not part of a precedence order, however plausibly it
would fit. What arrives later — desk-level values, and whatever reads them —
goes between the project file and the built-ins, and this line changes in the
commit that adds it.

The desk-level `desk.json` — the only place an identity provider may be
configured — is **not read yet**; whether the page learns it through a
read-only `GET /api/desk-config` or is told at connect time is an open question,
and Admin › Identity provider says so in words rather than leaving the reader to
infer it.

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

**Admin never PUTs configuration.** It renders effective values, their source,
the path they came from and the exact JSON to paste, and its only control that
changes **persisted desk-layout state** clears the pane record above; the Copy
button beside each paste block changes the clipboard and its own transient
"copied" label, which is why the claim is scoped to persisted layout rather
than to state in general.

That is a claim about **Admin**, and it is deliberately not the broader one it
used to make. `jpack-desk.json` is an ordinary project file — the desk reads it
through the same `GET /api/file` every other file goes through — so the generic
Author editor lists it and can write it exactly like any project file. Saying
"nothing is ever PUT to a configuration file" was a sentence this repository's
own file API refutes; what is true is that no *configuration surface* writes
it, and the editor that can is the one that treats it as bytes and forms no
opinion about what they mean. `runtime.jpackBin` and
`project.dir` are not in the schema at all: the chassis executes the binary it
was given, so a config-supplied path would be a way to run code on this machine
by editing a file.

**Identity is display, never a gate.** `identity.provider` is one nullable
field — null, or an object. There is no `kind`, no vendor string and no third
shape, and that absence is what stops an issuer someone else operates from
acquiring anything an issuer you run yourself lacks. Configuring one changes
what the header shows and nothing about who may reach the desk.

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

It prints the URL to open, including the session token:

```
judgment-pack desk
  project: /path/to/project
  runtime: /path/to/jpack
  open:    http://127.0.0.1:8791/?token=1f3c…
```

`projectDir` defaults to the current directory. `--jpack` defaults to `jpack`
on `PATH`, and `--port` defaults to `8791`.

## Development mode

Two processes: the chassis for the relay, Vite for hot reload.

```sh
# terminal 1 — chassis with a fixed token so the URL is stable across restarts
# (flags come before the project directory: Go stops parsing flags at the
# first positional argument)
go run . --dev-token dev --port 8791 --jpack /path/to/jpack /path/to/project

# terminal 2 — Vite dev server, proxying /ws and /api to the chassis
npm --prefix web run dev
```

Then open <http://localhost:5173/?token=dev>.

Vite proxies `/ws` to `127.0.0.1:8791` (override with `JPACK_DESK_CHASSIS`).
Passing `--dev-token` is what additionally permits the Vite dev server's origin
— without it the chassis refuses the proxied upgrade, because the browser's
`Origin` is the dev server's and never matches the host it reaches the chassis
under.

To check a running chassis end to end with the desk's own client code:

```sh
npm --prefix web run smoke -- 'http://127.0.0.1:8791/?token=dev'
```

## Security model

The desk drives a runtime that reads your project, and — since the authoring
surface — writes files in it. Two capabilities are gated, and gated the same
way: **`/ws`**, the relay, and **`/api/*`**, the file API. Static assets are
not; they are the page, and the page can do nothing without one of the two.

- **Loopback only.** The listener binds `127.0.0.1`. Nothing off the machine
  can reach it.
- **A session token.** A random 192-bit token is generated at startup and
  printed in the URL. Both capabilities require it as `?token=`, compared in
  constant time. (Length is still observable, which does not matter: the format
  is fixed and public, and the value is the secret.) The page copies it into
  `sessionStorage` under `jpack-desk-token` on first load, and it leaves the
  address bar at the first in-app navigation — nothing rewrites the URL on load,
  so a sentence claiming it disappears immediately would be false.
- **An origin check.** A request whose `Origin` is not the origin the page was
  served from is refused — **scheme and host both**, and an `Origin` carrying a
  path, query, fragment or userinfo is refused outright rather than matched on
  its host. This is what stops a page on another site from driving your runtime,
  or writing to your project, through your own browser: a token in a URL you
  have visited is not by itself protection against that. A request with no
  `Origin` at all is not from a browser — it is a script or a test holding the
  token — and the token is its authorization.

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

The chassis holds no credential and opens no outbound connection. It writes to
the project only through the file API, only inside the project root, and only
where a request carried the token and an acceptable origin. The runtime
subprocess inherits the project directory as its working directory and is killed
when the socket that started it closes. That sentence still holds with the shell
in place: the desk reads its configuration through the file API it already had,
and identity is display only. The change that would falsify it is wiring an
identity provider — discovery, JWKS, a redirect — and the PR that does it must
amend this paragraph in the same commit.

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

Three endpoints, proxied alongside `/ws` by the dev server, under the same two
checks as `/ws` — the session token first,
then the Origin — through one shared guard, because a new endpoint is a new
place to forget one:

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
  server.go          routing, SPA fallback, token and origin checks
  files.go           the file API: containment, atomic save, stale-write refusal
  relay.go           WebSocket ↔ `jpack mcp` subprocess
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
                     read-only Admin page and Help & About
  src/components/    evaluation, coverage, row and graph-walk views, plus the
                     trace and handoff-target renderers both the pack and graph
                     surfaces share
  src/shell/         the six regions, the pane state and its per-project
                     record, the published shortcut list, the dirty guards
                     both editors use — a `beforeunload` listener and a router
                     blocker whose predicate is the pathname alone — the icon
                     set, the console's
                     ring buffer, the fragment-scrolling hook the section menus
                     need, and the Create-pack dialog — which asks for a name,
                     a description and a template, and decides the file's
                     location from configuration
  src/ui/            the styled primitives: Button, Field, Input, TextArea,
                     Select, Tabs, Dialog and Alert, plus the editor's own —
                     SegmentedControl, Toolbar, CodeArea, SuggestInput and
                     AlertPanel — one CSS module each (see Styling)
  src/packs/         the pack surface: the RFC 6901 pointer module, the
                     span-preserving document writer, the validate reader, the
                     cross-reference reader, the packs pane and its windowing
                     hook, the scroll-spy — and what a new pack is called and
                     where it goes (the slug rule, the template shaping, and
                     the jpack.json amendment)
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
  src/config/        the jpack-desk.json schema, its strict decoder, the one
                     query that reads it, and the theme attribute it writes
  src/identity/      the identity slot: one nullable field, and the header
                     control that renders it
  scripts/smoke.ts   the desk's own client, driven outside a browser
```

## Styling

Four rules and one test that holds all four
(`web/src/ui/convention.test.ts`, which reads the source because vitest runs
with `css: false` and a component whose stylesheet was deleted renders exactly
like one whose stylesheet is intact).

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
every byte outside a touched span identical, a neighbour's indentation never
inherited, a blanked `nonEmptyString` removed rather than emptied, and a member
that is not there yet written at the position the schema gives it.
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
tokened URL off the `open:` line of the chassis' startup output, and drives the
desk's client three times. `MUTATE` is the jq expression that removes the fact,
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

```sh
npm --prefix web run smoke -- 'http://127.0.0.1:8791/?token=…' \
  --facts /path/to/full-facts.json --evidence /path/to/evidence.json

# the two calls the matrix and graph views make
npm --prefix web run smoke -- 'http://127.0.0.1:8791/?token=…' --matrix --graphs

# the graph-serving pair the walk diagram draws its edges from (ADR-0029):
# the inventory, then one document by its configured id, checked byte for byte
# against its own metadata and against the file on disk
npm --prefix web run smoke -- 'http://127.0.0.1:8791/?token=…' \
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
