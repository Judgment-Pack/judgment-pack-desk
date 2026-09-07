/**
 * Admin › Assistant: the endpoint this desk is configured for, and the one key
 * it keeps — as one of Admin's cards.
 *
 * **This section carries the desk's two writes**, and each is exactly as wide
 * as its reason. A key must never be pasted into a project file, so it cannot
 * go through the file API — which writes only inside the project — and gets
 * its own endpoint. The `assistant` object of the desk-level file is the
 * other: choosing a model and a thinking tier is something an author does
 * while working, and the alternative is telling them to edit a file in a
 * configuration directory by hand between attempts.
 *
 * **The prose is gone and the states are not.** What used to be four
 * paragraphs about deployment shapes, key custody and the probe is now the
 * card's own four facts plus the lines that come from closed vocabularies: the
 * key row's states, the binding, the probe's diagnostic. Those are not
 * narration — each is one answer out of a fixed set, and dropping one would
 * drop a state the desk can be in.
 *
 * **A configuration this desk could not read is its own state here too**, and
 * it is the card's Status line rather than a warning note: the form then holds
 * the built-in defaults rather than anything anybody configured, so it is shown
 * and not edited.
 *
 * Nothing here says chassis, bytes or path to the reader. The words are the
 * desk, this machine, and the file.
 */
import { useRef, useState, type ReactNode, type RefObject } from 'react'
import { CardField, SourceCard, type SourceStatus } from '../admin/SourceCard'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import type { DeskLevelSummary } from '../config/deskConfig'
import { DIAGNOSTIC_SAYS, type AssistantKeyState } from './client'
import { EndpointForm } from './EndpointForm'
import { keyBinding, type KeyBinding } from './keyBinding'
import { useAssistantSlot } from './useAssistantSlot'
import {
  useAssistantKey,
  useProbeAssistant,
  useRemoveAssistantKey,
  useStoreAssistantKey
} from './queries'

export function AssistantSection({ id, title }: { id: string; title: string }) {
  const { config, desk } = useEffectiveConfig()
  // **The same reading the tab and Describe it take.** A read that did not
  // produce a file establishes nothing about what is in it, and this section
  // is where a reader would go to find that out — so it must not be the one
  // surface still asserting an absence.
  const slot = useAssistantSlot()
  const unavailable = slot.state === 'unavailable'
  const endpoint = config.assistant.endpoint
  const key = useAssistantKey()
  const store = useStoreAssistantKey()
  const remove = useRemoveAssistantKey()
  const probe = useProbeAssistant()
  // **The answer to the last write, held until the key read disagrees with
  // it.** The chassis says `keyRebindRequired` at the instant the endpoint
  // moves, and waiting for the key read to be re-fetched would leave the row
  // saying the key is bound for as long as that took. The read is the
  // authority afterwards: storing a key answers with the new binding, and the
  // row goes back to reading it.
  const [rebindAsked, setRebindAsked] = useState(false)
  // **The field is uncontrolled, and that is the point.** It used to be React
  // state cleared with `setTyped('')` immediately before the request — which
  // reads as synchronous and is not: React batches the update, so `fetch`
  // could begin while both the input and the state still held the key. An
  // uncontrolled input is cleared by assigning to the DOM node, which happens
  // at the instant it is written and not at the next render.
  //
  // Nothing here mirrors the value into state. There is nothing to mirror it
  // for: the page never re-renders from it and never displays it.
  const field = useRef<HTMLInputElement | null>(null)
  // The outcome of the last store or removal, kept locally because the
  // mutation takes no credential variable and exposes no retained
  // credential-bearing state. (An earlier version called `reset()` on
  // settlement and this comment said so; resetting turned out not to clear
  // the mutation cache's copy at all, which is why the key stopped being a
  // mutation variable in the first place. See `useStoreAssistantKey`.)
  const [storeProblem, setStoreProblem] = useState<string | undefined>(undefined)
  const [removeProblem, setRemoveProblem] = useState<string | undefined>(undefined)

  const read = keyBinding(key.data)
  const binding: KeyBinding = rebindAsked && read === 'bound' ? 'rebind' : read

  const submitKey = () => {
    const input = field.current
    const value = input?.value ?? ''
    // Cleared on the node, before the request is made. This assignment has
    // taken effect by the next statement; a `setState` would not have.
    if (input) input.value = ''
    setStoreProblem(undefined)
    store.submit(value, {
      onError: (error) => setStoreProblem(error.message),
      onStored: () => setRebindAsked(false)
    })
  }

  return (
    <SourceCard
      id={id}
      title={title}
      location={
        desk === undefined ? (
          <span className="quiet">nothing has asked for it</span>
        ) : (
          <code>{desk.path}</code>
        )
      }
      status={assistantStatus(desk)}
      content={{ text: desk?.text, member: 'assistant', value: config.assistant }}
      fields={
        <>
          <CardField label="Assistant">
            <strong>
              {unavailable
                ? 'this desk could not read its own configuration'
                : endpoint === null
                  ? 'none — no endpoint configured'
                  : 'a model endpoint'}
            </strong>
          </CardField>
          <KeyControl
            state={key.data ?? NOTHING_READ}
            answered={key.isSuccess}
            failed={key.error}
            binding={binding}
            field={field}
            onStore={submitKey}
            storeProblem={storeProblem}
            onRemove={() => {
              setRemoveProblem(undefined)
              remove.mutate(undefined, {
                onError: (error) => setRemoveProblem(error.message),
                onSettled: () => remove.reset()
              })
            }}
            removeProblem={removeProblem}
          />
          <CardField label="Reachability">
            <button type="button" onClick={() => probe.mutate()}>
              Check reachability
            </button>{' '}
            {probe.isPending && <span className="quiet">asking the endpoint…</span>}
            {probe.data !== undefined && !probe.isPending && <ProbeReading result={probe.data} />}
            {probe.error !== null && !probe.isPending && (
              <span className="quiet">
                the check was refused: <code className="partial-reason">{probe.error.message}</code>
              </span>
            )}
          </CardField>
        </>
      }
      save={
        <EndpointForm
          bound={binding === 'bound'}
          unavailable={unavailable}
          onWritten={(answer) => setRebindAsked(answer.keyRebindRequired)}
        />
      }
    />
  )
}

