/**
 * A row of controls that is one tab stop.
 *
 * The editor's toolbar carries two segmented controls, three buttons and a
 * separator between them. As nine tab stops it sits between the document and
 * every control under it; Radix `Toolbar` makes it one, with the arrow keys
 * moving inside it — the same bargain the document itself makes with its
 * roving tab index.
 *
 * `Toolbar.Button` and `Toolbar.Separator` are exported rather than wrapped so
 * the caller composes its own row: a toolbar that took a list of buttons would
 * have to grow a member for every kind of control that ever stands in one.
 * `asChild` is what puts this repo's own `Button` inside it.
 */
import { Toolbar as RadixToolbar } from 'radix-ui'
import type { ReactNode } from 'react'
import styles from './Toolbar.module.css'

export function Toolbar({ label, children }: { label: string; children: ReactNode }) {
  return (
    <RadixToolbar.Root className={styles.toolbar} aria-label={label} orientation="horizontal">
      {children}
    </RadixToolbar.Root>
  )
}

/** One control inside the row, wrapping whatever the caller renders. */
export function ToolbarItem({ children }: { children: ReactNode }) {
  return (
    <RadixToolbar.Button asChild className={styles.item}>
      {children}
    </RadixToolbar.Button>
  )
}

/** A control that is not a button — a segmented group, a status dot. */
export function ToolbarSlot({ children }: { children: ReactNode }) {
  return <div className={styles.slot}>{children}</div>
}

export function ToolbarSeparator() {
  return <RadixToolbar.Separator className={styles.separator} />
}

/** The gap that pushes what follows to the end of the row. */
export function ToolbarSpacer() {
  return <div className={styles.spacer} aria-hidden="true" />
}
