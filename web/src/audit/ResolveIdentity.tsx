/**
 * This project's identity, where it was written in another folder that no
 * longer holds it, and Desk cannot tell whether this folder is that one,
 * moved here, or a copy of it (issue #309, the second line audit's finding
 * N1), or that records no folder at all, as an earlier Desk wrote it or as
 * Desk took it from the signing key the project's jpack.json names, which a
 * copy names too (issue #319, `unbound`): Desk makes, rotates and recovers
 * no signing key under it until the owner says which.
 *
 * Each answer opens a confirmation that says what it does, and only the
 * confirmation sends the token the decision record gave for that answer.
 * What the answer was is kept by the panel, which checks the trail again
 * after it, so the answer outlives the check.
 */
import { useRef, useState } from 'react'
import { msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { resolveIdentity, type AuditIdentity, type IdentityChoice } from './client'
import styles from './DecisionRecord.module.css'

/** What the last answer given here did: the choice it made, or why it made none. */
export type IdentityOutcome = { kind: 'resolved'; choice: IdentityChoice } | { kind: 'failed'; message: string }

export function ResolveIdentity({ identity, outcome, onOutcome }: {
  identity?: AuditIdentity
  outcome?: IdentityOutcome
  onOutcome: (outcome: IdentityOutcome) => void
}) {
  useLocale()
  const [asked, setAsked] = useState<IdentityChoice>()
  const [busy, setBusy] = useState(false)
  const moved = useRef<HTMLButtonElement>(null)
  const copy = useRef<HTMLButtonElement>(null)
  if (!identity && !outcome) return null
  const confirm = async () => {
    if (!identity || !asked) return
    setBusy(true)
    try {
      onOutcome({ kind: 'resolved', choice: await resolveIdentity(asked, asked === 'moved' ? identity.moved : identity.copy) })
    } catch (error) {
      onOutcome({ kind: 'failed', message: error instanceof Error ? error.message : String(error) })
    } finally {
      setBusy(false)
      setAsked(undefined)
    }
  }
  const unbound = identity?.kind === 'unbound'
  const movedWords = unbound ? msg('This is that project') : msg('This folder was moved here')
  const answer = asked === 'copy' ? msg('This is a copy') : movedWords
  return <section aria-label={msg('This project’s identity')}>
    <h4 className={styles.heading}>{msg('This project’s identity')}</h4>
    <div className={styles.card}>
      {outcome?.kind === 'resolved' && <p role="status">{outcome.choice === 'copy'
        ? msg('This folder has an identity of its own from now on.')
        : msg('This folder keeps the project’s identity from now on. Desk decides what a stopped key creation or rotation left under it when it next starts.')}</p>}
      {outcome?.kind === 'failed' && <p role="alert">{systemMessage(outcome.message)}</p>}
      {identity && <>
        <p role="alert">{unbound
          ? msg('This project’s identity records no folder: an earlier Desk wrote it so, or Desk took it from the signing key this project’s jpack.json names, which a copy of the project names too. Desk cannot tell whether this folder is the project that identity was made for or a copy of it, so it makes, rotates and recovers no signing key under that identity until you say which.')
          : msg('This project’s identity was written in another folder, which no longer holds it. Desk cannot tell whether this folder is that one, moved here, or a copy of it, so it makes, rotates and recovers no signing key under that identity until you say which.')}</p>
        <div className={styles.actions}>
          <Button ref={moved} onClick={() => setAsked('moved')}>{movedWords}</Button>
          <Button ref={copy} onClick={() => setAsked('copy')}>{msg('This is a copy')}</Button>
        </div>
      </>}
    </div>
    <Dialog open={asked !== undefined} onOpenChange={value => { if (!busy && !value) setAsked(undefined) }} openerRef={asked === 'copy' ? copy : moved}
      title={asked === 'copy' ? msg('Is this folder a copy?') : unbound ? msg('Is this the project that identity was made for?') : msg('Was this folder moved here?')}
      footer={<DialogActions>
        <Button disabled={busy} onClick={() => setAsked(undefined)}>{msg('Cancel')}</Button>
        <Button variant="primary" disabled={busy} onClick={() => void confirm()}>{busy ? msg('Saving…') : answer}</Button>
      </DialogActions>}>
      <p>{asked === 'copy'
        ? msg('Desk gives this folder an identity of its own. What Desk keeps under the identity it was copied with, its signing key and stamping settings, stays as it is for the folder that identity was written in. This folder’s jpack.json still names that signing key until you change it.')
        : unbound
          ? msg('Desk binds the project’s identity to this folder from now on, and decides what a stopped key creation or rotation left under it when it next starts. Answer this only if this is the project that identity was made for, and no copy of it is in use.')
          : msg('Desk names this folder by the project’s identity from now on, and decides what a stopped key creation or rotation left under it when it next starts. Answer this only if this is the folder the identity was written in, and no copy of it is in use.')}</p>
    </Dialog>
  </section>
}
