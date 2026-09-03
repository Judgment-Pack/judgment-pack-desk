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
 *
 * **This file does not own the slot.** The portal target is published upwards
 * through a callback ref, because the context that carries it has to sit above
 * `<main>` for a route to reach it — a provider around this pane alone was one
 * routes could never see. The ref is a callback and not an effect, so it fires
 * on every mount and every unmount: a drawer that starts closed publishes
 * `null` and publishes the element when it opens, and a breakpoint swap
 * replaces a detached node rather than keeping it.
 */
import { Dialog, VisuallyHidden } from 'radix-ui'
import { type CSSProperties, type ReactNode, type RefObject } from 'react'
import { IconClose } from './icons'

const EMPTY_STATE = 'Select a row, a node or a file to inspect it here.'

export function RightPane({
  open,
  onClose,
  asDrawer,
  declaredWidth,
  publishTarget,
  publishPane,
  openerRef,
  children
}: {
  open: boolean
  onClose: () => void
  asDrawer: boolean
  /**
   * The width the project file **stated**, or undefined where it stated none.
   *
   * Undefined is not "use the default" — it is "do not write a width at all",
   * so `.desk-drawer`'s own 320px fallback stands. The column form's default
   * is 360px and the drawer's has always been 320px; supplying the effective
   * value unconditionally moved every unconfigured desk's drawer to 360px,
   * which is a behaviour change dressed as applying configuration.
   */
  declaredWidth: number | undefined
  /** Called with the portal target on mount and with null on unmount. */
  publishTarget: (target: HTMLDivElement | null) => void
  /**
   * Called with the pane itself, on the same terms.
   *
   * The frame measures it: the slot promises a route "the pane's width", and
   * the configured number is not that — the sheet caps it against the
   * viewport, and the drawer form ignores it entirely unless the file stated
   * one. What is published is the element; the measurement is the frame's.
   */
  publishPane: (pane: HTMLElement | null) => void
  /**
   * The header control that opened the drawer. Radix restores focus to its own
   * `Dialog.Trigger`, and this pane has none — the opener is a toggle in the
   * header, two grid cells away — so the restoration is wired by hand.
   */
  openerRef: RefObject<HTMLButtonElement | null>
  children?: ReactNode
}) {
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
      <div ref={publishTarget} className="desk-inspector-slot" />
      <p className="desk-pane-empty">{EMPTY_STATE}</p>
      {children}
    </>
  )

  if (asDrawer) {
    return (
      <Dialog.Root open={open} onOpenChange={(next) => !next && onClose()}>
        <Dialog.Portal>
          <Dialog.Overlay className="desk-overlay" />
          {/* The id is on both forms, and the header emits `aria-controls`
              only where the form carrying it is mounted: a closed drawer's
              portal is not in the document, and pointing at an id that is not
              there offers assistive technology a broken relationship. */}
          <Dialog.Content
            ref={publishPane}
            className="desk-drawer desk-drawer-right"
            id="desk-inspector"
            aria-label="Inspector"
            style={
              declaredWidth === undefined
                ? undefined
                : ({ '--drawer-w': `${declaredWidth}px` } as CSSProperties)
            }
            onCloseAutoFocus={(event) => {
              event.preventDefault()
              openerRef.current?.focus()
            }}
          >
            <VisuallyHidden.Root>
              <Dialog.Title>Inspector</Dialog.Title>
            </VisuallyHidden.Root>
            {body}
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    )
  }

  return (
    <aside
      ref={publishPane}
      className="desk-inspector"
      aria-label="Inspector"
      id="desk-inspector"
      hidden={!open}
    >
      {body}
    </aside>
  )
}
