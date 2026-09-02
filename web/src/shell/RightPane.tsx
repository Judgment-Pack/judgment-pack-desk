/**
 * The Inspector.
 *
 * **Closed is the `hidden` attribute plus `[hidden] { display: none !important }`
 * in the shell sheet**, and both halves are load-bearing: the attribute alone
 * is beaten by any authored `display`, and a pane that is merely invisible
 * still holds tab stops. A test asserts the closed panel contributes none.
 *
 * **Below 1100px it is a `Dialog` drawer instead**, and two consequences are
 * stated here rather than left for someone to find. The wrapper swap remounts
 * the subtree, so inspector-local state resets at that breakpoint. And in
 * drawer form `Escape` closes it — because it *is* a dialog then, and a dialog
 * that swallowed Escape would be worse. That is the one place the rule "Escape
 * does not close a pane" does not hold, and it is in the README for the same
 * reason it is here.
 */
import { Dialog, VisuallyHidden } from 'radix-ui'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { InspectorSlotContext, type InspectorSlot } from './InspectorSlot'
import { IconClose } from './icons'

export const INSPECTOR_WIDTH = 360

const EMPTY_STATE = 'Select a row, a node or a file to inspect it here.'

export function RightPane({
  open,
  onClose,
  asDrawer,
  children
}: {
  open: boolean
  onClose: () => void
  asDrawer: boolean
  children?: ReactNode
}) {
  const targetRef = useRef<HTMLDivElement | null>(null)
  const [target, setTarget] = useState<HTMLElement | null>(null)
  const [tab, setTab] = useState<string | null>(null)

  // The target is published after mount, so a route that portals into it on
  // its own first render finds an element rather than null on the next.
  useEffect(() => setTarget(targetRef.current), [])

  const slot: InspectorSlot = {
    open,
    size: open ? INSPECTOR_WIDTH : 0,
    tab,
    setTab,
    target
  }

  const body = (
    <>
      <div className="desk-pane-head">
        <span>Inspector</span>
        <button
          type="button"
          className="desk-icon-button"
          aria-label="Close inspector"
          onClick={onClose}
        >
          <IconClose />
        </button>
      </div>
      {/* Always mounted, so a route's portal target never disappears under it.
          Phase A publishes nothing into it, so the empty state stands. */}
      <div ref={targetRef} className="desk-inspector-slot" />
      <p className="desk-pane-empty">{EMPTY_STATE}</p>
      {children}
    </>
  )

  if (asDrawer) {
    return (
      <InspectorSlotContext.Provider value={slot}>
        <Dialog.Root open={open} onOpenChange={(next) => !next && onClose()}>
          <Dialog.Portal>
            <Dialog.Overlay className="desk-overlay" />
            {/* The id is on both forms, because the header's toggle points at
                it with `aria-controls` in both — and a dangling reference
                offers assistive technology a broken relationship rather than
                none. Only one form is ever mounted, so the id stays unique. */}
            <Dialog.Content
              className="desk-drawer desk-drawer-right"
              id="desk-inspector"
              aria-label="Inspector"
            >
              <VisuallyHidden.Root>
                <Dialog.Title>Inspector</Dialog.Title>
              </VisuallyHidden.Root>
              {body}
            </Dialog.Content>
          </Dialog.Portal>
        </Dialog.Root>
      </InspectorSlotContext.Provider>
    )
  }

  return (
    <InspectorSlotContext.Provider value={slot}>
      <aside className="desk-inspector" aria-label="Inspector" id="desk-inspector" hidden={!open}>
        {body}
      </aside>
    </InspectorSlotContext.Provider>
  )
}
