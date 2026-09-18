import { useId, useState } from 'react'
import { DropdownMenu } from 'radix-ui'
import { msg, useLocale } from '../i18n'
import { IconGear, IconGoogleDrive, IconPaperclip, IconPlus } from '../shell/icons'
import { Tooltip } from '../ui/Tooltip'
import styles from './AttachmentMenu.module.css'

/** Shared by home chat and pack Assistant. Provider availability is explicit:
 * a configured document extractor is not a Google Drive connection. */
export function AttachmentMenu({ disabled, documentsConfigured, onUpload, onSettings }: {
  disabled: boolean; documentsConfigured: boolean; onUpload: () => void; onSettings: () => void
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const id = useId()
  return <DropdownMenu.Root open={open} onOpenChange={setOpen}>
    <Tooltip content={msg('Attach files')} disabled={open} openOnFocus={false}>
      <DropdownMenu.Trigger type="button" className="desk-icon-button" aria-label={msg('Attach files')} disabled={disabled}><IconPlus /></DropdownMenu.Trigger>
    </Tooltip>
    <DropdownMenu.Portal>
      <DropdownMenu.Content className={`desk-menu ${styles.menu}`} aria-label={msg('Attach files')} side="top" align="start" sideOffset={8} collisionPadding={16}
        onEscapeKeyDown={event => event.stopPropagation()}>
        <DropdownMenu.Item className={`desk-menu-item ${styles.item}`} textValue={msg('Upload files')} aria-labelledby={`${id}-upload`} aria-describedby={`${id}-upload-hint`} onSelect={onUpload}>
          <IconPaperclip />
          <span className={styles.copy}><span id={`${id}-upload`}>{msg('Upload files')}</span><span id={`${id}-upload-hint`} className={styles.description}>{msg('PDFs and text files from your computer')}</span></span>
        </DropdownMenu.Item>
        <DropdownMenu.Item className={`desk-menu-item ${styles.item}`} textValue="Google Drive" disabled aria-labelledby={`${id}-drive`} aria-describedby={`${id}-drive-hint`}>
          <IconGoogleDrive />
          <span className={styles.copy}><span id={`${id}-drive`}>{msg('Google Drive')}</span><span id={`${id}-drive-hint`} className={styles.description}>{msg('Not available yet. Download a PDF or text file from Drive, then upload it here.')}</span></span>
        </DropdownMenu.Item>
        <DropdownMenu.Separator className={styles.separator} />
        <DropdownMenu.Item className={`desk-menu-item ${styles.item}`} onSelect={onSettings}>
          <IconGear /><span>{documentsConfigured ? msg('Document settings') : msg('Configure Documents')}</span>
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu.Portal>
  </DropdownMenu.Root>
}
