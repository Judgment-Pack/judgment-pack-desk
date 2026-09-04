/**
 * The window, with a viewport that has a size.
 *
 * **Every other test of this list runs against jsdom's zero heights**, which
 * take the hook's "nothing measurable, render everything" branch — so they
 * exercise the fallback and never the arithmetic. The measurement is stubbed
 * here instead: `clientHeight` is defined on the node and `scrollTop` is
 * written, which is exactly what the hook reads. That makes 300 rows in a 400px
 * viewport a real window, and it is the only way the defects below can fail.
 */
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import { moveFocus, useWindowedRows } from './useWindowedRows'

afterEach(cleanup)

const ROW = 40
const VIEWPORT = 400

/**
 * A list whose scrolling element reports a real height.
 *
 * `present` removes the scrolling element **without unmounting the hook**,
 * which is what a failed refetch does to the pane: `PacksPane` stays mounted
 * and swaps the list for a failure sentence. Unmounting the whole component
 * instead resets the hook's state, and a test that does that cannot see a hook
 * holding on to the element it was given first.
 */
function List({
  count,
  rowHeight = ROW,
  present = true,
  bring
}: {
  count: number
  rowHeight?: number
  present?: boolean
  bring?: number
}) {
  const window = useWindowedRows(count, rowHeight)
  const [node, setNode] = useState<HTMLElement | null>(null)
  return (
    <div>
      {present && (
        <div
          data-testid="list"
          ref={(element) => {
            if (element !== null && element !== node) {
              Object.defineProperty(element, 'clientHeight', {
                value: VIEWPORT,
                configurable: true
              })
              setNode(element)
            }
            window.ref(element)
          }}
        >
          <div data-testid="padTop" style={{ height: window.padTop }} />
          <ul>
            {Array.from({ length: window.end - window.start }, (_, offset) => (
              <li key={window.start + offset} data-row={window.start + offset}>
                row {window.start + offset}
              </li>
            ))}
          </ul>
          <div data-testid="padBottom" style={{ height: window.padBottom }} />
        </div>
      )}
      {bring !== undefined && (
        <button type="button" onClick={() => window.scrollRowIntoView(bring)}>
          bring {bring} in
        </button>
      )}
      <p data-testid="window">{`${window.start}:${window.end}:${window.padTop}`}</p>
    </div>
  )
}

const listNode = () => screen.getByTestId('list')
const windowOf = () => screen.getByTestId('window').textContent!.split(':').map(Number)

/** Scroll the element the way a wheel does, and let the listener see it. */
function scrollTo(top: number) {
  act(() => {
    const node = listNode()
    Object.defineProperty(node, 'scrollTop', { value: top, configurable: true, writable: true })
    node.dispatchEvent(new Event('scroll'))
  })
}

describe('a window over a viewport that has a height', () => {
  it('renders a screenful and not three hundred rows', () => {
    render(<List count={300} />)
    const [start, end] = windowOf()
    expect(start).toBe(0)
    // A 400px viewport at 40px a row is ten rows, plus the overscan.
    expect(end).toBeGreaterThan(10)
    expect(end).toBeLessThan(30)
  })

  it('moves the window down as the list is scrolled', () => {
    render(<List count={300} />)
    scrollTo(4000)
    const [start, end, padTop] = windowOf()
    expect(start).toBeGreaterThan(90)
    expect(end).toBeGreaterThan(start!)
    expect(padTop).toBe(start! * ROW)
  })

  it('renders rows after a filter empties the list beneath the scroll', () => {
    // Scrolled to the bottom of 300 rows and then filtered to one, the window
    // used to be computed from the old scroll position — `{start: 244, end: 1}`
    // — so the list rendered *nothing* and a reader with one match saw an
    // empty pane.
    const { rerender } = render(<List count={300} />)
    scrollTo(10000)
    expect(windowOf()[0]).toBeGreaterThan(200)
    act(() => {
      rerender(<List count={1} />)
    })
    const [start, end] = windowOf()
    expect(start).toBe(0)
    expect(end).toBe(1)
    expect(screen.getByText('row 0')).toBeTruthy()
  })

  it('clamps the scroll position to the shorter list', () => {
    const { rerender } = render(<List count={300} />)
    scrollTo(10000)
    act(() => {
      rerender(<List count={3} />)
    })
    // Three rows at 40px is shorter than the viewport, so the top is the only
    // position there is.
    expect(listNode().scrollTop).toBe(0)
  })

  it('clamps the scroll when the rows get shorter under the same count', () => {
    // The other half of the clamp: `count` is unchanged and `rowHeight` is not.
    // 300 rows at 40px is 12,000px of list; at 20px it is 6,000, and a viewer
    // scrolled to the bottom of the first is 6,000px past the end of the second.
    const { rerender } = render(<List count={300} />)
    scrollTo(11600)
    expect(windowOf()[0]).toBeGreaterThan(280)

    act(() => {
      rerender(<List count={300} rowHeight={20} />)
    })
    // 300 × 20 − 400 of viewport is the last position there is.
    expect(listNode().scrollTop).toBe(5600)
    const [start, end] = windowOf()
    expect(start).toBeLessThan(300)
    expect(end).toBe(300)
  })

  it('keeps scrolling after the list is taken away and put back', () => {
    // The listener effect used to depend on the ref object and the row count,
    // and neither changes when a failed refetch removes the list and a
    // same-count retry mounts a new node — so the listener stayed on a
    // detached element and scrolling did nothing, for ever.
    //
    // **The hook stays mounted throughout**, because that is the case: the pane
    // swaps the list for a failure sentence and keeps its own state. A test
    // that unmounts the component gets a fresh hook and can see none of this.
    const { rerender } = render(<List count={300} />)
    const first = listNode()
    scrollTo(4000)
    expect(windowOf()[0]).toBeGreaterThan(90)

    act(() => {
      rerender(<List count={300} present={false} />)
    })
    act(() => {
      rerender(<List count={300} />)
    })
    expect(listNode()).not.toBe(first)
    // A new node, and it scrolls.
    scrollTo(6000)
    expect(windowOf()[0]).toBeGreaterThan(140)
  })

  it('renders every row where nothing can be measured', () => {
    // The fallback, still. jsdom lays nothing out, and a window computed from
    // a height of zero has no rows in it — which is not "the list is short".
    function Unmeasured() {
      const window = useWindowedRows(300, ROW)
      return <p data-testid="window">{`${window.start}:${window.end}`}</p>
    }
    render(<Unmeasured />)
    expect(screen.getByTestId('window').textContent).toBe('0:300')
  })
})

