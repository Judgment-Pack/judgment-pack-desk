import { Message } from '../../i18n/Message'
import { msg, useLocale } from '../../i18n'
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
  useLocale()
  const statusId = useId()
  return <PageHeader variant="title" title={title}
    actions={<div className={styles.actions}>
      <Button ref={backRef} onClick={onBack} disabled={toolbar.saving}>{msg("Back to pack")}</Button>
      <Button variant="primary" onClick={onSave} disabled={Boolean(saveReason)}
        aria-describedby={statusId} aria-busy={toolbar.saving}>
        {toolbar.saving ? msg("Saving…") : msg("Save")}
      </Button>
    </div>}
    description={<div id={statusId} className={styles.status} role="status">
      <span><Message text={"Editing · <0/>"} slots={[status]} /></span>
      {unwritten > 0 && <span className={styles.unwritten}>
        {unwritten === 1 ? msg("1 field is not written yet") : msg("{{value0}} fields are not written yet", { value0: unwritten })}
        <span className={styles.help}>{msg("Finish or discard unfinished fields before leaving. Save writes the rest of the draft.")}</span>
      </span>}
      {saveReason && saveReason !== status && <span>{saveReason}</span>}
    </div>}
    navigation={<div className={styles.toolbar}><EditToolbar {...toolbar} /></div>} />
}
