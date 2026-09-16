import { Popover as RadixPopover } from 'radix-ui'
import { useId, type ReactElement, type ReactNode, type ComponentProps } from 'react'
import styles from './Popover.module.css'
import { Tooltip } from './Tooltip'

/** Close a popover after following a link or choosing a transient action. */
export function PopoverClose({ children }: { children: ReactElement }) {
  return <RadixPopover.Close asChild>{children}</RadixPopover.Close>
}

/** Portaled details with collision handling, dismissal and focus restoration. */
export function Popover({ trigger, title, children, open, onOpenChange, onCloseAutoFocus, onOpenAutoFocus, onEscapeKeyDown, triggerTooltip, variant = 'default', size = 'medium' }: {
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
}) {
  const titleId = useId()
  const control = <RadixPopover.Trigger asChild>{trigger}</RadixPopover.Trigger>
  return <RadixPopover.Root open={open} onOpenChange={onOpenChange}>
    {triggerTooltip ? <Tooltip content={triggerTooltip} disabled={open} openOnFocus={false}>{control}</Tooltip> : control}
    <RadixPopover.Portal>
      <RadixPopover.Content className={styles.content} data-variant={variant} data-size={size} align="end" sideOffset={8}
        collisionPadding={16} aria-labelledby={titleId} onCloseAutoFocus={onCloseAutoFocus} onOpenAutoFocus={onOpenAutoFocus} onEscapeKeyDown={onEscapeKeyDown}>
        <h2 id={titleId} className={styles.title}>{title}</h2>
        {children}
      </RadixPopover.Content>
    </RadixPopover.Portal>
  </RadixPopover.Root>
}
