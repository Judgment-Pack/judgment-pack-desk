/**
 * **Describe it**: the third way to start a pack, beside the runtime's
 * templates.
 *
 * Type the policy in your own words, press Propose, and the assistant works the
 * runtime's own `author_pack` prompt through the tools it is allowed — the
 * schema, the examples, `validate`, a rehearsal evaluation — to a proposal. The
 * dialog shows what it did and what it produced, with the runtime's checks
 * quoted whole.
 *
 * **Nothing is written here.** This section proposes; the dialog's Create
 * button writes, exactly as it writes a template, and until it is pressed there
 * is no file. ADR-0001: the proposal is the only sink.
 *
 * **The run is the run hook's, never this section's.** `useAssistantRun` owns
 * the connection, the abort controller and the one terminal event, and this
 * section only starts and stops it. That matters most where the dialog closes
 * mid-run: the close path calls `discard()`, which stops the run *through the
 * hook* — one terminal event on the stream, one socket close — rather than
 * leaving an unmount to abort an engine iterator nobody is reading.
 *
 * **Nothing is persisted.** Closing the dialog ends the session and discards
 * the proposal; opening it again is a new one. There is no draft of a
 * conversation to restore, and a dialog that reopened one would be re-offering
 * a document nobody accepted.
 *
 * **It renders as a control only where there is an assistant to run.** No
 * endpoint, or no key on this machine, and it is one line saying where that is
 * configured — the prompt this dialog already offers for copying out stays
 * there for any chat client.
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import { EventList } from '../assistant/EventList'
import {
  ProposalSummaryLine,
  ProposalUnknowns,
  RuntimeChecks
} from '../assistant/ProposalReport'
import { useAssistantRun } from '../assistant/useAssistantRun'
import { useAssistantSlot } from '../assistant/useAssistantSlot'
import { AUTHOR_PACK_PROMPT, usePromptNames, usePromptText } from '../mcp/prompts'
import { Button } from '../ui/Button'
import { CodeArea } from '../ui/CodeArea'
import { TextArea } from '../ui/TextArea'
import styles from './DescribeIt.module.css'
import type { AssistantEvent } from '../assistant/engine'
import type { AssistantEndpointConfig } from '../config/deskConfig'

/** The question the field asks, which is the whole of what the desk adds. */
export const DESCRIBE_LABEL =
  'What should this pack decide? Who decides, on what facts, with what outcomes?'

/** Said where the disclosure is opened and there is no assistant to run. */
export const NO_ASSISTANT =
  'No assistant is configured on this desk. Configure an endpoint in Admin › Assistant.'
export const NO_KEY =
  'An endpoint is configured and no key is stored on this machine. Add one in Admin › Assistant.'

/** Why Create cannot act on a proposal, where the reason is the section's. */
export const STILL_RUNNING = 'The assistant is still running. Stop it or wait for it to end.'
export const NOTHING_PROPOSED =
  'The assistant ended without a document, so there is nothing to write.'
/** Said where the assistant was taken away with a session in progress. */
export const SLOT_LOST =
  'The assistant went away while this was open, so its session was ended and anything it had proposed was discarded.'

type ProposalEvent = Extract<AssistantEvent, { type: 'proposal' }>

/**
 * The section's whole state, held by the dialog around it.
 *
 * A hook rather than component state because the **dialog** has to be able to
 * stop the run — closing is not the section's event — and because Create reads
 * the proposal to decide what it would write. Two consumers, one reading.
 */
export interface DescribeItState {
  /** An endpoint is configured and a key is stored on this machine. */
  usable: boolean
  /** Which line to render where it is not usable. */
  unusableBecause: string
  /** Whether the runtime advertises the prompt this section runs. */
  advertised: boolean
  /** The engine, the model and the tier, as the tab states them. */
  standing: string
  typed: string
  setTyped: (text: string) => void
  /**
   * There is a proposal on offer: the latest submission produced one and
   * nothing went wrong after it.
   *
   * The dialog offers the proposal as a source on exactly this, so a source
   * that cannot be written is never on the list. Why there is none is
   * `blocking`, which holds Create whatever source is selected.
   */
  offered: boolean
  /**
   * **Why Create must not act at all, or ''.**
   *
   * Not a fact about the selected source: a submission that is in flight, or
   * that settled without a document this desk can write, holds the whole dialog
   * — because the alternative is a Create that quietly falls back to a template
   * the author did not choose, one press after they asked for something else.
   * The way out is another Propose, or closing the dialog.
   */
  blocking: string
  /** The prompt is being read, or the engine is running. */
  running: boolean
  events: readonly AssistantEvent[]
  /**
   * The proposal **of the latest submission**, where that submission's run
   * ended with one and nothing went wrong after it.
   *
   * The canonical frozen snapshot the run hook ingested, and never a value
   * read back off an engine's own event. Undefined is a real answer with three
   * causes — no run has ended, the run produced no proposal, or it produced one
   * and then failed — and `problem` says which.
   */
  proposal: ProposalEvent | undefined
  /** The last thing that went wrong on the stream, or ''. */
  problem: string
  propose: () => void
  stop: () => void
  /** Stop the run **and** drop everything it produced. The close path. */
  discard: () => void
}

