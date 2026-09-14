import { Popover as RadixPopover } from 'radix-ui'
import { useId, type ReactElement, type ReactNode, type ComponentProps } from 'react'
import styles from './Popover.module.css'

/** Close a popover after following a link or choosing a transient action. */
export function PopoverClose({ children }: { children: ReactElement }) {
  return <RadixPopover.Close asChild>{children}</RadixPopover.Close>
}

/** Portaled details with collision handling, dismissal and focus restoration. */
export function Popover({ trigger, title, children, open, onOpenChange, onCloseAutoFocus }: {
  trigger: ReactElement
  title: string
  children: ReactNode
  open?: boolean
  onOpenChange?: (open: boolean) => void
  onCloseAutoFocus?: ComponentProps<typeof RadixPopover.Content>['onCloseAutoFocus']
}) {
  const titleId = useId()
  return <RadixPopover.Root open={open} onOpenChange={onOpenChange}>
    <RadixPopover.Trigger asChild>{trigger}</RadixPopover.Trigger>
    <RadixPopover.Portal>
      <RadixPopover.Content className={styles.content} align="end" sideOffset={8}
        collisionPadding={16} aria-labelledby={titleId} onCloseAutoFocus={onCloseAutoFocus}>
        <h2 id={titleId} className={styles.title}>{title}</h2>
        {children}
      </RadixPopover.Content>
    </RadixPopover.Portal>
  </RadixPopover.Root>
}
