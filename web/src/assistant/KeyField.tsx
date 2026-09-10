/**
 * The API key: where it goes, where it lives, and which endpoint it is for.
 *
 * **The field is never populated from anything.** There is no value to populate
 * it with — no endpoint returns the key — and a masked field showing a
 * placeholder of the right length would be this page inventing evidence about a
 * value it has never seen.
 *
 * **The field is not offered where storing one cannot work.** A key is written
 * bound to the endpoint configured at that instant, so a desk with none has
 * nothing to bind it to and the chassis refuses. The line then asks for the
 * endpoint to be saved instead of offering a field and letting the refusal
 * explain — and on this form that is one action away, because Connect saves the
 * endpoint first.
 *
 * **The binding is the chassis' and this only reads it.** See `keyBinding`: the
 * desk records the scheme, host and wire protocol an entered key was for,
 * presents it only there, and refuses otherwise; nothing here compares a URL to
 * anything.
 */
import type { ReactNode, RefObject } from 'react'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { KIND_LABEL } from './endpointDraft'
import type { AssistantKeyState } from './client'
import type { KeyBinding } from './keyBinding'
import type { EndpointKind } from '../config/deskConfig'

/** Where the key is kept, and who can read it. Under the narration bound. */
const WHERE_IT_LIVES =
  'Stored on this computer only, never in the project. Readable by your user account only.'

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

export function KeyField({
  state,
  answered,
  failed,
  binding,
  field,
  onTyped,
  onStore,
  storeProblem,
  onRemove,
  removeProblem
}: {
  state: AssistantKeyState | undefined
  answered: boolean
  failed: Error | null
  binding: KeyBinding
  field: RefObject<HTMLInputElement | null>
  /** Whether anything at all has been typed. Never what. */
  onTyped: (typed: boolean) => void
  onStore: () => void
  storeProblem: string | undefined
  onRemove: () => void
  removeProblem: string | undefined
}) {
  const read = state ?? NOTHING_READ
  // **The desk's own origin for the configured endpoint**, never one this page
  // computed: the browser and Go disagree about an explicit default port, and a
  // label naming a destination the chassis would not present to would be this
  // page inventing the very fact the line exists to report.
  const destination = read.configuredOrigin === '' ? undefined : read.configuredOrigin

  return (
    <>
      <Field label="API key" hint={WHERE_IT_LIVES}>
        {(wiring) => (
          <input
            {...wiring}
            ref={field}
            type="password"
            autoComplete="off"
            spellCheck={false}
            defaultValue=""
            onChange={(event) => onTyped(event.target.value !== '')}
          />
        )}
      </Field>

      <p className="quiet">{keySays(read, answered, failed)}</p>
      {binding !== 'bound' && binding !== 'unread' && (
        <p className="quiet">{bindingSays(binding, read, destination)}</p>
      )}

      <p className="actions">
        {/* Store is the second action and never the first: on a form whose
            primary action is Connect, it is what replaces a key on an endpoint
            that is already saved.

            **And it is not offered where storing cannot work.** A key is
            written bound to the endpoint configured at that instant, so a desk
            with none has nothing to bind it to and the chassis refuses. Connect
            is the action that reaches this state, because it saves the endpoint
            first; a second button that could only produce a refusal is an
            affordance that lies about what the page can do. */}
        {binding !== 'no-endpoint' && (
          <Button variant="quiet" onClick={onStore}>
            Store key
          </Button>
        )}{' '}
        {read.present && (
          <Button variant="quiet" onClick={onRemove}>
            Remove key
          </Button>
        )}
      </p>

      {storeProblem !== undefined && (
        <p className="quiet">
          not stored: <code className="partial-reason">{storeProblem}</code>
        </p>
      )}
      {removeProblem !== undefined && (
        <p className="quiet">
          not removed: <code className="partial-reason">{removeProblem}</code>
        </p>
      )}
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
export function keySays(
  state: AssistantKeyState,
  answered: boolean,
  failed: Error | null
): string {
  if (failed !== null) return `this desk could not say — ${failed.message}`
  if (!answered) return 'Not read yet'
  if (!state.present) return 'No key stored'
  const provider = providerName(state.kind)
  if (state.fingerprint === '') {
    return `Stored — too short to show any of it without showing all of it, for ${provider}`
  }
  return `Stored — ${state.fingerprint}, for ${provider}`
}

/**
 * The one sentence a binding that is not simply "bound" is worth.
 *
 * **Both halves of a mismatch are named.** A line that said only "enter the key
 * again" would leave a reader unable to see *which* of the two moved — the
 * endpoint they just saved, or a key entered months ago for somewhere else —
 * and neither half is a secret: both are in the file this page already reads.
 */
export function bindingSays(
  binding: KeyBinding,
  state: AssistantKeyState,
  destination: string | undefined
): ReactNode {
  if (binding === 'no-endpoint') {
    return <>Connect saves the endpoint first: a key is kept bound to the endpoint it is for.</>
  }
  if (binding === 'none') {
    return (
      <>
        No key is stored for <code>{destination}</code>.
      </>
    )
  }
  return (
    <>
      Entered for <code>{state.origin}</code> over <code>{providerName(state.kind)}</code>. This
      desk is configured for <code>{destination}</code> over{' '}
      <code>{providerName(state.configuredKind)}</code>, so nothing will be sent — enter the key
      for <code>{destination}</code>.
    </>
  )
}

/**
 * A wire protocol, as the person chose it.
 *
 * The chassis answers with the file's spelling, which is what a *reported*
 * value is; this line is about the provider somebody picked, so it is the
 * picker's own label — and a spelling outside the closed list is shown as it
 * came rather than translated into one that is not it.
 */
function providerName(kind: string): string {
  return KIND_LABEL[kind as EndpointKind] ?? kind
}
