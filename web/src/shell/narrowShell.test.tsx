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
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import {
  DESK_DEFAULTS,
  NOTHING_DECLARED,
  effectiveConfig,
  type DeclaredPanes,
  type PanesConfig
} from '../config/deskConfig'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AppShell } from './AppShell'
import { forgetAuthorBridge } from './authorBridge'
import { forgetConsole } from './consoleLog'
import { projectKey, shellStateKey } from './paneState'

const ROOT = '/home/someone/a-project'

beforeEach(() => {
  vi.stubGlobal('fetch', async (url: string) =>
    String(url).includes('/api/files')
      ? {
          ok: true,
          status: 200,
          statusText: '',
          text: async () => JSON.stringify({ root: ROOT, files: [] })
        }
      : {
          ok: false,
          status: 404,
          statusText: '',
          text: async () => JSON.stringify({ error: 'no such file' })
        }
  )
})

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

/**
 * One viewport, and one handle for moving it.
 *
 * `matchMedia` is stubbed as a real subscription rather than a frozen answer,
 * because the desk has to survive the transition and not only the two ends of
 * it: a tablet rotates, a window is dragged wider, and the layout an untouched
 * pane is clamped to has to be re-taken when that happens.
 */
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

function renderShell(panes?: PanesConfig, declaredPanes: DeclaredPanes = NOTHING_DECLARED) {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: PROJECT.client })}>
            <DeskConfigFixture
              value={{
                ...effectiveConfig(undefined),
                config: { ...DESK_DEFAULTS, panes: panes ?? DESK_DEFAULTS.panes },
                declaredPanes
              }}
            >
              <AppShell>
                <h1>a route</h1>
              </AppShell>
            </DeskConfigFixture>
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
    // **No IDREF while the drawer is closed.** A closed `Dialog` unmounts its
    // portal, so `aria-controls="desk-rail"` named an element that is not in
    // the document — a broken relationship offered to assistive technology in
    // place of none. `aria-expanded` carries the state either way.
    expect(opener.hasAttribute('aria-controls')).toBe(false)
    expect(document.getElementById('desk-rail')).toBeNull()
    expect(screen.queryByRole('navigation', { name: 'Project' })).toBeNull()

    fireEvent.click(opener)
    await waitFor(() => expect(screen.getByRole('navigation', { name: 'Project' })).toBeTruthy())
    // The control it points at is the element that arrived.
    expect(opener.getAttribute('aria-controls')).toBe('desk-rail')
    expect(document.getElementById('desk-rail')).toBeTruthy()
    // Read off the DOM rather than through a role query, because the drawer is
    // a **modal** dialog: Radix marks the rest of the page `aria-hidden` while
    // it is open, so the opener is correctly not in the accessibility tree at
    // that moment. The attribute is still what the header wrote.
    expect(opener.getAttribute('aria-expanded')).toBe('true')
  })

  it('offers a visible way out of the drawer, and hands focus back to the opener', async () => {
    // Escape closed it and the overlay closed it, and neither is something a
    // viewer can see — on the width whose likeliest device has no keyboard.
    // And this drawer has no `Dialog.Trigger`, so Radix had nothing to restore
    // focus to: closing it dropped focus on the body.
    viewport(800)
    renderShell()
    const opener = screen.getByRole('button', { name: 'Project navigation' })
    fireEvent.click(opener)
    await screen.findByRole('navigation', { name: 'Project' })

    const close = screen.getByRole('button', { name: 'Close navigation' })
    fireEvent.click(close)
    await waitFor(() => expect(screen.queryByRole('navigation', { name: 'Project' })).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(opener))
  })

  it('returns focus to the opener when Escape closes the drawer', async () => {
    viewport(800)
    renderShell()
    const opener = screen.getByRole('button', { name: 'Project navigation' })
    fireEvent.click(opener)
    await screen.findByRole('navigation', { name: 'Project' })
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await waitFor(() => expect(document.activeElement).toBe(opener))
  })

  it('closes the drawer on every navigation it offers', async () => {
    // The drawer is a **modal** dialog: the page beneath it is `aria-hidden`.
    // A NavLink that navigated and left it standing put the destination behind
    // an overlay the viewer then had to dismiss to see what they asked for —
    // including the case where they were already on that route.
    viewport(800)
    renderShell()
    for (const name of ['Matrix and coverage', 'Author', 'Admin', 'Help & About']) {
      fireEvent.click(screen.getByRole('button', { name: 'Project navigation' }))
      await screen.findByRole('navigation', { name: 'Project' })
      fireEvent.click(screen.getByRole('link', { name }))
      await waitFor(() =>
        expect(screen.queryByRole('navigation', { name: 'Project' })).toBeNull()
      )
      // And the page beneath is back in the accessibility tree.
      expect(screen.getAllByRole('main')).toHaveLength(1)
    }
  })

  it('closes the drawer on the Packs destination too, not only the fixed entries', async () => {
    // The rail's pack rows moved into main's left pane. Packs is one
    // destination now, and it must dismiss the drawer like every other link —
    // in drawer form the rail is a modal dialog over the page it navigated to.
    viewport(800)
    renderShell()
    fireEvent.click(screen.getByRole('button', { name: 'Project navigation' }))
    await screen.findByRole('navigation', { name: 'Project' })
    fireEvent.click(await screen.findByRole('link', { name: /^Packs/ }))
    await waitFor(() => expect(screen.queryByRole('navigation', { name: 'Project' })).toBeNull())
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
    expect(await screen.findByRole('link', { name: /^Packs/ })).toBeTruthy()
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
    // Closed, the drawer's portal is not in the document, so the toggle names
    // nothing rather than naming an id that resolves to nothing.
    expect(toggle.hasAttribute('aria-controls')).toBe(false)
    expect(document.getElementById('desk-inspector')).toBeNull()
    fireEvent.click(toggle)
    await waitFor(() => expect(screen.getByRole('dialog', { name: 'Inspector' })).toBeTruthy())
    expect(toggle.getAttribute('aria-controls')).toBe('desk-inspector')
    expect(document.getElementById('desk-inspector')).toBeTruthy()
    expect(document.getElementById('desk-inspector')!.getAttribute('role')).toBe('dialog')
  })

  it('takes the drawer’s width from the configuration, where the file states one', async () => {
    viewport(1000)
    renderShell(
      { ...DESK_DEFAULTS.panes, inspector: { open: false, width: 420 } },
      { ...NOTHING_DECLARED, inspectorWidth: true }
    )
    fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
    const drawer = await screen.findByRole('dialog', { name: 'Inspector' })
    expect(drawer.style.getPropertyValue('--drawer-w')).toBe('420px')
  })

  it('leaves the drawer on the sheet’s own 320px where the file states none', async () => {
    // The column's default is 360px and the drawer's has always been 320px.
    // Supplying the effective width unconditionally moved every unconfigured
    // desk's drawer to 360px — a behaviour change dressed as applying
    // configuration. `--drawer-w` is written only where a width was stated.
    viewport(1000)
    renderShell()
    fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
    const drawer = await screen.findByRole('dialog', { name: 'Inspector' })
    expect(drawer.style.getPropertyValue('--drawer-w')).toBe('')
    // Radix writes `pointer-events` of its own; what must be absent is the
    // width, so the sheet's `min(var(--drawer-w, 320px), 85vw)` falls back.
    expect(drawer.getAttribute('style') ?? '').not.toContain('--drawer-w')
  })

  it('returns focus to the header toggle when the Inspector drawer closes', async () => {
    viewport(1000)
    renderShell()
    const toggle = screen.getByRole('button', { name: 'Inspector' })
    fireEvent.click(toggle)
    await screen.findByRole('dialog', { name: 'Inspector' })
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await waitFor(() => expect(document.activeElement).toBe(toggle))
  })
})

