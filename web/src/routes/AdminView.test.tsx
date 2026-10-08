import { respondToDiscardDialogs } from '../testing/discardDialogs'
respondToDiscardDialogs()
/**
 * Admin: settings grouped by task, one open section, and the file in the right
 * pane.
 *
 * **What changed shape here, and why each change is written down.** The page
 * was two groups by file (This project: Project, Organization, Storage & data;
 * This desk: Assistant, Connections, Sign-in & access), each row carrying a
 * summary of its setting. It is now two groups by task (Workspace: General,
 * Assistant, Research, Storage & backups, Decision safeguards; Connections &
 * access: Connections, Document processing, Sign-in & access), and a row is its
 * title. So:
 *
 * - **the order and count cases** assert eight rows under two group titles.
 * - **the summary cases** are gone with the summaries: a row says its title.
 * - **the Project section's cases** moved with what it carried. The project
 *   file's Location and Status are in its Configuration file pane (Details ›
 *   Configuration file, under General); the startup control is General's; the
 *   gates, the decision record and the Jobs record are Decision safeguards'.
 *   The startup control's line no longer names the desk-level file it writes:
 *   that file is named in the pane of the sections it supplies.
 * - **the runtime line** is in the right pane (Details › Runtime details), not
 *   a popover.
 * - **the stacked-shell cases** are gone: in the shell the list is always in
 *   the settings sidebar, and the inline column is only for standalone renders.
 * - **the storage kind** is a value ("Local folder"), not the decoder's sentence
 *   about kinds it does not offer.
 *
 * The narration sweep, the control cases and the pane's safety cases are
 * unchanged as rules and run over the new sections.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { RouterProvider, createMemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture, DeskConfigProvider } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { AppShell } from '../shell/AppShell'
import { ShellStateProvider } from '../shell/paneState'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { narrationIn } from '../admin/narration'
import { NO_BYTES_SAYS } from '../admin/ConfigPane'
import buttonStyles from '../ui/Button.module.css'
import selectStyles from '../ui/Select.module.css'
import { AdminView } from './AdminView'
import { readAuditRecord } from '../audit/client'
import { ADMIN_GROUPS, ADMIN_SECTIONS, adminSectionId, connectionTab } from './adminSections'

// The decision-record panel asks the chassis to run the runtime's audit
// verify; here it is told the project keeps no trail, and counted.
vi.mock(import('../audit/client'), async original => ({ ...(await original()), readAuditRecord: vi.fn(async () => ({ state: 'no-trail' as const })) }))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.localStorage.clear()
  vi.mocked(readAuditRecord).mockClear()
})

const QUIET = stubClient({ list_packs: () => ({ text: JSON.stringify({ packs: [] }) }) })

/** The pane's own empty state, which is what "the claim was released" looks like. */
const EMPTY_STATE = 'Select a row, a node or a file to inspect it here.'

/** One chassis refusal, with its provenance carried as the reader gets it. */
const CHASSIS_413 = {
  reason: 'the file is too large to read',
  responseReceived: true,
  status: 413,
  source: 'chassis'
} as const

/** The chassis' project root this desk is open on. */
const ROOT = '/home/someone/a-project'
const DESK_PATH = '/home/someone/.config/jpack-desk/desk.json'

const SECTION_TITLES = [
  'General',
  'Assistant',
  'Research',
  'Storage & backups',
  'Decision safeguards',
  'Connections',
  'Document processing',
  'Sign-in & access'
]

/**
 * `null` means "the chassis has not answered yet". Not `undefined`: a default
 * parameter takes over for an explicit `undefined`, so the provisional case
 * silently got the resolved root and asserted nothing.
 */
function renderAdmin(
  value = effectiveConfig(undefined),
  // `/admin` opens the first section, which is the whole of the landing state.
  path = '/admin',
  projectIdentity: string | null = ROOT,
  mcp: Partial<McpConnection> = {}
) {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: QUIET.client, ...mcp })}>
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
  const view = render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return { ...view, router }
}

/**
 * The same page inside the real shell, which is the only place the right pane
 * exists.
 *
 * `renderAdmin` above renders the route alone: nothing is published and no
 * pane is asserted by accident. A case about the pane needs the frame that
 * holds the slot, and one about *leaving* needs a second route to leave to —
 * so the harness is a router whose element switches on the address and whose
 * `AppShell` does not remount when it does. The pane is opened from the
 * existing control, `details` below, never on the page's behalf.
 */
function renderInShell(value = effectiveConfig(undefined), path = '/admin', mcp: Partial<McpConnection> = {}) {
  vi.stubGlobal('fetch', async () => ({
    ok: false,
    status: 404,
    statusText: '',
    text: async () => JSON.stringify({ error: 'no such file' })
  }))
  function Harness() {
    const { pathname } = useLocation()
    return (
      <AppShell>{pathname.startsWith('/admin') ? <AdminView /> : <h1>another route</h1>}</AppShell>
    )
  }
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: QUIET.client, ...mcp })}>
            <DeskConfigFixture value={value}>
              <Harness />
            </DeskConfigFixture>
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: [path] }
  )
  const view = render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return { ...view, router }
}

/** Opens one item of the header's Details menu. */
async function details(name: 'Configuration file' | 'Runtime details' = 'Configuration file') {
  fireEvent.keyDown(screen.getByRole('button', { name: 'Details' }), { key: 'ArrowDown' })
  fireEvent.click(await screen.findByRole('menuitem', { name }))
}

/** The items the Details menu offers here, by name. */
async function detailsItems(): Promise<string[]> {
  fireEvent.keyDown(screen.getByRole('button', { name: 'Details' }), { key: 'ArrowDown' })
  const menu = await screen.findByRole('menu')
  const items = within(menu).getAllByRole('menuitem').map((each) => each.textContent ?? '')
  fireEvent.keyDown(menu, { key: 'Escape' })
  return items
}

/** The right pane, which is the shell's and not the page's. */
function inspector(): HTMLElement {
  return document.querySelector<HTMLElement>('.desk-inspector')!
}

/** The pane's Location row, as it reads. */
function paneLocation(): string | null {
  const row = Array.from(inspector().querySelectorAll('dt')).find((each) => each.textContent === 'Location')
  return row?.nextElementSibling?.textContent ?? null
}

/** Opens Details › Runtime details in the shell and returns its two rows. */
async function runtimeLine(): Promise<HTMLElement> {
  await details('Runtime details')
  return await waitFor(() => {
    const line = within(inspector()).getByRole('heading', { name: 'Runtime details' }).closest('section')!.querySelector('dl')
    expect(line).not.toBeNull()
    return line as HTMLElement
  })
}

/** The desk's own page, which is the `<article>` and not the shell around it. */
function page(container: HTMLElement): HTMLElement {
  return container.querySelector('article')!
}

/** The navigation column, which is the whole of the list. */
function rail(container: HTMLElement): HTMLElement {
  return page(container).querySelector<HTMLElement>('nav[aria-label="Settings"]')!
}

/** The column's group titles, in the order they are rendered. */
function railTitles(container: HTMLElement): (string | null)[] {
  return Array.from(rail(container).querySelectorAll('p')).map((each) => each.textContent)
}

/** The column's rows, in the order they are rendered. */
function rowsIn(container: HTMLElement): HTMLAnchorElement[] {
  return Array.from(page(container).querySelectorAll<HTMLAnchorElement>('li a[href^="/admin#"]'))
}

/** The address of every row marked current, which is never more than one. */
function currentRows(container: HTMLElement): (string | null)[] {
  return rowsIn(container)
    .filter((row) => row.getAttribute('aria-current') !== null)
    .map((row) => row.getAttribute('href'))
}

/** Every row's title. */
function rowTitles(container: HTMLElement): (string | null)[] {
  return rowsIn(container).map((row) => row.textContent)
}

/** Opens one section through its row. */
function openRow(container: HTMLElement, id: string) {
  fireEvent.click(rowsIn(container).find((row) => row.getAttribute('href') === `/admin#${id}`)!)
}

/**
 * A desk serving one project file, with the write left in whatever state a
 * case is about.
 *
 * `renderAdmin` above is a *fixture*: the configuration is handed in, so no
 * card has bytes to write over and every Save is disabled. These cases are
 * about what a section says while it is writing, so they need the real provider
 * over a real read.
 */
