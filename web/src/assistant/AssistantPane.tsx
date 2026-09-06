/**
 * The Assistant tab: type what the pack should decide, and watch the runtime
 * be consulted about it.
 *
 * **It proposes. It does not write, and it does not decide.** Everything on
 * screen is either the assistant's own account of what it did or the runtime's
 * words quoted whole — the `validate` report and the rehearsal evaluation are
 * shown as the runtime wrote them, never summarised, because a summary of a
 * verdict is a second verdict.
 *
 * **Accept into draft is the desk's action on the proposal, not an engine
 * call.** It applies the diff to the buffer through the same span-preserving
 * writer a form edit uses, in one `write` — so it is one undo entry, every byte
 * the proposal did not move survives, and nothing is saved: the check runs
 * again over the new bytes and Save is still the author's to press. On the
 * reading route there is no buffer to write into, and the pane says so in one
 * line rather than drawing a control that would refuse.
 *
 * **A proposal belongs to the draft it was given, and to no other.** A run
 * captures its baseline where it starts — which file, which incarnation of the
 * buffer, and the exact bytes it sent — and everything about the proposal is
 * about that: the diff is computed against the baseline snapshot rather than
 * against the live buffer, so what is on screen is what Accept would apply, and
 * Accept is offered only while the page still holds those bytes. An author who
 * typed while the model was thinking is told so and offered another run, which
 * is the only honest way forward: applying the proposal would write the whole
 * document the model saw and take the typing with it.
 *
 * **It renders only where there is an assistant to run.** No endpoint, or no
 * key on this machine, and it says in one line where that is configured rather
 * than showing a control that would refuse.
 *
 * **Nothing is persisted.** Leaving the route ends the session and closes its
 * connection; coming back is a new one.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  AUTHOR_PACK_PROMPT,
  FIX_PACK_PROMPT,
  TEST_PACK_PROMPT,
  usePromptNames,
  usePromptText
} from '../mcp/prompts'
import { Button } from '../ui/Button'
import { CodeArea } from '../ui/CodeArea'
import { TextArea } from '../ui/TextArea'
import { useEditing } from '../packs/edit/editingContext'
import type { BufferIdentity } from '../packs/edit/useDocumentBuffer'
import { EventList } from './EventList'
import { ProposalDiffView } from './ProposalDiff'
import { ProposalUnknowns, RefutationReport, RuntimeChecks } from './ProposalReport'
import { DRAFT_MOVED, acceptState, applyProposal, writable, type Disposition } from './acceptProposal'
import { diffProposal } from './proposalDiff'
import { outcomeOf } from './runOutcome'
import { stateFromEvents, thinkingLine } from './thinking'
import { useAssistantRun } from './useAssistantRun'
import { useAssistantSlot } from './useAssistantSlot'
import styles from './AssistantPane.module.css'
import type { AssistantEvent } from './engine'

/**
 * The one sentence the draft travels under, fixed.
 *
 * It says what the bytes are and what to do with them, and nothing else: a
 * sentence that also described the draft would be this desk telling a model
 * what a document says, which is the runtime's prompt's job and not this
 * pane's.
 */
export const DRAFT_SENTENCE = 'This is the draft being edited; propose the whole document.'

/**
 * The runtime's prompt, with the draft after it — **verbatim, and fenced**.
 *
 * Fenced so the model can tell the document it was handed from the
 * instructions around it, which is the shape the runtime's own prompts use for
 * caller-supplied material (`writeFencedBlock` in `internal/mcp/prompts.go`).
 * Verbatim because the bytes in the editor are the bytes the proposal has to be
 * an edit of: a reformatted copy would be a draft nobody has.
 *
 * A page with no bytes sends the prompt alone, and what comes back is a
 * document rather than an edit.
 */
export function withDraft(prompt: string, draft: string | undefined): string {
  if (!carriesDraft(draft)) return prompt
  return `${prompt}\n\n${DRAFT_SENTENCE}\n\n\`\`\`json\n${draft}\n\`\`\``
}

/** Whether there are bytes on this page worth calling a draft. */
export function carriesDraft(draft: string | undefined): draft is string {
  return draft !== undefined && draft.trim() !== ''
}

/** What one run was about: which document, and the bytes of it. */
interface Baseline {
  bytes: string | undefined
  path: string | undefined
  generation: number | undefined
}

/** Nothing is in the way. The default, for a caller with no draft to speak of. */
const noBusy = () => ''

