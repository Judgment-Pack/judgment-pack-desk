/**
 * The Assistant tab: type what the pack should decide, and watch the runtime
 * be consulted about it.
 *
 * **It proposes. It does not write, and it does not decide.** Everything on
 * screen is either the assistant's own account of what it did or the runtime's
 * words quoted whole — the `validate` report and the rehearsal evaluation are
 * shown as the runtime wrote them, never summarised, because a summary of a
 * verdict is a second verdict. Accept and Reject are drawn and disabled: the
 * diff on the draft and the span-preserving write are the next chunk, and a
 * button that looked ready would be this pane promising something nothing here
 * does.
 *
 * **It renders only where there is an assistant to run.** No endpoint, or no
 * key on this machine, and it says in one line where that is configured rather
 * than showing a control that would refuse.
 *
 * **Nothing is persisted.** Leaving the route ends the session and closes its
 * connection; coming back is a new one.
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import { AUTHOR_PACK_PROMPT, usePromptNames, usePromptText } from '../mcp/prompts'
import { Button } from '../ui/Button'
import { CodeArea } from '../ui/CodeArea'
import { TextArea } from '../ui/TextArea'
import { useAssistantRun } from './useAssistantRun'
import { useAssistantSlot } from './useAssistantSlot'
import styles from './AssistantPane.module.css'
import type { AssistantEvent } from './engine'

const ACCEPT_LATER = 'Accept into draft arrives in the next chunk'

/** The bytes of one tool answer, said the way the desk says byte counts. */
function byteCount(text: string): number {
  return new TextEncoder().encode(text).length
}

export function AssistantPane() {
  const slot = useAssistantSlot()
  const prompts = usePromptNames()
  const advertised = (prompts.data ?? []).includes(AUTHOR_PACK_PROMPT)

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
  const [submitted, setSubmitted] = useState<{ id: number; policy: string } | null>(null)
  const nextRun = useRef(0)
  const prompt = usePromptText(
    AUTHOR_PACK_PROMPT,
    advertised && submitted !== null,
    submitted === null ? undefined : { policy: submitted.policy }
  )

  const run = useAssistantRun({
    // Only rendered where the endpoint exists; the fallback keeps the hook
    // unconditional, which is the rule React enforces.
    endpoint: slot.endpoint ?? { url: '', kind: 'openai-compatible', model: '', tools: [] },
    engine: slot.engine,
    thinking: slot.thinking
  })

  // The run starts when the prompt this submission asked for has arrived, and
  // once per submission: the query answers again on a refetch, and a second
  // start would be a second `jpack mcp` for one press of Run.
  const started = useRef<number | null>(null)
  const startRun = run.start
  useEffect(() => {
    if (submitted === null || prompt.data === undefined) return
    if (started.current === submitted.id) return
    started.current = submitted.id
    startRun(prompt.data.text)
  }, [submitted, prompt.data, startRun])

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

  if (slot.endpoint === null || !slot.keyPresent) {
    return (
      <p className={styles.empty}>
        {slot.endpoint === null
          ? 'No assistant is configured on this desk. Configure an endpoint in Admin › Assistant.'
          : 'An endpoint is configured and no key is stored on this machine. Add one in Admin › Assistant.'}
      </p>
    )
  }

  const running = run.status === 'running' || (submitted !== null && prompt.isFetching)
  const proposal = run.events.find(
    (event): event is Extract<AssistantEvent, { type: 'proposal' }> => event.type === 'proposal'
  )
  const results = run.events.filter(
    (event): event is Extract<AssistantEvent, { type: 'tool_result' }> =>
      event.type === 'tool_result'
  )
  const checked = (name: string) => [...results].reverse().find((result) => result.name === name)

  return (
    <div className={styles.pane}>
      <p className={styles.status}>
        {run.engineId} · {slot.endpoint.model} · thinking {slot.thinking}
      </p>
      {run.substituted !== undefined && <p className={styles.notice}>{run.substituted}</p>}

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
          onClick={() => setSubmitted({ id: (nextRun.current += 1), policy: typed })}
        >
          Run
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
          The runtime’s {AUTHOR_PACK_PROMPT} prompt could not be read: {prompt.error.message}
        </p>
      )}

      {run.events.length > 0 && (
        <ol className={styles.stream} aria-label="What the assistant did">
          {run.events.map((event, index) => (
            <li key={index} className={lineClass(event)}>
              {describe(event)}
            </li>
          ))}
        </ol>
      )}

      {proposal !== undefined && (
        <section className={styles.proposal} aria-label="The proposal">
          <p className={styles.heading}>Proposal</p>
          <p className={styles.honesty}>
            Nothing has been written. This is a document to accept or reject, and the checks below
            are the runtime’s own words.
          </p>
          <CodeArea
            value={JSON.stringify(proposal.document, null, 2)}
            readOnly
            aria-label="The proposed document"
          />
          <p className={styles.label}>Unknowns the assistant declared</p>
          {proposal.unknowns.length === 0 ? (
            <p className={styles.honesty}>It declared none.</p>
          ) : (
            <ul className={styles.unknowns}>
              {proposal.unknowns.map((unknown) => (
                <li key={unknown}>{unknown}</li>
              ))}
            </ul>
          )}
          {(['validate', 'experimental_evaluate'] as const).map((name) => {
            const result = checked(name)
            if (result === undefined) return null
            return (
              <div key={name}>
                <p className={styles.label}>
                  {name} — the runtime’s answer, quoted{result.isError ? ' (isError)' : ''}
                </p>
                <CodeArea value={result.text} readOnly aria-label={`${name}, as the runtime wrote it`} />
              </div>
            )
          })}
          <div className={styles.actions}>
            <Button variant="primary" disabled title={ACCEPT_LATER}>
              Accept
            </Button>
            <Button disabled title={ACCEPT_LATER}>
              Reject
            </Button>
          </div>
        </section>
      )}
    </div>
  )
}

function lineClass(event: AssistantEvent): string {
  if (event.type === 'guardrail' || event.type === 'thinking_unavailable') return styles.guard!
  if (event.type === 'error') return styles.error!
  return styles.line!
}

/**
 * One event as one line.
 *
 * A tool result is reported as its byte count and the runtime's own `isError`,
 * never as a reading of what it said: the pane has no opinion about a report it
 * did not write, and the proposal below quotes the two that matter in full.
 */
function describe(event: AssistantEvent): string {
  switch (event.type) {
    case 'tool_call':
      return `called ${event.name}(${Object.keys((event.args ?? {}) as object).join(', ')})`
    case 'tool_result':
      return `${event.name} answered ${byteCount(event.text)} bytes${
        event.isError ? ' (isError)' : ''
      }${event.structured === undefined ? '' : ' with structured content'}`
    case 'guardrail':
      return `${event.action} ${event.tool}: ${event.detail}`
    case 'thinking_unavailable':
      return event.detail
    case 'reasoning':
      return `${event.text.length} characters of reasoning`
    case 'critique':
      return event.text
    case 'proposal':
      return `proposed a document with ${event.unknowns.length} unknown(s); nothing was written`
    case 'error':
      return event.message
    case 'end':
      return 'the session ended'
  }
}