function servesAdmin(content: string, put: 'pending' | 'stale'): { puts: number } {
  const seen = { puts: 0 }
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    if (String(url).includes('/api/desk-config')) {
      return answered({
        path: DESK_PATH,
        present: false,
        sha256: '',
        project: { dir: '/p', file: '/p/jpack-desk.json' },
        runtime: { bin: 'jpack' }
      })
    }
    if (String(url).includes('/api/files')) {
      return answered({ root: '/p', files: [{ path: 'packs/a.pack.json', bytes: 1, sha256: 'aa' }] })
    }
    if (init?.method === 'PUT') {
      seen.puts += 1
      // A request that never answers is what "writing" is: the section must say
      // so while it is in the air, not after it has come back.
      if (put === 'pending') return new Promise(() => {})
      return answered(
        {
          error: 'the file on disk is not the file this edit started from',
          code: 'stale',
          path: 'jpack-desk.json',
          expectedSha256: 'a'.repeat(64),
          actualSha256: 'c'.repeat(64),
          exists: true
        },
        409
      )
    }
    return answered({
      path: 'jpack-desk.json',
      bytes: content.length,
      sha256: 'a'.repeat(64),
      content
    })
  })
  return seen
}

function answered(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    statusText: '',
    status,
    text: async () => JSON.stringify(body)
  }
}

/** The same shell as `renderAdmin`, over the real configuration provider. */
function renderLiveAdmin(path = '/admin') {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: QUIET.client })}>
            <DeskConfigProvider>
              <ShellStateProvider
                projectIdentity={ROOT}
                viewport={{ railIsDrawer: false, inspectorIsDrawer: false }}
              >
                <AdminView />
              </ShellStateProvider>
            </DeskConfigProvider>
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

/**
 * One card's Status row, or nothing where it states none.
 *
 * A card in Admin's settings form states its status only where it is not
 * "read": writing, refused, holding a stale write, or a file that could not be
 * read. The card is found by its title's id.
 */
function statusOf(id: string): string | null {
  const card = document.getElementById(`${id}-title`)!.closest('section')!
  const row = Array.from(card.querySelectorAll('dl > div')).find(
    (each) => each.querySelector('dt')?.textContent === 'Status'
  )
  return row?.querySelector('dd')?.textContent ?? null
}

/**
 * One sentence, as a pattern that matches it inside a longer hint.
 */
function escaped(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** One file listing, or a refusal, for the Storage section to describe. */
function servesListing(options: {
  files?: { path: string; bytes: number; sha256: string }[]
  partial?: string[]
  fail?: boolean
}) {
  vi.stubGlobal('fetch', async () =>
    options.fail
      ? {
          ok: false,
          status: 503,
          statusText: '',
          text: async () => JSON.stringify({ error: 'the project could not be read' })
        }
      : {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({
              root: '/p',
              files: options.files ?? [],
              ...(options.partial ? { partial: options.partial } : {})
            })
        }
  )
}

/**
 * One desk-level read that answered, with whatever the file says — and with the
 * file's own bytes, because the pane quotes them.
 */
function deskRead(file: object) {
  const text = JSON.stringify({ deskConfigVersion: 1, ...file })
  return {
    path: DESK_PATH,
    present: true,
    sha256: 'd'.repeat(64),
    chassis: {
      projectDir: '/this/launch',
      projectFile: '/this/launch/jpack-desk.json',
      runtimeBin: 'jpack'
    },
    text,
    decoded: decodeDeskConfig(text, 'desk')
  }
}

/** A project file whose bytes are not what a re-serialisation of them would be. */
const PROJECT_TEXT =
  '{\n  "deskConfigVersion": 1,\n  "organization": {"name": "Acme", "mark": null},\n' +
  '  "storage": {"packs": {"dir": "decisions", "idBase": "https://acme.example/d"}}\n}'

/** Both files read, every section carrying something. */
function everythingConfigured() {
  return effectiveConfig(
    decodeDeskConfig(PROJECT_TEXT, 'project'),
    undefined,
    undefined,
    deskRead({
      assistant: {
        endpoint: {
          url: 'https://api.example.invalid/v1',
          kind: 'gemini',
          model: 'a-model',
          tools: ['validate']
        },
        engine: 'vercel',
        thinking: 'ultra'
      },
      identity: {
        provider: { label: 'Acme SSO', issuer: 'https://issuer.example', clientId: 'a' }
      }
    }),
    PROJECT_TEXT
  )
}

/** A desk-level read that names a chassis and nothing else. */
function chassisOnly(projectDir = '/this/launch', runtimeBin = 'jpack'): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: DESK_PATH,
    present: false,
    sha256: '',
    chassis: {
      projectDir,
      projectFile: `${projectDir}/jpack-desk.json`,
      runtimeBin
    }
  })
}