describe('crossing a breakpoint', () => {
  it('gives an untouched pane back the layout the configuration asked for', async () => {
    // A desk opened narrow clamps the rail to icons and the Inspector closed —
    // correctly. What it must not do is leave them there: the clamp is about
    // the viewport, and the viewport changed. The re-seed used to exclude the
    // viewport deliberately, so an untouched rail stayed 56px wide on a
    // monitor and a configured-open Inspector never opened.
    const wide = viewport(800)
    renderShell({
      left: { mode: 'expanded', width: 248 },
      inspector: { open: true, width: 360 },
      console: { open: false, height: 240 }
    })
    // Narrow: the rail is a drawer and the Inspector is clamped shut.
    expect(screen.queryByRole('navigation', { name: 'Project' })).toBeNull()
    expect(screen.queryByRole('complementary', { name: 'Inspector' })).toBeNull()

    wide.resizeTo(1400)
    await waitFor(() =>
      expect(screen.getByRole('navigation', { name: 'Project' }).dataset.mode).toBe('expanded')
    )
    expect(screen.getByRole('complementary', { name: 'Inspector' })).toBeTruthy()

    // And back again, still untouched.
    wide.resizeTo(800)
    await waitFor(() =>
      expect(screen.queryByRole('complementary', { name: 'Inspector' })).toBeNull()
    )
  })

  it('restores a choice made on an earlier visit when the window widens', async () => {
    // The narrow→wide leg the whole-record defect broke. The record says the
    // Inspector is open; a narrow desk clamps it shut, and widening must give
    // the viewer's own choice back — not the configuration's, and not the
    // built-in default.
    window.localStorage.setItem(
      shellStateKey(projectKey(ROOT)),
      JSON.stringify({ v: 1, left: { mode: 'icons' }, inspector: { open: true } })
    )
    const wide = viewport(800)
    renderShell()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Console' })).toBeTruthy())
    expect(screen.queryByRole('complementary', { name: 'Inspector' })).toBeNull()

    wide.resizeTo(1400)
    await waitFor(() =>
      expect(screen.getByRole('complementary', { name: 'Inspector' })).toBeTruthy()
    )
    expect(screen.getByRole('navigation', { name: 'Project' }).dataset.mode).toBe('icons')
  })

  it('leaves a pane the viewer moved exactly where they left it', async () => {
    const wide = viewport(1400)
    renderShell({
      left: { mode: 'expanded', width: 248 },
      inspector: { open: true, width: 360 },
      console: { open: false, height: 240 }
    })
    // The file asks for an open Inspector; the viewer closes it.
    await waitFor(() => expect(screen.getByRole('complementary', { name: 'Inspector' })).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
    expect(screen.queryByRole('complementary', { name: 'Inspector' })).toBeNull()

    // Narrowing and widening again must not reopen it over their shoulder.
    wide.resizeTo(800)
    wide.resizeTo(1400)
    await waitFor(() =>
      expect(screen.getByRole('navigation', { name: 'Project' }).dataset.mode).toBe('expanded')
    )
    expect(screen.queryByRole('complementary', { name: 'Inspector' })).toBeNull()
  })
})
