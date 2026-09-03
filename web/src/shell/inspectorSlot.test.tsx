/**
 * The Inspector slot, from a **route** — which is where it was broken.
 *
 * The provider used to wrap `RightPane` alone. Routes render as a sibling of
 * that pane, so `useInspectorSlot()` in a route read the context's closed
 * default for ever: `target` was null, every `createPortal` was a no-op, and
 * the one mechanism the shell offers for publishing into the Inspector could
 * not be used by the only things that would ever publish into it. Nothing
 * failed loudly — the pane simply showed its empty state.
 *
 * So the case is written as a route, in the real `AppShell`, at each of the
 * three widths and across a transition between two of them.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { createPortal } from 'react-dom'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AppShell } from './AppShell'
import { useInspectorPortal, useInspectorSlot } from './InspectorSlot'
import { forgetAuthorBridge } from './authorBridge'
import { forgetConsole } from './consoleLog'

const PROJECT = stubClient({
  list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [] }) })
})

beforeEach(() => {
  vi.stubGlobal('fetch', async () => ({
    ok: false,
    status: 404,
    statusText: '',
    text: async () => JSON.stringify({ error: 'no such file' })
  }))
})

afterEach(() => {
  cleanup()
  forgetConsole()
  forgetAuthorBridge()
  vi.unstubAllGlobals()
  window.localStorage.clear()
})

/**
 * A `ResizeObserver` that actually observes, and a way to drive it.
 *
 * `testing/setup.ts` shims one that does nothing, which is right for every
 * test that only needs the global to exist — but `slot.size` is now a
 * *measurement*, and jsdom performs no layout, so a no-op observer and a
 * rect of zero would let a broken measurement pass. This stub records what was
 * observed and lets a case say "this element is now 360px wide", which is the
 * only way the assertion can discriminate here.
 */
function observedResize() {
  const watched: { element: Element; notify: () => void }[] = []
  class Stub {
    private readonly notify: () => void
    constructor(callback: () => void) {
      this.notify = callback
    }
    observe(element: Element) {
      watched.push({ element, notify: this.notify })
    }
    unobserve() {}
    disconnect() {}
  }
  vi.stubGlobal('ResizeObserver', Stub)
  return {
    /** Give every observed element a width, and tell its observer. */
    resizeTo(width: number) {
      act(() => {
        for (const { element, notify } of watched) {
          ;(element as HTMLElement).getBoundingClientRect = () =>
            ({ width, height: 0, top: 0, left: 0, right: width, bottom: 0, x: 0, y: 0 }) as DOMRect
          notify()
        }
      })
    },
    get observedCount() {
      return watched.length
    }
  }
}

/** One viewport, movable, so a breakpoint swap can be driven. */
function viewport(width: number) {
  const listeners = new Set<() => void>()
  let current = width
  vi.stubGlobal('matchMedia', (query: string) => {
    const limit = /max-width:\s*(\d+)px/.exec(query)
    return {
      media: query,
      get matches() {
        return limit === null ? false : current <= Number(limit[1])
      },
      onchange: null,
      addListener() {},
      removeListener() {},
      addEventListener(_: string, handler: () => void) {
        listeners.add(handler)
      },
      removeEventListener(_: string, handler: () => void) {
        listeners.delete(handler)
      },
      dispatchEvent: () => false
    }
  })
  return {
    resizeTo(next: number) {
      current = next
      act(() => {
        for (const listener of [...listeners]) listener()
      })
    }
  }
}

/**
 * A route that publishes through the hook, and can stop.
 *
 * The publisher is its own component, mounted conditionally, because that is
 * the shape a route has: the hook is called unconditionally by whatever is
 * mounted, and "not publishing" is that thing not being mounted.
 */
function Publisher() {
  return useInspectorPortal(<p>published from the route</p>)
}

function ClaimingRoute() {
  const [publishing, setPublishing] = useState(true)
  return (
    <>
      <h1>a route</h1>
      <button type="button" onClick={() => setPublishing((on) => !on)}>
        toggle the publisher
      </button>
      {publishing && <Publisher />}
    </>
  )
}

function renderClaiming() {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: PROJECT.client })}>
            <AppShell>
              <ClaimingRoute />
            </AppShell>
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: ['/'] }
  )
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

const EMPTY_STATE = 'Select a row, a node or a file to inspect it here.'

/** A route that publishes into the pane, exactly as a real one would. */
function PublishingRoute() {
  const slot = useInspectorSlot()
  return (
    <>
      <h1>a route</h1>
      <p data-testid="slot-size">{slot.size}</p>
      <p data-testid="slot-open">{String(slot.open)}</p>
      <button type="button" onClick={() => slot.setTab('rows')}>
        select the rows tab
      </button>
      <p data-testid="slot-tab">{slot.tab ?? 'none'}</p>
      {slot.target !== null && createPortal(<p>published from the route</p>, slot.target)}
    </>
  )
}