describe('Admin, grouped by task', () => {
  it('renders every group title and one row per section, in order, as links to their sections', () => {
    // **The order case, in the shape the column has.** It fails if a group or a
    // section is added without being declared, declared without being rendered,
    // or rendered out of order — and a row is a link to that section's own
    // fragment.
    const { container } = renderAdmin()
    expect(railTitles(container)).toEqual(ADMIN_GROUPS.map((group) => group.title))
    expect(railTitles(container)).toEqual(['Workspace', 'Connections & access'])
    expect(rowTitles(container)).toEqual(ADMIN_SECTIONS.map((section) => section.title))
    expect(rowsIn(container).map((row) => row.getAttribute('href'))).toEqual(
      ADMIN_SECTIONS.map((section) => `/admin#${section.id}`)
    )
    for (const group of ADMIN_GROUPS) {
      const block = document.getElementById(`rail-${group.id}`)!.parentElement!
      expect(
        Array.from(block.querySelectorAll<HTMLAnchorElement>('a[href^="/admin#"]')).map((row) =>
          row.getAttribute('href')
        ),
        group.title
      ).toEqual(group.sections.map((each) => `/admin#${each.id}`))
    }
    expect(within(rail(container)).getByRole('list', { name: 'Workspace' })).toBeTruthy()
    expect(within(rail(container)).getByRole('list', { name: 'Connections & access' })).toBeTruthy()
  })

  it('is exactly eight rows under exactly two group titles, and states no file’s head', () => {
    // **The column is a list of sections and nothing else**, asserted in four
    // states, because a head that came back on a refusal would be invisible to
    // a case that only ever rendered a desk with nothing configured.
    const states = [
      ['nothing read', () => effectiveConfig(undefined), '/admin'],
      ['both files read', everythingConfigured, '/admin#assistant'],
      [
        'a refused project file',
        () =>
          effectiveConfig({
            values: undefined,
            problems: [{ key: 'colour', reason: 'unknown key' }],
            notices: []
          }),
        '/admin#storage'
      ],
      [
        'a project file that could not be read',
        () => effectiveConfig(undefined, undefined, CHASSIS_413),
        '/admin#identity-provider'
      ]
    ] as const
    for (const [where, build, path] of states) {
      const { container } = renderAdmin(build(), path)
      expect(railTitles(container), where).toEqual(['Workspace', 'Connections & access'])
      expect(rowsIn(container), where).toHaveLength(8)
      expect(rowTitles(container), where).toEqual(SECTION_TITLES)
      expect(rail(container).querySelectorAll('dt'), where).toHaveLength(0)
      expect(rail(container).textContent, where).not.toContain('Location')
      expect(rail(container).textContent, where).not.toContain('Status')
      expect(rail(container).textContent, where).not.toContain(DESK_PATH)
      // And no form and no button in the column: a row is a link.
      expect(rail(container).querySelectorAll('button'), where).toHaveLength(0)
      cleanup()
    }
  })

  it.each([
    ['', 'general'],
    ['#project', 'general'],
    ['#organization', 'general'],
    ['#documents', 'gateway'],
    ['#connections-ai', 'connections'],
    ['#connections-files', 'connections'],
    ['#connections-search', 'connections'],
    ['#workspace', 'general'],
    ['#not-real', 'general'],
    ['#%', 'general']
  ])('resolves the fragment %s to the %s section', (hash, id) => {
    expect(adminSectionId(hash)).toBe(id)
  })

  it('opens a Connections tab only for its own fragment, and Files & apps otherwise', () => {
    expect(connectionTab('#connections-ai')).toBe('ai')
    expect(connectionTab('#connections-search')).toBe('search')
    for (const hash of ['#connections', '#connections-files', '#connections-processing', '', '#connections-AI']) {
      expect(connectionTab(hash), hash).toBe('files')
    }
  })

  it('offers no All settings link, and no address that is not a section or one of its tabs', () => {
    // An address inside a section — the desk's model preferences point at the
    // shared AI settings, Research at the shared search connections — is a
    // section's address too, or a tab of one.
    const tabs = ['/admin#connections-ai', '/admin#connections-search']
    for (const path of [
      '/admin',
      '/admin#storage',
      '/admin#assistant',
      '/admin#research',
      '/admin#connections-search',
      '/admin#not-a-section'
    ]) {
      const { container } = renderAdmin(everythingConfigured(), path)
      expect(screen.queryByRole('link', { name: 'All settings' }), path).toBeNull()
      expect(page(container).textContent, path).not.toContain('All settings')
      const addresses = Array.from(page(container).querySelectorAll('a'))
        .map((each) => each.getAttribute('href') ?? '')
        .filter((href) => href.startsWith('/admin'))
      const sections = ADMIN_SECTIONS.map((section) => `/admin#${section.id}`)
      expect(addresses.slice(0, sections.length), path).toEqual(sections)
      expect(addresses.filter((each) => !sections.includes(each) && !tabs.includes(each)), path).toEqual([])
      cleanup()
    }
  })

  it('opens runtime details in the right pane from the Details menu, outside the page', async () => {
    const { container } = renderInShell(everythingConfigured())
    const line = await runtimeLine()
    expect(page(container).contains(line)).toBe(false)
    expect(line.querySelectorAll('dt')).toHaveLength(2)
    expect(page(container).querySelector('pre')).toBeNull()
  })

  it('states what this desk is running on in two rows, and no Location or Status', async () => {
    // Two facts, neither of them a setting: the connection and the binary the
    // chassis was launched with, each one the connection's or the chassis' own
    // answer.
    renderInShell(chassisOnly('/real/a-project', '/usr/local/bin/jpack'))
    const line = await runtimeLine()
    expect(Array.from(line.querySelectorAll('dt')).map((each) => each.textContent)).toEqual([
      'Runtime',
      'Binary'
    ])
    await waitFor(() => expect(line.textContent).toContain('connected — '))
    expect(line.textContent).toContain('/usr/local/bin/jpack')
    expect(line.textContent).not.toContain('Location')
    expect(line.textContent).not.toContain('Status')
  })

  it('reads the connection off its status, not off the runtime it last met', async () => {
    // `server` is retained across a reconnect — the provider spreads the
    // previous state — so a line that read "connected" off its presence said
    // so while the socket was down and the banner said the connection was
    // lost. The name is only said where the connection is actually up.
    renderInShell(effectiveConfig(undefined), '/admin', {
      status: 'reconnecting',
      client: null,
      attempt: 3
    })
    const line = await runtimeLine()
    await waitFor(() => expect(line.textContent).toContain('reconnecting'))
    expect(line.textContent).not.toContain('connected —')
    expect(line.textContent).not.toContain('jpack')
    cleanup()

    renderInShell(effectiveConfig(undefined), '/admin', { status: 'failed', client: null })
    expect((await runtimeLine()).textContent).toContain('not connected')
  })

  it('names the runtime it is connected to where it actually is', async () => {
    renderInShell()
    const line = await runtimeLine()
    await waitFor(() => expect(line.textContent).toContain('connected — jpack test'))
  })

  it('names neither configuration file on the runtime line, because the file’s own pane does', async () => {
    renderInShell(chassisOnly('/real/a-project', '/usr/local/bin/jpack'))
    const line = await runtimeLine()
    expect(line.textContent).not.toContain('/real/a-project/jpack-desk.json')
    expect(line.textContent).not.toContain(DESK_PATH)
    // The project's own file is stated in General's Configuration file.
    await details()
    await waitFor(() => expect(paneLocation()).toBe('/real/a-project/jpack-desk.json'))
  })

  it('states a file’s Location in its pane, and never on the page or in the column', async () => {
    const { container } = renderInShell(
      effectiveConfig(undefined, 'no configuration was read: no such file', undefined, {
        path: DESK_PATH,
        present: false,
        sha256: ''
      })
    )
    // Nowhere on the page: a Location is the pane's, and the cards in the main
    // column state a status only where it is not "read".
    const labels = Array.from(page(container).querySelectorAll('section dt')).map(
      (each) => each.textContent
    )
    expect(labels).not.toContain('Location')
    // In the shell the column is in the settings sidebar, not in the page.
    expect(document.querySelector('nav[aria-label="Settings"]')!.querySelectorAll('dt')).toHaveLength(0)
    // And an absent file is absent, never "read".
    expect(screen.getAllByText('not present — defaults in use').length).toBeGreaterThan(0)
    await details()
    await waitFor(() => expect(paneLocation()).toBe('the desk has not said'))
  })

  it('says which file supplied a member: shared on this computer, or this desk', () => {
    // A member the desk-level file supplied is shared by every desk on this
    // computer; one the project file supplied is this desk's.
    renderAdmin(
      effectiveConfig(
        decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1 }), 'project'),
        undefined,
        undefined,
        {
          path: DESK_PATH,
          present: true,
          sha256: '',
          decoded: decodeDeskConfig(
            JSON.stringify({ deskConfigVersion: 1, storage: { packs: { dir: 'elsewhere' } } }),
            'desk'
          )
        }
      ),
      '/admin#storage'
    )
    const card = document.getElementById('pack-storage-title')!.closest('section')!
    expect(card.querySelector('header p')!.textContent).toBe('Shared on this computer')
    cleanup()
    renderAdmin(everythingConfigured(), '/admin#storage')
    expect(document.getElementById('pack-storage-title')!.closest('section')!.querySelector('header p')!.textContent).toBe('This desk')
  })

  it('names no user management, roles, invitations or assignment anywhere', () => {
    const { container } = renderAdmin()
    const text = container.textContent ?? ''
    for (const absent of ['Invite', 'Add user', 'Assign', 'Members', 'Permissions']) {
      expect(text).not.toContain(absent)
    }
  })

  it('offers no appearance at all, because it is not this page’s to offer', () => {
    // Theme and density are a person's preference and not an organization's
    // setting; they are in the user menu.
    const { container } = renderAdmin()
    expect(document.getElementById('appearance')).toBeNull()
    expect(screen.queryByLabelText('Theme')).toBeNull()
    expect(screen.queryByLabelText('Density')).toBeNull()
    expect(container.textContent).not.toContain('Appearance')
    expect(effectiveConfig(undefined).config.appearance).toEqual({
      theme: 'system',
      density: 'comfortable'
    })
  })

  it('takes every location from the chassis, and composes none of them', async () => {
    // A page that joined the reported directory to a file name would be
    // asserting a path on a filesystem it cannot see, and would be wrong the
    // first time a project was reached through a symlink — which is exactly
    // what the chassis resolves before it reports.
    const { container } = renderInShell(chassisOnly('/real/a-project', '/usr/local/bin/jpack'))
    await details()
    await waitFor(() => expect(paneLocation()).toBe('/real/a-project/jpack-desk.json'))
    expect((await runtimeLine()).textContent).toContain('/usr/local/bin/jpack')
    // And the page never offers the project-relative name it reads the file by.
    expect(page(container).textContent).not.toContain('jpack-desk.json')
  })

  it('names a configuration that could not be read, and does not call it absent', () => {
    renderAdmin(effectiveConfig(undefined, undefined, CHASSIS_413))
    expect(screen.getAllByText(/not read — the desk answered/).length).toBeGreaterThan(0)
    expect(screen.getAllByText('the file is too large to read').length).toBeGreaterThan(0)
  })

  it('sources an unread reason to whoever actually said it', () => {
    // Three provenances, three sentences, and each one carried rather than
    // inferred.
    renderAdmin(effectiveConfig(undefined, undefined, CHASSIS_413))
    expect(screen.getAllByText(/and its own reason/).length).toBeGreaterThan(0)
    expect(screen.queryByText(/this page’s reason/)).toBeNull()
    cleanup()

    renderAdmin(
      effectiveConfig(undefined, undefined, {
        reason: 'Failed to fetch',
        responseReceived: false,
        source: 'browser'
      })
    )
    expect(screen.getAllByText(/the browser’s own reason/).length).toBeGreaterThan(0)
    expect(screen.queryByText(/the desk answered/)).toBeNull()
    cleanup()

    renderAdmin(
      effectiveConfig(undefined, undefined, {
        reason: 'the desk answered 200 with text that is not JSON',
        responseReceived: true,
        status: 200,
        source: 'desk'
      })
    )
    expect(screen.getAllByText(/this page’s reason/).length).toBeGreaterThan(0)
    expect(screen.queryByText(/the browser’s own reason/)).toBeNull()
  })

  it('says a file is absent where it is simply absent, and never refused', () => {
    renderAdmin(effectiveConfig(undefined, 'no configuration was read: no such file'))
    expect(screen.getAllByText('not present — defaults in use').length).toBeGreaterThan(0)
    expect(screen.queryByText(/^refused:/)).toBeNull()
  })

  it('reports a refused configuration by naming every problem, and stays on defaults', () => {
    const value = effectiveConfig({
      values: undefined,
      problems: [{ key: 'colour', reason: 'unknown key' }],
      notices: []
    })
    renderAdmin(value)
    expect(screen.getAllByText('refused:', { exact: false }).length).toBeGreaterThan(0)
    expect(screen.getAllByText('colour: unknown key').length).toBeGreaterThan(0)
  })

  it('refuses the desk-level file on its own, without blaming the project one', async () => {
    // Two files, two verdicts. A bad key in one must not be reported as the
    // other's, and neither is repaired by the other being fine.
    const value = () =>
      effectiveConfig(
        decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1 }), 'project'),
        undefined,
        undefined,
        {
          path: DESK_PATH,
          present: true,
          decoded: decodeDeskConfig(
            JSON.stringify({ deskConfigVersion: 1, assistant: { endpoint: { apiKey: 'sk-oops' } } }),
            'desk'
          )
        }
      )
    renderAdmin(value())
    // The project file's own card says what reading *it* produced — this file
    // carries no organization, so the defaults are in use — and nothing about
    // the other file's refusal.
    expect(statusOf('branding')).toBe('not present — defaults in use')
    expect(screen.queryByText(/a key is never stored/)).toBeNull()
    cleanup()

    renderInShell(value(), '/admin#connections-ai')
    await details()
    // The refusal says the thing that is actually wrong, in the decoder's words.
    await waitFor(() =>
      expect(
        screen.getAllByText(/assistant.endpoint.apiKey: a key is never stored/).length
      ).toBeGreaterThan(0)
    )
  })
})

