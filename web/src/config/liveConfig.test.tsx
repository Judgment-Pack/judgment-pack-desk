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
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
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
})

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

const PROJECT = stubClient({
  list_packs: () => ({
    text: JSON.stringify({ status: 'valid', configPath: '/p/jpack.json', packs: [] })
  })
})

/** The file API, answering the one path the shell asks for and nothing else. */
function serveConfig(answer: unknown, status = 200) {
  const asked: string[] = []
  vi.stubGlobal('fetch', async (url: string) => {
    asked.push(String(url))
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
    // No new endpoint: nothing asks for a desk-config route.
    expect(asked.some((url) => url.includes('desk-config'))).toBe(false)
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