/** Why Fix is not offered, where it is not. */
function fixWhy(diagnostics: number, advertised: boolean, listed: boolean): string | undefined {
  if (!advertised && listed) return 'This runtime advertises no fix_pack prompt.'
  if (diagnostics === 0) return 'The check on this page reports no diagnostic to fix.'
  return undefined
}

export function AssistantPane({
  draft,
  editing = false,
  identity,
  busy = noBusy,
  diagnostics
}: {
  /**
   * The bytes this page is about: the editor's buffer on `?edit`, the saved
   * document elsewhere, and undefined before either has been read.
   *
   * It is the route's rather than read from a context here, because the route
   * is the one place that knows which of the two the page is showing — and
   * because "the document the diff is against" and "the document the assistant
   * was given" have to be the same string or the diff is about a draft nobody
   * sent.
   */
  draft?: string
  /**
   * True where this page is being edited **and** has bytes to edit.
   *
   * The route's own condition for whether the JSON view is writable, passed in
   * rather than derived here: a pane that decided for itself would be a second
   * reading of what "editable" means, and the two would drift.
   */
  editing?: boolean
  /**
   * Which document these bytes are, and which incarnation of the buffer.
   *
   * Bytes alone would let a proposal made about pack A be accepted onto pack B
   * that happens to read the same, and the pane outlives a navigation between
   * packs: the route re-renders, the tab does not remount.
   */
  identity?: BufferIdentity
  /**
   * Why the draft cannot be moved at all right now, or the empty string.
   *
   * **A function, and it is called again at the instant of the click.** A save
   * is claimed synchronously by the route's own latch and reported to React a
   * render later, so a control that consulted only the rendered value could
   * write into a buffer whose save had already been submitted. The rendered
   * value disables the button; the call at the click is what refuses.
   */
  busy?: () => string
  /**
   * The runtime's own diagnostics for the bytes on this page, from the check
   * that already ran beside them: how many there are, and **the exact bytes**
   * the runtime wrote them as.
   *
   * The bytes and not the objects. `fix_pack` takes the validator's
   * diagnostics, and what this pane hands it is the member cut out of the
   * runtime's own answer — not a re-serialization of a parse of it, which
   * would differ in whitespace, in escaping, and possibly in member order. A
   * paraphrase of a refusal is a second refusal. Undefined where there is
   * nothing to fix.
   */
  diagnostics?: { count: number; bytes: string }
} = {}) {
  const slot = useAssistantSlot()
  // The editing session is the only way bytes change on this desk, and `write`
  // is the whole of what this pane uses it for. There is no `commit` here to
  // reach for: the context does not carry one.
  const session = useEditing()
  // The bytes and the identity as of now, readable from a callback that must
  // not be rebuilt on every keystroke in the editor beside this pane.
  const busyNow = useRef(busy)
  busyNow.current = busy
  const prompts = usePromptNames()
  const advertised = (prompts.data ?? []).includes(AUTHOR_PACK_PROMPT)
  const canFix = (prompts.data ?? []).includes(FIX_PACK_PROMPT)
  /** How many the check found, for the control and the line beside it. */
  const diagnosticCount = diagnostics?.count ?? 0

  const [typed, setTyped] = useState('')
  /**
   * The submission a person actually made, **keyed by a run id**.
   *
   * The id is why this is an object and not the policy text. Pressing Run twice
   * with the text unchanged used to set the same string: React saw no state
   * change, the effect below saw the same value it had already started, and an
   * enabled button did nothing at all. Two runs of one policy is an ordinary
   * thing to want — a model is not a pure function — so each press is its own
   * submission whatever it says.
   */
  const [submitted, setSubmitted] = useState<{
    id: number
    /** Which of the runtime's prompts this run is of. */
    name: string
    /** Its arguments, as `prompts/get` takes them. */
    args: Record<string, string>
  } | null>(null)
  const nextRun = useRef(0)
  const accepts = useRef(0)
  /**
   * **What this run is about**: the document it was given, and the bytes of it.
   *
   * Captured where the run starts rather than read as the pane renders, because
   * the buffer moves while a session runs. Everything downstream is about this:
   * the diff is computed against these bytes, Accept applies to a page that
   * still holds them, and whether the proposal is an update or a new document
   * is decided by whether there were any — never by a `kind` the model wrote,
   * which is a statement about its own work.
   */
  const [baseline, setBaseline] = useState<Baseline | null>(null)
  /**
   * The bytes an accept left behind, so "accepted" can be **derived** rather
   * than remembered.
   *
   * Undo puts the draft back exactly as it was, and a stored disposition then
   * said "already in the draft" about a draft that no longer carried it — with
   * Accept disabled and no way to put it back. What is true is a comparison:
   * this proposal is in the draft exactly while the draft is the bytes it made.
   */
  const [accepted, setAccepted] = useState<string | null>(null)
  const [rejected, setRejected] = useState(false)
  // The bytes and the identity as of now, readable from the effect that starts
  // a run without making it restart on every keystroke in the editor beside it.
  const draftNow = useRef<string | undefined>(draft)
  draftNow.current = draft
  const identityNow = useRef<BufferIdentity | undefined>(identity)
  identityNow.current = identity
  const prompt = usePromptText(
    submitted?.name ?? AUTHOR_PACK_PROMPT,
    submitted !== null && (prompts.data ?? []).includes(submitted.name),
    submitted?.args
  )
  /** Which prompt the run on screen is of, once one has started. */
  const [ran, setRan] = useState<string | undefined>(undefined)

  /**
   * The runtime's testing prompt, for the refutation pass.
   *
   * Read **only where a critic will run** — the pass is gated on the tier, so a
   * desk at `off` spends no `prompts/get` on it — and read with no `pack`
   * argument, because the document the critic will be given does not exist
   * until the session has produced it. The critic is handed the document
   * itself, fenced, beside this guidance.
   */
  const advertisesTest = (prompts.data ?? []).includes(TEST_PACK_PROMPT)
  const testPrompt = usePromptText(TEST_PACK_PROMPT, slot.thinking !== 'off' && advertisesTest)
  /**
   * Whether this session still owes the critic the runtime's testing prompt.
   *
   * A session begins by reading the runtime's prompts, and the refutation pass
   * needs a **second** one. Starting on the first alone handed the engine an
   * empty `testPrompt`, and the critic then ran on the desk's sentence with
   * none of the runtime's instructions — the one thing the pass is not allowed
   * to be. A runtime that advertises no `test_pack` is not waited for: the pass
   * reports that it cannot run, and the proposal is shown without a line.
   *
   * **A read that failed is a settled read.** `data` alone stays undefined for
   * ever on the error state, so a `prompts/get` the runtime refused deadlocked
   * the whole session: the authoring prompt had arrived, no engine started, and
   * the tab said nothing about why. A rejection settles this the same way an
   * answer does — the run goes ahead **without a critic**, and the reason is a
   * line of its own beside the stream.
   */
  const waitingForTest =
    slot.thinking !== 'off' &&
    advertisesTest &&
    testPrompt.data === undefined &&
    testPrompt.error === null
  const run = useAssistantRun({
    // Only rendered where the endpoint exists; the fallback keeps the hook
    // unconditional, which is the rule React enforces.
    endpoint: slot.endpoint ?? { url: '', kind: 'openai-compatible', model: '', tools: [] },
    engine: slot.engine,
    thinking: slot.thinking,
    testPrompt: testPrompt.data?.text ?? ''
  })

  // The run starts when the prompt this submission asked for has arrived, and
  // once per submission: the query answers again on a refetch, and a second
  // start would be a second `jpack mcp` for one press of Run.
  const started = useRef<number | null>(null)
  const startRun = run.start
  useEffect(() => {
    if (submitted === null || prompt.data === undefined) return
    // Both prompts, or neither: see `waitingForTest`.
    if (waitingForTest) return
    if (started.current === submitted.id) return
    started.current = submitted.id
    setRejected(false)
    setAccepted(null)
    setBaseline({
      bytes: draftNow.current,
      path: identityNow.current?.path,
      generation: identityNow.current?.generation
    })
    setRan(submitted.name)
    startRun(withDraft(prompt.data.text, draftNow.current))
  }, [submitted, prompt.data, waitingForTest, startRun])

  /**
   * Stop, in **both** phases of a session.
   *
   * A session begins with the desk reading the runtime's prompt, and only then
   * does an engine run. Stop was wired to the engine alone, so pressing it
   * while the prompt was still being read did nothing at all — the control was
   * enabled and inert. Clearing the submission disables that query and keeps
   * the effect above from starting a run for it.
   */
  const stopRun = run.stop
  const stop = useCallback(() => {
    setSubmitted(null)
    stopRun()
  }, [stopRun])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      // Not `preventDefault`: Escape closes the Inspector drawer below 1100px,
      // and a pane that swallowed it would trap a reader in a dialog.
      stop()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [stop])

  /**
   * The proposal this session produced, and whether the session stands behind
   * it — **two questions, and only one of them is this pane's to answer.**
   *
   * `outcomeOf` is the desk's one reading of "is this run clean", shared with
   * the Create dialog so that a rule about a session is not kept in two places.
   * What the two do about the answer differs, and reasonably: the dialog offers
   * no source to write, and this pane keeps the session on screen — the stream,
   * the document, the runtime's quoted checks — with Accept refused and the
   * run's own words on the control. A reader who has just watched a session
   * fail is better served by seeing what it produced and why it cannot be used
   * than by the whole thing disappearing.
   */
  const outcome = outcomeOf(run)
  const proposal = run.events.find(
    (event): event is Extract<AssistantEvent, { type: 'proposal' }> => event.type === 'proposal'
  )
  /**
   * What accepting this proposal would do to the draft.
   *
   * Computed from the two documents and from nothing the model said about its
   * own work. It is memoised on the draft and the proposal because the pane
   * re-renders on every event of the next run.
   */
  const proposed = proposal?.document
  /**
   * The diff, against the **baseline** and never against the live buffer.
   *
   * What is drawn has to be what Accept would apply, and Accept applies the
   * proposal to the draft it was made about. A diff recomputed against a buffer
   * that has moved describes an edit nobody proposed.
   */
  const diff = useMemo(
    () => (proposed === undefined ? undefined : diffProposal(baseline?.bytes, proposed)),
    [baseline, proposed]
  )
  /** Whether the page still holds the document this proposal is about. */
  const onBaseline =
    baseline !== null &&
    baseline.bytes === draft &&
    baseline.path === identity?.path &&
    baseline.generation === identity?.generation
  const sentDraft = carriesDraft(baseline?.bytes)
  const disposition: Disposition = rejected
    ? 'rejected'
    : accepted !== null && draft === accepted
      ? 'accepted'
      : 'open'

  const write = session.write
  const acceptIntoDraft = useCallback(() => {
    if (proposed === undefined) return
    // **Asked again at the instant of the click.** The rendered `busy` disables
    // the button a render after a save is claimed, and the claim is
    // synchronous; the baseline is re-checked here for the same reason.
    if (busyNow.current() !== '' || !onBaseline) return
    // **One write, and one undo entry.** The key is this accept's own, so a
    // second accept is a second action rather than being coalesced into the
    // first — which would make one Undo take both of them back.
    const landed = { text: '' }
    write(
      (current) => {
        const next = applyProposal(current, proposed)
        // Read back on the next line: `write` runs this synchronously, and the
        // bytes it produced are what "already in the draft" is a claim about.
        landed.text = next.text
        return next
      },
      { coalesceKey: `assistant-accept:${(accepts.current += 1)}` }
    )
    setAccepted(landed.text)
  }, [proposed, write, onBaseline])

  if (slot.endpoint === null || !slot.keyPresent) {
    return (
      <p className={styles.empty}>
        {slot.endpoint === null
          ? 'No assistant is configured on this desk. Configure an endpoint in Admin › Assistant.'
          : 'An endpoint is configured and no key is stored on this machine. Add one in Admin › Assistant.'}
      </p>
    )
  }

  // **Running covers the whole of a session**, including the reading of the
  // runtime's prompts. Stop has to be usable while the desk is waiting for one:
  // a wait nobody can end is the same trap as a run nobody can end.
  const running =
    run.status === 'running' || (submitted !== null && (prompt.isFetching || waitingForTest))
  const accept = acceptState({
    editing,
    proposal: proposal !== undefined,
    failure: outcome.failure,
    running,
    onBaseline,
    busy: busy(),
    disposition,
    writable: proposed !== undefined && writable(proposed)
  })
  return (
    <div className={styles.pane}>
      <p className={styles.status}>
        {/*
          **The tier the file asked for, and the state the session actually
          reached.** ADR-0001's five states: three a person selects, and two the
          desk discovers and must report — a model that always thinks, and an
          endpoint with no thinking at all. The state is read off this run's own
          events rather than remembered, so the line and the stream cannot
          disagree.
        */}
        {run.engineId} · {slot.endpoint.model} ·{' '}
        {thinkingLine(slot.thinking, stateFromEvents(slot.thinking, run.events))}
      </p>
      {ran !== undefined && (
        <p className={styles.status}>
          Running the runtime’s {ran} prompt
          {ran === FIX_PACK_PROMPT ? `, over ${diagnosticCount} diagnostic${
            diagnosticCount === 1 ? '' : 's'
          }` : ''}
          .
        </p>
      )}

      <label className={styles.label} htmlFor="assistant-policy">
        What should this pack decide?
      </label>
      <TextArea
        id="assistant-policy"
        rows={4}
        value={typed}
        disabled={running}
        onChange={(event) => setTyped(event.currentTarget.value)}
      />
      <div className={styles.actions}>
        <Button
          variant="primary"
          disabled={running || typed.trim() === '' || !advertised}
          onClick={() =>
            setSubmitted({
              id: (nextRun.current += 1),
              name: AUTHOR_PACK_PROMPT,
              args: { policy: typed }
            })
          }
        >
          Run
        </Button>
        {/*
          **Fix is the same session with the runtime's other prompt.** Same
          engine, same gate, same proposal path — what changes is which of the
          runtime's own words the model is given, and that it is handed the
          diagnostics the check on this page already produced.
        */}
        <Button
          disabled={running || diagnostics === undefined || !canFix}
          title={fixWhy(diagnosticCount, canFix, prompts.isSuccess)}
          onClick={() => {
            if (diagnostics === undefined) return
            setSubmitted({
              id: (nextRun.current += 1),
              name: FIX_PACK_PROMPT,
              // The runtime's own bytes, handed on unread.
              args: { diagnostics: diagnostics.bytes }
            })
          }}
        >
          Fix
        </Button>
        <Button disabled={!running} onClick={stop}>
          Stop
        </Button>
      </div>
      {!advertised && prompts.isSuccess && (
        <p className={styles.notice}>
          This runtime advertises no {AUTHOR_PACK_PROMPT} prompt, so there is nothing for the
          assistant to run.
        </p>
      )}
      {prompt.error !== null && submitted !== null && (
        <p className={styles.notice}>
          The runtime’s {submitted.name} prompt could not be read: {prompt.error.message}
        </p>
      )}
      {/*
        **Which of the two ways the desk has no testing prompt.** The engine
        says only that it has none; this says why, because only this knows. The
        session runs either way — without a critic.
      */}
      {testPrompt.error !== null && (
        <p className={styles.notice}>
          The runtime’s {TEST_PACK_PROMPT} prompt could not be read, so the refutation pass did
          not run: {testPrompt.error.message}
        </p>
      )}

      <EventList events={run.events} failure={run.failure} />

      {proposal !== undefined && disposition === 'rejected' && (
        <p className={styles.honesty}>
          The proposal was rejected. Nothing was written, and what the assistant did is still
          above.
        </p>
      )}

      {proposal !== undefined && disposition !== 'rejected' && (
        <section className={styles.proposal} aria-label="The proposal">
          <p className={styles.heading}>
            Proposal — {sentDraft ? 'an update to the draft it was given' : 'a new document'}
          </p>
          <p className={styles.honesty}>
            Nothing has been written. This is a document to accept or reject, and the checks below
            are the runtime’s own words.
          </p>
          {outcome.failure !== '' && (
            <p className={styles.notice}>
              This session did not stand behind what it proposed: {outcome.failure}. It cannot be
              accepted into the draft.
            </p>
          )}
          {diff !== undefined && <ProposalDiffView diff={diff} onBaseline={onBaseline} />}
          <p className={styles.label}>The whole proposed document</p>
          <CodeArea
            value={JSON.stringify(proposal.document, null, 2)}
            readOnly
            aria-label="The proposed document"
          />
          <ProposalUnknowns unknowns={proposal.unknowns} />
          <RuntimeChecks events={run.events} />
          <RefutationReport events={run.events} />
          <div className={styles.actions}>
            {/*
              **The reading route has no draft to accept into**, so it says
              where one is rather than drawing a control that would refuse.
              The rest of the tab runs there exactly as it does on `?edit`.
            */}
            {editing ? (
              <Button
                variant="primary"
                disabled={!accept.enabled}
                title={accept.why === '' ? undefined : accept.why}
                onClick={acceptIntoDraft}
              >
                Accept into draft
              </Button>
            ) : (
              <p className={styles.honesty}>Open Edit to accept.</p>
            )}
            <Button disabled={disposition !== 'open' || running} onClick={() => setRejected(true)}>
              Reject
            </Button>
          </div>
          {disposition === 'open' && editing && !onBaseline && (
            <p className={styles.notice}>{DRAFT_MOVED}</p>
          )}
          {disposition === 'accepted' && (
            <p className={styles.honesty}>
              Accepted into the draft. <strong>Nothing has been saved.</strong> The check runs
              again over the new bytes, Undo takes the whole accept back in one step, and Save is
              yours to press.
            </p>
          )}
        </section>
      )}
    </div>
  )
}