/** General: the desk's name, the project file's branding, and the startup project. */
describe('the General section', () => {
  it('keeps branding and the startup control on General, distinguishes the desk name, and runs no audit on arrival', async () => {
    const writes: string[] = []
    vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
      const path = new URL(url, 'http://localhost').pathname
      if (init.method && init.method !== 'GET') writes.push(path)
      const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
      if (path === '/api/desks') return json({ current: { id: '', name: 'Example desk', folder: '/work', managed: false }, desks: [], location: '/private' })
      return json({ error: 'not available' }, 404)
    })
    const { container } = renderAdmin(chassisOnly())
    expect(await screen.findByText('Example desk')).toBeTruthy()
    expect(screen.getByRole('heading', { level: 2, name: 'General' })).toBeTruthy()
    expect(screen.getByRole('textbox', { name: 'Organization name' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Open this project at startup' })).toBeTruthy()
    // And nothing of another section: no pack folder, no decision record.
    expect(screen.queryByRole('textbox', { name: 'Pack folder' })).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Decision record' })).toBeNull()
    expect(readAuditRecord).not.toHaveBeenCalled()
    expect(writes).toEqual([])
    expect(currentRows(container)).toEqual(['/admin#general'])
  })

  it('says the desk name could not be read rather than loading for ever', async () => {
    vi.stubGlobal('fetch', async () => answered({ error: 'not available' }, 503))
    renderAdmin(chassisOnly())
    expect(await screen.findByText('could not be read')).toBeTruthy()
  })

  it('carries exactly the state-changing controls it names, and no others', () => {
    // This fixture is the state in which nothing asked for the desk-level file,
    // so this page has never seen the bytes a write would replace — and the
    // Save node under the control is the sentence that says so.
    const { container } = renderAdmin()
    expect(
      Array.from(container.querySelectorAll('button[disabled]')).map(
        (element) => element.textContent
      )
    ).toContain('Open this project at startup')
    expect(
      screen.getByText(/has not read its own configuration file/).closest('section')!.querySelector('h3')!.textContent
    ).toBe('Startup')
  })

  it('will not offer the nomination where the chassis has not named this project', () => {
    // The one value the route accepts is the chassis'. A page that offered the
    // control without it could only compose a path or send nothing.
    renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: false,
        sha256: ''
      })
    )
    const nominate = screen.getByRole('button', {
      name: 'Open this project at startup'
    }) as HTMLButtonElement
    expect(nominate.disabled).toBe(true)
    expect(screen.getByText(/has not said where its own configuration file is/)).toBeTruthy()
  })

  it('says what the startup control changes, and when', () => {
    renderAdmin(chassisOnly())
    const row = screen.getByText('Startup project').parentElement!
    expect(row.textContent).toContain('Changes apply to the next launch without a project folder. The current desk stays open.')
  })

  it('says the desk has not said where the file is, rather than offering the name it reads it by', async () => {
    // `jpack-desk.json` is the address this page reads the file at, not an
    // established location on a filesystem — and the row is about where the
    // file **is**.
    const { container } = renderInShell()
    expect(page(container).textContent).not.toContain('jpack-desk.json')
    await details()
    await waitFor(() => expect(paneLocation()).toBe('the desk has not said'))
  })

  it('names the file the chassis resolved the moment it answers', async () => {
    renderInShell(chassisOnly('/real/a-project'))
    await details()
    await waitFor(() => expect(paneLocation()).toBe('/real/a-project/jpack-desk.json'))
    expect(inspector().textContent).not.toContain('the desk has not said')
  })

  it('offers the nomination in two states and clears it in the third, by the chassis’ path', () => {
    // **The comparison is against the path the chassis resolved**, so before
    // it has answered this page does not know which of the other two is true.
    renderAdmin()
    expect((screen.getByRole('button', { name: 'Open this project at startup' }) as HTMLButtonElement).disabled).toBe(true)
    cleanup()

    renderAdmin(effectiveConfig(undefined, undefined, undefined, deskRead({})))
    expect(screen.getByRole('button', { name: 'Open this project at startup' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Clear startup project' })).toBeNull()
    cleanup()

    renderAdmin(
      effectiveConfig(
        undefined,
        undefined,
        undefined,
        deskRead({ project: { file: '/this/launch/jpack-desk.json' } })
      )
    )
    expect(screen.getByRole('button', { name: 'Clear startup project' })).toBeTruthy()
  })
})

/** Decision safeguards: the gates, the decision record and the Jobs record. */
describe('the Decision safeguards section', () => {
  it('carries the decision record after the gates, and the Jobs record after it (ADR-0010)', async () => {
    vi.stubGlobal('fetch', async (url: string) =>
      String(url).includes('/api/runner-key')
        ? answered({ runnerKey: { state: 'signed', keyId: 'a'.repeat(32), publicKey: 'b'.repeat(64) } })
        : answered({ error: 'not available' }, 404))
    renderAdmin(effectiveConfig(undefined), '/admin#safeguards')
    const gates = screen.getByRole('heading', { level: 2, name: 'Decision safeguards' })
    const record = screen.getByRole('heading', { name: 'Decision record' })
    const jobs = await screen.findByRole('heading', { name: 'Jobs record' })
    expect(gates.compareDocumentPosition(record) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(record.compareDocumentPosition(jobs) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(record.closest('[hidden]')).toBeNull()
    expect(jobs.closest('[hidden]')).toBeNull()
  })

  it('runs the decision record and asks for the Runner each time its section is opened, and not while another section is', async () => {
    // The Jobs record asks whether this desk has a Runner each time it becomes
    // visible, and only then.
    let runnerAsked = 0
    vi.stubGlobal('fetch', async (url: string) => {
      if (String(url).includes('/api/runner-key')) runnerAsked += 1
      return answered({ error: 'not available' }, 404)
    })
    const { container } = renderAdmin(everythingConfigured())
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 20)) })
    expect(readAuditRecord).not.toHaveBeenCalled()
    expect(runnerAsked).toBe(0)
    openRow(container, 'safeguards')
    await waitFor(() => expect(readAuditRecord).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(runnerAsked).toBe(1))
    openRow(container, 'storage')
    openRow(container, 'general')
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 20)) })
    expect(readAuditRecord).toHaveBeenCalledTimes(1)
    expect(runnerAsked).toBe(1)
    openRow(container, 'safeguards')
    await waitFor(() => expect(readAuditRecord).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(runnerAsked).toBe(2))
  })
})

