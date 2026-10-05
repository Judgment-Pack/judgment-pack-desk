/**
 * Rotating this desk's signing key, on the owner's word (ADR-0010, section 1,
 * "Rotating it").
 *
 * Shown beside the keys Desk keeps. Where the panel offers a rotation, one
 * button opens a confirmation that says what a rotation does and does not do,
 * in the runtime's terms: the current key stops signing and the new one signs
 * the records after the rotation; a record written meanwhile may be unsigned;
 * nothing is revoked, so a holder must be given the new public key and told
 * about the old one; the old key's file loses its name, not its bytes; and a
 * lost key cannot be rotated away from. Only the confirmation sends the
 * panel's token. Where no rotation is offered, it says why; where one did not
 * finish, it says so, and what Desk does with it or why it cannot tell.
 *
 * What a rotation answered is kept by the panel, which checks the trail again
 * after it, so the answer outlives the check.
 */
import { useRef, useState } from 'react'
import { msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { rotateSigningKey, type AuditRotation, type RotationResult } from './client'
import styles from './DecisionRecord.module.css'

/** What the last rotation asked here answered: the keys it rotated, or why it did not. */
export type RotationOutcome = { kind: 'rotated'; result: RotationResult; number: number } | { kind: 'failed'; message: string }

export function RotateSigningKey({ rotation, keyCount, outcome, onOutcome }: {
  rotation?: AuditRotation
  /** How many keys the panel shows: the next key's number is one more. */
  keyCount: number
  outcome?: RotationOutcome
  onOutcome: (outcome: RotationOutcome) => void
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const opener = useRef<HTMLButtonElement>(null)
  if (!rotation && !outcome) return null
  const confirm = async () => {
    if (rotation?.state !== 'available') return
    setBusy(true)
    try {
      onOutcome({ kind: 'rotated', result: await rotateSigningKey(rotation.token), number: keyCount + 1 })
    } catch (error) {
      onOutcome({ kind: 'failed', message: error instanceof Error ? error.message : String(error) })
    } finally {
      setBusy(false)
      setOpen(false)
    }
  }
  return <section aria-label={msg('Rotating the key')}>
    <h4 className={styles.heading}>{msg('Rotating the key')}</h4>
    <div className={styles.card}>
      {outcome?.kind === 'rotated' && <p role="status">{msg('The key was rotated: key {{number}} signs the records after record {{at}}, and key {{previous}} signs nothing more. Hand the new public key to each holder.', { number: outcome.number, at: outcome.result.at, previous: outcome.number - 1 })}</p>}
      {outcome?.kind === 'failed' && <p role="alert">{systemMessage(outcome.message)}</p>}
      {rotation?.state === 'unfinished' && <p role="alert">{msg('A rotation of this desk’s signing key did not finish.')} {systemMessage(rotation.reason)}</p>}
      {rotation?.state === 'unavailable' && <p className={styles.quiet}>{systemMessage(rotation.reason)}</p>}
      {rotation?.state === 'available' && <>
        <p className={styles.quiet}>{msg('Rotation hands this desk’s signing over to a new key. It is never done on a schedule: only when you ask.')}</p>
        <div className={styles.actions}><Button ref={opener} onClick={() => setOpen(true)}>{msg('Rotate signing key')}</Button></div>
      </>}
    </div>
    <Dialog open={open} onOpenChange={value => { if (!busy) setOpen(value) }} openerRef={opener}
      title={msg('Rotate this desk’s signing key?')}
      description={msg('Desk has the runtime make a new key beside the current one and hand signing over to it, with jpack audit key rotate.')}
      footer={<DialogActions>
        <Button disabled={busy} onClick={() => setOpen(false)}>{msg('Cancel')}</Button>
        <Button variant="primary" disabled={busy} onClick={() => void confirm()}>{busy ? msg('Rotating…') : msg('Rotate the key')}</Button>
      </DialogActions>}>
      <ul className={styles.list} aria-label={msg('What a rotation does and does not do')}>
        <li>{msg('The current key stops signing. Records written after the rotation are signed with the new key.')}</li>
        <li>{msg('A record written while the rotation is in progress may be unsigned.')}</li>
        <li>{msg('A rotation revokes nothing: whoever holds the old key can still sign as it. Give each holder the new public key, and tell them about the old one if you no longer trust it: only a holder’s own jpack audit verify --revoked refuses what it signs.')}</li>
        <li>{msg('The old key’s file loses its name, but its bytes may remain on the disk.')}</li>
        <li>{msg('A lost key cannot be rotated away from: a rotation needs the key in force.')}</li>
      </ul>
    </Dialog>
  </section>
}
