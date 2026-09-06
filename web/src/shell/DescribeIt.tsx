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
  /** A run has been asked for in this dialog, and has not been discarded. */
  asked: boolean
  /** The prompt is being read, or the engine is running. */
  running: boolean
  events: readonly AssistantEvent[]
  /**
   * The run's proposal: the canonical frozen snapshot the run hook ingested,
   * and never a value read back off an engine's own event.
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
  /** True once this session has been thrown away, until the next Propose. */
  const [discarded, setDiscarded] = useState(true)
  const prompt = usePromptText(AUTHOR_PACK_PROMPT, submitted !== null && advertised, submitted?.args)
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
    if (started.current === submitted.id) return
    started.current = submitted.id
    // **No draft.** There is no document yet — that is what this section is
    // for — so what comes back is a whole document rather than an edit.
    startRun(prompt.data.text)
  }, [submitted, prompt.data, startRun])

  const stopRun = run.stop
  /**
   * Stop, in **both** phases of a session.
   *
   * A session begins with the desk reading the runtime's prompt, and only then
   * does an engine run. Clearing the submission disables that query and keeps
   * the effect above from starting a run for it.
   */
  const stop = useCallback(() => {
    setSubmitted(null)
    stopRun()
  }, [stopRun])

  const discard = useCallback(() => {
    setSubmitted(null)
    setTyped('')
    setDiscarded(true)
    stopRun()
  }, [stopRun])

  const propose = useCallback(() => {
    setDiscarded(false)
    setSubmitted({ id: (nextRun.current += 1), args: { policy: typed } })
  }, [typed])

  const events = discarded ? EMPTY : run.events
  const proposal = events.find(
    (event): event is ProposalEvent => event.type === 'proposal'
  )
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
    promptFailed === undefined &&
    (started.current !== submitted.id || run.status === 'running')

  return {
    usable: slot.endpoint !== null && slot.keyPresent,
    unusableBecause: slot.endpoint === null ? NO_ASSISTANT : NO_KEY,
    advertised,
    standing:
      slot.endpoint === null
        ? ''
        : `${slot.engine} · ${slot.endpoint.model} · thinking ${slot.thinking}`,
    typed,
    setTyped,
    asked: !discarded && submitted !== null,
    running,
    events,
    proposal,
    problem: promptFailed ?? failures[failures.length - 1]?.message ?? '',
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
      {proposal === undefined && state.asked && !state.running && (
        <p className={styles.notice}>{state.problem === '' ? NOTHING_PROPOSED : state.problem}</p>
      )}
    </div>
  )
}
