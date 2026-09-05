/**
 * Admin › Assistant: the model slot, and the one key this desk keeps.
 *
 * **This section carries the only write controls on Admin**, and the reason is
 * narrow enough to state in a sentence: a key must never be pasted into a
 * project file, so it cannot go through the file API, which writes only inside
 * the project. Everything else here is read-only in exactly the way the rest
 * of the page is — effective values, their source, and the exact JSON to
 * paste — because the endpoint *is* ordinary configuration and belongs in a
 * file a person edits.
 *
 * **The three deployment states are text, not a control.** None, an endpoint
 * you already have, and an endpoint someone operates for you are not three
 * shapes: they are one nullable field with a different URL in it. Rendering
 * them as three choices would invent a distinction the schema refuses, and
 * would give one of the three somewhere to acquire an affordance the other two
 * lack. So they are described, and what is configurable is the endpoint.
 *
 * Nothing on this page says chassis, bytes or path to the reader. The words
 * are the desk, this machine, and the file.
 */
import { useState } from 'react'
import { Fields } from '../components/primitives'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { ASSISTANT_TOOLS } from '../config/deskConfig'
import { PasteBlock, SourceBadge } from '../routes/adminBlocks'
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
  // Held here and never anywhere else: it is cleared the moment the store
  // answers, and it is not written to any record on this machine.
  const [typed, setTyped] = useState('')

  return (
    <>
      <h2 id={id} className="section-title">
        {title}
      </h2>
      <p className="quiet">{ASSISTANT_STANDING_SENTENCE}</p>

      <p>
        Assistant:{' '}
        <strong>
          {endpoint === null ? 'none — no assistant, and no key' : 'a model endpoint'}
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
        <strong>wire protocol</strong> rather than who runs it: the two protocols put the key in
        different headers and the call on a different path, so no single request could satisfy
        both. Nothing in the desk reads the host, compares it to a list, or behaves differently
        for one endpoint than another.
      </p>

      {endpoint !== null ? (
        <Fields
          items={[
            ['Endpoint', <code key="url">{endpoint.url}</code>],
            ['Protocol', <code key="kind">{endpoint.kind}</code>],
            ['Model', <code key="model">{endpoint.model}</code>],
            [
              'Tools it may call',
              endpoint.tools.length === 0 ? (
                <span key="tools" className="quiet">
                  none — the assistant may call no tool
                </span>
              ) : (
                <code key="tools">{endpoint.tools.join(' · ')}</code>
              )
            ]
          ]}
        />
      ) : (
        <p className="quiet">
          No endpoint is configured, so nothing here is set. Writing the block below into the
          desk-level file configures one.
        </p>
      )}

      <p className="quiet">
        The assistant may be given only these tools:{' '}
        <code>{ASSISTANT_TOOLS.join(', ')}</code>. Every one of them is a question put to the
        runtime, and the last is a rehearsal — it consults no reviewed set and decides no outcome.
        A name outside that list is refused when the file is read, rather than accepted and
        ignored, because a setting that appears to grant something is a grant to whoever wrote it.
      </p>

      <PasteBlock
        label="Add to the desk-level desk.json"
        json={{
          deskConfigVersion: 1,
          assistant: {
            endpoint: {
              url: 'https://api.example.invalid/v1',
              kind: 'openai-compatible',
              model: 'a-model',
              tools: [...ASSISTANT_TOOLS]
            }
          }
        }}
      />
      <p className="quiet">
        <strong>The key is not in that block, and there is no member it could go in.</strong> A
        name that looks like a key — <code>apiKey</code>, <code>secret</code>, <code>token</code> —
        refuses the whole file where it is written, rather than being quietly carried in a file
        that may be committed. The key is stored below instead, on this machine only.
      </p>

      <KeyControl
        state={
          key.data ?? { present: false, fingerprint: '' }
        }
        answered={key.isSuccess}
        failed={key.error}
        typed={typed}
        onType={setTyped}
        onStore={() => {
          const value = typed
          store.mutate(value, { onSuccess: () => setTyped('') })
        }}
        storeError={store.error}
        onRemove={() => remove.mutate()}
        removeError={remove.error}
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
        a message bounded to one token — waits at most ten seconds, and reports what came back
        word for word. It is refused, by name, where there is no endpoint to reach or no key to
        present.
      </p>
    </>
  )
}

/**
 * The key: whether there is one, and the two things that can be done about it.
 *
 * **The field is never populated from anything.** There is no value to
 * populate it with — no endpoint returns the key — and a masked field showing
 * a placeholder of the right length would be this page inventing evidence
 * about a value it has never seen.
 */
function KeyControl({
  state,
  answered,
  failed,
  typed,
  onType,
  onStore,
  storeError,
  onRemove,
  removeError
}: {
  state: { present: boolean; fingerprint: string }
  answered: boolean
  failed: Error | null
  typed: string
  onType: (value: string) => void
  onStore: () => void
  storeError: Error | null
  onRemove: () => void
  removeError: Error | null
}) {
  return (
    <>
      <p>
        Key: <strong>{keySays(state, answered, failed)}</strong>
      </p>
      <p>
        <label htmlFor="assistant-key">Key</label>{' '}
        <input
          id="assistant-key"
          type="password"
          autoComplete="off"
          spellCheck={false}
          value={typed}
          onChange={(event) => onType(event.target.value)}
        />{' '}
        <button type="button" onClick={onStore}>
          Store key
        </button>
        {state.present && (
          <>
            {' '}
            <button type="button" onClick={onRemove}>
              Remove key
            </button>
          </>
        )}
      </p>
      {storeError !== null && (
        <p className="quiet">
          the key was not stored:{' '}
          <code className="partial-reason">{storeError.message}</code>
        </p>
      )}
      {removeError !== null && (
        <p className="quiet">
          the key was not removed:{' '}
          <code className="partial-reason">{removeError.message}</code>
        </p>
      )}
      <p className="quiet">
        What is typed here goes to the desk and is written to one file on this machine, readable
        by you and nobody else. It is never written into a project, never sent back to this page,
        and never printed in the desk&apos;s log. What is shown above is four characters from each
        end — enough to tell one key from another, and not enough to use.
      </p>
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
  state: { present: boolean; fingerprint: string },
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
  result: { reachable: boolean; status: number; latencyMs: number; detail: string }
}) {
  return (
    <span className="quiet">
      {result.reachable ? 'reachable' : 'not reachable'}
      {' · '}
      {result.status === 0 ? 'no answer arrived' : `answered ${result.status}`}
      {' · '}
      {result.latencyMs} ms
      {result.detail !== '' && (
        <>
          {' · '}
          <code className="partial-reason">{result.detail}</code>
        </>
      )}
    </span>
  )
}
