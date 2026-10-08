/**
 * Desk's archive of keys (the maintainer's decision of 2026-10-08, after the
 * third pass of the ADR-0010 line audit): Desk never removes a signing key on
 * its own. A seed, a next seed or a list of public keys Desk once would have
 * removed, it moves to its archive, under its own custody, with one line
 * saying why; and a key it cannot prove it no longer needs stays there until
 * the owner removes it.
 *
 * Shown beside the keys Desk keeps, whatever the trail's state: every file
 * archived, the identity it is kept under, the trail and record its name
 * records, when it was archived, and Desk's sentence on why. Each file that
 * is there has a Remove button, which opens a confirmation that says what a
 * removal does; only the confirmation sends the token the decision record
 * gave for that file. A line whose file is not there is said, with no
 * button; so is a marker left at its name under a project's name this desk
 * does not hold, which no start of this Desk's settles (review round 1 of
 * #327, finding 5).
 *
 * What a removal answered is kept by the panel, which checks the trail again
 * after it, so the answer outlives the check.
 */
import { useRef, useState } from 'react'
import { msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { removeArchivedKey, type ArchivedKey, type AuditArchive } from './client'
import styles from './DecisionRecord.module.css'

/** What the last removal asked here answered: the file it removed, or why it removed none. */
export type ArchiveOutcome = { kind: 'removed'; file: string } | { kind: 'failed'; message: string }

/** What an archived file is, in the owner's language. */
function kindWords(kind: ArchivedKey['kind']): string {
  switch (kind) {
    case 'seed': return msg('A signing key')
    case 'next.seed': return msg('A next signing key')
    case 'keys.jsonl': return msg('A list of public keys')
    case 'creating': return msg('A key creation’s marker')
    case 'rotating': return msg('A rotation’s journal')
  }
}

export function ArchivedKeys({ archive, outcome, onOutcome }: {
  archive?: AuditArchive
  outcome?: ArchiveOutcome
  onOutcome: (outcome: ArchiveOutcome) => void
}) {
  useLocale()
  const [asked, setAsked] = useState<ArchivedKey>()
  const [busy, setBusy] = useState(false)
  const opener = useRef<HTMLButtonElement | null>(null)
  if (!archive && !outcome) return null
  const confirm = async () => {
    if (!asked) return
    setBusy(true)
    try {
      await removeArchivedKey(asked)
      onOutcome({ kind: 'removed', file: asked.file })
    } catch (error) {
      onOutcome({ kind: 'failed', message: error instanceof Error ? error.message : String(error) })
    } finally {
      setBusy(false)
      setAsked(undefined)
    }
  }
  return <section aria-label={msg('Archived keys')}>
    <h4 className={styles.heading}>{msg('Archived keys')}</h4>
    <div className={styles.card}>
      {outcome?.kind === 'removed' && <p role="status">{msg('The archived file {{file}} was removed.', { file: outcome.file })}</p>}
      {outcome?.kind === 'failed' && <p role="alert">{systemMessage(outcome.message)}</p>}
      {archive?.problem && <p role="alert">{systemMessage(archive.problem)}</p>}
      {archive && <>
        <p className={styles.quiet}>{msg('Desk never removes a signing key on its own. A key, or a list of keys, it would once have removed is kept here, in its own configuration folder, with why; a key kept here can still sign, and stays until you remove it.')}</p>
        <ul className={styles.list} aria-label={msg('What Desk archived')}>{archive.entries.map(entry => <li key={entry.scope + '/' + entry.identity + '/' + entry.file}>
          <p><strong>{kindWords(entry.kind)}</strong></p>
          <p className={styles.quiet}>{entry.scope === 'runner'
            ? msg('Kept for Runner under {{identity}}', { identity: entry.identity })
            : entry.own ? msg('Kept under {{identity}}, this desk’s name', { identity: entry.identity }) : msg('Kept under {{identity}}', { identity: entry.identity })}</p>
          {entry.trail && <p className={styles.quiet}>{entry.sequence
            ? msg('Trail {{trail}}, record {{sequence}}', { trail: entry.trail, sequence: entry.sequence })
            : msg('Trail {{trail}}', { trail: entry.trail })}</p>}
          <p className={styles.quiet}>{msg('Archived {{at}}', { at: entry.at })}</p>
          <p lang="en">{systemMessage(entry.why)}</p>
          {entry.unresolved
            ? <p role="note">{msg('Kept at its name, not in the archive: no start of this Desk’s decides it.')}</p>
            : entry.missing
            ? <p role="note">{msg('This file is not in the archive now.')}</p>
            : entry.token && <div className={styles.actions}><Button onClick={event => { opener.current = event.currentTarget; setAsked(entry) }}>{msg('Remove')}</Button></div>}
        </li>)}</ul>
        {archive.more ? <p className={styles.quiet}>{msg('And {{count}} more, older, not listed.', { count: archive.more })}</p> : null}
      </>}
    </div>
    <Dialog open={asked !== undefined} onOpenChange={value => { if (!busy && !value) setAsked(undefined) }} openerRef={opener}
      title={msg('Remove this archived file?')}
      footer={<DialogActions>
        <Button disabled={busy} onClick={() => setAsked(undefined)}>{msg('Cancel')}</Button>
        <Button variant="primary" disabled={busy} onClick={() => void confirm()}>{busy ? msg('Removing…') : msg('Remove for good')}</Button>
      </DialogActions>}>
      <ul className={styles.list} aria-label={msg('What a removal does')}>
        <li>{msg('Desk removes this file from its archive of keys for good, and cannot bring it back.')}</li>
        <li>{msg('A signing key removed signs nothing more from here; whoever copied it can still sign as it.')}</li>
        <li>{msg('A holder still checks the records it signed: its public key stays in the list Desk keeps.')}</li>
      </ul>
    </Dialog>
  </section>
}
