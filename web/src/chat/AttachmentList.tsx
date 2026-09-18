import { DocumentPreview } from '../documents/DocumentPreview'
import { msg, useLocale } from '../i18n'
import { Popover } from '../ui/Popover'
import { Button } from '../ui/Button'
import { CodeBlock } from '../ui/CodeBlock'
import { Tooltip } from '../ui/Tooltip'
import { IconClose } from '../shell/icons'
import type { ChatAttachment } from './store'
import styles from './ChatWorkspace.module.css'

/** Shared by home, draft review and the pack Assistant. */
export function AttachmentList({ files, disabled, onRemove, onChange }: {
  files: ChatAttachment[]; disabled: boolean; onRemove: (id: string) => void; onChange?: (file: ChatAttachment) => void
}) {
  useLocale()
  if (!files.length) return null
  return <ul className={styles.attachments} aria-label={msg("Attached files")}>{files.map(file => <li key={file.id}>
    <>{file.document ? <DocumentPreview name={file.name} reference={file.document} disabled={disabled} onChange={onChange ? document => onChange({ ...file, document }) : undefined} /> : <Popover title={file.name} trigger={<Button variant="quiet">{file.name}</Button>}>
      <div className={styles.settingsBody}><CodeBlock text={file.text} label={msg("Attachment")} /></div>
    </Popover>}</>
    {file.document && (!file.document.pages.length ? <small>{msg("No readable pages")}</small> : file.document.needsReview && !file.document.allowPartial ? <small>{msg("Review pages")}</small> : null)}
    <Tooltip content={msg("Remove {{value0}}", { value0: file.name })}><button type="button" className="desk-icon-button" aria-label={msg("Remove {{value0}}", { value0: file.name })} disabled={disabled} onClick={() => onRemove(file.id)}><IconClose /></button></Tooltip>
  </li>)}</ul>
}