describe('one section at a time', () => {
  it('lands on the first section when there is no fragment, with the list beside it', () => {
    const { container } = renderAdmin(everythingConfigured(), '/admin')
    expect(screen.getByRole('heading', { level: 2, name: 'General' })).toBeTruthy()
    expect(currentRows(container)).toEqual(['/admin#general'])
    expect(rowTitles(container)).toEqual(SECTION_TITLES)
    expect(screen.queryByRole('link', { name: 'All settings' })).toBeNull()
  })

  it('opens the section a fragment names, and marks its row current', () => {
    const { container } = renderAdmin(everythingConfigured(), '/admin#storage')
    expect(screen.getByRole('heading', { level: 2, name: 'Storage & backups' })).toBeTruthy()
    expect(currentRows(container)).toEqual(['/admin#storage'])
    expect(rowTitles(container)).toEqual(SECTION_TITLES)
    // And only that section's form is on the page.
    expect(screen.getByDisplayValue('decisions')).toBeTruthy()
    expect(screen.queryByLabelText('Organization name')).toBeNull()
  })

  it('opens what a section of the old layout held for its old fragment', () => {
    const { container } = renderAdmin(everythingConfigured(), '/admin#organization')
    expect(screen.getByRole('heading', { level: 2, name: 'General' })).toBeTruthy()
    expect(currentRows(container)).toEqual(['/admin#general'])
    expect(screen.getByDisplayValue('Acme')).toBeTruthy()
  })

  it('a row opens its section, and the one that was open closes', () => {
    const { container } = renderAdmin(everythingConfigured())
    expect(screen.getByRole('heading', { level: 2, name: 'General' })).toBeTruthy()
    expect(screen.queryByRole('heading', { level: 2, name: 'Storage & backups' })).toBeNull()
    openRow(container, 'storage')
    expect(screen.getByRole('heading', { level: 2, name: 'Storage & backups' })).toBeTruthy()
    expect(screen.getByLabelText('Pack folder')).toBeTruthy()
    expect(screen.queryByRole('heading', { level: 2, name: 'General' })).toBeNull()
    expect(currentRows(container)).toEqual(['/admin#storage'])

    openRow(container, 'general')
    expect(screen.getByRole('heading', { level: 2, name: 'General' })).toBeTruthy()
    expect(screen.getByLabelText('Pack folder').closest('[hidden]')).not.toBeNull()
    expect(currentRows(container)).toEqual(['/admin#general'])
  })

  it('retains a section draft and guards leaving Admin while a hidden form is dirty', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { router } = renderInShell(everythingConfigured(), '/admin#general')
    const name = screen.getByLabelText('Organization name') as HTMLInputElement
    fireEvent.change(name, { target: { value: 'Unsaved organization' } })
    await act(() => router.navigate('/admin#storage'))
    expect(name.closest('[hidden]')).not.toBeNull()
    const reload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(reload)
    expect(reload.defaultPrevented).toBe(true)
    await act(() => router.navigate('/packs'))
    expect(confirm).toHaveBeenCalledOnce()
    expect(router.state.location.pathname).toBe('/admin')
    await act(() => router.navigate('/admin#general'))
    expect((screen.getByLabelText('Organization name') as HTMLInputElement).value).toBe('Unsaved organization')
    expect(confirm).toHaveBeenCalledOnce()
    confirm.mockReturnValue(true)
    await act(() => router.navigate('/packs'))
    expect(router.state.location.pathname).toBe('/packs')
    confirm.mockRestore()
  })

  it('changes nothing on Escape, because there is nothing to leave', () => {
    const { container } = renderAdmin(everythingConfigured(), '/admin#storage')
    const before = page(container).innerHTML
    fireEvent.keyDown(document.body, { key: 'Escape' })
    expect(screen.getByRole('heading', { level: 2, name: 'Storage & backups' })).toBeTruthy()
    expect(currentRows(container)).toEqual(['/admin#storage'])
    expect(page(container).innerHTML).toBe(before)

    // Including one from inside a dialog, which owns its own Escape.
    const dialog = document.createElement('div')
    dialog.setAttribute('role', 'dialog')
    document.body.append(dialog)
    fireEvent.keyDown(dialog, { key: 'Escape' })
    expect(screen.getByRole('heading', { level: 2, name: 'Storage & backups' })).toBeTruthy()
    dialog.remove()
  })

  it('opens the first section for every fragment that names none', () => {
    for (const path of [
      '/admin',
      '/admin#',
      '/admin#not-a-section',
      // A group is not a section.
      '/admin#workspace',
      '/admin#services',
      // And neither is a fragment that is not valid percent-encoding.
      '/admin#%zz',
      '/admin#storage%'
    ]) {
      const { container } = renderAdmin(everythingConfigured(), path)
      expect(screen.getByRole('heading', { level: 2, name: 'General' }), path).toBeTruthy()
      expect(currentRows(container), path).toEqual(['/admin#general'])
      expect(rowTitles(container), path).toEqual(SECTION_TITLES)
      cleanup()
    }
  })

  it('opens the requested Connections tab, and keeps one row current', async () => {
    const { container } = renderAdmin(effectiveConfig(undefined), '/admin#connections-search')
    expect(screen.getByRole('tab', { name: 'Web search' }).getAttribute('aria-selected')).toBe('true')
    expect(currentRows(container)).toEqual(['/admin#connections'])
    expect(screen.getByRole('button', { name: 'Add connection' })).toBeTruthy()
    // This desk's default is Research's, not the shared connections'.
    expect(screen.queryByRole('combobox', { name: 'Default search connection' })).toBeNull()
    cleanup()
    renderAdmin(effectiveConfig(undefined), '/admin#connections')
    expect(screen.getByRole('tab', { name: 'Files & apps' }).getAttribute('aria-selected')).toBe('true')
  })

  it('keeps per-desk research apart from the shared search credentials', () => {
    renderAdmin(effectiveConfig(undefined), '/admin#research')
    expect(screen.getByRole('heading', { level: 2, name: 'Research' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Manage search connections' }).getAttribute('href')).toBe('/admin#connections-search')
    expect(screen.queryByRole('button', { name: 'Add connection' })).toBeNull()
    expect(screen.queryByLabelText('API key')).toBeNull()
  })

  it('does not mount the shared AI forms on the desk’s own Assistant section', () => {
    renderAdmin(effectiveConfig(undefined), '/admin#assistant')
    expect(screen.getByRole('link', { name: 'Manage shared AI settings' }).getAttribute('href')).toBe('/admin#connections-ai')
    expect(screen.queryByRole('combobox', { name: 'Connection method' })).toBeNull()
  })

  it('names the storage kind, the location and the id prefix', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({
        deskConfigVersion: 1,
        storage: { packs: { dir: 'decisions', idBase: 'https://acme.example/d' } }
      }),
      'project'
    )
    renderAdmin(effectiveConfig(decoded), '/admin#storage')
    expect(screen.getByText('Storage').parentElement!.textContent).toContain('Local folder')
    expect(screen.getByDisplayValue('decisions')).toBeTruthy()
    // The prefix as it will actually be written — normalised at decode — so a
    // Save that does not touch it writes back what the file already means.
    expect(screen.getByDisplayValue('https://acme.example/d/')).toBeTruthy()
  })

  it('keeps pack storage and chat data apart from research and document processing', () => {
    renderAdmin(effectiveConfig(undefined), '/admin#storage')
    expect(screen.getByRole('textbox', { name: 'Pack folder' })).toBeTruthy()
    expect(screen.getByRole('textbox', { name: 'Pack ID prefix' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Chat data' })).toBeTruthy()
    expect(screen.queryByRole('heading', { name: 'Research' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Manage PDF processing' })).toBeNull()
  })

  it('shows the built-in location and prefix where the project configured none', () => {
    renderAdmin(effectiveConfig(undefined), '/admin#storage')
    expect(screen.getByDisplayValue('packs')).toBeTruthy()
    expect(screen.getByDisplayValue('https://example.invalid/judgment-packs/')).toBeTruthy()
  })

  it('says a location holds files only where the listing shows one', async () => {
    // The listing reports regular files only, so the page can say a location
    // holds files and can never claim an empty one exists.
    servesListing({ files: [{ path: 'packs/a.pack.json', bytes: 1, sha256: 'aa' }] })
    renderAdmin(effectiveConfig(undefined), '/admin#storage')
    expect(await screen.findByText(/holds files/)).toBeTruthy()
  })

  it('says only that no file is under it, which is what the listing can show', async () => {
    servesListing({ files: [{ path: 'jpack.json', bytes: 1, sha256: 'aa' }] })
    renderAdmin(effectiveConfig(undefined), '/admin#storage')
    expect(
      await screen.findByText(/no file is under it — the first pack asks for it to be created/)
    ).toBeTruthy()
  })

  it('tells the four states apart that used to share one sentence', async () => {
    const cases: [Parameters<typeof servesListing>[0], string][] = [
      [{ fail: true }, 'the file listing failed, so nothing is known about it'],
      [
        { files: [{ path: 'jpack.json', bytes: 1, sha256: 'aa' }], partial: ['packs: permission denied'] },
        'the file listing came back incomplete, so nothing is known about it'
      ],
      [
        { files: [{ path: 'packs', bytes: 1, sha256: 'aa' }] },
        'a file is there under that exact name — nothing can be created inside it'
      ],
      [
        { files: [{ path: 'packs/a.pack.json', bytes: 1, sha256: 'aa' }] },
        'holds files'
      ]
    ]
    for (const [listing, says] of cases) {
      servesListing(listing)
      renderAdmin(effectiveConfig(undefined), '/admin#storage')
      expect(await screen.findByText(new RegExp(escaped(says))), says).toBeTruthy()
      cleanup()
    }
  })

  it('says the listing has not answered rather than describing what it has not seen', async () => {
    vi.stubGlobal('fetch', () => new Promise(() => {}))
    renderAdmin(effectiveConfig(undefined), '/admin#storage')
    expect(await screen.findByText(/the file listing has not answered yet/)).toBeTruthy()
  })

  it('renders the one storage kind as a value, and not as a control with one option', () => {
    // **A Select with one option is a control that cannot be operated.**
    renderAdmin(effectiveConfig(undefined), '/admin#storage')
    const storage = document.getElementById('pack-storage-title')!.closest('section')!
    expect(storage.querySelector('[role="combobox"]')).toBeNull()
    expect(storage.querySelector('select')).toBeNull()
    expect(screen.getByText('Storage').parentElement!.textContent).toContain('Local folder')
  })

  it('keeps sign-in policy separate, and offers no configuration file for it', async () => {
    renderAdmin(everythingConfigured(), '/admin#identity-provider')
    expect(screen.getByRole('heading', { name: 'Sign-in & access', level: 2 })).toBeTruthy()
    expect(screen.getByText('Shared on this computer · Control who can open Desk.')).toBeTruthy()
    expect(screen.queryByText('This provider describes the identity displayed in the header. Sign-in is not available yet.')).toBeNull()
    expect(screen.queryByRole('combobox', { name: 'Connection method' })).toBeNull()
    expect(await detailsItems()).toEqual(['Runtime details', 'Project files'])
  })

  it('offers the configuration file where the open section is about one', async () => {
    for (const [path, offered] of [
      ['/admin', true],
      ['/admin#storage', true],
      ['/admin#gateway', true],
      ['/admin#connections-ai', true],
      ['/admin#connections', false],
      ['/admin#connections-search', false],
      ['/admin#assistant', false],
      ['/admin#research', false],
      ['/admin#safeguards', false]
    ] as const) {
      renderAdmin(everythingConfigured(), path)
      expect((await detailsItems()).includes('Configuration file'), path).toBe(offered)
      cleanup()
    }
  })

  it('carries exactly the controls each open section names, and no others', () => {
    // The controls case, per state — one render per section, which is the whole
    // of what this page can change.
    const controls = (container: HTMLElement) =>
      Array.from(page(container).querySelectorAll('section button, section input, section select, section textarea'))
        .filter((element) => element.getAttribute('role') !== 'combobox' && element.getAttribute('role') !== 'tab')
        .filter((element) => element.closest('[hidden]') === null)
        .map((element) => element.textContent?.trim())
        .filter((label) => label !== '')
    for (const [fragment, expected] of [
      ['general', ['Upload file', 'Reset to default', 'Upload file', 'Use logo', 'Save', 'Open this project at startup']],
      ['storage', ['Save']],
      [
        // **This desk's model preferences**, and nothing that writes the
        // shared connection: the endpoint, its key and its models are under
        // Connections › AI (docs/ai-connections.md).
        'assistant',
        ['Save preferences', 'Reload from disk']
      ],
      ['identity-provider', []]
    ] as const) {
      const view = renderAdmin(effectiveConfig(undefined), `/admin#${fragment}`)
      expect(controls(view.container), fragment).toEqual(expected)
      // The nomination is General's, and it is carried into none of the others.
      if (fragment !== 'general') {
        expect(
          screen.queryByRole('button', { name: 'Open this project at startup' }),
          fragment
        ).toBeNull()
      }
      cleanup()
    }
  })

  it('offers access, API provider and review settings without an engine implementation picker', () => {
    // The shared AI settings, under Connections › AI: with no AI connections
    // read, the desk-level assistant slot as it was.
    renderAdmin(effectiveConfig(undefined), '/admin#connections-ai')
    const container = screen.getByRole('tabpanel', { name: 'AI' })
    const triggers = Array.from(container.querySelectorAll('[role="combobox"]')).map(
      (element) => element.textContent
    )
    expect(triggers).toEqual(['API key', 'OpenAI-compatible', 'off'])
    const offered = Array.from(container.querySelectorAll('select')).map(
      (element) => element.textContent
    )
    expect(offered).toEqual(['OpenAI-compatibleAnthropicGoogle Gemini', 'offstandarddeep'])
    expect(container.querySelectorAll('input[type="password"]')).toHaveLength(1)
    expect(screen.queryByRole('combobox', { name: 'Model' })).toBeNull()
  })

  it('enables a Save once the file behind it has been read', () => {
    // A Save states the bytes it replaces, so a section this desk has never
    // read the file for offers a control it cannot use — and says which file.
    renderAdmin(effectiveConfig(undefined), '/admin#general')
    const branding = document.getElementById('branding-title')!.closest('section')!
    expect((within(branding).getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/has not read this project/)).toBeTruthy()
    cleanup()

    renderAdmin(chassisOnly(), '/admin#assistant')
    // The desk-level file has been read, so the assistant's Save says nothing
    // about not having read it.
    expect(screen.queryByText(/has not read its own configuration file/)).toBeNull()
  })

  it('starts each section at the top of the page, not at the section', () => {
    // A full load of a fragment is scrolled by the browser itself, and a click
    // on a row while a tall section is scrolled would otherwise open the next
    // one halfway down.
    const scrolled: string[] = []
    const original = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = function scrollIntoView(this: Element) {
      scrolled.push(this.id === '' ? this.tagName.toLowerCase() : this.id)
    }
    try {
      renderAdmin(effectiveConfig(undefined))
      expect(scrolled).toEqual(['article'])
      cleanup()
      scrolled.length = 0
      const { container } = renderAdmin(effectiveConfig(undefined), '/admin#storage')
      expect(scrolled).toEqual(['article'])
      openRow(container, 'research')
      expect(scrolled).toEqual(['article', 'article'])
    } finally {
      Element.prototype.scrollIntoView = original
    }
  })
})

describe('a section’s own write', () => {
  const FILE = `{\n  "deskConfigVersion": 1,\n  "organization": { "name": "Unveiled", "mark": null },\n  "storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/d/" } }\n}\n`

  it('says the write is in the air on the card that is doing it', async () => {
    servesAdmin(FILE, 'pending')
    renderLiveAdmin('/admin#general')
    // The read has to land first: a Save pressed before it is refused for
    // having no bytes to write over, which is a different state.
    await waitFor(() => expect(screen.getByDisplayValue('Unveiled')).toBeTruthy())
    // While nothing is happening the card repeats nothing: the file was read.
    expect(statusOf('branding')).toBeNull()

    fireEvent.change(screen.getByDisplayValue('Unveiled'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('branding-title')!.closest('section')!.querySelector('form button[type="submit"]')!
    )
    await waitFor(() => expect(statusOf('branding')).toContain('writing'))
    expect(statusOf('branding')).toBe('writing — nothing is written until the desk answers')
    expect(document.getElementById('appearance')).toBeNull()
  })

  it('says what this page’s own decoder refused, on the card it refused it for', async () => {
    servesAdmin(FILE, 'pending')
    renderLiveAdmin('/admin#storage')
    await waitFor(() => expect(screen.getByDisplayValue('https://acme.example/d/')).toBeTruthy())
    fireEvent.change(screen.getByLabelText('Pack folder'), { target: { value: '../escape' } })
    fireEvent.click(
      document.getElementById('pack-storage-title')!.closest('section')!.querySelector('form button[type="submit"]')!
    )
    await waitFor(() => expect(statusOf('pack-storage')).toContain('not written:'))
    expect(statusOf('pack-storage')).toContain('storage.packs.dir')
  })

  it('says the file moved under a card, on the card the write was refused for', async () => {
    servesAdmin(FILE, 'stale')
    renderLiveAdmin('/admin#general')
    await waitFor(() => expect(screen.getByDisplayValue('Unveiled')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('Unveiled'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('branding-title')!.closest('section')!.querySelector('form button[type="submit"]')!
    )
    await waitFor(() =>
      expect(statusOf('branding')).toBe('the file changed on disk — nothing was written')
    )
  })

  it('says nothing about a write nobody made, on any card or row', async () => {
    servesAdmin(FILE, 'pending')
    const { container } = renderLiveAdmin()
    await waitFor(() => expect(screen.getByDisplayValue('Unveiled')).toBeTruthy())
    expect(statusOf('branding')).toBeNull()
    for (const row of rowsIn(container)) expect(row.textContent).not.toContain('writing')
  })
})

describe('what Admin puts in the right pane', () => {
  it('shows the whole project file under General, by the name it reads it at', async () => {
    const { container } = renderInShell(everythingConfigured())
    await details()
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'jpack-desk.json' })).toBeTruthy()
    )
    // By the node's own text rather than by a text query: the file is
    // multi-line and a query normalises whitespace, which is the one thing a
    // quotation of somebody's file must not do.
    expect(document.querySelector('.desk-inspector pre code')!.textContent).toBe(PROJECT_TEXT)
    // And the main column carries none of it.
    expect(page(container).querySelector('pre')).toBeNull()
  })

  it('shows the member of the file the open section is about, as it is written', async () => {
    // The bytes, not a re-serialisation of the decode. `idBase` is normalised
    // at decode — it gains the separator it was missing.
    renderInShell(everythingConfigured(), '/admin#storage')
    await details()
    await waitFor(() =>
      expect(
        screen.getByRole('heading', { level: 2, name: 'jpack-desk.json › storage' })
      ).toBeTruthy()
    )
    expect(
      screen.getByText('{"packs": {"dir": "decisions", "idBase": "https://acme.example/d"}}')
    ).toBeTruthy()
    expect(screen.getByDisplayValue('https://acme.example/d/')).toBeTruthy()
  })

  it('names the desk-level file for a member only that file carries', async () => {
    renderInShell(everythingConfigured(), '/admin#connections-ai')
    await details()
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'desk.json › assistant' })).toBeTruthy()
    )
    expect(within(inspector()).getByText(DESK_PATH)).toBeTruthy()
    expect(screen.getByText(/^sha256 dddddddddddd…$/)).toBeTruthy()
    cleanup()
    renderInShell(everythingConfigured(), '/admin#gateway')
    await details()
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'desk.json › research' })).toBeTruthy()
    )
  })

  it('renders no bytes of a file the decoder refused, in the pane either', async () => {
    // **The refusal is about a member, and rendering the file anyway puts that
    // member on the surface that reported it.**
    const refusedDesk = '{"deskConfigVersion":1,"identity":{"apiKey":"sk-live-secret"}}'
    renderInShell(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: true,
        text: refusedDesk,
        decoded: decodeDeskConfig(refusedDesk, 'desk')
      }),
      '/admin#connections-ai'
    )
    await details()
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'desk.json › assistant' })).toBeTruthy()
    )
    expect(document.body.textContent).not.toContain('sk-live-secret')
    expect(screen.getAllByText(/identity.apiKey: a key is never stored/).length).toBeGreaterThan(0)
    cleanup()

    // And the project file, where the whole document is what the pane shows.
    const refusedProject = '{"deskConfigVersion":1,"storage":{"apiKey":"sk-live-secret"}}'
    renderInShell(
      effectiveConfig(
        decodeDeskConfig(refusedProject, 'project'),
        undefined,
        undefined,
        undefined,
        refusedProject
      )
    )
    await details()
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'jpack-desk.json' })).toBeTruthy()
    )
    expect(document.body.textContent).not.toContain('sk-live-secret')
    expect(screen.getAllByText(/storage.apiKey: a key is never stored/).length).toBeGreaterThan(0)
  })

  it('renders no bytes of a file that could not be read at all', async () => {
    renderInShell(effectiveConfig(undefined, undefined, CHASSIS_413, undefined, 'UNREAD_BYTES'))
    await details()
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'jpack-desk.json' })).toBeTruthy()
    )
    expect(document.querySelector('.desk-inspector pre')).toBeNull()
    expect(document.body.textContent).not.toContain('UNREAD_BYTES')
    expect(screen.getAllByText(/not read — the desk answered 413/).length).toBeGreaterThan(0)
    expect(screen.queryByText(NO_BYTES_SAYS)).toBeNull()
  })

  it('says the bytes could not be established rather than offering a decode', async () => {
    const text = '{"deskConfigVersion": 1}'
    renderInShell(
      effectiveConfig(decodeDeskConfig(text, 'project'), undefined, undefined, undefined, text),
      '/admin#storage'
    )
    await details()
    await waitFor(() => expect(screen.getByText(NO_BYTES_SAYS)).toBeTruthy())
    expect(document.querySelector('.desk-inspector pre')).toBeNull()
  })

  it('releases the claim when the route leaves, so the pane is the next one’s', async () => {
    const { router } = renderInShell(everythingConfigured())
    await details()
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'jpack-desk.json' })).toBeTruthy()
    )
    expect(screen.queryByText(EMPTY_STATE)).toBeNull()

    await act(async () => {
      await router.navigate('/elsewhere')
    })
    await waitFor(() => expect(screen.getByText(EMPTY_STATE)).toBeTruthy())
    expect(screen.queryByRole('heading', { level: 2, name: 'jpack-desk.json' })).toBeNull()
    expect(screen.getByRole('heading', { name: 'another route' })).toBeTruthy()
  })
})

