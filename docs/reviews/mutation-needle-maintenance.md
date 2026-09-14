# Restore the mutation checks after the UI refactors

Recorded on 2026-09-14 against Desk main `19ec074850f848010ced3a36aeddc1cf77d33391`.
The pre-merge needle check found 16 missing targets and one ambiguous target in
885 mutation rows. The safeguard code had moved or changed shape, but the rows
still pointed at the older implementation. No application behavior changes in
this repair.

All 885 rows remain. The 17 affected rows now identify one current safeguard
each. The former “last way into what-if” row now names the primary Test pack
link in the shared PackHeader; Tests is also a navigation entry. The unfinished
field notice moved from EditToolbar to PackEditHeader. Rail request mutations
now inject the forbidden query alongside usePacks, since navigation no longer
reads graph inventory. The other repairs retain their original failure mode.

The needle checker now fails for ambiguous/empty/missing targets, missing files
and an empty row collection. It names the affected row and uses a private
cleaned-up temporary file. Mutation application itself also refuses anything
other than one nonempty match before writing. Seven subprocess regression tests
exercise the actual checker and apply function. CI runs those guards and checks
all row targets before the Go suite.

## Evidence

The relevant unmutated web suites passed 193 tests; the two selected Go tests
passed, with 171 named passing events including subtests. Each repaired mutation
was then applied alone and checked against its corresponding focused suite,
with the source bytes restored in a finally block. No compile error or unnamed
suite failure was counted as a caught mutation.

The pane re-seeding row initially survived its focused provider suite. An added
regression changes the rail, then supplies new configuration: the chosen rail
must stay put while an untouched Inspector adopts the configuration. The provider
suite then passed eight tests unmutated, and this exact new test failed with the
re-seeding mutation. All 17 repaired mutations are now caught by named assertions.
This is targeted evidence for the repaired rows, not a claim that all 885
mutations were executed.

| Repaired mutation | Focused test command selection | Example failing assertion |
| --- | --- | --- |
| http is accepted off loopback | `TestSharedFixturesDecodeAsTheVerdictSays` | TestSharedFixturesDecodeAsTheVerdictSays/refused-endpoint-http-off-loopback |
| a configuration write accepts a second value behind the first | `TestDeskConfigWriteRefusesAnythingAfterTheObject` | TestDeskConfigWriteRefusesAnythingAfterTheObject |
| rail calls the whole-project graph walk | `src/shell/LeftRail.test.tsx` | the left rail never runs the whole-project graph walk, on any runtime |
| an unchosen layout is persisted anyway | `src/shell/shellState.test.tsx` | the shell state provider cancels a write already on its way when the panes are reset |
| a record from another shell version is restored anyway | `src/shell/paneState.test.ts` | readShellState discards another version silently |
| the inspector drawer answers to no id | `src/shell/narrowShell.test.tsx` | the shell at 1000px, where the Inspector is a drawer gives the drawer the id its toggle claims to control |
| the rail fetches starter templates on every route | `src/shell/LeftRail.test.tsx` | the left rail leaves starter requests to the creation page |
| one moved pane suppresses the re-seed for every pane | `src/shell/shellState.test.tsx` | the shell state provider keeps the chosen rail while applying arriving configuration to untouched panes |
| closing the inspector drawer drops focus on the body | `src/shell/narrowShell.test.tsx` | the shell at 1000px, where the Inspector is a drawer returns focus to the header toggle when the Inspector drawer closes |
| a configured pane may take the whole frame | `src/shell/shellSheet.test.ts` | no pane may eat the frame, whatever the file says caps both side columns and the console against the viewport |
| the slot reports a configured width the pane does not have | `src/shell/inspectorSlot.test.tsx` | a route publishing into the Inspector reaches the pane at the widest breakpoint, where the panel is a column |
| the condition tree loses the operand type | `src/packs/document/ConditionTree.test.tsx` | makes an empty set distinct from an absent operand |
| the primary Test pack link disappears | `src/routes/PackView.test.tsx` | the other two views on this pack offers the what-if view, which nothing else links to any more |
| the shell flips the Inspector where a route asked it to open | `src/shell/inspectorSlot.test.tsx` | a route publishing into the Inspector opens the pane when a route says the viewer asked to inspect something |
| the packs pane never takes focus to the row it arrowed to | `src/packs/PacksPane.test.tsx` | the packs pane moves focus between rows with the arrow keys, Home and End |
| the editor header hides unfinished fields | `src/packs/edit/forms.test.tsx` | an operand holding text that is not JSON says so, counts it in the toolbar, and keeps it across the mode toggle |
| the page measure unstated by a route (graphs takes the default) | `src/routes/measure.test.ts` | every route states the width kind of its page GraphView.tsx carries data-measure on its top-level element |

## Reproduction

Run the inexpensive guard checks from the repository root:

```sh
python3 scripts/needle-check_test.py
scripts/needle-check.sh .
```

Expected: seven guard tests pass and `rows checked: 885   invalid: 0`.
For a full-suite reproduction of any repaired row, use a clean disposable
checkout with dependencies installed and pass its name to the existing harness:

```sh
scripts/mutation-check.sh web 'one moved pane suppresses the re-seed for every pane'
scripts/mutation-check.sh go 'a configuration write accepts a second value behind the first'
```

The harness first requires a passing unmutated suite. Go HTTP tests need local
socket access. The local focused evidence above deliberately avoided the known
machine-specific inotify exhaustion in an unrelated full-suite watcher test;
CI remains responsible for the full Go suite on a clean runner.
