/**
 * The wrapper every rendered thing goes through, and the selection it carries.
 *
 * One element, one pointer, four uses: `data-pointer` for the renderer, the
 * element `id` for a deep link, a tab index so focus can be moved there, and a
 * click that writes the pointer into `?at` for the Inspector.
 *
 * **Selecting is not mouse-only.** Ninety-odd blocks cannot each be a tab stop
 * — a reader would Tab through the whole document to reach the header — so the
 * document is one stop with a **roving tab index**: exactly one block carries
 * `tabIndex={0}` and the rest carry `-1`, the arrow keys move that stop, and
 * Enter or Space selects. `PackDocumentView` owns the cursor and the key
 * handling, because both are facts about the document as a whole; this
 * component only reads which block is the current stop. No `role` is claimed:
 * these are the document's own regions, nested inside one another, and
 * `role="button"` on a container holding more of them would be a lie about
 * both.
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
import {
  createContext,
  useContext,
  type ElementType,
  type KeyboardEvent,
  type ReactNode
} from 'react'
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

/** Which block is the document's one tab stop, and how to move it. */
export interface DocumentCursor {
  /** The pointer of the block carrying `tabIndex={0}`, or null before one does. */
  at: string | null
  /** Put the stop on this block, because focus or a click just went there. */
  move: (pointer: string) => void
}

export const CursorContext = createContext<DocumentCursor>({ at: null, move: () => {} })

export function useDocumentCursor(): DocumentCursor {
  return useContext(CursorContext)
}

/** Whether a click landed on something that is its own control. */
function insideControl(target: unknown): boolean {
  if (target === null || typeof target !== 'object') return false
  const element = target as { closest?: (selector: string) => unknown }
  if (typeof element.closest !== 'function') return false
  return (
    element.closest(
      'a, button, input, select, textarea, label, [role="combobox"], [role="radio"], [role="checkbox"]'
    ) !== null
  )
}

export function Block({
  pointer,
  as,
  className,
  label,
  onKeyDown,
  children
}: {
  pointer: string
  as?: ElementType
  className?: string
  /** An accessible name, where the element is a landmark-ish region. */
  label?: string
  /**
   * A chord this block owns, on the block itself.
   *
   * It has to be here rather than on a wrapper inside, because this is the
   * element focus is moved to — the roving tab stop, and the element a reorder
   * focuses afterwards. A handler on a child never hears a keydown on its own
   * parent, so a chord bound one level in worked once and then went dead the
   * moment its own effect moved focus to the block.
   */
  onKeyDown?: (event: KeyboardEvent<HTMLElement>) => void
  children: ReactNode
}) {
  const { at, select } = useDocumentSelection()
  const cursor = useDocumentCursor()
  const selected = at === pointer
  const Tag = (as ?? 'section') as ElementType
  return (
    <Tag
      id={elementIdFor(pointer)}
      data-pointer={pointer}
      tabIndex={cursor.at === pointer ? 0 : -1}
      aria-current={selected ? 'true' : undefined}
      aria-label={label}
      onKeyDown={onKeyDown}
      className={[styles.block, selected ? styles.selected : undefined, className]
        .filter(Boolean)
        .join(' ')}
      onClick={(event: { stopPropagation: () => void; target: unknown }) => {
        // The innermost block wins: a click on a condition operand selects the
        // operand, not the rule that contains it.
        event.stopPropagation()
        // **A block that wraps a control does not steal the click.** In edit
        // mode a block's contents are an input, a select or a button, and
        // clicking one of those is a gesture about that control — typing into
        // a field must not also move the Inspector's selection and the
        // document's tab stop out from under the caret. The key handler in
        // `PackDocumentView` already skips these elements; this is the click
        // path, which did not.
        if (insideControl(event.target)) return
        select(pointer)
        cursor.move(pointer)
      }}
    >
      {children}
    </Tag>
  )
}