function renderDesk() {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: PROJECT.client })}>
            <AppShell>
              <PublishingRoute />
            </AppShell>
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: ['/'] }
  )
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('a route publishing into the Inspector', () => {
  it('reaches the pane at the widest breakpoint, where the panel is a column', async () => {
    viewport(1400)
    const resize = observedResize()
    renderDesk()
    // The column form is always mounted — `hidden` while closed — so the
    // target is a real element before the pane is ever opened.
    await waitFor(() => expect(screen.getByText('published from the route')).toBeTruthy())
    const aside = document.querySelector('aside[aria-label="Inspector"]')!
    expect(aside.querySelector('.desk-inspector-slot')!.textContent).toBe(
      'published from the route'
    )
    expect(screen.getByTestId('slot-open').textContent).toBe('false')
    expect(screen.getByTestId('slot-size').textContent).toBe('0')

    fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
    await waitFor(() => expect(screen.getByTestId('slot-open').textContent).toBe('true'))

    // **Measured, not configured.** The pane is observed, and the size a route
    // reads is whatever the pane is — not `panes.inspector.width`, which the
    // sheet caps against the viewport and the drawer form ignores unless the
    // file stated one. Nothing has laid out in jsdom, so it is zero until the
    // observer says otherwise.
    expect(resize.observedCount).toBeGreaterThan(0)
    expect(screen.getByTestId('slot-size').textContent).toBe('0')
    resize.resizeTo(440)
    await waitFor(() => expect(screen.getByTestId('slot-size').textContent).toBe('440'))
    // And it follows the pane rather than latching: a dragged window changes
    // the cap, with no React state change to hang a recalculation off.
    resize.resizeTo(300)
    await waitFor(() => expect(screen.getByTestId('slot-size').textContent).toBe('300'))
  })

  it('reports zero for a closed pane however wide the last measurement was', async () => {
    viewport(1400)
    const resize = observedResize()
    renderDesk()
    fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
    await waitFor(() => expect(screen.getByTestId('slot-open').textContent).toBe('true'))
    resize.resizeTo(440)
    await waitFor(() => expect(screen.getByTestId('slot-size').textContent).toBe('440'))

    fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
    await waitFor(() => expect(screen.getByTestId('slot-open').textContent).toBe('false'))
    expect(screen.getByTestId('slot-size').textContent).toBe('0')
  })

  it('carries the tab the route selected, through the shell and back', () => {
    viewport(1400)
    renderDesk()
    expect(screen.getByTestId('slot-tab').textContent).toBe('none')
    fireEvent.click(screen.getByRole('button', { name: 'select the rows tab' }))
    expect(screen.getByTestId('slot-tab').textContent).toBe('rows')
  })

  it('reports no target while the drawer form is closed, and one when it opens', async () => {
    // Honest rather than convenient: below 1100px the pane is a `Dialog`, and
    // a closed dialog's portal is not in the document. The route is told there
    // is nowhere to publish rather than handed a detached node.
    viewport(1000)
    renderDesk()
    expect(screen.queryByText('published from the route')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
    await waitFor(() => expect(screen.getByText('published from the route')).toBeTruthy())
    const drawer = screen.getByRole('dialog', { name: 'Inspector' })
    expect(drawer.querySelector('.desk-inspector-slot')!.textContent).toBe(
      'published from the route'
    )

    // And closing it takes the target away again rather than leaving a
    // detached element behind.
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByText('published from the route')).toBeNull())
  })

  it('takes the empty state away while something is published, and brings it back', async () => {
    // The paragraph used to be unconditional, so the first route to publish
    // showed its panel *and* the empty state underneath it. A portal cannot
    // tell React it happened, so the pane counts claims instead.
    viewport(1400)
    observedResize()
    renderClaiming()
    await waitFor(() => expect(screen.getByText('published from the route')).toBeTruthy())
    expect(screen.queryByText(EMPTY_STATE)).toBeNull()

    // Unmounting the publisher releases the claim, and the empty state is back.
    fireEvent.click(screen.getByRole('button', { name: 'toggle the publisher' }))
    await waitFor(() => expect(screen.getByText(EMPTY_STATE)).toBeTruthy())
    expect(screen.queryByText('published from the route')).toBeNull()

    // And it goes away again, so this is a count rather than a one-way latch.
    fireEvent.click(screen.getByRole('button', { name: 'toggle the publisher' }))
    await waitFor(() => expect(screen.queryByText(EMPTY_STATE)).toBeNull())
  })

  it('follows the pane across a breakpoint swap, publishing into the new element', async () => {
    // The swap remounts the subtree, so the element the route was portalling
    // into is detached. A one-shot effect kept it; a callback ref replaces it.
    const screenWidth = viewport(1400)
    renderDesk()
    await waitFor(() => expect(screen.getByText('published from the route')).toBeTruthy())
    const wideSlot = document.querySelector('.desk-inspector-slot')!

    fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
    screenWidth.resizeTo(1000)
    await waitFor(() => expect(screen.getByRole('dialog', { name: 'Inspector' })).toBeTruthy())

    const drawerSlot = screen
      .getByRole('dialog', { name: 'Inspector' })
      .querySelector('.desk-inspector-slot')!
    expect(drawerSlot).not.toBe(wideSlot)
    expect(drawerSlot.textContent).toBe('published from the route')
    expect(document.body.contains(wideSlot)).toBe(false)
  })
})
