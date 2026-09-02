/**
 * The desk below its two breakpoints.
 *
 * Every other test in this tree runs at the widest breakpoint, because
 * `testing/setup.ts` answers every media query `matches: false`. That left the
 * two drawer forms — the rail below 900px, the Inspector below 1100px —
 * asserted nowhere, and one of them shipped with no way in: the rail's collapse
 * toggle is not rendered in drawer form, so the only opener was the Mod+B
 * chord, on the width whose likeliest device has no keyboard at all.
 *
 * The stub is a real width rather than a boolean per query, so a test says
 * "800px" and the component's own breakpoint strings decide what that means.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AppShell } from './AppShell'
import { forgetAuthorBridge } from './authorBridge'
import { forgetConsole } from './consoleLog'

afterEach(() => {
  cleanup()
  forgetConsole()
  forgetAuthorBridge()
  vi.unstubAllGlobals()
  window.localStorage.clear()
})

const PROJECT = stubClient({
  list_packs: () => ({
    text: JSON.stringify({ status: 'valid', configPath: '/p/jpack.json', packs: [{ id: 'intake-triage' }] })
  })
})

/** Answer every `(max-width: Npx)` query against one viewport width. */
function viewport(width: number) {
  vi.stubGlobal('matchMedia', (query: string) => {
    const limit = /max-width:\s*(\d+)px/.exec(query)
    return {
      media: query,
      matches: limit === null ? false : width <= Number(limit[1]),
      onchange: null,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent: () => false
    }
  })
}

function renderShell() {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: PROJECT.client })}>
            <AppShell>
              <h1>a route</h1>
            </AppShell>
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: ['/'] }
  )
  return {
    router,
    ...render(
      <QueryClientProvider client={testQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
  }
}

describe('the shell at 800px, where the rail is a drawer', () => {
  it('offers a visible control that opens the navigation', async () => {
    // The claim README and Help & About both make — "every shortcut has a
    // visible button" — is false at this width without it, and Mod+B is not
    // available to a viewer with no keyboard.
    viewport(800)
    renderShell()
    const opener = screen.getByRole('button', { name: 'Project navigation' })
    expect(opener.getAttribute('aria-expanded')).toBe('false')
    expect(opener.getAttribute('aria-controls')).toBe('desk-rail')
    expect(screen.queryByRole('navigation', { name: 'Project' })).toBeNull()

    fireEvent.click(opener)
    await waitFor(() => expect(screen.getByRole('navigation', { name: 'Project' })).toBeTruthy())
    // The control it points at is the element that arrived.
    expect(document.getElementById('desk-rail')).toBeTruthy()
    // Read off the DOM rather than through a role query, because the drawer is
    // a **modal** dialog: Radix marks the rest of the page `aria-hidden` while
    // it is open, so the opener is correctly not in the accessibility tree at
    // that moment. The attribute is still what the header wrote.
    expect(opener.getAttribute('aria-expanded')).toBe('true')
  })

  it('reaches every destination the rail carries once the drawer is open', async () => {
    viewport(800)
    renderShell()
    fireEvent.click(screen.getByRole('button', { name: 'Project navigation' }))
    await screen.findByRole('navigation', { name: 'Project' })
    expect(screen.getByRole('button', { name: 'Create a pack' })).toBeTruthy()
    for (const name of ['Matrix and coverage', 'Graphs', 'Author', 'Admin', 'Help & About']) {
      expect(screen.getByRole('link', { name })).toBeTruthy()
    }
    expect(await screen.findByRole('link', { name: 'intake-triage' })).toBeTruthy()
  })

  it('exposes the navigation landmark exactly while the drawer is open, and the page beneath the rest of the time', async () => {
    // Two properties, and the second is why the first cannot be stated as
    // "the same landmarks as the wide desk". The drawer is a modal dialog, so
    // while it is open the page beneath it is `aria-hidden` — banner, main and
    // contentinfo are deliberately out of the accessibility tree, which is what
    // a modal is for. What must not happen is the rail's own landmark going
    // missing at this width, which is what it did before the drawer carried a
    // `<nav>` of its own.
    viewport(800)
    renderShell()
    expect(screen.getAllByRole('banner')).toHaveLength(1)
    expect(screen.queryByRole('navigation', { name: 'Project' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Project navigation' }))
    await waitFor(() =>
      expect(screen.getAllByRole('navigation', { name: 'Project' })).toHaveLength(1)
    )
    expect(screen.queryByRole('banner')).toBeNull()

    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await waitFor(() => expect(screen.getAllByRole('banner')).toHaveLength(1))
    expect(screen.getAllByRole('main')).toHaveLength(1)
    expect(screen.getAllByRole('contentinfo')).toHaveLength(1)
    expect(screen.queryByRole('navigation', { name: 'Project' })).toBeNull()
  })
})

describe('the shell at 1000px, where the Inspector is a drawer', () => {
  it('gives the drawer the id its toggle claims to control', async () => {
    // Below 1100px the Inspector is a `Dialog`, and the header's toggle
    // carries `aria-controls="desk-inspector"` in both forms. A toggle
    // pointing at an element that is not there offers assistive technology a
    // broken relationship rather than none.
    viewport(1000)
    renderShell()
    // At this width the rail is still a column, not a drawer.
    expect(screen.queryByRole('button', { name: 'Project navigation' })).toBeNull()
    const toggle = screen.getByRole('button', { name: 'Inspector' })
    expect(toggle.getAttribute('aria-controls')).toBe('desk-inspector')
    fireEvent.click(toggle)
    await waitFor(() => expect(screen.getByRole('dialog', { name: 'Inspector' })).toBeTruthy())
    expect(document.getElementById('desk-inspector')).toBeTruthy()
    expect(document.getElementById('desk-inspector')!.getAttribute('role')).toBe('dialog')
  })
})