/**
 * The desk-level file's state, which is this card's state: `assistant` may be
 * configured nowhere else.
 */
function assistantStatus(desk: DeskLevelSummary | undefined): SourceStatus {
  if (desk === undefined) return { state: 'pending' }
  if (desk.problems.length > 0) return { state: 'refused', problems: desk.problems }
  if (desk.readFailure !== undefined) return { state: 'unread', failure: desk.readFailure }
  if (!desk.present) return { state: 'absent' }
  return { state: 'read' }
}

/** What a row that has not been answered renders from. */
const NOTHING_READ: AssistantKeyState = {
  present: false,
  fingerprint: '',
  origin: '',
  kind: '',
  configuredOrigin: '',
  configuredKind: '',
  bound: false
}

/**
 * The key: whether there is one, **which endpoint it is for**, and the two
 * things that can be done about it.
 *
 * **The field is never populated from anything.** There is no value to
 * populate it with — no endpoint returns the key — and a masked field showing
 * a placeholder of the right length would be this page inventing evidence
 * about a value it has never seen.
 *
 * **The field is not offered where storing one cannot work.** A key is written
 * bound to the endpoint configured at that instant, so a desk with none has
 * nothing to bind it to and the chassis refuses. The row asks for an endpoint
 * to be saved instead of offering a field and letting the refusal explain.
 */
