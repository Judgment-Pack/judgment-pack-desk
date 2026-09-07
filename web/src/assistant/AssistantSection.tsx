/**
 * Admin › Assistant: the endpoint this desk is configured for, and the one key
 * it keeps.
 *
 * **This section carries the desk's two writes**, and each is exactly as wide
 * as its reason. A key must never be pasted into a project file, so it cannot
 * go through the file API — which writes only inside the project — and gets
 * its own endpoint. The `assistant` object of the desk-level file is the
 * other: choosing a model and a thinking tier is something an author does
 * while working, and the alternative is telling them to edit a file in a
 * configuration directory by hand between attempts.
 *
 * **The paste block is gone, and this paragraph is why.** It existed because
 * the page could not write the file. It can, under a conditional commit that
 * refuses a write the shared decoder would refuse — so the block would now be
 * a second way to do one thing, and the one where a reader hand-edits the file
 * this desk is also rewriting.
 *
 * **The three deployment states are text, not a control.** None, an endpoint
 * you already have, and an endpoint someone operates for you are not three
 * shapes: they are one nullable field with a different URL in it. Rendering
 * them as three choices would invent a distinction the schema refuses, and
 * would give one of the three somewhere to acquire an affordance the other two
 * lack. So they are described, and what is configurable is the endpoint.
 *
 * **The key row says which host the key is for.** The chassis records the
 * scheme, host and wire protocol a key was entered for and presents it only
 * there; a configuration write that moves any of the three leaves the key in
 * place and unusable and answers `keyRebindRequired`. So the row reads the
 * binding rather than leaving somebody to discover it by meeting a refusal.
 *
 * Nothing on this page says chassis, bytes or path to the reader. The words
 * are the desk, this machine, and the file.
 */
import { useRef, useState, type ReactNode, type RefObject } from 'react'
import { Fields } from '../components/primitives'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import type { AssistantEndpointConfig } from '../config/deskConfig'
import { SourceBadge } from '../routes/adminBlocks'
import { DIAGNOSTIC_SAYS, type AssistantKeyState } from './client'
import { EndpointForm } from './EndpointForm'
import { endpointOrigin, keyBinding, type KeyBinding } from './keyBinding'
import {
  useAssistantKey,
  useProbeAssistant,
  useRemoveAssistantKey,
  useStoreAssistantKey
} from './queries'

/** The sentence this section carries above everything else, verbatim. */
export const ASSISTANT_STANDING_SENTENCE =
  'The assistant proposes edits to the draft; you accept them; the runtime checks them. It ' +
  'never saves a file and never decides an outcome. Its key is kept by the desk on this ' +
  'machine and never written into a project.'

/**
 * The three deployment states, as sentences.
 *
 * The third one is the load-bearing one and says so out loud: an endpoint
 * somebody operates for you is configured in exactly the fields above, travels
 * exactly the same code path, and is not a product the desk sells you.
 */
export const DEPLOYMENT_STATES: [string, string][] = [
  ['None', 'Keyless. The authoring prompts still run in any chat client you already use.'],
  [
    'Bring your own',
    'Your endpoint and your key. The desk stores the endpoint, keeps the key on this machine, ' +
      'and has no relationship with whoever issued it.'
  ],
  [
    'Supplied',
    'An endpoint someone else operates for you. Configure it in exactly the fields above — an ' +
      'ordinary endpoint, the same code path, nothing it can do that yours cannot.'
  ]
]

