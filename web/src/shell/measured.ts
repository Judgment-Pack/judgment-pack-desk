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
 * and zero* — `hidden` plus `display: none`. Admin says "not mounted" for one
 * and "collapsed" for the other.
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

/** The three panes' rendered boxes, for Admin to print beside the configured ones. */
export interface RenderedPanes {
  rail: MeasuredBox | undefined
  inspector: MeasuredBox | undefined
  console: MeasuredBox | undefined
}

/**
 * Measure the frame the page is inside, by the ids it already carries.
 *
 * A DOM query rather than a context, and the reason is that this is the one
 * page whose job is to report on the frame rather than to live in it: threading
 * three measurements through the shell for a diagnostics page would put a
 * measurement in the render path of every route that does not want one.
 *
 * `signature` is what re-runs it — the shell state a toggle changes — because
 * a pane appearing or disappearing is not a resize of anything already
 * observed.
 */
export function useRenderedPanes(signature: string): RenderedPanes {
  const [panes, setPanes] = useState<RenderedPanes>({
    rail: undefined,
    inspector: undefined,
    console: undefined
  })
  useEffect(() => {
    const read = () => {
      const of = (id: string) => {
        const element = document.getElementById(id)
        return element === null ? undefined : measure(element)
      }
      setPanes({ rail: of('desk-rail'), inspector: of('desk-inspector'), console: of('desk-console') })
    }
    read()
    if (!hasResizeObserver()) return
    const observer = new ResizeObserver(read)
    for (const id of ['desk-rail', 'desk-inspector', 'desk-console']) {
      const element = document.getElementById(id)
      if (element !== null) observer.observe(element)
    }
    return () => observer.disconnect()
  }, [signature])
  return panes
}
