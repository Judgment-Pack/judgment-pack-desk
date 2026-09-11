import { Popover as RadixPopover } from 'radix-ui'
import { useId, type ReactElement, type ReactNode } from 'react'
import styles from './Popover.module.css'

/** Portaled details with collision handling, dismissal and focus restoration. */
export function Popover({ trigger, title, children }: {
  trigger: ReactElement
  title: string
  children: ReactNode
}) {
  const titleId = useId()
  return <RadixPopover.Root>
    <RadixPopover.Trigger asChild>{trigger}</RadixPopover.Trigger>
    <RadixPopover.Portal>
      <RadixPopover.Content className={styles.content} align="end" sideOffset={8}
        collisionPadding={16} aria-labelledby={titleId}>
        <h2 id={titleId} className={styles.title}>{title}</h2>
        {children}
      </RadixPopover.Content>
    </RadixPopover.Portal>
  </RadixPopover.Root>
}