describe('the keyboard reaches the whole list', () => {
  it('steps and jumps over every row, not only the rendered ones', () => {
    // `moveFocus` used to be given the rendered anchors, so with 300 rows in a
    // 400px viewport focus stopped at row 21: End reached the last *rendered*
    // row, and ArrowDown from there called preventDefault and moved nothing.
    const prevented: string[] = []
    const key = (name: string, current: number) =>
      moveFocus({ key: name, preventDefault: () => prevented.push(name) }, 300, current)

    expect(key('ArrowDown', 20)).toBe(21)
    expect(key('ArrowDown', 21)).toBe(22)
    expect(key('End', 21)).toBe(299)
    expect(key('Home', 299)).toBe(0)
    // And it still stops at the ends rather than running off them.
    expect(key('ArrowDown', 299)).toBe(299)
    expect(key('ArrowUp', 0)).toBe(0)
  })

  it('asks for nothing from an empty list', () => {
    expect(moveFocus({ key: 'End', preventDefault: () => {} }, 0, 0)).toBeUndefined()
  })

  it('leaves every other key to the browser', () => {
    let prevented = false
    const result = moveFocus(
      { key: 'a', preventDefault: () => (prevented = true) },
      300,
      5
    )
    expect(result).toBeUndefined()
    expect(prevented).toBe(false)
  })
})

describe('scrolling a row into view', () => {
  it('brings an off-window row in, so focus can follow it', () => {
    // The keyboard's half of the fix: a row that is not rendered cannot be
    // focused, so the destination is scrolled to first and focused in the
    // render that brings it. Asserted through `scrollRowIntoView` itself — a
    // test that only scrolls the element exercises the listener and not this.
    render(<List count={300} bring={250} />)
    expect(screen.queryByText('row 250')).toBeNull()
    act(() => {
      fireEvent.click(screen.getByRole('button', { name: 'bring 250 in' }))
    })
    expect(listNode().scrollTop).toBeGreaterThan(0)
    expect(screen.getByText('row 250')).toBeTruthy()
  })

  it('leaves a row that is already on screen where it is', () => {
    render(<List count={300} bring={2} />)
    act(() => {
      fireEvent.click(screen.getByRole('button', { name: 'bring 2 in' }))
    })
    expect(listNode().scrollTop).toBe(0)
    expect(screen.getByText('row 2')).toBeTruthy()
  })

  it('scrolls back up for a row above the window', () => {
    render(<List count={300} bring={5} />)
    scrollTo(8000)
    expect(screen.queryByText('row 5')).toBeNull()
    act(() => {
      fireEvent.click(screen.getByRole('button', { name: 'bring 5 in' }))
    })
    expect(listNode().scrollTop).toBe(5 * ROW)
    expect(screen.getByText('row 5')).toBeTruthy()
  })
})
