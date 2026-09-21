import { useId, useState, type RefObject } from 'react'
import { DropdownMenu } from 'radix-ui'
import { msg, useLocale } from '../i18n'
import { IconPaperclip, IconPlus, IconLink } from '../shell/icons'
import { Tooltip } from '../ui/Tooltip'
import { ProviderIcon } from '../connections/ProviderIcon'
import { providerName } from '../connections/registry'
import type { ConnectionDescriptor } from '../connections/catalog'
import styles from './AttachmentMenu.module.css'

type MenuConnection = { provider: ConnectionDescriptor['id']; selection: ConnectionDescriptor['selection']; onSelect: () => void }

/** The gateway catalog and current status determine the connected shortcuts. */
export function AttachmentMenu({ disabled, onUpload, connections = [], triggerRef, onMore, onLink }: {
  onLink?: () => void; onMore?: () => void; connections?: MenuConnection[];
  disabled: boolean; onUpload: () => void; triggerRef?: RefObject<HTMLButtonElement | null>
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const id = useId()
  const [visible, setVisible] = useState<MenuConnection[]>([])
  const [linkVisible, setLinkVisible] = useState(false)
  return <DropdownMenu.Root open={open} onOpenChange={value => { if (value) { setVisible(connections); setLinkVisible(Boolean(onLink)) } setOpen(value) }}>
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
        {linkVisible && <DropdownMenu.Item className={`desk-menu-item ${styles.item}`} textValue={msg('Add link')} disabled={!onLink} onSelect={onLink}>
          <IconLink /><span className={styles.copy}><span>{msg('Add link')}</span><span className={styles.description}>{msg('Public web pages')}</span></span>
        </DropdownMenu.Item>}
        {visible.map(item => {
          const current = connections.find(connection => connection.provider === item.provider)
          return <DropdownMenu.Item key={item.provider} className={`desk-menu-item ${styles.item}`} textValue={providerName(item.provider)} disabled={!current} onSelect={current?.onSelect}>
            <ProviderIcon provider={item.provider} /><span className={styles.copy}><span>{providerName(item.provider)}</span><span className={styles.description}>{!current ? msg('Unavailable') : item.selection === 'browser-picker' ? msg('Choose files') : item.selection === 'mail-search' ? msg('Choose emails') : msg('Choose notes')}</span></span>
          </DropdownMenu.Item>
        })}
        {onMore && <><DropdownMenu.Separator className="desk-menu-separator" /><DropdownMenu.Item className="desk-menu-item" onSelect={onMore}>{msg('More connections')}</DropdownMenu.Item></>}
      </DropdownMenu.Content>
    </DropdownMenu.Portal>
  </DropdownMenu.Root>
}
