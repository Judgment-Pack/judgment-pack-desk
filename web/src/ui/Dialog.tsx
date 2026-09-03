/**
 * A modal dialog on the desk's surface, border and radius.
 *
 * The overlay and the panel are this component's; the title is required
 * because Radix requires one for the dialog's accessible name, and a
 * description is optional because not every dialog has something to say that
 * the fields do not already say better.
 *
 * **No description means no `aria-describedby`, not an empty one.** Radix
 * tracks whether a `Description` was rendered and omits the attribute when
 * none was, so a dialog with nothing to add simply has nothing to add. The
 * shape this replaced rendered an empty `<Description />` "to mean there is
 * none", which left every dialog pointing a screen reader at an empty
 * paragraph — the same defect `Field` documents at length and refuses, two
 * files away.
 *
 * `Dialog.Close` is re-exported rather than wrapped: a Cancel is a close, and
 * a caller composing one out of `Close` + `Button asChild` is doing the plain
 * thing rather than working around a wrapper that took no `asChild`.
 */
import { Dialog as RadixDialog } from 'radix-ui'
import type { ReactNode } from 'react'
import styles from './Dialog.module.css'

export const DialogClose = RadixDialog.Close

export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  children
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: ReactNode
  children: ReactNode
}) {
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className={styles.overlay} />
        <RadixDialog.Content className={styles.content}>
          <RadixDialog.Title className={styles.title}>{title}</RadixDialog.Title>
          {description ? (
            <RadixDialog.Description className={styles.description}>
              {description}
            </RadixDialog.Description>
          ) : null}
          {children}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  )
}

/** The row a dialog's buttons sit in, primary last. */
export function DialogActions({ children }: { children: ReactNode }) {
  return <div className={styles.actions}>{children}</div>
}
