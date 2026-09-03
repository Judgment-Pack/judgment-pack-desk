/**
 * Render the rows that are on screen, and reserve the space for the rest.
 *
 * A fixed row height, an overscan, and a scroll listener — about sixty lines,
 * and no dependency: a virtual-list library would bring a measurement engine
 * for a list whose rows are all one height.
 *
 * **The load-bearing detail is the fallback.** jsdom performs no layout, so
 * every measured height is 0 there, and a window computed from zero renders
 * zero rows — every test of the pane would then assert against an empty list
 * and pass for the wrong reason. A viewport that cannot be measured therefore
 * renders **every** row. It has its own mutation row.
 */
import { useCallback, useEffect, useState, type RefObject } from 'react'

export interface Window {
  /** The first row to render. */
  start: number
  /** One past the last row to render. */
  end: number
  /** The space above the first rendered row, in pixels. */
  padTop: number
  /** The space below the last rendered row, in pixels. */
  padBottom: number
}

export function useWindowedRows(
  container: RefObject<HTMLElement | null>,
  count: number,
  rowHeight: number,
  overscan = 6
): Window {
  const [scrollTop, setScrollTop] = useState(0)
  const [height, setHeight] = useState(0)

  const measure = useCallback(() => {
    const element = container.current
    if (element === null) return
    setScrollTop(element.scrollTop)
    setHeight(element.clientHeight)
  }, [container])

  useEffect(() => {
    const element = container.current
    if (element === null) return
    measure()
    element.addEventListener('scroll', measure, { passive: true })
    let observer: ResizeObserver | undefined
    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(measure)
      observer.observe(element)
    }
    return () => {
      element.removeEventListener('scroll', measure)
      observer?.disconnect()
    }
  }, [container, measure, count])

  // Nothing measurable: render everything. A window computed from a height of
  // zero is a window with no rows in it, which is not "the list is short" — it
  // is "the list was never laid out".
  if (height <= 0 || rowHeight <= 0) {
    return { start: 0, end: count, padTop: 0, padBottom: 0 }
  }

  const first = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan)
  const visible = Math.ceil(height / rowHeight) + overscan * 2
  const last = Math.min(count, first + visible)
  return {
    start: first,
    end: last,
    padTop: first * rowHeight,
    padBottom: Math.max(0, (count - last) * rowHeight)
  }
}

/**
 * Move focus between rows with the arrow keys.
 *
 * Rows are links, so tab order is native and this adds nothing to it. What it
 * adds is a way through the list that does not depend on Tab: ArrowUp and
 * ArrowDown step, Home and End jump.
 *
 * **`rows` is what is rendered, not what the list holds.** A windowed list can
 * be longer than its window, and End then reaches the last rendered row rather
 * than the last row of all — the window follows focus rather than the other
 * way round. Reaching past the window means scrolling first, which is what a
 * pointer does anyway. Stated because a reader of this function would
 * reasonably assume otherwise, and the fix belongs with the keyboard work the
 * editor phase brings, not smuggled in under a name that says `moveFocus`.
 */
export function moveFocus(
  event: { key: string; preventDefault: () => void },
  rows: HTMLElement[],
  current: number
): number | undefined {
  const last = rows.length - 1
  let next: number | undefined
  if (event.key === 'ArrowDown') next = Math.min(last, current + 1)
  else if (event.key === 'ArrowUp') next = Math.max(0, current - 1)
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = last
  if (next === undefined || next < 0) return undefined
  event.preventDefault()
  return next
}
