import { useId, useState, type RefObject } from 'react'
import { DropdownMenu } from 'radix-ui'
import { msg, useLocale } from '../i18n'
import { IconGoogleDrive, IconMail, IconPaperclip, IconPlus } from '../shell/icons'
import { Tooltip } from '../ui/Tooltip'
import { ProviderIcon } from '../connections/ProviderIcon'
import type { SourceProvider } from '../connections/client'
import styles from './AttachmentMenu.module.css'

/** Shared by home chat and pack Assistant. Provider availability is explicit:
 * a configured document extractor is not a Google Drive connection. */
export function AttachmentMenu({ disabled, onUpload, onDrive, driveState, onGmail, gmailState, sources = [], triggerRef, onMore, connectedOnly = false }: {
  onMore?: () => void; connectedOnly?: boolean;
  sources?: { provider: SourceProvider; state?: string; onSelect: () => void }[];
  disabled: boolean; onUpload: () => void; onDrive?: () => void; driveState?: string; onGmail?: () => void; gmailState?: string; triggerRef?: RefObject<HTMLButtonElement | null>
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const id = useId()
  return <DropdownMenu.Root open={open} onOpenChange={setOpen}>
    <Tooltip content={msg('Attach files')} disabled={open} openOnFocus={false}>
      <DropdownMenu.Trigger ref={triggerRef} type="button" className="desk-icon-button" aria-label={msg('Attach files')} disabled={disabled}><IconPlus /></DropdownMenu.Trigger>
    </Tooltip>
    <DropdownMenu.Portal>
      <DropdownMenu.Content className={`desk-menu ${styles.menu}`} aria-label={msg('Attach files')} side="top" align="start" sideOffset={8} collisionPadding={16}
        onEscapeKeyDown={event => event.stopPropagation()}>
        <DropdownMenu.Item className={`desk-menu-item ${styles.item}`} textValue={msg('Upload files')} aria-labelledby={`${id}-upload`} aria-describedby={`${id}-upload-hint`} onSelect={onUpload}>
          <IconPaperclip />
          <span className={styles.copy}><span id={`${id}-upload`}>{msg('Upload files')}</span><span id={`${id}-upload-hint`} className={styles.description}>{msg('PDFs and text files')}</span></span>
        </DropdownMenu.Item>
        {(!connectedOnly || driveState === 'connected') && <DropdownMenu.Item className={`desk-menu-item ${styles.item}`} textValue="Google Drive" disabled={!onDrive || !driveState || ['unavailable', 'blocked'].includes(driveState)} onSelect={onDrive} aria-labelledby={`${id}-drive`} aria-describedby={`${id}-drive-hint`}>
          <IconGoogleDrive />
          <span className={styles.copy}><span id={`${id}-drive`}>{msg('Google Drive')}</span><span id={`${id}-drive-hint`} className={styles.description}>{driveState === 'connected' ? msg('Choose files') : driveState === 'not-connected' || driveState === 'setup-required' ? msg('Connect') : msg('Unavailable')}</span></span>
        </DropdownMenu.Item>}
        {(!connectedOnly || gmailState === 'connected') && <DropdownMenu.Item className={`desk-menu-item ${styles.item}`} textValue="Gmail" disabled={!onGmail || !gmailState || ['unavailable', 'blocked'].includes(gmailState)} onSelect={onGmail} aria-labelledby={`${id}-gmail`} aria-describedby={`${id}-gmail-hint`}>
          <IconMail />
          <span className={styles.copy}><span id={`${id}-gmail`}>{msg('Gmail')}</span><span id={`${id}-gmail-hint`} className={styles.description}>{gmailState === 'connected' ? msg('Choose emails') : gmailState === 'not-connected' || gmailState === 'setup-required' ? msg('Connect') : msg('Unavailable')}</span></span>
        </DropdownMenu.Item>}
        {sources.filter(source => !connectedOnly || source.state === 'connected').map(source => <DropdownMenu.Item key={source.provider} className={`desk-menu-item ${styles.item}`} textValue={source.provider === 'notion' ? 'Notion' : 'Obsidian'} disabled={!source.state || ['unavailable','blocked'].includes(source.state)} onSelect={source.onSelect}>
          <ProviderIcon provider={source.provider} /><span className={styles.copy}><span>{source.provider === 'notion' ? 'Notion' : 'Obsidian'}</span><span className={styles.description}>{source.state === 'connected' ? msg('Choose notes') : source.state === 'not-connected' ? msg('Connect') : msg('Unavailable')}</span></span>
        </DropdownMenu.Item>)}
        {onMore && <><DropdownMenu.Separator className="desk-menu-separator" /><DropdownMenu.Item className="desk-menu-item" onSelect={onMore}>{msg('More connections')}</DropdownMenu.Item></>}
      </DropdownMenu.Content>
    </DropdownMenu.Portal>
  </DropdownMenu.Root>
}