/** An endpoint-shaped nothing, so the run hook stays unconditional. */
const NO_ENDPOINT: AssistantEndpointConfig = {
  url: '',
  kind: 'openai-compatible',
  model: '',
  tools: []
}

/** One list, allocated once, so an idle section re-renders to the same value. */
const EMPTY: readonly AssistantEvent[] = []

export function useDescribeIt(): DescribeItState {
  const slot = useAssistantSlot()
  const prompts = usePromptNames()
  const advertised = (prompts.data ?? []).includes(AUTHOR_PACK_PROMPT)
  const [typed, setTyped] = useState('')
  /**
   * The submission a person actually made, **keyed by a run id**.
   *
   * The id is why this is an object and not the policy text: pressing Propose
   * twice with the text unchanged is two runs, because a model is not a pure
   * function, and React sees no state change in the same string.
   */
  const [submitted, setSubmitted] = useState<{ id: number; args: Record<string, string> } | null>(
    null
  )
  const nextRun = useRef(0)
  /**
   * The submission whose run the hook is carrying events for.
   *
   * **State, not a ref, because the rendering depends on it.** `useAssistantRun`
   * clears its event list when a run *starts*, which is one effect after the
   * submission that asked for it — so between pressing Propose and the engine
   * beginning, `run.events` still holds the *previous* run's proposal. A
   * section that read them then would offer a document the latest Propose did
   * not produce, and if that second run never started at all — a refused
   * `prompts/get`, a Stop while the prompt was still being read — it would go
   * on offering it for ever. A proposal belongs to the submission that produced
   * it; this is the id that says which one, and events from any other are not
   * this section's.
   */
  const [ranId, setRanId] = useState<number | null>(null)
  /**
   * The submission a person stopped, if any.
   *
   * Stop used to clear `submitted`, which is now the thing that identifies
   * whose events are on screen: clearing it took the stopped run's own stream
   * off the page along with the terminal event it had just written. So the
   * submission stays and is marked, which is what disables the prompt query and
   * keeps the start effect from starting a run for it.
   */
  const [stoppedId, setStoppedId] = useState<number | null>(null)
  /** True once this session has been thrown away, until the next Propose. */
  const [discarded, setDiscarded] = useState(true)
  /**
   * Set where the assistant was taken away mid-session.
   *
   * It holds Create afterwards, and that is deliberate: the source the author
   * chose has just been discarded, and a Create that quietly fell back to a
   * template would write a document they did not pick — the same trap as a
   * stale proposal, arriving from the other side.
   */
  const [lost, setLost] = useState('')
  const prompt = usePromptText(
    AUTHOR_PACK_PROMPT,
    submitted !== null && submitted.id !== stoppedId && advertised,
    submitted?.args
  )
  const run = useAssistantRun({
    // Only ever started where the endpoint exists; the fallback keeps the hook
    // unconditional, which is the rule React enforces.
    endpoint: slot.endpoint ?? NO_ENDPOINT,
    engine: slot.engine,
    thinking: slot.thinking
  })

  // The run starts once the prompt this submission asked for has arrived, and
  // once per submission: the query answers again on a refetch, and a second
  // start would be a second `jpack mcp` for one press of Propose.
  const started = useRef<number | null>(null)
  const startRun = run.start
  useEffect(() => {
    if (submitted === null || prompt.data === undefined) return
    if (submitted.id === stoppedId) return
    if (started.current === submitted.id) return
    started.current = submitted.id
    setRanId(submitted.id)
    // **No draft.** There is no document yet — that is what this section is
    // for — so what comes back is a whole document rather than an edit.
    startRun(prompt.data.text)
  }, [submitted, stoppedId, prompt.data, startRun])

  const stopRun = run.stop
  /**
   * Stop, in **both** phases of a session.
   *
   * A session begins with the desk reading the runtime's prompt, and only then
   * does an engine run. Clearing the submission disables that query and keeps
   * the effect above from starting a run for it.
   */
  const stop = useCallback(() => {
    setSubmitted((current) => {
      if (current !== null) setStoppedId(current.id)
      return current
    })
    stopRun()
  }, [stopRun])

  /**
   * The latest `discard`, callable from an effect that must not re-run when it
   * is rebuilt. The effect above is about `usable` changing and nothing else.
   */
  const discardNow = useRef<() => void>(() => {})

  const discard = useCallback(() => {
    setLost('')
    setSubmitted(null)
    setRanId(null)
    setStoppedId(null)
    setTyped('')
    setDiscarded(true)
    stopRun()
  }, [stopRun])
  discardNow.current = discard

  /**
   * **Losing the assistant ends the session, it does not merely hide it.**
   *
   * `usable` used to control rendering alone: the key going out of the store,
   * or the endpoint leaving the file, replaced the controls with a sentence
   * while the run behind them carried on — its `jpack mcp` alive, its proposal
   * still selected, and Create still willing to write it. A slot that is gone
   * is a session that cannot be finished, so it is stopped through the run hook
   * (one terminal event, one connection close) and everything it produced goes
   * with it.
   *
   * It runs on the way in as well, where there is nothing to discard: the key
   * read has not answered yet and `usable` is honestly false.
   */
  const usable = slot.endpoint !== null && slot.keyPresent
  /**
   * Whether there is a session to take away, read at the instant of the loss.
   *
   * A ref because the effect below is about `usable` changing and nothing else:
   * on the way in, before the key read has answered, `usable` is honestly false
   * and there is nothing to end — and a dialog that opened holding Create shut
   * over a session nobody started would be worse than the defect this fixes.
   */
  const hadSession = useRef(false)
  hadSession.current = !discarded && submitted !== null
  useEffect(() => {
    if (usable) {
      setLost('')
      return
    }
    if (!hadSession.current) return
    discardNow.current()
    setLost(SLOT_LOST)
  }, [usable])

  /**
   * A new submission, and the previous proposal gone **at the press**.
   *
   * The id is what invalidates it, and it does so here rather than where the
   * run starts: `ranId` still names the run before this one, the events gate
   * below compares the two, and everything between the press and the start is
   * therefore a moment in which the section has no proposal rather than the
   * previous one. A second Propose that fails before its run begins — a refused
   * `prompts/get`, a Stop while the prompt is still being read — never reaches
   * the place that would have cleared it, which is the whole of the finding.
   *
   * (An explicit `setRanId(null)` stood here and is gone: the ids are strictly
   * increasing, so it could never change the comparison, and the mutation
   * harness reported it as a line nothing holds.)
   */
  const propose = useCallback(() => {
    setLost('')
    setDiscarded(false)
    setStoppedId(null)
    setSubmitted({ id: (nextRun.current += 1), args: { policy: typed } })
  }, [typed])

  /**
   * The events of the **latest submission's** run, and of no other.
   *
   * The gate is the id and not the emptiness of the list: `run.events` is a
   * live list belonging to whichever run the hook last started, and the whole
   * of this finding is that the section must not read it while it belongs to an
   * older submission.
   */
  const events =
    discarded || submitted === null || ranId !== submitted.id ? EMPTY : run.events
  /**
   * The proposal, and whether anything went wrong **after** it.
   *
   * The contract does not make `proposal` an engine's last non-terminal event.
   * An engine that proposes a document and then fails a final check has not
   * offered that document — it has shown its work and then said the work did
   * not stand — so an `error` after a proposal withdraws it. Before it is
   * another matter: a tool call that failed and was retried is an ordinary run.
   */
  /**
   * What this run failed with **after** its terminal event, where it did.
   *
   * The stream cannot carry it — one `end` is the contract — so the run hook
   * reports it beside the events, and it counts the same way an `error` on the
   * stream does: a session that fell over while unwinding is not one whose
   * document this dialog may write.
   */
  const unwound = discarded || submitted === null || ranId !== submitted.id ? undefined : run.failure
  const proposedAt = events.findIndex((event) => event.type === 'proposal')
  const withdrawn =
    proposedAt !== -1 &&
    (unwound !== undefined ||
      events.slice(proposedAt + 1).some((event) => event.type === 'error'))
  const proposal =
    proposedAt === -1 || withdrawn ? undefined : (events[proposedAt] as ProposalEvent)
  const failures = events.filter(
    (event): event is Extract<AssistantEvent, { type: 'error' }> => event.type === 'error'
  )
  const promptFailed =
    submitted !== null && prompt.error !== null ? prompt.error.message : undefined
  /**
   * **In flight from the press of Propose until the run's terminal event.**
   *
   * Not `run.status === 'running'`, and not that plus "the prompt is being
   * fetched" either: both leave a gap. A session begins with the desk reading
   * the runtime's prompt and only then does an engine start, and the start
   * happens in an effect — which React runs *after* it has painted. So between
   * a cached prompt answering and the run beginning there is a frame in which
   * the status is still `idle` (or `finished`, from the run before) and the
   * fetch is not in flight, and the dialog around this would offer Create with
   * the previous session's proposal as its source.
   *
   * The submission's own id closes it: a submission the effect has not started
   * yet is in flight by definition. A prompt that was *refused* is the one way
   * out — the effect will never start that run, and a section stuck reporting a
   * run that cannot begin is worse than one saying why it did not.
   */
  const running =
    !discarded &&
    submitted !== null &&
    submitted.id !== stoppedId &&
    promptFailed === undefined &&
    (ranId !== submitted.id || run.status === 'running')

  /**
   * The whole of what this section says to Create.
   *
   * Read in order: no submission is nothing to say; a submission in flight
   * holds Create because its events are about to be replaced; a settled
   * submission with no usable proposal holds it and quotes whatever went
   * wrong — a refused prompt, a run that failed, a proposal withdrawn by an
   * error after it, or a document that could not be read as JSON data.
   */
  const problem = promptFailed ?? unwound ?? failures[failures.length - 1]?.message ?? ''
  const blocking =
    lost !== ''
      ? lost
      : discarded || submitted === null
        ? ''
        : running
          ? STILL_RUNNING
          : proposal === undefined
            ? problem === ''
              ? NOTHING_PROPOSED
              : problem
            : ''

  return {
    usable,
    unusableBecause: slot.endpoint === null ? NO_ASSISTANT : NO_KEY,
    advertised,
    standing:
      slot.endpoint === null
        ? ''
        : `${slot.engine} · ${slot.endpoint.model} · thinking ${slot.thinking}`,
    typed,
    setTyped,
    offered: proposal !== undefined,
    blocking,
    running,
    events,
    proposal,
    problem,
    propose,
    stop,
    discard
  }
}

