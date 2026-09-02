/**
 * Admin: six headed sections, a sentence carried verbatim, and exactly one
 * interactive control.
 *
 * The last of those is the assertion worth having. A read-only page that grew
 * a control would be a page that writes configuration, which is a decision
 * nobody has taken — and the one control it does have clears a single
 * `localStorage` key, not the origin's storage.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig } from '../config/deskConfig'
import { McpContext } from '../mcp/McpProvider'
import { ShellStateProvider } from '../shell/paneState'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AdminView } from './AdminView'
import { ADMIN_DISCLAIMER, ADMIN_SECTIONS } from './adminSections'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.localStorage.clear()
})

const QUIET = stubClient({ list_packs: () => ({ text: JSON.stringify({ packs: [] }) }) })

function renderAdmin(value = effectiveConfig(undefined), path = '/admin') {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: QUIET.client })}>
            <DeskConfigFixture value={value}>
              <ShellStateProvider
                projectKey="a-project"
                viewport={{ railIsDrawer: false, inspectorIsDrawer: false }}
              >
                <AdminView />
              </ShellStateProvider>
            </DeskConfigFixture>
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: [path] }
  )
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('the Admin page', () => {
  it('renders the six sections in order, with those exact headings', () => {
    renderAdmin()
    const headings = screen
      .getAllByRole('heading', { level: 2 })
      .map((heading) => heading.textContent)
    expect(headings.slice(0, 6)).toEqual(ADMIN_SECTIONS.map((section) => section.title))
  })

  it('carries the standing disclaimer character for character', () => {
    renderAdmin()
    expect(screen.getByText(ADMIN_DISCLAIMER)).toBeTruthy()
    expect(ADMIN_DISCLAIMER).toContain('defines no accounts and no roles')
    expect(ADMIN_DISCLAIMER).toContain('the loopback bind, the session token this tab holds')
  })

  it('names no user management, roles, invitations or assignment anywhere', () => {
    const { container } = renderAdmin()
    const text = container.textContent ?? ''
    for (const absent of ['Invite', 'Add user', 'Assign', 'Members', 'Permissions']) {
      expect(text).not.toContain(absent)
    }
  })

  it('has exactly one interactive control, and it is the pane reset', () => {
    const { container } = renderAdmin()
    const interactive = container.querySelectorAll('button, input, select, textarea')
    const labels = Array.from(interactive).map((element) => element.textContent?.trim())
    // The copy buttons are the only others, and they copy rather than change.
    expect(labels.filter((label) => label === 'Reset panes on this machine')).toHaveLength(1)
    expect(container.querySelectorAll('input, select, textarea')).toHaveLength(0)
    expect(container.querySelectorAll('[disabled]')).toHaveLength(0)
  })

  it('clears exactly one localStorage key when the reset is pressed', () => {
    window.localStorage.setItem('jpack-desk:shell:v1:a-project', '{"v":1}')
    window.localStorage.setItem('jpack-desk:shell:v1:another', '{"v":1}')
    window.localStorage.setItem('jpack-desk-token', 'a token')
    renderAdmin()
    fireEvent.click(screen.getByRole('button', { name: 'Reset panes on this machine' }))
    expect(window.localStorage.getItem('jpack-desk:shell:v1:a-project')).toBeNull()
    expect(window.localStorage.getItem('jpack-desk:shell:v1:another')).toBe('{"v":1}')
    expect(window.localStorage.getItem('jpack-desk-token')).toBe('a token')
  })

  it('reports a refused configuration by naming every problem, and stays on defaults', () => {
    const value = effectiveConfig({
      values: undefined,
      problems: [{ key: 'colour', reason: 'unknown key' }]
    })
    renderAdmin(value)
    expect(screen.getByText(/was refused, and the desk is on its defaults/)).toBeTruthy()
    expect(screen.getByText('colour: unknown key')).toBeTruthy()
  })

  it('says the desk-level file is not read yet, and names the open question', () => {
    renderAdmin()
    expect(screen.getByText(/desk-level desk.json/)).toBeTruthy()
    expect(screen.getByText(/open question 2/)).toBeTruthy()
  })

  it('states that identity gates nothing', () => {
    renderAdmin()
    expect(screen.getByText(/Configuring a provider gates nothing/)).toBeTruthy()
  })

  it('scrolls to the section a fragment names', () => {
    // The rail's Admin menu and the user menu both link to `/admin#…`. The
    // router does no fragment scrolling, and the document is not the scroll
    // container here — `.desk-main` is — so without the hook those links
    // changed the address bar and moved nothing.
    const scrolled: string[] = []
    const original = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = function scrollIntoView(this: Element) {
      scrolled.push(this.id)
    }
    try {
      renderAdmin(effectiveConfig(undefined), '/admin#panes')
      expect(scrolled).toContain('panes')
    } finally {
      Element.prototype.scrollIntoView = original
    }
  })

  it('says the theme is applied and the density is not, rather than claiming both', () => {
    renderAdmin()
    expect(screen.getByText(/values are the light ones/)).toBeTruthy()
    expect(screen.getByText(/read by nothing yet/)).toBeTruthy()
  })

  it('reports a copy that did not happen as one that did not happen', async () => {
    // `navigator.clipboard` is absent in an insecure context and `writeText`
    // can be refused; a page that says "copied" on the strength of having
    // asked is stating what it did not observe.
    vi.stubGlobal('navigator', { clipboard: { writeText: async () => Promise.reject(new Error('no')) } })
    renderAdmin()
    fireEvent.click(screen.getAllByRole('button', { name: /Copy/ })[0]!)
    expect(await screen.findByText(/did not allow the copy/)).toBeTruthy()
    expect(screen.queryByText('copied')).toBeNull()
  })

  it('says copied only where the clipboard took it', async () => {
    const written: string[] = []
    vi.stubGlobal('navigator', {
      clipboard: { writeText: async (text: string) => void written.push(text) }
    })
    renderAdmin()
    fireEvent.click(screen.getAllByRole('button', { name: /Copy/ })[0]!)
    await waitFor(() => expect(screen.getByText('copied')).toBeTruthy())
    expect(written[0]).toContain('deskConfigVersion')
  })
})
