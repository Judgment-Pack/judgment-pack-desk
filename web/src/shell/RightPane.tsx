import { msg, useLocale } from '../i18n'
import { Tooltip } from '../ui/Tooltip'
/**
 * The Inspector.
 *
 * **Closed is the `hidden` attribute plus `[hidden] { display: none !important }`
 * in the shell sheet**, and both halves are load-bearing: the attribute alone
 * is beaten by any authored `display`, and a pane that is merely invisible
 * still holds tab stops. A test asserts the closed panel contributes none.
 *
 * Workspace tools use a full-width inline takeover on narrow screens, keeping
 * these portal targets mounted. Other route inspectors may use a `Dialog` drawer, and two consequences are
 * stated here rather than left for someone to find. The wrapper swap remounts
 * the subtree, so inspector-local state resets at that breakpoint. And in
 * drawer form `Escape` closes it — because it *is* a dialog then, and a dialog
 * that swallowed Escape would be worse. That is the one place the rule "Escape
 * does not close a pane" does not hold, and it is in the README for the same
 * reason it is here.
 *
 * **The empty state stands only where nothing is published.** It used to be
 * unconditional, so the first route to publish showed its panel *and* the
 * "select a row" paragraph underneath it. A portal cannot tell React it
 * happened, so this cannot be read off the children; the frame counts claims
 * and hands the answer down. A CSS `:empty` sibling rule would have been
 * shorter and is rejected in `InspectorSlot.tsx` for a reason worth repeating
 * here: vitest runs with `css: false`, so nothing in this repo could hold it.
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
import { WorkspaceActivity } from './WorkspaceActivity'
import { IconFocus, IconPanelLeft, IconChevronLeft } from './icons'
import { Button } from '../ui/Button'
import { PaneWidthMenu } from './PaneWidthMenu'
import { PaneDivider, type PaneResizePreview } from '../ui/PaneDivider'
import { PaneToggle } from './PaneToggle'
import { Diagnostics } from './Diagnostics'

const EMPTY_STATE = 'Select a row, a node or a file to inspect it here.'

export function RightPane({
  title = 'Details',
  open,
  onClose,
  asDrawer,
  declaredWidth,
  publishTarget,
  publishHeaderTarget,
  publishPane,
  restoreFocusRef,
  showEmpty,
  utility,
  publishUtilityTarget,
  navigation,
  children,
  tool = 'route',
  workspaceTools = false,
  expanded = false,
  fullWidth = false,
  contextTitle,
  returnLabel,
  onExpand,
  onReturn,
  expandButtonRef,
  resize,

  brief, publishBriefHeaderTarget,
  publishDetailsTarget,
  inspection,
  hasDetails = false
}: {
  title?: string
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
  publishHeaderTarget?: (target: HTMLDivElement | null) => void
  /**
   * Called with the pane itself, on the same terms.
   *
   * The frame measures it: the slot promises a route "the pane's width", and
   * the configured number is not that — the sheet caps it against the
   * viewport, and the drawer form ignores it entirely unless the file stated
   * one. What is published is the element; the measurement is the frame's.
   */
  publishPane: (pane: HTMLElement | null) => void
  /** The contextual action to restore after a drawer closes. */
  restoreFocusRef?: RefObject<HTMLElement | null>
  /** False while a route is publishing into the slot. */
  showEmpty: boolean
  utility?: boolean
  publishUtilityTarget?: (target: HTMLDivElement | null) => void
  navigation?: ReactNode
  children?: ReactNode
  tool?: 'route' | 'brief' | 'details' | 'activity' | 'runtime' | 'diagnostics'
  workspaceTools?: boolean
  expanded?: boolean
  fullWidth?: boolean
  contextTitle?: string
  returnLabel?: string
  onExpand?: () => void
  onReturn?: () => void
  expandButtonRef?: RefObject<HTMLButtonElement | null>
  resize?: { preview?: PaneResizePreview; value: number; min: number; max: number; onChange: (width: number) => void; onReset: () => void }

  publishDetailsTarget?: (target: HTMLDivElement | null) => void
  inspection?: ReactNode
  brief?: ReactNode
  publishBriefHeaderTarget?: (node: HTMLDivElement | null) => void
  hasDetails?: boolean
}) {
  useLocale()
  const detailsVisible = !utility && tool === 'details'
  const body = (
    <>
      {resize && <PaneDivider label={title} controls="desk-inspector" {...resize} onCollapse={onReturn ?? onClose} />}
      <div className="desk-pane-head">

        <div className="desk-pane-heading">{navigation}<span>{title}</span>{(expanded || fullWidth) && contextTitle && <span className="desk-pane-context" title={contextTitle}>{contextTitle}</span>}</div>
        <div ref={publishHeaderTarget} className="desk-pane-head-slot" hidden={utility || tool !== 'route' || fullWidth} />
        <div ref={publishBriefHeaderTarget} className="desk-pane-head-slot desk-brief-head" hidden={utility || tool !== 'brief'} />
        <div className="desk-pane-actions">
        {workspaceTools && (expanded || fullWidth) ? <>
          {resize && <PaneWidthMenu {...resize} />}
          <Button ref={expandButtonRef} className="desk-pane-return" onClick={onReturn}>{fullWidth ? <IconChevronLeft /> : <IconPanelLeft />}{fullWidth ? returnLabel ?? msg('Back to pack') : msg('Return to split view')}</Button>
        </> : workspaceTools && !utility && <Tooltip content={msg('Expand pane')}><button ref={expandButtonRef} className="desk-icon-button" type="button" aria-label={msg('Expand pane')} aria-pressed={false} onClick={onExpand}><IconFocus /></button></Tooltip>}
        <PaneToggle side="right" expanded label={msg('Collapse {{value0}}', { value0: title.toLowerCase() })} controls="desk-inspector" onClick={onClose} />
        </div>
      </div>
      {showEmpty && !utility && tool === 'route' && <p className="desk-pane-empty">{msg(EMPTY_STATE)}</p>}
      {/* Always mounted, so a route's portal target never disappears under
          it — including while nothing is published. */}
      <div className="desk-tool-content">
      <section className="desk-brief-retained" aria-label={msg('Brief')} hidden={utility || tool !== 'brief'}>{brief}</section>
      <section id="desk-right-details" className="desk-right-details" data-reading={inspection != null || undefined} aria-label={msg('Details')} hidden={!detailsVisible}>
        {detailsVisible && !hasDetails && !inspection && <p className="desk-pane-empty">{msg(EMPTY_STATE)}</p>}
        <div className="desk-details-slot" ref={publishDetailsTarget} hidden={inspection != null} />
        {inspection}
      </section>
      <div className="desk-inspector-retained" hidden={utility || tool !== 'route'}>
      <div ref={publishTarget} className="desk-inspector-slot" />
      </div>
      <div ref={publishUtilityTarget} className="desk-inspector-slot" hidden={!utility} />
      <div className="desk-tool-activity" hidden={utility || tool !== 'activity'}><WorkspaceActivity /></div>
      <div className="desk-tool-activity" hidden={utility || tool !== 'runtime'}><WorkspaceActivity runtime /></div>
      <div className="desk-tool-activity" hidden={utility || tool !== 'diagnostics'}><Diagnostics /></div>
      {children}
      </div>
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
            data-modal-surface
            ref={publishPane}
            className="desk-drawer desk-pane-drawer desk-drawer-right"
            id="desk-inspector"
            aria-label={title}
            style={
              declaredWidth === undefined
                ? undefined
                : ({ '--drawer-w': `${declaredWidth}px` } as CSSProperties)
            }
            onCloseAutoFocus={(event) => {
              event.preventDefault()
              const gesture = restoreFocusRef?.current
              if (gesture?.isConnected && gesture.getClientRects().length) gesture.focus()
              else document.getElementById('main')?.focus({ preventScroll: true })
            }}
          >
            <VisuallyHidden.Root>
              <Dialog.Title>{title}</Dialog.Title>
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
      aria-label={title}
      id="desk-inspector"
      hidden={!open}
    >
      {body}
    </aside>
  )
}