export function DescribeIt({ state }: { state: DescribeItState }) {
  // One line, and no control that would refuse. The prompt copy-out this
  // dialog already offers is what an unconfigured desk uses instead.
  if (!state.usable) return <p className={styles.quiet}>{state.unusableBecause}</p>
  return (
    <details className={styles.disclosure}>
      <summary className={styles.summary}>Describe it instead</summary>
      <Section state={state} />
    </details>
  )
}

function Section({ state }: { state: DescribeItState }) {
  const { proposal } = state
  return (
    <div className={styles.section}>
      <p className={styles.quiet}>{state.standing}</p>
      <label className={styles.label} htmlFor="describe-policy">
        {DESCRIBE_LABEL}
      </label>
      <TextArea
        id="describe-policy"
        rows={4}
        value={state.typed}
        disabled={state.running}
        onChange={(event) => state.setTyped(event.currentTarget.value)}
      />
      <div className={styles.actions}>
        <Button
          disabled={state.running || state.typed.trim() === '' || !state.advertised}
          onClick={state.propose}
        >
          Propose
        </Button>
        <Button disabled={!state.running} onClick={state.stop}>
          Stop
        </Button>
      </div>
      <p className={styles.quiet}>
        Nothing is written until you press Create. The name above gives the pack its id and its
        file name, whatever the assistant proposes to call it.
      </p>
      {!state.advertised && (
        <p className={styles.notice}>
          This connection advertises no {AUTHOR_PACK_PROMPT} prompt, so there is nothing for the
          assistant to run.
        </p>
      )}
      <EventList events={state.events} label="What the assistant did" compact />
      {proposal !== undefined && (
        <section className={styles.proposal} aria-label="The proposal">
          <ProposalSummaryLine document={proposal.document} />
          <ProposalUnknowns unknowns={proposal.unknowns} />
          <RuntimeChecks events={state.events} />
          <details className={styles.disclosure}>
            <summary className={styles.summary}>Show document</summary>
            <CodeArea
              value={JSON.stringify(proposal.document, null, 2)}
              readOnly
              aria-label="The proposed document"
            />
          </details>
        </section>
      )}
      {proposal === undefined && state.blocking !== '' && !state.running && (
        <p className={styles.notice}>{state.blocking}</p>
      )}
    </div>
  )
}
