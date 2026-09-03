/**
 * The desk reading its own configuration, end to end through the file API.
 *
 * **Why this test exists rather than a live check.** A running chassis can be
 * curled, and doing so proves the two halves that live on the wire: that
 * `GET /api/file?path=jpack-desk.json` answers with the file's bytes, and that
 * every route — `/admin` and `/help` included — is served the SPA. What curl
 * cannot see is the third half: the organization name is painted by React
 * *after* the query resolves, so it is in the JavaScript bundle and never in
 * the HTML the server sends. The served `index.html` carries `<div id="root">`
 * and nothing else. So the read is driven here instead, against a stub of the
 * same endpoint, answering the same body the live chassis answered.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { IdentityProvider } from '../identity/IdentityProvider'
import { McpContext } from '../mcp/McpProvider'
import { AppShell } from '../shell/AppShell'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { DeskConfigProvider } from './DeskConfigProvider'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.localStorage.clear()
  document.documentElement.removeAttribute('data-theme')
})

/** Past the pane record's write debounce, in real time. */
const PAST_THE_DEBOUNCE = 400

async function wait(ms: number) {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, ms))
  })
}

/** Every shell record this browser holds, as `[key, value]`. */
function shellRecords(): [string, string][] {
  return Object.keys(window.localStorage)
    .filter((key) => key.startsWith('jpack-desk:shell:'))
    .map((key) => [key, window.localStorage.getItem(key) ?? ''])
}

/**
 * Exactly what the chassis answered on 2026-09-02, running against a copy of
 * the runtime's own graph fixture project with this file beside it. The digest
 * is the chassis' own.
 */
const LIVE_ANSWER = {
  path: 'jpack-desk.json',
  bytes: 361,
  sha256: '9e4dc5f489e01f96c62babdf62451a7b22628d59316a8f6f494baa70e6a94a6c',
  content: `{
  "deskConfigVersion": 1,
  "organization": { "name": "Acme Co.", "mark": null },
  "user": { "displayName": "desk operator" },
  "appearance": { "theme": "system", "density": "comfortable" },
  "panes": {
    "left": { "mode": "expanded", "width": 248 },
    "inspector": { "open": false, "width": 360 },
    "console": { "open": true, "height": 240 }
  }
}
`
}

const PROJECT_ROOT = '/home/someone/a-project'

const PROJECT = stubClient({
  list_packs: () => ({
    text: JSON.stringify({ status: 'valid', configPath: '/p/jpack.json', packs: [] })
  })
})

/**
 * The file API, answering the one path the shell asks for and nothing else.
 *
 * `delayMs` is not a flourish: the configuration is read over the network and
 * `list_packs` is not, so which of the two answers first is a race the shell
 * has to survive in both orders. A stub that always resolves in a microtask
 * tests only the order that happens to be convenient.
 */
function serveConfig(answer: unknown, status = 200, delayMs = 0) {
  const asked: string[] = []
  vi.stubGlobal('fetch', async (url: string) => {
    const path = String(url)
    asked.push(path)
    // The listing is answered separately and immediately: its `root` is the
    // project identity the pane record is keyed on, and it is not the thing
    // any case here is delaying or refusing.
    if (path.includes('/api/files')) {
      return {
        ok: true,
        status: 200,
        statusText: '',
        text: async () => JSON.stringify({ root: PROJECT_ROOT, files: [] })
      }
    }
    if (delayMs > 0) await new Promise((resolve) => setTimeout(resolve, delayMs))
    return {
      ok: status >= 200 && status < 300,
      status,
      statusText: '',
      text: async () => JSON.stringify(answer)
    }
  })
  return asked
}

function renderDesk() {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: PROJECT.client })}>
            {/* The nesting `main.tsx` uses: the configuration feeds both the
                identity slot and the pane defaults, so it is outermost. */}
            <DeskConfigProvider>
              <IdentityProvider>
                <AppShell>
                  <h1>a route</h1>
                </AppShell>
              </IdentityProvider>
            </DeskConfigProvider>
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

