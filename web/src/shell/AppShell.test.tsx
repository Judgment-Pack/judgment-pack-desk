/**
 * The frame, as a page: its landmarks, its defaults, and the one property the
 * whole arrangement rests on — that `<main>` never remounts.
 *
 * A closed pane is **absent from the accessibility tree**, not merely
 * invisible, which is why the landmark case opens both panes before counting.
 * That is the behaviour the shell wants: a viewer who has collapsed the
 * Inspector should not be able to tab into it, and a screen reader should not
 * be offered a region that is not there.
 */
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AppShell } from './AppShell'
import { forgetConsole } from './consoleLog'
import { forgetAuthorBridge } from './authorBridge'
import { projectKey, shellStateKey } from './paneState'

/** The chassis' project root, which is what keys this desk's pane record. */
const ROOT = '/home/someone/a-project'

/**
 * The file API, answering the two things the shell asks it for: the listing,
 * whose `root` is the project identity, and `jpack-desk.json`, which is absent
 * here. Without the first the key stays provisional and nothing is written
 * under it — which is the behaviour, not a limitation of the stub.
 */
beforeEach(() => {
  vi.stubGlobal('fetch', async (url: string) => {
    const path = String(url)
    if (path.includes('/api/files')) {
      return {
        ok: true,
        status: 200,
        statusText: '',
        text: async () => JSON.stringify({ root: ROOT, files: [] })
      }
    }
    return {
      ok: false,
      status: 404,
      statusText: '',
      text: async () => JSON.stringify({ error: 'no such file' })
    }
  })
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
    text: JSON.stringify({
      status: 'valid',
      configPath: '/p/jpack.json',
      packs: [{ id: 'intake-triage', matrix: true }]
    })
  })
})

