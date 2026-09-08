/**
 * What a pane is **actually** the size of, as opposed to what was configured.
 *
 * The two had drifted, and the drift was reported as fact. `panes.inspector.width`
 * is a decoded number the sheet then caps against the viewport — an accepted
 * 720px Inspector renders 440px wide at 1100px — and the drawer form ignores it
 * altogether unless the file stated one. Admin printed the decoded number as
 * though it were the pane, and `InspectorSlot.size` promised "the pane's width"
 * and handed a route the same pre-cap value. A route that laid something out
 * against it would have laid it out against a width nothing on screen has.
 *
 * Admin no longer prints any of this: the Panes card is gone, and with it the
 * three-pane reader that measured the frame by its ids. What is left is one
 * element's box, which is what the Inspector slot promises a route.
 *
 * So a rendered size is measured rather than derived. `ResizeObserver` is the
 * mechanism because the caps are viewport-relative: the number changes when the
 * window is dragged, with no React state change to hang a recalculation off.
 * The observer is optional at runtime — a browser without one, or a test that
 * has not stubbed one, still gets the initial measurement — because a shell
 * that threw where it is absent would make the shim a dependency rather than a
 * convenience.
 */
import { useEffect, useState } from 'react'

export interface MeasuredBox {
  width: number
  height: number
}

function measure(element: Element): MeasuredBox {
  const box = element.getBoundingClientRect()
  return { width: Math.round(box.width), height: Math.round(box.height) }
}

function hasResizeObserver(): boolean {
  return typeof globalThis.ResizeObserver === 'function'
}

/**
 * One element's rendered box, or undefined while there is no element.
 *
 * Undefined and `{ width: 0 }` are different answers and both are used:
 * the drawer form is *absent* while closed, and the column form is *mounted
 * and zero* — `hidden` plus `display: none`, which is a real element of no
 * size rather than an absent one.
 */
export function useMeasuredBox(element: Element | null): MeasuredBox | undefined {
  const [box, setBox] = useState<MeasuredBox | undefined>(undefined)
  useEffect(() => {
    if (element === null) {
      setBox(undefined)
      return
    }
    const read = () => setBox(measure(element))
    read()
    if (!hasResizeObserver()) return
    const observer = new ResizeObserver(read)
    observer.observe(element)
    return () => observer.disconnect()
  }, [element])
  return box
}
