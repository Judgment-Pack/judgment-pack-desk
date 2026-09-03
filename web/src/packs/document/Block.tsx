/**
 * The wrapper every rendered thing goes through, and the selection it carries.
 *
 * One element, one pointer, four uses: `data-pointer` for the renderer, the
 * element `id` for a deep link, `tabIndex={-1}` so a deep link can move focus
 * there, and a click that writes the pointer into `?at` for the Inspector.
 *
 * **The selection lives in the route.** `RightPane` swaps its wrapper at
 * 1100px and remounts the subtree, so a selection held in the pane is lost at
 * that breakpoint; a selection in the address is not, and it is also a link
 * someone can send. This context carries the route's value down; nothing here
 * stores one.
 *
 * The id may contain `/` and `~`. That is legal HTML and is **not** a valid
 * CSS selector, so every lookup goes through `document.getElementById` — see
 * `packs/pointers.ts`.
 */
import { createContext, useContext, type ElementType, type ReactNode } from 'react'
import { elementIdFor } from '../pointers'
import styles from './PackDocument.module.css'

export interface DocumentSelection {
  /** The pointer `?at` holds, or null where nothing is selected. */
  at: string | null
  /** Write a pointer into `?at`. Replaces rather than pushes: see PackView. */
  select: (pointer: string) => void
}

export const SelectionContext = createContext<DocumentSelection>({
  at: null,
  select: () => {}
})

export function useDocumentSelection(): DocumentSelection {
  return useContext(SelectionContext)
}

export function Block({
  pointer,
  as,
  className,
  label,
  children
}: {
  pointer: string
  as?: ElementType
  className?: string
  /** An accessible name, where the element is a landmark-ish region. */
  label?: string
  children: ReactNode
}) {
  const { at, select } = useDocumentSelection()
  const selected = at === pointer
  const Tag = (as ?? 'section') as ElementType
  return (
    <Tag
      id={elementIdFor(pointer)}
      data-pointer={pointer}
      tabIndex={-1}
      aria-current={selected ? 'true' : undefined}
      aria-label={label}
      className={[styles.block, selected ? styles.selected : undefined, className]
        .filter(Boolean)
        .join(' ')}
      onClick={(event: { stopPropagation: () => void }) => {
        // The innermost block wins: a click on a condition operand selects the
        // operand, not the rule that contains it.
        event.stopPropagation()
        select(pointer)
      }}
    >
      {children}
    </Tag>
  )
}