export function AssistantSection({ id, title }: { id: string; title: string }) {
  const { config, sources, desk } = useEffectiveConfig()
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

  const read = keyBinding(key.data, endpoint)
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
    <>
      <h2 id={id} className="section-title">
        {title}
      </h2>
      <p className="quiet">{ASSISTANT_STANDING_SENTENCE}</p>

      <p>
        Assistant:{' '}
        <strong>
          {endpoint === null ? 'none — no endpoint configured' : 'a model endpoint'}
        </strong>
        <br />
        <SourceBadge source={sources.assistant} path="jpack-desk.json" deskPath={desk?.path} />
      </p>
      <p className="quiet">
        There are two settings and not three: <strong>None</strong>, which is the default and asks
        for no key at all, and <strong>a model endpoint</strong>. The slot is one nullable field —{' '}
        <code>assistant.endpoint</code> is null or an object — exactly as{' '}
        <code>identity.provider</code> is, and for the same reason.
      </p>

      <Fields items={DEPLOYMENT_STATES} />

      <p className="quiet">
        The one member that does branch is <code>kind</code>, and it names the endpoint&apos;s{' '}
        <strong>wire protocol</strong> rather than who runs it: the three protocols put the key in
        different headers and the call on a different path, so no single request could satisfy
        them. Nothing in the desk reads the host, compares it to a list, or behaves differently
        for one endpoint than another.
      </p>

      <EndpointForm
        bound={binding === 'bound'}
        onWritten={(answer) => setRebindAsked(answer.keyRebindRequired)}
      />

      <p className="quiet">
        Saving writes only the assistant part of the file on this machine and carries everything
        else in it across exactly as you wrote it. It refuses the write outright if the file
        changed since this page read it, and refuses it again — before anything is written — if
        what it would write is not something this desk reads.
      </p>
      <p className="quiet">
        <strong>There is no key on this form, and no field it could go in.</strong> A name that
        looks like a key — <code>apiKey</code>, <code>secret</code>, <code>token</code> — refuses
        the whole file wherever it is written, rather than being quietly carried in a file that
        may be committed. The key is stored below instead, on this machine only.
      </p>

      <KeyControl
        state={key.data ?? { present: false, fingerprint: '', origin: '', kind: '' }}
        answered={key.isSuccess}
        failed={key.error}
        binding={binding}
        endpoint={endpoint}
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

      <p>
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
      </p>
      <p className="quiet">
        The check is made by the desk and not by this page, because the key never reaches this
        page. It sends the smallest request the configured protocol defines — a model listing, or
        a message bounded to one token — waits at most ten seconds, and reports whether it was
        reached, what it answered, how long it took, and one word from a fixed list about why not.
        It never repeats what the endpoint wrote: a body an endpoint controls can carry the key
        back in a form no substitution would find. It is refused, by name, where there is no endpoint to reach or no key to
        present.
      </p>
    </>
  )
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
  endpoint,
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
  endpoint: AssistantEndpointConfig | null
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
  const destination = endpoint === null ? undefined : endpointOrigin(endpoint.url)
  const label =
    destination === undefined ? 'Key' : `Key for ${destination}`

  return (
    <>
      <p>
        Key: <strong>{keySays(state, answered, failed)}</strong>
      </p>
      <p>{bindingSays(binding, state, endpoint, destination)}</p>
      {entry && (
        <p>
          <label htmlFor="assistant-key">{label}</label>{' '}
          <input
            id="assistant-key"
            ref={field}
            type="password"
            autoComplete="off"
            spellCheck={false}
            defaultValue=""
          />{' '}
          <button type="button" onClick={onStore}>
            Store key
          </button>
        </p>
      )}
      <p>
        {binding === 'bound' && !replacing && (
          <button type="button" onClick={() => setReplacing(true)}>
            Replace key
          </button>
        )}
        {state.present && (
          <>
            {' '}
            <button type="button" onClick={onRemove}>
              Remove key
            </button>
          </>
        )}
      </p>
      {storeProblem !== undefined && (
        <p className="quiet">
          the key was not stored: <code className="partial-reason">{storeProblem}</code>
        </p>
      )}
      {removeProblem !== undefined && (
        <p className="quiet">
          the key was not removed: <code className="partial-reason">{removeProblem}</code>
        </p>
      )}
      <p className="quiet">
        What is typed here goes to the desk and is written to one file on this machine, readable
        by you and nobody else. It is never written into a project, never sent back to this page,
        and never printed in the desk&apos;s log. What is shown above is four characters from each
        end — enough to tell one key from another, and not enough to use. The field is emptied at
        the instant it is sent, whether the desk takes it or refuses it.
      </p>
      <p className="quiet">
        <strong>The key and the endpoint are separate.</strong> Removing the endpoint from the
        form above does not remove the key; the line above is what says whether one is still kept
        here, and Remove key is what takes it away. It travels only to the endpoint it was
        entered for: change the host or the protocol and it stays here, unusable, until somebody
        enters it again — which this page cannot do for you, because it has never held it.
      </p>
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
  endpoint: AssistantEndpointConfig | null,
  destination: string | undefined
): ReactNode {
  if (binding === 'unread') return <span className="quiet">this desk has not been asked yet</span>
  if (binding === 'no-endpoint') {
    return (
      <span className="quiet">
        Save an endpoint above before storing a key: a key is kept bound to the endpoint it was
        entered for, so there has to be one to bind it to.
      </span>
    )
  }
  if (binding === 'none') {
    return (
      <span className="quiet">
        No key is stored for <code>{destination}</code>.
      </span>
    )
  }
  if (binding === 'bound') {
    return (
      <span className="quiet">
        The key stored here was entered for <code>{state.origin}</code> over{' '}
        <code>{state.kind}</code>, which is where this desk is configured.
      </span>
    )
  }
  return (
    <span className="quiet">
      The key stored here was entered for <code>{state.origin}</code> over{' '}
      <code>{state.kind}</code>. This desk is configured for <code>{destination}</code> over{' '}
      <code>{endpoint?.kind}</code>, so it will not be presented and nothing will be sent —
      enter the key for <code>{destination}</code>.
    </span>
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
