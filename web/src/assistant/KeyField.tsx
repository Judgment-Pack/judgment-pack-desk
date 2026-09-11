/** API key entry, explicit saving, replacement, and confirmed removal. */
import { useState, type ReactNode, type RefObject } from 'react'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { KIND_LABEL } from './endpointDraft'
import styles from './EndpointForm.module.css'
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
  replacing,
  typed,
  saving,
  saveDisabled,
  onReplace,
  onCancel,
  saved,
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
  replacing: boolean
  typed: boolean
  saving: boolean
  saveDisabled: boolean
  onReplace: () => void
  onCancel: () => void
  saved: string | undefined
  onStore: () => void
  storeProblem: string | undefined
  onRemove: () => void
  removeProblem: string | undefined
}) {
  const read = state ?? NOTHING_READ
  const [confirmingRemoval, setConfirmingRemoval] = useState(false)
  // The field is offered wherever a key is wanted: none stored, stored for
  // somewhere else, or a replacement asked for. A read that has not answered is
  // not "a key is stored", so it is offered there too.
  const entry = binding !== 'bound' || replacing
  // **The desk's own origin for the configured endpoint**, never one this page
  // computed: the browser and Go disagree about an explicit default port, and a
  // label naming a destination the chassis would not present to would be this
  // page inventing the very fact the line exists to report.
  const destination = read.configuredOrigin === '' ? undefined : read.configuredOrigin

  return (
    <div className={styles.keyField}>
      {entry && (
        <Field label="API key" hint={WHERE_IT_LIVES}>
          {(wiring) => (
            <Input
              {...wiring}
              ref={field}
              type="password"
              autoComplete="off"
              spellCheck={false}
              defaultValue=""
              onChange={(event) => onTyped(event.target.value.trim() !== '')}
              onKeyDown={(event) => {
                if (event.key !== 'Enter') return
                event.preventDefault()
                if (!saveDisabled) onStore()
              }}
            />
          )}
        </Field>
      )}

      {(read.present || !answered || failed !== null) && <p className="quiet">{keySays(read, answered, failed)}</p>}
      {(binding === 'rebind' || binding === 'no-endpoint') && (
        <p className="quiet">{bindingSays(binding, read, destination)}</p>
      )}

      <div className={styles.keyActions}>
        {entry && (
          <>
            <Button variant="primary" disabled={saveDisabled} onClick={onStore}>
              {saving ? 'Saving API key…' : 'Save API key'}
            </Button>
            {(replacing || typed) && <Button variant="quiet" onClick={onCancel}>Cancel</Button>}
          </>
        )}
        {!entry && <Button variant="quiet" onClick={onReplace}>Replace key</Button>}
        {read.present && !confirmingRemoval && (
          <Button variant="danger" onClick={() => setConfirmingRemoval(true)}>
            Remove key
          </Button>
        )}
        {!read.present && answered && failed === null && <span className="quiet">{keySays(read, answered, failed)}</span>}
      </div>
      {saved !== undefined && <p className="quiet" role="status">{saved}</p>}
      {entry && typed && !saving && <p className="quiet">API key changes are not saved.</p>}
      {confirmingRemoval && read.present && (
        <div className={styles.removal}>
          <p>Removing the key prevents assistant requests until you save another key.</p>
          <Button variant="danger" onClick={() => {
            onCancel()
            setConfirmingRemoval(false)
            onRemove()
          }}>Confirm removal</Button>{' '}
          <Button variant="quiet" onClick={() => setConfirmingRemoval(false)}>Keep key</Button>
        </div>
      )}

      {storeProblem !== undefined && (
        <p className="quiet" role="alert">
          not stored: <code className="partial-reason">{storeProblem}</code>
        </p>
      )}
      {removeProblem !== undefined && (
        <p className="quiet" role="alert">
          not removed: <code className="partial-reason">{removeProblem}</code>
        </p>
      )}
    </div>
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
    return <>Save API key saves the endpoint first: a key is kept bound to the endpoint it is for.</>
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