function renderShell(
  ui: React.ReactNode,
  overrides: Partial<McpConnection> = {},
  path = '/'
) {
  const value = connected({ client: PROJECT.client, ...overrides })
  const router = createMemoryRouter(
    [{ path: '*', element: <McpContext.Provider value={value}>{ui}</McpContext.Provider> }],
    { initialEntries: [path] }
  )
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('the shell frame', () => {
  it('renders the six landmarks exactly once, each with its name', async () => {
    renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    // Both collapsible panes are opened first: closed is `hidden`, and a
    // hidden region is correctly absent from the accessibility tree.
    fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))

    expect(screen.getAllByRole('banner')).toHaveLength(1)
    expect(screen.getAllByRole('navigation', { name: 'Project' })).toHaveLength(1)
    expect(screen.getAllByRole('main')).toHaveLength(1)
    expect(screen.getAllByRole('complementary', { name: 'Inspector' })).toHaveLength(1)
    expect(screen.getAllByRole('region', { name: 'Console' })).toHaveLength(1)
    expect(screen.getAllByRole('contentinfo')).toHaveLength(1)
  })

  it('puts the skip link first and points it at main', () => {
    const { container } = renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    const first = container.querySelector('a,button,input,textarea,select,[tabindex]')!
    expect(first.textContent).toBe('Skip to main content')
    expect(first.getAttribute('href')).toBe('#main')
    expect(screen.getByRole('main').id).toBe('main')
    expect(screen.getByRole('main').getAttribute('tabindex')).toBe('-1')
  })

  it('opens with the rail expanded, the inspector closed and the console collapsed', () => {
    renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    expect(screen.getByRole('navigation', { name: 'Project' }).dataset.mode).toBe('expanded')
    expect(screen.queryByRole('complementary', { name: 'Inspector' })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Console' })).toBeNull()
    // The strip is the console's collapsed face and is always there.
    expect(screen.getByRole('contentinfo')).toBeTruthy()
  })

  it('leaves the closed inspector contributing no tabbable element', () => {
    const { container } = renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    const aside = container.querySelector('aside[aria-label="Inspector"]')!
    expect(aside.hasAttribute('hidden')).toBe(true)
    const tabbable = aside.querySelectorAll(
      'a[href],button,input,textarea,select,[tabindex]:not([tabindex="-1"])'
    )
    // `hidden` alone is beaten by an authored `display`, which is why the
    // shell sheet carries `[hidden] { display: none !important }` beside it.
    // What this asserts is the half a test can see: nothing inside is offered.
    expect(Array.from(tabbable).every((element) => element.closest('[hidden]') !== null)).toBe(true)
  })

  it('carries the strip’s two sentences verbatim', async () => {
    const { unmount } = renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    expect(screen.getByRole('contentinfo').textContent).toContain('connected to jpack test')
    unmount()
    renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>,
      { server: null }
    )
    expect(screen.getByRole('contentinfo').textContent).toContain('not connected')
  })

  it('renders the strip identically whether the console is open or collapsed', () => {
    renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    const collapsed = screen.getByRole('contentinfo').textContent
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))
    expect(screen.getByRole('contentinfo').textContent).toBe(collapsed)
  })

  it('keeps a buffer typed into main across a pane change and a route change', async () => {
    // The property everything else rests on. `AuthorView` holds an unsaved
    // buffer in component state; a frame that remounted main on a layout
    // change would throw it away for a collapsed pane.
    renderShell(
      <AppShell>
        <textarea aria-label="the buffer" defaultValue="" />
      </AppShell>
    )
    const buffer = screen.getByLabelText('the buffer') as HTMLTextAreaElement
    fireEvent.change(buffer, { target: { value: 'unsaved work' } })

    fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))
    fireEvent.keyDown(document, { key: 'b', ctrlKey: true })

    expect((screen.getByLabelText('the buffer') as HTMLTextAreaElement).value).toBe('unsaved work')
    await waitFor(() =>
      expect(screen.getByRole('navigation', { name: 'Project' }).dataset.mode).toBe('icons')
    )
    expect((screen.getByLabelText('the buffer') as HTMLTextAreaElement).value).toBe('unsaved work')
  })

  it('persists the layout under this project’s own key, and restores it', async () => {
    const { unmount } = renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))
    await waitFor(() => {
      const keys = Object.keys(window.localStorage).filter((key) =>
        key.startsWith('jpack-desk:shell:v1:')
      )
      // The chassis' root, not the runtime's config path: two projects with no
      // `jpack.json` used to share the single literal `default` record.
      expect(keys).toEqual([shellStateKey(projectKey(ROOT))])
      expect(window.localStorage.getItem(keys[0]!)).toContain('"open":true')
    })
    unmount()

    renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    await waitFor(() => expect(screen.getByRole('region', { name: 'Console' })).toBeTruthy())
  })

  it('writes nothing at all while the project identity is provisional', async () => {
    // The listing never answers, so the key is the literal `default` — which
    // is not this project's record and is whichever project answers slowly
    // next. A layout written there is one desk's choice stored under another
    // desk's name.
    vi.stubGlobal('fetch', () => new Promise(() => {}))
    renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))
    await new Promise((resolve) => setTimeout(resolve, 400))
    expect(
      Object.keys(window.localStorage).filter((key) => key.startsWith('jpack-desk:shell:'))
    ).toEqual([])
  })

  it('keeps a rail and Inspector chosen on an earlier visit through a console-only write', async () => {
    // The whole-record defect, end to end. With `left` and `inspector` already
    // on disk, a Console toggle rewrote the key as `{"v":1,"console":…}` — so
    // the choices survived the visit that made them and nothing after it.
    window.localStorage.setItem(
      shellStateKey(projectKey(ROOT)),
      JSON.stringify({ v: 1, left: { mode: 'icons' }, inspector: { open: true } })
    )
    const first = renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    await waitFor(() =>
      expect(screen.getByRole('navigation', { name: 'Project' }).dataset.mode).toBe('icons')
    )
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))
    await waitFor(() => {
      const stored = window.localStorage.getItem(shellStateKey(projectKey(ROOT)))
      expect(JSON.parse(stored!)).toEqual({
        v: 1,
        left: { mode: 'icons' },
        inspector: { open: true },
        console: { open: true, tab: 'connection' }
      })
    })
    first.unmount()

    // And the next visit restores all three, which is the point of keeping them.
    renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    await waitFor(() =>
      expect(screen.getByRole('navigation', { name: 'Project' }).dataset.mode).toBe('icons')
    )
    expect(screen.getByRole('complementary', { name: 'Inspector' })).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Console' })).toBeTruthy()
  })

  it('stores only the pane the viewer moved, not the two they did not', async () => {
    // One global touched bit made a single toggle speak for all three: the
    // rail's mode and the Inspector's flag were serialized from the built-in
    // defaults, and a stored record outranks `panes` in the configuration file
    // for ever after.
    renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))
    await waitFor(() => {
      const stored = window.localStorage.getItem(shellStateKey(projectKey(ROOT)))
      expect(stored).not.toBeNull()
      expect(JSON.parse(stored!)).toEqual({ v: 1, console: { open: true, tab: 'connection' } })
    })
  })
})