describe('the desk reading jpack-desk.json', () => {
  it('asks for exactly that path, through the file API it already had', async () => {
    const asked = serveConfig(LIVE_ANSWER)
    renderDesk()
    await waitFor(() => expect(asked.length).toBeGreaterThan(0))
    expect(asked.some((url) => url.includes('/api/file') && url.includes('path=jpack-desk.json'))).toBe(
      true
    )
    // No new endpoint: nothing asks for a desk-config route, and the only
    // other thing the shell reads is the listing it already shared with
    // `/author` and the Create dialog.
    expect(asked.some((url) => url.includes('desk-config'))).toBe(false)
    expect(
      asked.every((url) => url.includes('/api/file?') || url.includes('/api/files?'))
    ).toBe(true)
  })

  it('honours a late `panes` block for every pane the viewer has not moved', async () => {
    // One global touched bit made a single toggle suppress the whole re-seed:
    // collapsing the Console before the file was read froze the rail and the
    // Inspector on the built-in defaults, and then wrote both into a record
    // that outranks the file for ever after.
    serveConfig(
      {
        ...LIVE_ANSWER,
        content: JSON.stringify({
          deskConfigVersion: 1,
          panes: {
            left: { mode: 'icons', width: 248 },
            inspector: { open: true, width: 360 },
            console: { open: true, height: 240 }
          }
        })
      },
      200,
      300
    )
    renderDesk()
    // The viewer opens the Console before the file arrives, and closes it.
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))

    // The file's rail and Inspector still land; the Console stays where the
    // viewer left it.
    await waitFor(() =>
      expect(screen.getByRole('navigation', { name: 'Project' }).dataset.mode).toBe('icons')
    )
    expect(screen.getByRole('complementary', { name: 'Inspector' })).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Console' })).toBeNull()

    // And only the Console is written down.
    await waitFor(() => expect(shellRecords()).toHaveLength(1))
    expect(JSON.parse(shellRecords()[0]![1])).toEqual({
      v: 1,
      console: { open: false, tab: 'connection' }
    })
  })

  it('applies the configured pane sizes to the grid, rather than three built-in numbers', async () => {
    serveConfig({
      ...LIVE_ANSWER,
      content: JSON.stringify({
        deskConfigVersion: 1,
        panes: {
          left: { mode: 'expanded', width: 300 },
          inspector: { open: false, width: 420 },
          console: { open: false, height: 180 }
        }
      })
    })
    const { container } = renderDesk()
    const desk = container.querySelector('.desk') as HTMLElement
    await waitFor(() => expect(desk.style.getPropertyValue('--rail-w')).toBe('300px'))
    expect(desk.style.getPropertyValue('--inspector-w')).toBe('420px')
    expect(desk.style.getPropertyValue('--console-h')).toBe('180px')
    // And collapse still writes one of the two values, not a third number.
    expect(desk.style.getPropertyValue('--rail-current')).toBe('var(--rail-w)')
  })

  it('paints the configured organization name in the header', async () => {
    serveConfig(LIVE_ANSWER)
    renderDesk()
    // Before the query answers, the desk's own fallback stands.
    expect(screen.getByRole('link', { name: 'judgment‑pack desk' })).toBeTruthy()
    expect(await screen.findByRole('link', { name: 'Acme Co.' })).toBeTruthy()
  })

  it('paints the configured display name in the user control', async () => {
    serveConfig(LIVE_ANSWER)
    renderDesk()
    expect(await screen.findByText('desk operator')).toBeTruthy()
    expect(screen.getByText('local')).toBeTruthy()
  })

  it('opens the console because the file said so', async () => {
    serveConfig(LIVE_ANSWER)
    renderDesk()
    await waitFor(() => expect(screen.getByRole('region', { name: 'Console' })).toBeTruthy())
  })

  it('does not re-seed over a pane the viewer has already moved', async () => {
    // The seed is re-taken because both of its inputs arrive after the first
    // paint. That is also the risk: a viewer who collapses the console in the
    // half-second before the file is read must not have it reopened over their
    // shoulder.
    serveConfig(LIVE_ANSWER)
    renderDesk()
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))
    expect(screen.getByRole('region', { name: 'Console' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))
    // The file says the console is open; the viewer has said otherwise.
    await screen.findByRole('link', { name: 'Acme Co.' })
    expect(screen.queryByRole('region', { name: 'Console' })).toBeNull()
  })

  it('opens the console because the file said so, even when the read is slow', async () => {
    // The ordering that used to lose. `list_packs` answers immediately and the
    // file read does not, so the shell's own debounced write landed first — and
    // a stored record is preferred over the configured one, so the file was
    // shadowed by a layout the shell had written to itself.
    serveConfig(LIVE_ANSWER, 200, 600)
    renderDesk()
    await wait(PAST_THE_DEBOUNCE)
    expect(screen.queryByRole('region', { name: 'Console' })).toBeNull()
    await waitFor(
      () => expect(screen.getByRole('region', { name: 'Console' })).toBeTruthy(),
      { timeout: 2000 }
    )
  })

  it('writes no record at all for a layout nobody chose', async () => {
    // Every visit used to store the built-in defaults 250ms after mount. That
    // record then beat the config on the next visit, which is how a `panes`
    // block could be permanently inert on a browser that had opened the desk
    // once before the file existed.
    serveConfig({ error: 'no such file' }, 404)
    renderDesk()
    await screen.findByRole('link', { name: 'judgment‑pack desk' })
    await wait(PAST_THE_DEBOUNCE)
    expect(shellRecords()).toEqual([])
  })

  it('honours a file written after a visit that chose nothing', async () => {
    // Visit one: no file, and nothing chosen. Visit two: the file says the
    // console is open. Deterministic — no timing at all.
    serveConfig({ error: 'no such file' }, 404)
    const first = renderDesk()
    await screen.findByRole('link', { name: 'judgment‑pack desk' })
    await wait(PAST_THE_DEBOUNCE)
    first.unmount()

    vi.unstubAllGlobals()
    serveConfig(LIVE_ANSWER)
    renderDesk()
    await waitFor(() => expect(screen.getByRole('region', { name: 'Console' })).toBeTruthy())
  })

  it('still remembers a layout the viewer chose', async () => {
    // The other half of the same rule: the gate is on *choice*, not on writes.
    serveConfig({ error: 'no such file' }, 404)
    const first = renderDesk()
    await screen.findByRole('link', { name: 'judgment‑pack desk' })
    fireEvent.click(screen.getByRole('button', { name: 'Console' }))
    await waitFor(() => expect(shellRecords()).toHaveLength(1))
    expect(shellRecords()[0]![1]).toContain('"open":true')
    first.unmount()

    renderDesk()
    await waitFor(() => expect(screen.getByRole('region', { name: 'Console' })).toBeTruthy())
  })

  it('applies the configured theme to the root element, and takes it off for system', async () => {
    serveConfig({
      ...LIVE_ANSWER,
      content: JSON.stringify({ deskConfigVersion: 1, appearance: { theme: 'dark', density: 'comfortable' } })
    })
    const dark = renderDesk()
    await waitFor(() => expect(document.documentElement.getAttribute('data-theme')).toBe('dark'))
    dark.unmount()

    vi.unstubAllGlobals()
    serveConfig(LIVE_ANSWER)
    renderDesk()
    // LIVE_ANSWER asks for `system`, which is the absence of the attribute —
    // `prefers-color-scheme` answers instead.
    await screen.findByRole('link', { name: 'Acme Co.' })
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
  })

  it('says the file was refused somewhere other than Admin', async () => {
    // A refused file is the built-in defaults, which is right — but that made
    // it indistinguishable from having no file at all from every surface
    // except /admin. The strip is always visible and always says which it is.
    serveConfig({
      ...LIVE_ANSWER,
      content: JSON.stringify({ deskConfigVersion: 1, organization: { name: 'Acme Co.' }, colour: 'blue' })
    })
    renderDesk()
    const cue = await screen.findByRole('link', { name: 'configuration refused — see Admin' })
    expect(cue.getAttribute('href')).toBe('/admin')
    expect(screen.getByRole('contentinfo').textContent).toContain('configuration refused')
  })

  it('says a configuration it could not read is unread, not absent', async () => {
    // Absence is the ordinary case and stays silent. A 413, a permission
    // refusal or a socket that never answered is a file that exists and was
    // not honoured, and until this cue the two were the same silence.
    serveConfig({ error: 'the file is too large to read' }, 413)
    renderDesk()
    const cue = await screen.findByRole('link', {
      name: 'configuration could not be read — see Admin'
    })
    expect(cue.getAttribute('href')).toBe('/admin')
    expect(screen.queryByText(/configuration refused/)).toBeNull()
  })

  it('keeps the cue’s full sentence as its accessible name, and a short one on the face', async () => {
    // The link neither shrinks nor wraps, deliberately — and the full sentence
    // is wider than a 320px strip has left beside the console button. Both
    // spellings are in the DOM and CSS paints one; the name is on the link, so
    // it is the full sentence at every width.
    serveConfig({ error: 'the file is too large to read' }, 413)
    renderDesk()
    const cue = await screen.findByRole('link', {
      name: 'configuration could not be read — see Admin'
    })
    expect(cue.querySelector('.desk-strip-warn-full')!.textContent).toBe(
      'configuration could not be read — see Admin'
    )
    expect(cue.querySelector('.desk-strip-warn-short')!.textContent).toBe('config unread')
    // Neither span may reach the accessible name, or it would read twice.
    for (const span of cue.querySelectorAll('span')) {
      expect(span.getAttribute('aria-hidden')).toBe('true')
    }
  })

  it('says nothing about the configuration where the file is simply absent', async () => {
    serveConfig({ error: 'no such file' }, 404)
    renderDesk()
    await screen.findByRole('link', { name: 'judgment‑pack desk' })
    expect(screen.queryByText(/configuration could not be read/)).toBeNull()
    expect(screen.queryByText(/configuration refused/)).toBeNull()
  })

  it('says nothing about the configuration where there is no problem with it', async () => {
    serveConfig(LIVE_ANSWER)
    renderDesk()
    await screen.findByRole('link', { name: 'Acme Co.' })
    expect(screen.queryByText(/configuration refused/)).toBeNull()
  })

  it('keeps the desk’s own name where the file is absent', async () => {
    serveConfig({ error: 'no such file' }, 404)
    renderDesk()
    await waitFor(() =>
      expect(screen.getByRole('link', { name: 'judgment‑pack desk' })).toBeTruthy()
    )
    // Defaults, with no banner and no error over the desk.
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByRole('region', { name: 'Console' })).toBeNull()
  })

  it('keeps the desk’s own name where the file is refused', async () => {
    serveConfig({
      ...LIVE_ANSWER,
      content: JSON.stringify({ deskConfigVersion: 1, organization: { name: 'Acme Co.' }, colour: 'blue' })
    })
    renderDesk()
    await waitFor(() =>
      expect(screen.getByRole('link', { name: 'judgment‑pack desk' })).toBeTruthy()
    )
    // One unknown key refuses the whole file, so the name it also carried is
    // not applied — and the problem travels to Admin rather than a banner.
    expect(screen.queryByRole('link', { name: 'Acme Co.' })).toBeNull()
  })
})
