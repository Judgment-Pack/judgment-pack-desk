import { useId, type ComponentProps, type RefObject } from 'react'
import { Button } from '../../ui/Button'
import { PageHeader } from '../../ui/PageLayout'
import { EditToolbar } from './EditToolbar'
import styles from './EditToolbar.module.css'

/** Persistent actions for one draft, independent of the editor's scroll position. */
export function PackEditHeader({ title, status, saveReason, unwritten, backRef, onBack, onSave, ...toolbar }: {
  title: string
  status: string
  saveReason?: string
  unwritten: number
  backRef: RefObject<HTMLButtonElement | null>
  onBack: () => void
  onSave: () => void
} & ComponentProps<typeof EditToolbar>) {
  const statusId = useId()
  return <PageHeader variant="title" title={title}
    actions={<div className={styles.actions}>
      <Button ref={backRef} onClick={onBack} disabled={toolbar.saving}>Back to pack</Button>
      <Button variant="primary" onClick={onSave} disabled={Boolean(saveReason)}
        aria-describedby={statusId} aria-busy={toolbar.saving}>
        {toolbar.saving ? 'Saving…' : 'Save'}
      </Button>
    </div>}
    description={<div id={statusId} className={styles.status} role="status">
      <span>Editing · {status}</span>
      {unwritten > 0 && <span className={styles.unwritten}>
        {unwritten === 1 ? '1 field is not written yet' : `${unwritten} fields are not written yet`}
        <span className={styles.help}>Finish or discard unfinished fields before leaving. Save writes the rest of the draft.</span>
      </span>}
      {saveReason && saveReason !== status && <span>{saveReason}</span>}
    </div>}
    navigation={<div className={styles.toolbar}><EditToolbar {...toolbar} /></div>} />
}