describe('Admin carries no narration', () => {
  /**
   * **The narration guard, over every state this page has.**
   *
   * 140 characters is a line; anything longer is a paragraph. Quoted material
   * is exempt — see `narrationIn`.
   */
  it.each([
    ['nothing configured, nothing read', () => effectiveConfig(undefined)],
    [
      'both files read and empty',
      () =>
        effectiveConfig(
          decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1 }), 'project'),
          undefined,
          undefined,
          { path: DESK_PATH, present: false, sha256: '' }
        )
    ],
    [
      'an assistant configured on the Gemini wire, at the deepest tier',
      () =>
        effectiveConfig(
          undefined,
          undefined,
          undefined,
          deskRead({
            assistant: {
              endpoint: {
                url: 'https://api.example.invalid/v1',
                kind: 'gemini',
                model: 'a-model',
                tools: ['validate']
              },
              engine: 'vercel',
              thinking: 'ultra'
            }
          })
        )
    ],
    [
      'an identity provider configured',
      () =>
        effectiveConfig(
          undefined,
          undefined,
          undefined,
          deskRead({
            identity: {
              provider: { label: 'Acme SSO', issuer: 'https://issuer.example', clientId: 'a' }
            }
          })
        )
    ],
    [
      'this project already the startup project',
      () =>
        effectiveConfig(
          undefined,
          undefined,
          undefined,
          deskRead({ project: { file: '/this/launch/jpack-desk.json' } })
        )
    ],
    [
      'a refused project file',
      () =>
        effectiveConfig({
          values: undefined,
          problems: [{ key: 'colour', reason: 'unknown key' }],
          notices: []
        })
    ],
    [
      'a project file that could not be read',
      () => effectiveConfig(undefined, undefined, CHASSIS_413)
    ],
    [
      'a refused desk-level file',
      () =>
        effectiveConfig(undefined, undefined, undefined, {
          path: DESK_PATH,
          present: true,
          decoded: decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1, nope: true }), 'desk')
        })
    ]
  ])('carries no paragraph with %s', (_state, build) => {
    const { container } = renderAdmin(build())
    const long = narrationIn(container)
    expect(long, long.map((each) => `${each.where}: ${each.says}`).join(' | ')).toEqual([])
  })

  /** **And over each open section, and each Connections tab.** */
  it.each([
    ...ADMIN_SECTIONS.map((section) => [section.id] as const),
    ['connections-ai'] as const,
    ['connections-search'] as const
  ])(
    'carries no paragraph with %s open',
    (id) => {
      const { container } = renderAdmin(everythingConfigured(), `/admin#${id}`)
      const long = narrationIn(container)
      expect(long, long.map((each) => `${each.where}: ${each.says}`).join(' | ')).toEqual([])
    }
  )

  it('carries no paragraph in the right pane, on either file', async () => {
    for (const path of ['/admin', '/admin#storage', '/admin#connections-ai']) {
      const { container } = renderInShell(everythingConfigured(), path)
      await details()
      await waitFor(() => expect(within(inspector()).getAllByRole('heading', { level: 2 }).length).toBeGreaterThan(0))
      const pane = container.querySelector<HTMLElement>('.desk-inspector')!
      const long = narrationIn(pane)
      expect(long, `${path}: ${long.map((each) => each.says).join(' | ')}`).toEqual([])
      cleanup()
    }
  })

  it.each([
    ['a connection that is still opening', { status: 'connecting' as const, client: null }],
    ['a connection being retried', { status: 'reconnecting' as const, client: null, attempt: 4 }],
    ['a connection that failed', { status: 'failed' as const, client: null, server: null }],
    [
      'a tool listing that did not answer',
      { known: false, capabilitiesError: new Error('the runtime did not answer list_tools') }
    ]
  ])('carries no paragraph with %s', (_state, mcp) => {
    const { container } = renderAdmin(effectiveConfig(undefined), '/admin', ROOT, mcp)
    const long = narrationIn(container)
    expect(long, long.map((each) => `${each.where}: ${each.says}`).join(' | ')).toEqual([])
  })

  it('carries no paragraph while a card is writing, refused, or holding a stale write', async () => {
    const FILE = `{\n  "deskConfigVersion": 1,\n  "organization": { "name": "Unveiled", "mark": null },\n  "storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/d/" } }\n}\n`
    const sweep = (container: HTMLElement, where: string) => {
      const long = narrationIn(container)
      expect(long, `${where}: ${long.map((each) => each.says).join(' | ')}`).toEqual([])
    }

    // A local decode refusal, which never leaves this page.
    servesAdmin(FILE, 'pending')
    const refused = renderLiveAdmin('/admin#storage')
    await waitFor(() => expect(screen.getByDisplayValue('https://acme.example/d/')).toBeTruthy())
    fireEvent.change(screen.getByLabelText('Pack folder'), { target: { value: '../escape' } })
    fireEvent.click(
      document.getElementById('pack-storage-title')!.closest('section')!.querySelector('form button[type="submit"]')!
    )
    await waitFor(() => expect(statusOf('pack-storage')).toContain('not written:'))
    sweep(refused.container, 'a refusal this page made')
    cleanup()
    vi.unstubAllGlobals()

    // A write in the air.
    servesAdmin(FILE, 'pending')
    const pending = renderLiveAdmin('/admin#general')
    await waitFor(() => expect(screen.getByDisplayValue('Unveiled')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('Unveiled'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('branding-title')!.closest('section')!.querySelector('form button[type="submit"]')!
    )
    await waitFor(() => expect(statusOf('branding')).toContain('writing'))
    sweep(pending.container, 'a write in the air')
    cleanup()
    vi.unstubAllGlobals()

    // The file moved underneath the write, with the digests disclosed.
    servesAdmin(FILE, 'stale')
    const stale = renderLiveAdmin('/admin#general')
    await waitFor(() => expect(screen.getByDisplayValue('Unveiled')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('Unveiled'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('branding-title')!.closest('section')!.querySelector('form button[type="submit"]')!
    )
    await waitFor(() => expect(statusOf('branding')).toContain('changed on disk'))
    sweep(stale.container, 'a stale write')
    for (const summary of stale.container.querySelectorAll('details summary')) {
      fireEvent.click(summary)
    }
    sweep(stale.container, 'a stale write, disclosed')
  })

  it('sweeps a paragraph split into short spans, which a node-length rule misses', () => {
    const { container } = renderAdmin()
    const paragraph = document.createElement('p')
    for (const half of ['x'.repeat(80), 'y'.repeat(80)]) {
      const span = document.createElement('span')
      span.textContent = half
      paragraph.append(span)
    }
    container.append(paragraph)
    const found = narrationIn(container)
    expect(found).toHaveLength(1)
    expect(found[0]!.where).toBe('p')
    expect(found[0]!.length).toBe(160)
  })

  it('leaves a long path alone, because a quotation is not narration', () => {
    const { container } = renderAdmin()
    const paragraph = document.createElement('p')
    const quotation = document.createElement('code')
    quotation.textContent = '/'.padEnd(300, 'a')
    paragraph.append(quotation)
    container.append(paragraph)
    expect(narrationIn(container)).toEqual([])
  })
})

/**
 * The page's controls, as rendered rather than as written: which elements are
 * actually on the page, and which class each of them came out carrying.
 */
describe('every control on Admin comes through the same component', () => {
  const buttonsOf = (element: HTMLElement) => Array.from(element.querySelectorAll('button'))

  it('renders every button through Button, and every picker through Select', () => {
    // The shared AI settings, because they carry every shape: three pickers,
    // a probe, a listing and a Save.
    renderAdmin(effectiveConfig(undefined), '/admin#connections-ai')
    const buttons = buttonsOf(screen.getByRole('tabpanel', { name: 'AI' }))
    expect(buttons.length).toBeGreaterThan(4)
    const triggers = buttons.filter((each) => each.getAttribute('role') === 'combobox')
    expect(triggers.length).toBeGreaterThan(0)
    for (const trigger of triggers) {
      expect(trigger.classList.contains(selectStyles.trigger), trigger.textContent ?? '').toBe(true)
    }
    const actions = buttons.filter((each) => each.getAttribute('role') !== 'combobox')
    expect(actions.length).toBeGreaterThan(0)
    for (const action of actions) {
      expect(
        action.classList.contains(buttonStyles.button),
        `${action.textContent?.trim()} is not a Button`
      ).toBe(true)
    }
  })

  it('carries at most one primary button per section, and none in the column', () => {
    for (const path of [
      '/admin',
      '/admin#general',
      '/admin#storage',
      '/admin#assistant',
      '/admin#research',
      '/admin#safeguards',
      '/admin#connections',
      '/admin#connections-ai',
      '/admin#gateway'
    ]) {
      const { container } = renderAdmin(effectiveConfig(undefined), path)
      const sections = Array.from(page(container).querySelectorAll('section'))
      expect(sections.length, path).toBeGreaterThan(0)
      for (const section of sections) {
        const own = Array.from(section.querySelectorAll('button')).filter(
          (button) => button.closest('section') === section
        )
        const primaries = own.filter(
          (button) =>
            button.classList.contains(buttonStyles.primary) &&
            button.closest('[role="alert"]') === null
        )
        const title = section.querySelector('h2, h3')?.textContent ?? '(untitled)'
        expect(primaries.length, `${path} ${title}: ${primaries.map((each) => each.textContent).join(', ')}`)
          .toBeLessThanOrEqual(1)
      }
      expect(rail(container).querySelectorAll('button'), path).toHaveLength(0)
      cleanup()
    }
  })

  it('puts the nomination on its own row, beside the value it changes', () => {
    renderAdmin(chassisOnly())
    const nomination = screen.getByRole('button', { name: 'Open this project at startup' })
    const row = screen.getByText('Startup project').parentElement!
    expect(row.contains(nomination)).toBe(true)
    expect(row.textContent).toContain('None')
    expect(nomination.classList.contains(buttonStyles.secondary)).toBe(true)
    expect(nomination.classList.contains(buttonStyles.primary)).toBe(false)
  })
})

// Bookmarked links from before Documents was renamed still open its settings.
it('opens Document processing for the legacy Documents fragment', () => {
  renderAdmin(effectiveConfig(undefined), '/admin#documents')
  expect(screen.getByRole('heading', { name: 'Document processing', level: 2 })).toBeTruthy()
  expect(screen.queryByRole('textbox', { name: 'Pack folder' })).toBeNull()
})
