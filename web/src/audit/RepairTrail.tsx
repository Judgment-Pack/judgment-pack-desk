/**
 * Repairing this desk's trail, on the owner's word (ADR-0010, section 4,
 * "Repair"; the maintainer's answer to question 8).
 *
 * Shown in the decision record only where the runtime's report names the
 * finding `incomplete-last-line`, and the panel offers the repair with a
 * token. One button opens a confirmation that says, before anything runs,
 * what a repair does and does not do, as ADR-0010 words it: it starts a new
 * segment and keeps the damaged bytes; it never restores the lost line; and
 * until it is done, every deciding run is refused. Only the confirmation
 * sends the panel's token. Desk never repairs on its own.
 *
 * What a repair answered is kept by the panel, which checks the trail again
 * after it, so the answer outlives the check: after a repair, the report says
 * `segmented` and shows the segments and the discontinuity, in the runtime's
 * words, and this says one sentence of Desk's own, which claims nothing. A
 * refusal is shown in Desk's words, with the runtime's beside them as it said
 * them.
 */
import { useRef, useState } from 'react'
import { msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { repairTrail, RepairRefused, type AuditDiagnostic, type AuditRepair } from './client'
import styles from './DecisionRecord.module.css'

/** What the last repair asked here answered: that it was made, or why not, with the runtime's words where it gave any. */
export type RepairOutcome = { kind: 'repaired' } | { kind: 'failed'; message: string; diagnostics: AuditDiagnostic[] }

export function RepairTrail({ repair, outcome, onOutcome }: {
  repair?: AuditRepair
  outcome?: RepairOutcome
  onOutcome: (outcome: RepairOutcome) => void
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const opener = useRef<HTMLButtonElement>(null)
  if (!repair && !outcome) return null
  const confirm = async () => {
    if (!repair) return
    setBusy(true)
    try {
      await repairTrail(repair.token)
      onOutcome({ kind: 'repaired' })
    } catch (error) {
      onOutcome({ kind: 'failed', message: error instanceof Error ? error.message : String(error), diagnostics: error instanceof RepairRefused ? error.diagnostics : [] })
    } finally {
      setBusy(false)
      setOpen(false)
    }
  }
  return <section aria-label={msg('Repairing the trail')}>
    <h4 className={styles.heading}>{msg('Repairing the trail')}</h4>
    <div className={styles.card}>
      {outcome?.kind === 'repaired' && <p role="status">{msg('The trail now has a new segment after the damaged line; the lost line is not restored.')}</p>}
      {outcome?.kind === 'failed' && <>
        <p role="alert">{systemMessage(outcome.message)}</p>
        {outcome.diagnostics.length > 0 && <ul className={styles.list} aria-label={msg('What the runtime said of the repair')}>{outcome.diagnostics.map((item, index) => <li key={index} lang="en"><code>{item.code}</code> {item.message}</li>)}</ul>}
      </>}
      {repair && <>
        <p className={styles.quiet}>{msg('The runtime’s report names line {{line}}, the trail’s last, as incomplete. Desk repairs the trail only when you ask, never on its own.', { line: repair.line })}</p>
        <div className={styles.actions}><Button ref={opener} onClick={() => setOpen(true)}>{msg('Repair the trail')}</Button></div>
      </>}
    </div>
    <Dialog open={open} onOpenChange={value => { if (!busy) setOpen(value) }} openerRef={opener}
      title={msg('Repair this desk’s trail?')}
      description={msg('Desk has the runtime repair the trail once, with jpack audit repair, where the trail is still as the decision record showed it.')}
      footer={<DialogActions>
        <Button disabled={busy} onClick={() => setOpen(false)}>{msg('Cancel')}</Button>
        <Button variant="primary" disabled={busy} onClick={() => void confirm()}>{busy ? msg('Repairing…') : msg('Run the repair')}</Button>
      </DialogActions>}>
      <ul className={styles.list} aria-label={msg('What a repair does and does not do')}>
        <li>{msg('Repair starts a new segment and keeps the damaged bytes.')}</li>
        <li>{msg('It never restores the lost line.')}</li>
        <li>{msg('Until it is done, every deciding run is refused.')}</li>
      </ul>
    </Dialog>
  </section>
}
