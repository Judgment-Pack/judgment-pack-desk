import { Popover as RadixPopover } from 'radix-ui'
import { useId, useState, type ReactElement, type ReactNode, type ComponentProps } from 'react'
import styles from './Popover.module.css'
import { Tooltip } from './Tooltip'
import { IconClose } from '../shell/icons'
import { msg } from '../i18n'

/** Close a popover after following a link or choosing a transient action. */
export function PopoverClose({ children }: { children: ReactElement }) {
  return <RadixPopover.Close asChild>{children}</RadixPopover.Close>
}

/** Portaled details with collision handling, dismissal and focus restoration. */
export function Popover({ trigger, title, children, open, onOpenChange, onCloseAutoFocus, onOpenAutoFocus, onEscapeKeyDown, triggerTooltip, variant = 'default', size = 'medium', align = 'end', dismissible = false }: {
  trigger: ReactElement
  title: string
  children: ReactNode
  open?: boolean
  onOpenChange?: (open: boolean) => void
  onCloseAutoFocus?: ComponentProps<typeof RadixPopover.Content>['onCloseAutoFocus']
  onOpenAutoFocus?: ComponentProps<typeof RadixPopover.Content>['onOpenAutoFocus']
  onEscapeKeyDown?: ComponentProps<typeof RadixPopover.Content>['onEscapeKeyDown']
  triggerTooltip?: string
  variant?: 'default' | 'list'
  size?: 'small' | 'medium'
  align?: 'start' | 'center' | 'end'
  dismissible?: boolean
}) {
  const titleId = useId()
  const [triggerElement, setTriggerElement] = useState<HTMLElement | null>(null)
  // Pane content can be portalled from a route outside the drawer's React tree.
  // A popover in that modal surface must join its modal stack so a click inside
  // the popover cannot be interpreted as an outside click on the drawer.
  const modal = Boolean(triggerElement?.closest('[data-modal-surface]'))
  const control = <RadixPopover.Trigger ref={setTriggerElement} asChild>{trigger}</RadixPopover.Trigger>
  return <RadixPopover.Root open={open} onOpenChange={onOpenChange} modal={modal}>
    {triggerTooltip ? <Tooltip content={triggerTooltip} disabled={open} openOnFocus={false}>{control}</Tooltip> : control}
    <RadixPopover.Portal>
      <RadixPopover.Content className={styles.content} data-variant={variant} data-size={size} align={align} sideOffset={8}
        collisionPadding={16} aria-labelledby={titleId} aria-modal={modal || undefined} onCloseAutoFocus={onCloseAutoFocus} onOpenAutoFocus={onOpenAutoFocus} onEscapeKeyDown={onEscapeKeyDown}>
        {dismissible ? <div className={styles.heading}><h2 id={titleId} className={styles.title}>{title}</h2><PopoverClose><button type="button" className="desk-icon-button" aria-label={msg('Close')}><IconClose /></button></PopoverClose></div> : <h2 id={titleId} className={styles.title}>{title}</h2>}
        {children}
      </RadixPopover.Content>
    </RadixPopover.Portal>
  </RadixPopover.Root>
}
