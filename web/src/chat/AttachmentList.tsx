import { msg, useLocale } from '../i18n'
import { Popover } from '../ui/Popover'
import { Button } from '../ui/Button'
import { CodeBlock } from '../ui/CodeBlock'
import { Tooltip } from '../ui/Tooltip'
import { IconClose } from '../shell/icons'
import type { ChatAttachment } from './store'
import styles from './ChatWorkspace.module.css'

/** Shared by home, draft review and the pack Assistant. */
export function AttachmentList({ files, disabled, onRemove }: {
  files: ChatAttachment[]; disabled: boolean; onRemove: (id: string) => void
}) {
  useLocale()
  if (!files.length) return null
  return <ul className={styles.attachments} aria-label={msg("Attached files")}>{files.map(file => <li key={file.id}>
    <Popover title={file.name} trigger={<Button variant="quiet">{file.name}</Button>}>
      <div className={styles.settingsBody}><CodeBlock text={file.text} label={msg("Attachment")} /></div>
    </Popover>
    <Tooltip content={msg("Remove {{value0}}", { value0: file.name })}><button type="button" className="desk-icon-button" aria-label={msg("Remove {{value0}}", { value0: file.name })} disabled={disabled} onClick={() => onRemove(file.id)}><IconClose /></button></Tooltip>
  </li>)}</ul>
}