function KeyControl({
  state,
  answered,
  failed,
  binding,
  field,
  onStore,
  storeProblem,
  onRemove,
  removeProblem
}: {
  state: AssistantKeyState
  answered: boolean
  failed: Error | null
  binding: KeyBinding
  field: RefObject<HTMLInputElement | null>
  onStore: () => void
  storeProblem: string | undefined
  onRemove: () => void
  removeProblem: string | undefined
}) {
  // Replace is a state of this row and not a second control: it opens the one
  // field there is. It is cleared whenever the binding changes underneath it,
  // because a row that has become "enter the key for another host" is already
  // asking for exactly what Replace asked for.
  const [replacing, setReplacing] = useState(false)
  const [openedAt, setOpenedAt] = useState(binding)
  if (openedAt !== binding) {
    setOpenedAt(binding)
    setReplacing(false)
  }
  const wanted = binding === 'none' || binding === 'rebind'
  const entry = wanted || (binding === 'bound' && replacing)
  // **The desk's own origin for the configured endpoint**, never one this
  // page computed: the browser and Go disagree about an explicit default port,
  // and a label that named a destination the chassis would not present to
  // would be this page inventing the very fact the row exists to report.
  const destination = state.configuredOrigin === '' ? undefined : state.configuredOrigin
  const label = destination === undefined ? 'Key' : `Key for ${destination}`

  return (
    <>
      <CardField label="Key" rule={bindingSays(binding, state, destination)}>
        <strong>{keySays(state, answered, failed)}</strong>
      </CardField>
      {entry && (
        <CardField label={label}>
          <input
            id="assistant-key"
            aria-label={label}
            ref={field}
            type="password"
            autoComplete="off"
            spellCheck={false}
            defaultValue=""
          />{' '}
          <button type="button" onClick={onStore}>
            Store key
          </button>
        </CardField>
      )}
      {binding === 'bound' && !replacing && (
        <CardField label="Replace">
          <button type="button" onClick={() => setReplacing(true)}>
            Replace key
          </button>
        </CardField>
      )}
      {state.present && (
        <CardField label="Remove">
          <button type="button" onClick={onRemove}>
            Remove key
          </button>
        </CardField>
      )}
      {storeProblem !== undefined && (
        <CardField label="Not stored">
          <code className="partial-reason">{storeProblem}</code>
        </CardField>
      )}
      {removeProblem !== undefined && (
        <CardField label="Not removed">
          <code className="partial-reason">{removeProblem}</code>
        </CardField>
      )}
    </>
  )
}

/**
 * The one sentence the binding is worth, per state.
 *
 * **Both halves of a mismatch are named.** A row that said only "enter the key
 * again" would leave a reader unable to see *which* of the two moved — the
 * endpoint they just saved, or a key entered months ago for somewhere else —
 * and neither half is a secret: both are in the file this page already reads.
 */
function bindingSays(
  binding: KeyBinding,
  state: AssistantKeyState,
  destination: string | undefined
): ReactNode {
  if (binding === 'unread') return <>this desk has not been asked yet</>
  if (binding === 'no-endpoint') {
    return <>Save an endpoint first: a key is kept bound to the endpoint it was entered for.</>
  }
  if (binding === 'none') {
    return (
      <>
        No key is stored for <code>{destination}</code>.
      </>
    )
  }
  if (binding === 'bound') {
    return (
      <>
        Entered for <code>{state.origin}</code> over <code>{state.kind}</code>, which is where
        this desk is configured.
      </>
    )
  }
  return (
    <>
      Entered for <code>{state.origin}</code> over <code>{state.kind}</code>. This desk is
      configured for <code>{destination}</code> over <code>{state.configuredKind}</code>, so
      nothing will be sent — enter the key for <code>{destination}</code>.
    </>
  )
}

/**
 * What the page may say about the key, in four states rather than two.
 *
 * A read that has not answered is not "no key": it is a page that has not been
 * told. And a key too short to fingerprint is present with nothing to show,
 * which is said rather than rendered as a stored key with a blank beside it.
 */
function keySays(
  state: AssistantKeyState,
  answered: boolean,
  failed: Error | null
): string {
  if (failed !== null) return `this desk could not say — ${failed.message}`
  if (!answered) return 'not read yet'
  if (!state.present) return 'none stored on this machine'
  if (state.fingerprint === '') {
    return 'stored on this machine — too short to show any of it without showing all of it'
  }
  return `stored on this machine — ${state.fingerprint}`
}

/**
 * One probe answer, reported as it came.
 *
 * `reachable` is the endpoint having answered *successfully*, and a refused
 * credential is therefore not reachable — a page that called a 401 reachable
 * would report a desk that cannot make one call as ready to work.
 */
function ProbeReading({
  result
}: {
  result: { reachable: boolean; status: number; latencyMs: number; diagnostic: string }
}) {
  return (
    <span className="quiet">
      {result.reachable ? 'reachable' : 'not reachable'}
      {' · '}
      {result.status === 0 ? 'no answer arrived' : `answered ${result.status}`}
      {' · '}
      {result.latencyMs} ms
      {result.diagnostic !== '' && (
        <>
          {' · '}
          {DIAGNOSTIC_SAYS[result.diagnostic] ?? result.diagnostic}
        </>
      )}
    </span>
  )
}
