import { useState } from 'react'
import { msg } from '../../i18n'
import { Button } from '../../ui/Button'
import { OverflowTooltip } from '../../ui/Tooltip'
import { RunStatus } from '../../ui/RunStatus'
import { expectedLabel, type TestCase } from './model'
import styles from './TestsWorkspace.module.css'

/** Suite review is workspace content, rather than a custom conversation renderer. */
export function ProposalReview({
  cases,
  saved,
  busy,
  stale,
  error,
  onEdit,
  onSave,
  onClose,
}: {
  cases: TestCase[]
  saved: Record<string, string>
  busy: boolean
  stale: boolean
  error: string
  onEdit: (test: TestCase) => void
  onSave: (cases: TestCase[]) => void
  onClose: () => void
}) {
  const [selected, setSelected] = useState(
    () => new Set(cases.filter((c) => !Object.hasOwn(saved, c.id)).map((c) => c.id)),
  )
  const available = cases.filter((c) => !Object.hasOwn(saved, c.id))
  const chosen = available.filter((c) => selected.has(c.id))
  const all = available.length > 0 && chosen.length === available.length
  return (
    <div className={styles.content}>
      <div className={styles.workspaceBody}>
        <div className={styles.toolbar}>
          <div className={styles.reviewHeading}>
            <Button variant="inline" onClick={onClose} disabled={busy}>
              {msg('Back to cases')}
            </Button>
            <h2>
              {msg('Proposed test cases')} <span className={styles.muted}>· {cases.length}</span>
            </h2>
          </div>
          <Button variant="primary" disabled={busy || stale || !chosen.length} onClick={() => onSave(chosen)}>
            {msg('Save selected cases')} ({chosen.length})
          </Button>
        </div>
        <p className={styles.muted}>
          {msg('Review inputs and expected results before saving. Saving does not run tests.')}
        </p>
        {busy && <RunStatus running>{msg('Checking and saving cases…')}</RunStatus>}
        {stale && <p role="status">{msg('The pack changed. Request new suggestions for this revision.')}</p>}
        {error && (
          <p className={styles.error} role="alert">
            {error}
          </p>
        )}
        {!available.length && (
          <p role="status">{msg('All proposed cases have been saved. Run them from Cases.')}</p>
        )}
        <div className={styles.tableWrap}>
          <table className={`${styles.table} ${styles.proposalTable}`}>
            <thead>
              <tr>
                <th className={styles.selectionCell}>
                  <input
                    type="checkbox"
                    aria-label={msg('Select all proposed cases')}
                    checked={all}
                    ref={(el) => {
                      if (el) el.indeterminate = chosen.length > 0 && !all
                    }}
                    disabled={busy || stale || !available.length}
                    onChange={(e) => setSelected(new Set(e.target.checked ? available.map((c) => c.id) : []))}
                  />
                </th>
                <th>{msg('Case')}</th>
                <th>{msg('Expected')}</th>
                <th>{msg('Status')}</th>
              </tr>
            </thead>
            <tbody>
              {cases.map((c) => (
                <tr key={c.id}>
                  <td className={styles.selectionCell}>
                    <input
                      type="checkbox"
                      aria-label={msg('Select {{name}}', { name: c.name })}
                      checked={!Object.hasOwn(saved, c.id) && selected.has(c.id)}
                      disabled={busy || stale || Object.hasOwn(saved, c.id)}
                      onChange={(e) =>
                        setSelected((prior) => {
                          const next = new Set(prior)
                          if (e.target.checked) next.add(c.id)
                          else next.delete(c.id)
                          return next
                        })
                      }
                    />
                  </td>
                  <td>
                    <OverflowTooltip content={c.name}>
                      <Button
                        variant="inline"
                        className={styles.caseName}
                        disabled={busy || Object.hasOwn(saved, c.id)}
                        onClick={() => onEdit(c)}
                      >
                        {c.name}
                      </Button>
                    </OverflowTooltip>
                  </td>
                  <td>{expectedLabel(c.row)}</td>
                  <td className={styles.muted}>
                    {Object.hasOwn(saved, c.id) ? msg('Saved') : msg('Not saved')}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
