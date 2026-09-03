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
 * **A portal cannot tell React it happened.** `createPortal` writes into a DOM
 * node that is not this component's child, so the pane has no way to know
 * whether anything is in its slot — and it rendered an empty-state paragraph
 * unconditionally, which stood underneath the first published panel. A CSS
 * `:empty` sibling rule is the tempting fix and is rejected: vitest runs with
 * `css: false`, so no stylesheet is processed in this repo's tests, and the
 * mutation harness could not discriminate a rule nothing evaluates. So the
 * slot counts **claims** instead — a publisher claims on mount and releases on
 * unmount — and the frame hands the count to the pane.
 */
import { createContext, useContext, useEffect, type ReactNode, type ReactPortal } from 'react'
import { createPortal } from 'react-dom'

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
  /**
   * Say that something is published into the slot, for as long as it is.
   *
   * Held by the frame, so the pane can stop rendering its empty state beside a
   * panel. Returns nothing; the release is the effect's cleanup.
   */
  claim: () => () => void
}

const CLOSED: InspectorSlot = {
  open: false,
  size: 0,
  tab: null,
  setTab: () => {},
  target: null,
  claim: () => () => {}
}

export const InspectorSlotContext = createContext<InspectorSlot>(CLOSED)

export function useInspectorSlot(): InspectorSlot {
  return useContext(InspectorSlotContext)
}

/**
 * Publish one node into the Inspector, and claim the slot while it is there.
 *
 * The claim and the portal are one call because they are one fact: something
 * is in the pane. A route that claimed without publishing would suppress the
 * empty state and show nothing, and a route that published without claiming is
 * the bug this exists to fix.
 *
 * A closed drawer publishes **no target**, so nothing is rendered and nothing
 * is claimed — the route is told there is nowhere to publish rather than
 * handed a detached node.
 */
export function useInspectorPortal(node: ReactNode): ReactPortal | null {
  const { target, claim } = useInspectorSlot()
  const publishing = target !== null
  useEffect(() => {
    if (!publishing) return
    return claim()
  }, [publishing, claim])
  return target === null ? null : createPortal(node, target)
}
