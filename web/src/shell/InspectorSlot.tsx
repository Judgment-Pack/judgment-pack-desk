/**
 * How a route puts something in the Inspector, pinned to one form.
 *
 * The shell renders an **always-mounted** div and exposes the element through
 * this context; a route that wants the pane calls `createPortal(node, target)`.
 * It is the one mechanism, and the alternative — a context holding a JSX node
 * that routes set — is the one this deliberately is not: a route's render
 * would then write shell state, which React forbids in spirit and punishes in
 * practice.
 *
 * **The provider sits above `<main>`.** It has to: a provider wrapped around
 * the Inspector alone is a sibling of the routes, so `useInspectorSlot()` in a
 * route read the closed default and every portal was a no-op. The pane
 * publishes its target upwards through a callback ref, and the frame holds
 * both the target and the tab.
 *
 * The context value is **metadata only**: whether the pane is open, how wide
 * it is, which tab is selected. No node, ever.
 *
 * Phase A ships the mechanism and the empty state. No route publishes into it
 * yet, so `target` is a real element that nothing has portalled into — which
 * is exactly the state a route's first publisher will find.
 */
import { createContext, useContext } from 'react'

export interface InspectorSlot {
  open: boolean
  /**
   * The pane's **measured** width in pixels while open, 0 while closed.
   *
   * Measured and not configured, because the two are different numbers: the
   * sheet caps the configured width against the viewport, and the drawer form
   * ignores it altogether unless the project file stated one. A route that
   * lays something out against this needs the width the pane actually has.
   * It updates as the window is dragged.
   */
  size: number
  tab: string | null
  setTab: (tab: string | null) => void
  /** The portal target, or null before the shell has mounted it. */
  target: HTMLElement | null
}

const CLOSED: InspectorSlot = {
  open: false,
  size: 0,
  tab: null,
  setTab: () => {},
  target: null
}

export const InspectorSlotContext = createContext<InspectorSlot>(CLOSED)

export function useInspectorSlot(): InspectorSlot {
  return useContext(InspectorSlotContext)
}
