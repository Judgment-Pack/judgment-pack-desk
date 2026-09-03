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
import { decodeDeskConfig, effectiveConfig } from '../config/deskConfig'
import { McpContext } from '../mcp/McpProvider'
import { ShellStateProvider, projectKey, shellStateKey } from '../shell/paneState'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AdminView } from './AdminView'
import { ADMIN_DISCLAIMER, ADMIN_SECTIONS } from './adminSections'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.localStorage.clear()
})

const QUIET = stubClient({ list_packs: () => ({ text: JSON.stringify({ packs: [] }) }) })

/** The chassis' project root, and the key the record therefore lives under. */
const ROOT = '/home/someone/a-project'
const KEY = shellStateKey(projectKey(ROOT))

/**
 * `null` means "the chassis has not answered yet". Not `undefined`: a default
 * parameter takes over for an explicit `undefined`, so the provisional case
 * silently got the resolved root and asserted nothing.
 */
function renderAdmin(
  value = effectiveConfig(undefined),
  path = '/admin',
  projectIdentity: string | null = ROOT
) {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: QUIET.client })}>
            <DeskConfigFixture value={value}>
              <ShellStateProvider
                projectIdentity={projectIdentity ?? undefined}
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
  it('renders every section in order, with those exact headings and no others', () => {
    // The whole list, not a slice of it. `slice(0, 6)` said nothing about a
    // seventh section and nothing about a heading nobody declared; this fails
    // if a section is added without being declared, declared without being
    // rendered, or rendered out of order.
    renderAdmin()
    const headings = screen
      .getAllByRole('heading', { level: 2 })
      .map((heading) => heading.textContent)
    expect(headings).toEqual([
      ...ADMIN_SECTIONS.map((section) => section.title),
      // The paste block at the foot, which is not a section of the page.
      'The whole file'
    ])
  })

  it('names the storage kind, the location, the id prefix and where it read them', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({
        deskConfigVersion: 1,
        storage: { packs: { dir: 'decisions', idBase: 'https://acme.example/d' } }
      }),
      'project'
    )
    renderAdmin(effectiveConfig(decoded))
    expect(screen.getByText('filesystem')).toBeTruthy()
    expect(screen.getByText('decisions')).toBeTruthy()
    // The prefix as it will actually be written — normalised at decode.
    expect(screen.getByText('https://acme.example/d/')).toBeTruthy()
    expect(screen.getAllByText(/source: project file/).length).toBeGreaterThan(0)
  })

  it('shows the built-in location and prefix where the project configured none', () => {
    renderAdmin()
    expect(screen.getByText('packs')).toBeTruthy()
    expect(screen.getByText('https://example.invalid/judgment-packs/')).toBeTruthy()
  })

  it('says a location holds files only where the listing shows one', async () => {
    // The listing reports regular files only, so the page can say a location
    // holds files and can never claim an empty one exists.
    vi.stubGlobal('fetch', async () => ({
      ok: true,
      status: 200,
      statusText: '',
      text: async () =>
        JSON.stringify({
          root: '/p',
          files: [{ path: 'packs/a.pack.json', bytes: 1, sha256: 'aa' }]
        })
    }))
    renderAdmin()
    expect(await screen.findByText('holds files')).toBeTruthy()
  })

  it('says the location is not there yet where the listing shows nothing under it', async () => {
    vi.stubGlobal('fetch', async () => ({
      ok: true,
      status: 200,
      statusText: '',
      text: async () =>
        JSON.stringify({ root: '/p', files: [{ path: 'jpack.json', bytes: 1, sha256: 'aa' }] })
    }))
    renderAdmin()
    expect(
      await screen.findByText('no file under it yet — the first pack creates it')
    ).toBeTruthy()
  })

  it('names the two future kinds as coming soon, as text and not as controls', () => {
    renderAdmin()
    expect(screen.getByText('database — coming soon')).toBeTruthy()
    expect(screen.getByText('cloud storage — coming soon')).toBeTruthy()
    // Neither is a control, and neither is a disabled one — which the page's
    // "exactly one interactive control" case is what actually proves.
    expect(screen.queryByRole('option', { name: /database/ })).toBeNull()
    expect(screen.queryByRole('radio')).toBeNull()
  })

  it('says the future kinds change nothing about creating a pack', () => {
    renderAdmin()
    expect(screen.getByText(/not available yet/)).toBeTruthy()
    expect(screen.getByText(/creates a pack by writing a file, always/)).toBeTruthy()
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

  it('has exactly one state-changing control, and it is the pane reset', () => {
    // Not "one interactive control": the page also carries a Copy button
    // beside every paste block, and calling those nothing was a claim the
    // page's own markup contradicted. What holds is the narrower statement —
    // the reset is the only control that changes anything.
    const { container } = renderAdmin()
    const interactive = container.querySelectorAll('button, input, select, textarea')
    const labels = Array.from(interactive).map((element) => element.textContent?.trim())
    expect(labels.filter((label) => label === 'Reset panes on this machine')).toHaveLength(1)
    // Every other control on the page is a Copy, and a copy changes nothing here.
    const others = labels.filter((label) => label !== 'Reset panes on this machine')
    expect(others.length).toBeGreaterThan(0)
    expect(others.every((label) => label?.includes('Copy'))).toBe(true)
    expect(container.querySelectorAll('input, select, textarea')).toHaveLength(0)
    expect(container.querySelectorAll('[disabled]')).toHaveLength(0)
  })

  it('clears exactly one localStorage key when the reset is pressed, and says so', () => {
    window.localStorage.setItem(KEY, '{"v":1}')
    window.localStorage.setItem('jpack-desk:shell:v1:another', '{"v":1}')
    window.localStorage.setItem('jpack-desk-token', 'a token')
    renderAdmin()
    fireEvent.click(screen.getByRole('button', { name: 'Reset panes on this machine' }))
    expect(window.localStorage.getItem(KEY)).toBeNull()
    expect(window.localStorage.getItem('jpack-desk:shell:v1:another')).toBe('{"v":1}')
    expect(window.localStorage.getItem('jpack-desk-token')).toBe('a token')
    expect(screen.getByText(/Cleared, and the panes are back/)).toBeTruthy()
  })

  it('reports a reset it could not make, rather than reporting one it did', () => {
    // A storage that refuses the deletion answered "Cleared." before. The page
    // reads the key back and says what it found.
    const backing = new Map<string, string>([[KEY, '{"v":1}']])
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => backing.get(key) ?? null,
      setItem: (key: string, value: string) => void backing.set(key, value),
      removeItem: () => {},
      clear: () => {}
    })
    renderAdmin()
    fireEvent.click(screen.getByRole('button', { name: 'Reset panes on this machine' }))
    expect(screen.getByText(/did not clear the record/)).toBeTruthy()
    expect(screen.queryByText(/Cleared, and the panes/)).toBeNull()
  })

  it('refuses to reset a provisional key, and says which key it is', () => {
    // Before the chassis reports the project root the key is the literal
    // `default`, which is not this project's record — clearing it would delete
    // whatever else had been written there and report success for a project
    // whose layout is untouched.
    window.localStorage.setItem(shellStateKey('default'), '{"v":1}')
    renderAdmin(effectiveConfig(undefined), '/admin', null)
    expect(screen.getByText(/provisional, because the chassis/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Reset panes on this machine' }))
    expect(screen.getByText(/has not yet been told which project/)).toBeTruthy()
    expect(window.localStorage.getItem(shellStateKey('default'))).toBe('{"v":1}')
  })

  it('names a configuration that could not be read, and does not call it absent', () => {
    // A 413, a permission error or a dead socket resolved to the defaults with
    // the reason recorded where nothing rendered it, so a desk that could not
    // open its own file looked exactly like a desk with no file.
    renderAdmin(effectiveConfig(undefined, undefined, 'the file is too large to read'))
    expect(screen.getByText(/could not be read, and the desk is on its defaults/)).toBeTruthy()
    expect(screen.getByText('the file is too large to read')).toBeTruthy()
  })

  it('says why no file was read where the file is simply absent', () => {
    renderAdmin(effectiveConfig(undefined, 'no configuration was read: no such file'))
    expect(screen.getByText('no configuration was read: no such file')).toBeTruthy()
    expect(screen.queryByText(/could not be read, and the desk/)).toBeNull()
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
