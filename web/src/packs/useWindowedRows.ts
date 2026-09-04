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
import { useCallback, useEffect, useState } from 'react'

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

export interface WindowedRows extends Window {
  /**
   * The ref to put on the scrolling element.
   *
   * **A callback ref, not a `RefObject`.** The effect that attaches the scroll
   * listener used to depend on the ref object — which never changes — and on
   * the row count, which does not change when a list is unmounted and mounted
   * again at the same length. A failed refetch removes the list; the retry
   * mounts a *new* element, the effect does not re-run, and the listener stays
   * on a node that is no longer in the document. Scrolling then does nothing,
   * for ever, with no way back but a reload. Depending on the node itself is
   * the only thing that cannot miss that.
   */
  ref: (node: HTMLElement | null) => void
  /** Put a row on screen, so focus may follow it there. */
  scrollRowIntoView: (index: number) => void
}

export function useWindowedRows(
  count: number,
  rowHeight: number,
  overscan = 6
): WindowedRows {
  const [node, setNode] = useState<HTMLElement | null>(null)
  const [scrollTop, setScrollTop] = useState(0)
  const [height, setHeight] = useState(0)

  const ref = useCallback((next: HTMLElement | null) => setNode(next), [])

  useEffect(() => {
    if (node === null) return
    const measure = () => {
      setScrollTop(node.scrollTop)
      setHeight(node.clientHeight)
    }
    measure()
    node.addEventListener('scroll', measure, { passive: true })
    let observer: ResizeObserver | undefined
    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(measure)
      observer.observe(node)
    }
    return () => {
      node.removeEventListener('scroll', measure)
      observer?.disconnect()
    }
  }, [node])

  /**
   * A shorter list starts at the top.
   *
   * Filtering 300 rows down to one while scrolled to 10,000px left the scroll
   * position where it was and computed `{start: 244, end: 1}` — a window that
   * begins after the only row there is, so the list rendered *nothing* and the
   * reader saw an empty pane with one match in it. The scroll position belongs
   * to the list that was there; a different list gets the top of itself.
   */
  useEffect(() => {
    if (node === null) return
    const limit = Math.max(0, count * rowHeight - node.clientHeight)
    if (node.scrollTop > limit) {
      node.scrollTop = limit
      setScrollTop(limit)
    }
  }, [node, count, rowHeight])

  const scrollRowIntoView = useCallback(
    (index: number) => {
      if (node === null || rowHeight <= 0) return
      const top = index * rowHeight
      const bottom = top + rowHeight
      if (top < node.scrollTop) node.scrollTop = top
      else if (bottom > node.scrollTop + node.clientHeight) {
        node.scrollTop = bottom - node.clientHeight
      }
      setScrollTop(node.scrollTop)
    },
    [node, rowHeight]
  )

  // Nothing measurable: render everything. A window computed from a height of
  // zero is a window with no rows in it, which is not "the list is short" — it
  // is "the list was never laid out".
  if (height <= 0 || rowHeight <= 0) {
    return { ref, scrollRowIntoView, start: 0, end: count, padTop: 0, padBottom: 0 }
  }

  // Clamped to the list that is actually there. `scrollTop` is state and the
  // effect above corrects the element, but a render between the two would
  // otherwise compute a window past the end.
  const maxFirst = Math.max(0, count - 1)
  const first = Math.min(maxFirst, Math.max(0, Math.floor(scrollTop / rowHeight) - overscan))
  const visible = Math.ceil(height / rowHeight) + overscan * 2
  const last = Math.min(count, first + visible)
  return {
    ref,
    scrollRowIntoView,
    start: first,
    end: last,
    padTop: first * rowHeight,
    padBottom: Math.max(0, (count - last) * rowHeight)
  }
}

/**
 * Which row the arrow keys ask for, in **the list's own indices**.
 *
 * `rows` used to be the rendered anchors, so every key was clamped to the
 * window: with 300 rows in a 400px viewport, focus stopped at row 21, End
 * reached row 21, and ArrowDown from there called `preventDefault` and did
 * nothing — the keyboard could not leave the first screenful of a list a
 * pointer scrolls freely. The count is the whole list now, and reaching a row
 * that is not rendered is the caller's business: scroll it in, then focus it.
 */
export function moveFocus(
  event: { key: string; preventDefault: () => void },
  count: number,
  current: number
): number | undefined {
  const last = count - 1
  if (last < 0) return undefined
  let next: number | undefined
  if (event.key === 'ArrowDown') next = Math.min(last, current + 1)
  else if (event.key === 'ArrowUp') next = Math.max(0, current - 1)
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = last
  if (next === undefined || next < 0) return undefined
  event.preventDefault()
  return next
}
