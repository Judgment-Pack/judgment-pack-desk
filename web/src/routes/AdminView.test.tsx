/**
 * Admin: an overview, one open section, and the file in the right pane.
 *
 * **What changed shape here, and why each change is written down.** The page
 * used to render every section at once, so most of this file could reach a form
 * by rendering `/admin` and looking for it. It renders an overview now, and a
 * section is open only where the fragment names it — so every case about a
 * form opens that form's own address, and the cases that were *about* the
 * stack are rewritten rather than adjusted:
 *
 * - **the order case** asserted a heading per section; the overview has no
 *   section headings at all, and what it has is one row per section, in order,
 *   each a link to that section's fragment. It is the same claim about the same
 *   derived list.
 * - **the location case** said each file's path is stated once, in its group,
 *   and never on a card; there are no cards on the overview, so it says never
 *   on a row — and adds the rail beside an open section, which states no path
 *   at all because the pane does.
 * - **the write cases** drove two forms in one render. One section is open at a
 *   time now, so each drives its own, and the overview is asserted to carry
 *   neither.
 * - **the Content cases** are gone from this file: the bytes are in the right
 *   pane, `ConfigPane.test.tsx` holds what may be quoted, and what is here is
 *   that Admin publishes the right file into the pane, releases it on leaving,
 *   and carries no disclosure in the main column at all.
 * - **the controls case** was the whole list of what on the page changes
 *   anything, in one render. It is now the whole list per state — the overview
 *   and each of the four sections — which is the same rule over five renders.
 *
 * The narration sweep is unchanged as a rule and wider in reach: every state of
 * the configuration, every state of the connection, each open section, and the
 * pane.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture, DeskConfigProvider } from '../config/DeskConfigProvider'
import { STORAGE_KIND_SAYS, decodeDeskConfig, effectiveConfig } from '../config/deskConfig'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { AppShell } from '../shell/AppShell'
import { ShellStateProvider } from '../shell/paneState'
import { INSPECTOR_DRAWER_BELOW } from '../shell/useMediaQuery'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { narrationIn } from '../admin/narration'
import { NO_BYTES_SAYS } from '../admin/ConfigPane'
import { SECTION_SUMMARY } from '../admin/sectionSummary'
import buttonStyles from '../ui/Button.module.css'
import selectStyles from '../ui/Select.module.css'
import { AdminView } from './AdminView'
import { ADMIN_GROUPS, ADMIN_SECTIONS } from './adminSections'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.localStorage.clear()
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

/**
 * `null` means "the chassis has not answered yet". Not `undefined`: a default
 * parameter takes over for an explicit `undefined`, so the provisional case
 * silently got the resolved root and asserted nothing.
 */
function renderAdmin(
  value = effectiveConfig(undefined),
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
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

/**
 * The same page inside the real shell, which is the only place the right pane
 * exists.
 *
 * `renderAdmin` above renders the route alone: `useInspectorSlot()` reads the
 * context's closed default there, so nothing is published and no pane is
 * asserted by accident. A case about the pane needs the frame that holds the
 * slot, and one about *leaving* needs a second route to leave to — so the
 * harness is a router whose element switches on the address and whose
 * `AppShell` does not remount when it does.
 */
function renderInShell(value = effectiveConfig(undefined), path = '/admin') {
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
          <McpContext.Provider value={connected({ client: QUIET.client })}>
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
  // **Opened from the existing control**, which is also the claim: Admin
  // publishes into the slot whether or not the pane is showing, and a closed
  // pane is `hidden`, so nothing inside it is in the accessibility tree for a
  // role query to find. Nothing here reveals the pane on the page's behalf.
  fireEvent.click(screen.getByRole('button', { name: 'Inspector' }))
  return { ...view, router }
}

/** The desk's own page, which is the `<article>` and not the shell around it. */
function page(container: HTMLElement): HTMLElement {
  return container.querySelector('article')!
}

/** The overview's rows, in the order they are rendered. */
function rowsIn(container: HTMLElement): HTMLAnchorElement[] {
  return Array.from(page(container).querySelectorAll<HTMLAnchorElement>('a[href^="/admin#"]'))
}

/** Every row's title, which is its first line and never its summary. */
function rowTitles(container: HTMLElement): (string | null)[] {
  return rowsIn(container).map((row) => row.firstElementChild?.textContent ?? null)
}

/** What one row says the setting currently is, or nothing where it says none. */
function rowSays(container: HTMLElement, id: string): string | null {
  const row = rowsIn(container).find((each) => each.getAttribute('href') === `/admin#${id}`)!
  return row.children.length > 1 ? (row.children[1]!.textContent ?? null) : null
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

/** One section's or group's Status row, or nothing where it states none. */
function statusOf(id: string): string | null {
  const card = document.getElementById(id)!.closest('section')!
  const row = Array.from(card.querySelectorAll(':scope > dl > div')).find(
    (each) => each.querySelector('dt')?.textContent === 'Status'
  )
  return row?.querySelector('dd')?.textContent ?? null
}

/**
 * One sentence, as a pattern that matches it inside a longer hint.
 *
 * The Packs-go-to hint states what the file listing established **and** the
 * decoder's rule for the field, so an exact-text query would be asserting that
 * the second half is absent.
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

/** Both files read, every section carrying something the overview can summarise. */
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
        engine: 'builtin',
        thinking: 'ultra'
      },
      identity: {
        provider: { label: 'Acme SSO', issuer: 'https://issuer.example', clientId: 'a' }
      }
    }),
    PROJECT_TEXT
  )
}

describe('the Admin overview', () => {
  it('renders every group and one row per section, in order, as links to their sections', () => {
    // **The order case, in the shape the overview has.** It fails if a group or
    // a section is added without being declared, declared without being
    // rendered, or rendered out of order — and a row is a link to that
    // section's own fragment, which is the address the rail and the user menu
    // have been sending readers to since these were headings.
    const { container } = renderAdmin()
    const groups = screen
      .getAllByRole('heading', { level: 2 })
      .map((heading) => heading.textContent)
    expect(groups).toEqual(ADMIN_GROUPS.map((group) => group.title))
    expect(groups).toEqual(['This project', 'This desk'])
    expect(rowTitles(container)).toEqual(ADMIN_SECTIONS.map((section) => section.title))
    expect(rowsIn(container).map((row) => row.getAttribute('href'))).toEqual(
      ADMIN_SECTIONS.map((section) => `/admin#${section.id}`)
    )
    // Every row is inside the group whose file its section is a member of.
    for (const group of ADMIN_GROUPS) {
      const section = document.getElementById(group.id)!.closest('section')!
      expect(
        Array.from(section.querySelectorAll<HTMLAnchorElement>('a[href^="/admin#"]')).map(
          (row) => row.getAttribute('href')
        ),
        group.title
      ).toEqual(group.sections.map((each) => `/admin#${each.id}`))
    }
    // And no form: the overview is what each setting *is*, not where it is set.
    expect(page(container).querySelector('form')).toBeNull()
  })

  it('says what each setting currently is, from the decoded configuration', () => {
    const { container } = renderAdmin(everythingConfigured())
    expect(rowSays(container, 'organization')).toBe('Acme')
    expect(rowSays(container, 'storage')).toBe('filesystem · decisions')
    expect(rowSays(container, 'assistant')).toBe('gemini · a-model · thinking ultra')
    expect(rowSays(container, 'identity-provider')).toBe('https://issuer.example')
  })

  it('says the one word for nothing configured, and never a name of its own', () => {
    // A summary that fell back to a name this page composed — the project's
    // directory, the desk's own brand — would be a value nobody wrote, and a
    // reader would have no way to tell it from one that is in the file.
    const { container } = renderAdmin()
    expect(rowSays(container, 'organization')).toBe('none')
    expect(rowSays(container, 'assistant')).toBe('none')
    expect(rowSays(container, 'identity-provider')).toBe('None')
    // The two that always have a value have the built-in one, said as it is.
    expect(rowSays(container, 'storage')).toBe('filesystem · packs')
  })

  it('has a summary for every section it declares', () => {
    // Derived rather than listed: a section added to `adminSections.ts` without
    // a summary is a row that says only its title, which is the menu this page
    // stopped being.
    for (const section of ADMIN_SECTIONS) {
      expect(SECTION_SUMMARY[section.id], section.id).toBeDefined()
    }
  })

  it('carries no disclosure in the main column at all', () => {
    // The bytes are in the right pane. A `details` back on the page is the
    // stack this chunk took apart, one section at a time.
    const { container } = renderAdmin(everythingConfigured())
    expect(page(container).querySelectorAll('details')).toHaveLength(0)
    expect(page(container).querySelector('pre')).toBeNull()
  })

  it('states what this desk is running on a line, not as a card', async () => {
    // Two facts, neither of them a setting: the connection and the binary the
    // chassis was launched with, each one the connection's or the chassis' own
    // answer.
    const { container } = renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: false,
        sha256: '',
        chassis: {
          projectDir: '/real/a-project',
          projectFile: '/real/a-project/jpack-desk.json',
          runtimeBin: '/usr/local/bin/jpack'
        }
      })
    )
    const line = container.querySelector('dl')!
    expect(Array.from(line.querySelectorAll('dt')).map((each) => each.textContent)).toEqual([
      'Runtime',
      'Binary'
    ])
    await waitFor(() => expect(line.textContent).toContain('connected — '))
    expect(line.textContent).toContain('/usr/local/bin/jpack')
    // And it is not a card: no heading, no Location row, no Status row.
    expect(line.closest('section')).toBeNull()
  })

  it('reads the connection off its status, not off the runtime it last met', async () => {
    // `server` is retained across a reconnect — the provider spreads the
    // previous state — so a line that read "connected" off its presence said
    // so while the socket was down and the banner said the connection was
    // lost. The name is only said where the connection is actually up.
    const { container } = renderAdmin(effectiveConfig(undefined), '/admin', ROOT, {
      status: 'reconnecting',
      client: null,
      attempt: 3
    })
    const line = container.querySelector('dl')!
    await waitFor(() => expect(line.textContent).toContain('reconnecting'))
    expect(line.textContent).not.toContain('connected —')
    expect(line.textContent).not.toContain('jpack')
    cleanup()

    renderAdmin(effectiveConfig(undefined), '/admin', ROOT, { status: 'failed', client: null })
    expect(document.querySelector('dl')!.textContent).toContain('not connected')
  })

  it('names the runtime it is connected to where it actually is', async () => {
    const { container } = renderAdmin()
    const line = container.querySelector('dl')!
    await waitFor(() => expect(line.textContent).toContain('connected — jpack test'))
  })

  it('names neither configuration file on the line, because the groups do', () => {
    // One path, one statement. The group header is the one that earns it: it
    // is the file the rows under it name, and the grouping exists so that a
    // file is named once rather than on every section that is a member of it.
    const { container } = renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: false,
        sha256: '',
        chassis: {
          projectDir: '/real/a-project',
          projectFile: '/real/a-project/jpack-desk.json',
          runtimeBin: '/usr/local/bin/jpack'
        }
      })
    )
    const line = container.querySelector('dl')!
    expect(line.textContent).not.toContain('/real/a-project/jpack-desk.json')
    expect(line.textContent).not.toContain(DESK_PATH)
    // Each is stated once as a location, and it is its group's own header.
    for (const [id, path] of [
      ['this-project', '/real/a-project/jpack-desk.json'],
      ['this-desk', DESK_PATH]
    ] as const) {
      const header = document.getElementById(id)!.closest('section')!.querySelector(':scope > dl')!
      expect(header.textContent, id).toContain(path)
    }
    // The desk-level path appears once more, and it is not a location: the
    // nomination's own line names the file that **control** writes, which is
    // not the file the group it sits in is about.
    const quoted = Array.from(container.querySelectorAll('code')).filter(
      (each) => each.textContent === DESK_PATH
    )
    expect(quoted).toHaveLength(2)
    const rule = quoted.map((each) => each.closest('p')).find((each) => each !== null)!
    expect(rule.textContent).toContain('used on the next launch')
    // And that one is inside the *project* group, which is where the control is.
    expect(rule.closest('section')!.querySelector('h2')!.id).toBe('this-project')
  })

  it('states each file’s location once, in its group, and not on the rows', () => {
    const { container } = renderAdmin(
      effectiveConfig(undefined, 'no configuration was read: no such file', undefined, {
        path: DESK_PATH,
        present: false,
        sha256: ''
      })
    )
    // Inside the sections, so the status line's own pairs — which are not a
    // Location and a Status — are not counted as either.
    const rows = Array.from(container.querySelectorAll('section dt')).map(
      (each) => each.textContent
    )
    // One Location per group, and none at all on the four rows under them.
    expect(rows.filter((label) => label === 'Location')).toHaveLength(ADMIN_GROUPS.length)
    expect(rows.slice(0, 2)).toEqual(['Location', 'Status'])
    for (const group of ADMIN_GROUPS) {
      const header = document.getElementById(group.id)!.closest('section')!
      const locations = Array.from(header.querySelectorAll('dt')).filter(
        (each) => each.textContent === 'Location'
      )
      expect(locations, group.title).toHaveLength(1)
      // And it is the group's own: the one Location is above the rows rather
      // than inside one of them.
      expect(locations[0]!.closest('section')).toBe(header)
    }
    // The desk-level file is named once inside the group that is about it.
    const deskGroup = document.getElementById('this-desk')!.closest('section')!
    expect(
      Array.from(deskGroup.querySelectorAll('code')).filter(
        (each) => each.textContent === DESK_PATH
      )
    ).toHaveLength(1)
    // And an absent file is absent, never "read".
    expect(screen.getAllByText('not present — defaults in use').length).toBeGreaterThan(0)
  })

  it('keeps a section’s own status on its row where it differs from its group’s', () => {
    // The whole file is absent, so every row says exactly what its group says
    // and none of them says it twice.
    const { container } = renderAdmin(
      effectiveConfig(undefined, 'no configuration was read: no such file', undefined, {
        path: DESK_PATH,
        present: false,
        sha256: ''
      })
    )
    const statuses = Array.from(container.querySelectorAll('section dt')).filter(
      (each) => each.textContent === 'Status'
    )
    expect(statuses).toHaveLength(ADMIN_GROUPS.length)
    for (const section of ADMIN_SECTIONS) {
      const row = rowsIn(container).find(
        (each) => each.getAttribute('href') === `/admin#${section.id}`
      )!
      expect(row.children.length, section.id).toBe(2)
    }
    cleanup()

    // A member the *other* file supplied is not one this group's header speaks
    // for: its row states its own status again.
    const second = renderAdmin(
      effectiveConfig(
        decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1 }), 'project'),
        undefined,
        undefined,
        {
          path: DESK_PATH,
          present: true,
          sha256: '',
          decoded: decodeDeskConfig(
            JSON.stringify({
              deskConfigVersion: 1,
              storage: { packs: { dir: 'elsewhere' } }
            }),
            'desk'
          )
        }
      )
    )
    const storage = rowsIn(second.container).find(
      (each) => each.getAttribute('href') === '/admin#storage'
    )!
    expect(storage.children).toHaveLength(3)
    expect(storage.children[2]!.textContent).toBe('read')
  })

  it('carries exactly the state-changing controls the overview names, and no others', () => {
    // The whole list rather than a count, so a control cannot be added without
    // appearing here. The overview offers one, and it is the nomination: no
    // form is on this page, so no Save is either.
    const { container } = renderAdmin()
    const interactive = page(container).querySelectorAll('button, input, select, textarea')
    expect(Array.from(interactive).map((element) => element.textContent?.trim())).toEqual([
      'Use this project as the default'
    ])
    expect(screen.queryByLabelText('Default project')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    // **One control is disabled here, and it is not a permanent one.** This
    // fixture is the state in which nothing asked for the desk-level file, so
    // this page has never seen the bytes a write would replace.
    expect(
      Array.from(container.querySelectorAll('button[disabled]')).map(
        (element) => element.textContent
      )
    ).toEqual(['Use this project as the default'])
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
      name: 'Use this project as the default'
    }) as HTMLButtonElement
    expect(nominate.disabled).toBe(true)
    expect(screen.getByText(/has not said where its own configuration file is/)).toBeTruthy()
  })

  it('names the file the group’s control writes, which is not the one it shows', () => {
    // The group's Location and Status are about the project's own file; its one
    // control writes the desk-level one. The line under the control names that
    // file, from the chassis' answer and never composed.
    renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: false,
        sha256: '',
        chassis: {
          projectDir: '/this/launch',
          projectFile: '/this/launch/jpack-desk.json',
          runtimeBin: 'jpack'
        }
      })
    )
    const project = document.getElementById('this-project')!.closest('section')!
    // Location: the project's own file.
    expect(project.querySelector('dd')!.textContent).toBe('/this/launch/jpack-desk.json')
    // The control's own line: the desk-level file it writes, and this launch.
    const rule = project.querySelector('p')!.textContent ?? ''
    expect(rule).toContain(DESK_PATH)
    expect(rule).toContain('used on the next launch without a directory')
    expect(rule).toContain('/this/launch')
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
    // setting: this card wrote them into the project's own file, so one viewer
    // choosing dark chose it for everyone who ever cloned it. They are in the
    // user menu now — and what left with the card is its Save, which is the
    // half a deleted form leaves behind.
    const { container } = renderAdmin()
    expect(document.getElementById('appearance')).toBeNull()
    expect(screen.queryByLabelText('Theme')).toBeNull()
    expect(screen.queryByLabelText('Density')).toBeNull()
    expect(container.textContent).not.toContain('Appearance')
    // The member itself is untouched: still in the schema, still decoded, and
    // still the default for everyone who has not chosen.
    expect(effectiveConfig(undefined).config.appearance).toEqual({
      theme: 'system',
      density: 'comfortable'
    })
  })

  it('takes every location from the chassis, and composes none of them', () => {
    // A page that joined the reported directory to a file name would be
    // asserting a path on a filesystem it cannot see, and would be wrong the
    // first time a project was reached through a symlink — which is exactly
    // what the chassis resolves before it reports.
    renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: false,
        sha256: '',
        chassis: {
          projectDir: '/real/a-project',
          projectFile: '/real/a-project/jpack-desk.json',
          runtimeBin: '/usr/local/bin/jpack'
        }
      })
    )
    expect(screen.getAllByText('/real/a-project/jpack-desk.json').length).toBeGreaterThan(0)
    expect(screen.getAllByText('/usr/local/bin/jpack').length).toBeGreaterThan(0)
    // And never the project-relative name once the chassis has answered.
    expect(screen.queryByText('jpack-desk.json')).toBeNull()
  })

  it('says the desk has not said, rather than offering the name it reads the file by', () => {
    // `jpack-desk.json` is the address this page reads the file at, not an
    // established location on a filesystem — and the row is about where the
    // file **is**. Before `/api/desk-config` answers, and for ever where it
    // carries no chassis facts, the row says so instead of standing in.
    const { container } = renderAdmin()
    const header = document
      .getElementById('this-project')!
      .closest('section')!
      .querySelector(':scope > dl')!
    expect(header.textContent).toContain('the desk has not said')
    expect(container.textContent).not.toContain('jpack-desk.json')
  })

  it('names the file the chassis resolved the moment it answers', () => {
    renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: false,
        sha256: '',
        chassis: {
          projectDir: '/real/a-project',
          projectFile: '/real/a-project/jpack-desk.json',
          runtimeBin: 'jpack'
        }
      })
    )
    const header = document
      .getElementById('this-project')!
      .closest('section')!
      .querySelector(':scope > dl')!
    expect(header.textContent).toContain('/real/a-project/jpack-desk.json')
    expect(header.textContent).not.toContain('the desk has not said')
  })

  it('names a configuration that could not be read, and does not call it absent', () => {
    renderAdmin(effectiveConfig(undefined, undefined, CHASSIS_413))
    expect(screen.getAllByText(/not read — the desk answered/).length).toBeGreaterThan(0)
    expect(screen.getAllByText('the file is too large to read').length).toBeGreaterThan(0)
  })

  it('sources an unread reason to whoever actually said it', () => {
    // Three provenances, three sentences, and each one carried rather than
    // inferred. "The reason is the chassis' own" was false for a browser
    // error; "the request never got an answer" is false for a 200 whose body
    // this desk cannot use, which is what inferring from a status produced.
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

    // The case the inference got wrong: an answer arrived, and the sentence
    // about it is this desk's rather than the chassis'.
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
      problems: [{ key: 'colour', reason: 'unknown key' }]
    })
    renderAdmin(value)
    expect(screen.getAllByText('refused:', { exact: false }).length).toBeGreaterThan(0)
    expect(screen.getAllByText('colour: unknown key').length).toBeGreaterThan(0)
  })

  it('refuses the desk-level file on its own, without blaming the project one', () => {
    // Two files, two verdicts. A bad key in one must not be reported as the
    // other's, and neither is repaired by the other being fine.
    renderAdmin(
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
    )
    // The refusal says the thing that is actually wrong, in the decoder's words.
    expect(
      screen.getAllByText(/assistant.endpoint.apiKey: a key is never stored/).length
    ).toBeGreaterThan(0)
    // And the project file's own group does not report it.
    expect(screen.getAllByText('read').length).toBeGreaterThan(0)
  })
})

describe('one section at a time', () => {
  it('opens the section a fragment names, and marks its row current', () => {
    const { container } = renderAdmin(everythingConfigured(), '/admin#organization')
    expect(screen.getByRole('heading', { level: 2, name: 'Organization' })).toBeTruthy()
    const current = rowsIn(container).filter(
      (row) => row.getAttribute('aria-current') !== null
    )
    expect(current.map((row) => row.getAttribute('href'))).toEqual(['/admin#organization'])
    // The list is still whole beside it: every section is one click away.
    expect(rowTitles(container)).toEqual(ADMIN_SECTIONS.map((section) => section.title))
    // And only that section's form is on the page.
    expect(screen.getByDisplayValue('Acme')).toBeTruthy()
    expect(screen.queryByLabelText('Packs go to')).toBeNull()
  })

  it('round-trips: a row opens its section, and All settings clears the fragment', () => {
    const { container } = renderAdmin(everythingConfigured())
    expect(screen.queryByRole('heading', { level: 2, name: 'Storage' })).toBeNull()
    fireEvent.click(
      rowsIn(container).find((row) => row.getAttribute('href') === '/admin#storage')!
    )
    expect(screen.getByRole('heading', { level: 2, name: 'Storage' })).toBeTruthy()
    expect(screen.getByLabelText('Packs go to')).toBeTruthy()

    fireEvent.click(screen.getByRole('link', { name: 'All settings' }))
    expect(screen.queryByRole('heading', { level: 2, name: 'Storage' })).toBeNull()
    expect(screen.queryByLabelText('Packs go to')).toBeNull()
    expect(rowsIn(container).some((row) => row.getAttribute('aria-current') !== null)).toBe(false)
  })

  it('leaves the section on Escape, and stays on it for a keystroke from a dialog', () => {
    renderAdmin(everythingConfigured(), '/admin#storage')
    // A dialog owns its own Escape — the Inspector's drawer form is one — so a
    // keystroke that reached the page from inside it is not this page's.
    const dialog = document.createElement('div')
    dialog.setAttribute('role', 'dialog')
    document.body.append(dialog)
    fireEvent.keyDown(dialog, { key: 'Escape' })
    expect(screen.getByRole('heading', { level: 2, name: 'Storage' })).toBeTruthy()
    dialog.remove()

    fireEvent.keyDown(document.body, { key: 'Escape' })
    expect(screen.queryByRole('heading', { level: 2, name: 'Storage' })).toBeNull()
    expect(screen.getByRole('heading', { level: 2, name: 'This project' })).toBeTruthy()
  })

  it('shows the overview for a fragment that names no section', () => {
    // The page a link that named nothing should land on. An error about a
    // section that does not exist would be a worse answer than the page the
    // reader asked for.
    const { container } = renderAdmin(everythingConfigured(), '/admin#not-a-section')
    expect(rowTitles(container)).toEqual(ADMIN_SECTIONS.map((section) => section.title))
    expect(rowsIn(container).some((row) => row.getAttribute('aria-current') !== null)).toBe(false)
    expect(page(container).querySelector('form')).toBeNull()
    cleanup()

    // A group is not a section, and neither is a fragment that is not valid
    // percent-encoding.
    for (const fragment of ['this-project', '%zz']) {
      const view = renderAdmin(everythingConfigured(), `/admin#${fragment}`)
      expect(view.container.querySelector('form'), fragment).toBeNull()
      cleanup()
    }
  })

  it('stacks the list above the section below the Inspector’s breakpoint, and bares the other rows', () => {
    // The shell is one column there and the Inspector is a drawer over the
    // page: a 14rem rail beside a form leaves neither of them room, and a
    // summary on every row is a second page of list above the thing the reader
    // opened.
    vi.stubGlobal('matchMedia', (query: string) => ({
      media: query,
      matches: query === INSPECTOR_DRAWER_BELOW,
      onchange: null,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent: () => false
    }))
    const { container } = renderAdmin(everythingConfigured(), '/admin#assistant')
    expect(rowTitles(container)).toEqual(ADMIN_SECTIONS.map((section) => section.title))
    // The open row keeps what it says; the rest are their titles and nothing.
    expect(rowSays(container, 'assistant')).toBe('gemini · a-model · thinking ultra')
    expect(rowSays(container, 'organization')).toBeNull()
    expect(rowSays(container, 'storage')).toBeNull()
    expect(rowSays(container, 'identity-provider')).toBeNull()
    // And the overview at the same width is the overview: nothing is collapsed
    // where there is no open section to make room for.
    cleanup()
    const overview = renderAdmin(everythingConfigured())
    expect(rowSays(overview.container, 'organization')).toBe('Acme')
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
    // The one kind, as the value it is.
    expect(screen.getByText('Kind').parentElement!.textContent).toContain('filesystem')
    expect(screen.getByDisplayValue('decisions')).toBeTruthy()
    // The prefix as it will actually be written — normalised at decode — so a
    // Save that does not touch it writes back what the file already means.
    expect(screen.getByDisplayValue('https://acme.example/d/')).toBeTruthy()
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
    // A substring: the hint states what the listing established and then the
    // decoder's own rule for the field, and both are meant to be there.
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
    // Pending, failed, incomplete and obstructed all rendered as "no file
    // under it yet — the first pack creates it". Three of those are not that,
    // and the last one is a promise a rename is the only way to keep.
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

  it('names the two future kinds in the decoder’s own words, and offers neither', () => {
    // The sentence is the one the decoder refuses `"database"` with, exported
    // and quoted rather than written again here: two answers about what is
    // available would be free to disagree, and the mutation table could break
    // one of them while the other went on saying it.
    renderAdmin(effectiveConfig(undefined), '/admin#storage')
    expect(screen.getByText(STORAGE_KIND_SAYS)).toBeTruthy()
    expect(STORAGE_KIND_SAYS).toContain('database')
    expect(STORAGE_KIND_SAYS).toContain('cloud storage')
  })

  it('renders the one storage kind as a value, and not as a control with one option', () => {
    // **A Select with one option is a control that cannot be operated.** It
    // looks like a choice and offers none, and it appears in every enumeration
    // of what on this page a reader can change. While the union has one member
    // the section says what the file says.
    const { container } = renderAdmin(effectiveConfig(undefined), '/admin#storage')
    const storage = document.getElementById('storage')!.closest('section')!
    expect(storage.querySelector('[role="combobox"]')).toBeNull()
    expect(storage.querySelector('select')).toBeNull()
    expect(screen.getByText('Kind').parentElement!.textContent).toContain('filesystem')
    // And nothing anywhere on the page offers the kind as an option.
    expect(container.textContent).toContain(STORAGE_KIND_SAYS)
  })

  it('says None where no identity provider is configured, and its issuer where one is', () => {
    renderAdmin(effectiveConfig(undefined), '/admin#identity-provider')
    // Off the field row: "None" is also what the overview's own row says about
    // a provider nobody configured.
    expect(screen.getByText('Provider').parentElement!.textContent).toContain('None')
    cleanup()
    renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: true,
        sha256: '',
        decoded: decodeDeskConfig(
          JSON.stringify({
            deskConfigVersion: 1,
            identity: {
              provider: {
                label: 'Acme SSO',
                issuer: 'https://issuer.example',
                clientId: 'a-client'
              }
            }
          }),
          'desk'
        )
      }),
      '/admin#identity-provider'
    )
    const provider = screen.getByText('Provider').parentElement!
    expect(provider.textContent).toContain('https://issuer.example')
    expect(provider.textContent).toContain('Acme SSO')
    // And no sentence about what a provider will do later.
    expect(screen.queryByText(/gates nothing/)).toBeNull()
  })

  it('carries exactly the controls each open section names, and no others', () => {
    // The controls case, per state. The overview's own list is above; these
    // four are the whole of what the rest of the page can change.
    const controls = (container: HTMLElement) =>
      Array.from(page(container).querySelectorAll('button, input, select, textarea'))
        .filter((element) => element.getAttribute('role') !== 'combobox')
        .map((element) => element.textContent?.trim())
        .filter((label) => label !== '')
    for (const [fragment, expected] of [
      ['organization', ['Save']],
      ['storage', ['Save']],
      [
        'assistant',
        [
          'Check reachability',
          'OpenAI-compatibleAnthropicGemini',
          'List models',
          'vercelbuiltin',
          'offonultra',
          'Save'
        ]
      ],
      ['identity-provider', []]
    ] as const) {
      const view = renderAdmin(effectiveConfig(undefined), `/admin#${fragment}`)
      expect(controls(view.container), fragment).toEqual(expected)
      // The nomination is the overview's, and it is not carried into a section.
      expect(
        screen.queryByRole('button', { name: 'Use this project as the default' }),
        fragment
      ).toBeNull()
      cleanup()
    }
  })

  it('offers the assistant’s three pickers, in two shapes, and no fourth', () => {
    const { container } = renderAdmin(effectiveConfig(undefined), '/admin#assistant')
    const triggers = Array.from(container.querySelectorAll('[role="combobox"]')).map(
      (element) => element.textContent
    )
    expect(triggers).toEqual(['OpenAI-compatible', 'vercel', 'off'])
    const offered = Array.from(container.querySelectorAll('select')).map(
      (element) => element.textContent
    )
    expect(offered).toEqual(['OpenAI-compatibleAnthropicGemini', 'vercelbuiltin', 'offonultra'])
    // No password field in this render: no endpoint is configured, so there is
    // nothing to bind a key to and the row says so instead of offering one.
    expect(container.querySelectorAll('input[type="password"]')).toHaveLength(0)
    for (const conditional of ['Store key', 'Remove key']) {
      expect(
        Array.from(container.querySelectorAll('button')).map((each) => each.textContent),
        conditional
      ).not.toContain(conditional)
    }
  })

  it('enables a Save once the file behind it has been read', () => {
    // A Save states the bytes it replaces, so a section this desk has never
    // read the file for offers a control it cannot use — and says which file.
    renderAdmin(effectiveConfig(undefined), '/admin#organization')
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/has not read this project/)).toBeTruthy()
    cleanup()

    renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: false,
        sha256: '',
        chassis: {
          projectDir: '/this/launch',
          projectFile: '/this/launch/jpack-desk.json',
          runtimeBin: 'jpack'
        }
      }),
      '/admin#assistant'
    )
    // The desk-level file has been read, so the assistant's Save says nothing
    // about not having read it.
    expect(screen.queryByText(/has not read its own configuration file/)).toBeNull()
  })

  it('scrolls to the section a fragment names', () => {
    const scrolled: string[] = []
    const original = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = function scrollIntoView(this: Element) {
      scrolled.push(this.id)
    }
    try {
      renderAdmin(effectiveConfig(undefined), '/admin#storage')
      expect(scrolled).toContain('storage')
    } finally {
      Element.prototype.scrollIntoView = original
    }
  })
})

describe('a section’s own write', () => {
  const FILE = `{\n  "deskConfigVersion": 1,\n  "organization": { "name": "Unveiled", "mark": null },\n  "storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/d/" } }\n}\n`

  it('says the write is in the air on the section that is doing it', async () => {
    servesAdmin(FILE, 'pending')
    renderLiveAdmin('/admin#organization')
    // The read has to land first: a Save pressed before it is refused for
    // having no bytes to write over, which is a different state.
    await waitFor(() => expect(screen.getByDisplayValue('Unveiled')).toBeTruthy())
    // While nothing is happening the section repeats nothing: the file was
    // read, and that is the group's sentence rather than this one's.
    expect(statusOf('organization')).toBeNull()

    fireEvent.change(screen.getByDisplayValue('Unveiled'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('organization')!.closest('section')!.querySelector('form button')!
    )
    await waitFor(() => expect(statusOf('organization')).toContain('writing'))
    expect(statusOf('organization')).toBe('writing — nothing is written until the desk answers')
    // And no card that did nothing says anything about a write: Admin carries
    // no Appearance section at all any more.
    expect(document.getElementById('appearance')).toBeNull()
  })

  it('says what this page’s own decoder refused, on the section it refused it for', async () => {
    servesAdmin(FILE, 'pending')
    renderLiveAdmin('/admin#storage')
    // The read has to land first, and the id prefix is the field that shows
    // it: `dir` is the built-in value in this file, so waiting on it would be
    // waiting for nothing to change.
    await waitFor(() => expect(screen.getByDisplayValue('https://acme.example/d/')).toBeTruthy())
    fireEvent.change(screen.getByLabelText('Packs go to'), { target: { value: '../escape' } })
    fireEvent.click(
      document.getElementById('storage')!.closest('section')!.querySelector('form button')!
    )
    await waitFor(() => expect(statusOf('storage')).toContain('not written:'))
    expect(statusOf('storage')).toContain('storage.packs.dir')
  })

  it('says the file moved under a section, on the section the write was refused for', async () => {
    servesAdmin(FILE, 'stale')
    renderLiveAdmin('/admin#organization')
    await waitFor(() => expect(screen.getByDisplayValue('Unveiled')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('Unveiled'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('organization')!.closest('section')!.querySelector('form button')!
    )
    await waitFor(() =>
      expect(statusOf('organization')).toBe('the file changed on disk — nothing was written')
    )
  })

  it('says on the overview what the file is, and nothing about a write nobody made', async () => {
    servesAdmin(FILE, 'pending')
    const { container } = renderLiveAdmin()
    await waitFor(() => expect(statusOf('this-project')).toBe('read'))
    for (const section of ADMIN_SECTIONS) {
      const row = rowsIn(container).find(
        (each) => each.getAttribute('href') === `/admin#${section.id}`
      )!
      expect(row.textContent, section.id).not.toContain('writing')
    }
  })
})

describe('what Admin puts in the right pane', () => {
  it('shows the whole project file on the overview, by the name it reads it at', async () => {
    const { container } = renderInShell(everythingConfigured())
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
    // at decode — it gains the separator it was missing — so a pane that
    // re-serialised would show a reader a member that is not the one in the
    // file, beside a Location row saying where that file is.
    renderInShell(everythingConfigured(), '/admin#storage')
    await waitFor(() =>
      expect(
        screen.getByRole('heading', { level: 2, name: 'jpack-desk.json › storage' })
      ).toBeTruthy()
    )
    expect(
      screen.getByText('{"packs": {"dir": "decisions", "idBase": "https://acme.example/d"}}')
    ).toBeTruthy()
    // The decode is on the field beside it, and says something else.
    expect(screen.getByDisplayValue('https://acme.example/d/')).toBeTruthy()
  })

  it('names the desk-level file for a member only that file carries', async () => {
    renderInShell(everythingConfigured(), '/admin#identity-provider')
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'desk.json › identity' })).toBeTruthy()
    )
    expect(screen.getByText(DESK_PATH)).toBeTruthy()
    expect(screen.getByText(/^sha256 dddddddddddd…$/)).toBeTruthy()
  })

  it('renders no bytes of a file the decoder refused, in the pane either', async () => {
    // **The refusal is about a member, and rendering the file anyway puts that
    // member on the surface that reported it.** One pane over is the same
    // disclosure.
    const refusedDesk = '{"deskConfigVersion":1,"identity":{"apiKey":"sk-live-secret"}}'
    renderInShell(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: true,
        text: refusedDesk,
        decoded: decodeDeskConfig(refusedDesk, 'desk')
      }),
      '/admin#identity-provider'
    )
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'desk.json › identity' })).toBeTruthy()
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
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'jpack-desk.json' })).toBeTruthy()
    )
    expect(document.body.textContent).not.toContain('sk-live-secret')
    expect(screen.getAllByText(/storage.apiKey: a key is never stored/).length).toBeGreaterThan(0)
  })

  it('renders no bytes of a file that could not be read at all', async () => {
    // There are none to render, and a pane that offered the decoded defaults
    // would be showing them as though they were the file.
    renderInShell(effectiveConfig(undefined, undefined, CHASSIS_413))
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 2, name: 'jpack-desk.json' })).toBeTruthy()
    )
    expect(document.querySelector('.desk-inspector pre')).toBeNull()
    expect(screen.getAllByText(/not read — the desk answered 413/).length).toBeGreaterThan(0)
    expect(screen.queryByText(NO_BYTES_SAYS)).toBeNull()
  })

  it('says the bytes could not be established rather than offering a decode', async () => {
    // The file was read and does not carry the member: `memberBytes` answers
    // nothing, and the pane says so instead of stringifying the value the
    // decoder computed from the defaults.
    const text = '{"deskConfigVersion": 1}'
    renderInShell(
      effectiveConfig(decodeDeskConfig(text, 'project'), undefined, undefined, undefined, text),
      '/admin#storage'
    )
    await waitFor(() => expect(screen.getByText(NO_BYTES_SAYS)).toBeTruthy())
    expect(document.querySelector('.desk-inspector pre')).toBeNull()
  })

  it('releases the claim when the route leaves, so the pane is the next one’s', async () => {
    const { router } = renderInShell(everythingConfigured())
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
   * 140 characters is a line; anything longer is a paragraph, and the page
   * that stood here had thirty of them. Quoted material is exempt — see
   * `narrationIn`. Parameterised because the first version rendered one
   * default state, so the configured assistant's sentences, a refusal and a
   * file that could not be read were never swept at all.
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
              engine: 'builtin',
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
      'this project already the default',
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
          problems: [{ key: 'colour', reason: 'unknown key' }]
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

  /**
   * **And over each open section**, which the overview does not render at all.
   *
   * The sweep above walks the list. Every form on this page is now behind a
   * fragment, so a sweep that only ever rendered `/admin` would have stopped
   * looking at the sections the moment they stopped being stacked on it.
   */
  it.each(ADMIN_SECTIONS.map((section) => [section.id] as const))(
    'carries no paragraph with %s open',
    (id) => {
      const { container } = renderAdmin(everythingConfigured(), `/admin#${id}`)
      const long = narrationIn(container)
      expect(long, long.map((each) => `${each.where}: ${each.says}`).join(' | ')).toEqual([])
    }
  )

  it('carries no paragraph in the right pane, on either file', async () => {
    for (const path of ['/admin', '/admin#storage', '/admin#assistant']) {
      const { container } = renderInShell(everythingConfigured(), path)
      await waitFor(() => expect(screen.getAllByRole('heading', { level: 2 }).length).toBeGreaterThan(0))
      const pane = container.querySelector<HTMLElement>('.desk-inspector')!
      const long = narrationIn(pane)
      expect(long, `${path}: ${long.map((each) => each.says).join(' | ')}`).toEqual([])
      cleanup()
    }
  })

  /**
   * **The states the configuration cannot express.**
   *
   * The sweep above builds an `EffectiveConfig` and renders it, so every state
   * it can reach is a state of the two files. The connection is not one of
   * those, and neither is anything a reader *does*: a save in the air, a file
   * that moved under one, a refusal. Those sentences are written in the same
   * components and were never swept.
   */
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

  it('carries no paragraph while a section is writing, refused, or holding a stale write', async () => {
    const FILE = `{\n  "deskConfigVersion": 1,\n  "organization": { "name": "Unveiled", "mark": null },\n  "storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/d/" } }\n}\n`
    const sweep = (container: HTMLElement, where: string) => {
      const long = narrationIn(container)
      expect(long, `${where}: ${long.map((each) => each.says).join(' | ')}`).toEqual([])
    }

    // A local decode refusal, which never leaves this page.
    servesAdmin(FILE, 'pending')
    const refused = renderLiveAdmin('/admin#storage')
    await waitFor(() => expect(screen.getByDisplayValue('https://acme.example/d/')).toBeTruthy())
    fireEvent.change(screen.getByLabelText('Packs go to'), { target: { value: '../escape' } })
    fireEvent.click(
      document.getElementById('storage')!.closest('section')!.querySelector('form button')!
    )
    await waitFor(() => expect(statusOf('storage')).toContain('not written:'))
    sweep(refused.container, 'a refusal this page made')
    cleanup()
    vi.unstubAllGlobals()

    // A write in the air.
    servesAdmin(FILE, 'pending')
    const pending = renderLiveAdmin('/admin#organization')
    await waitFor(() => expect(screen.getByDisplayValue('Unveiled')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('Unveiled'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('organization')!.closest('section')!.querySelector('form button')!
    )
    await waitFor(() => expect(statusOf('organization')).toContain('writing'))
    sweep(pending.container, 'a write in the air')
    cleanup()
    vi.unstubAllGlobals()

    // The file moved underneath the write, with the digests disclosed.
    servesAdmin(FILE, 'stale')
    const stale = renderLiveAdmin('/admin#organization')
    await waitFor(() => expect(screen.getByDisplayValue('Unveiled')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('Unveiled'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('organization')!.closest('section')!.querySelector('form button')!
    )
    await waitFor(() => expect(statusOf('organization')).toContain('changed on disk'))
    sweep(stale.container, 'a stale write')
    // And with the digests open, which is a disclosure of quoted material.
    for (const summary of stale.container.querySelectorAll('details summary')) {
      fireEvent.click(summary)
    }
    sweep(stale.container, 'a stale write, disclosed')
  })

  it('sweeps a paragraph split into short spans, which a node-length rule misses', () => {
    // The guard's own instrument, checked against the exact defeat the review
    // named: two eighty-character spans are a hundred and sixty characters of
    // prose to a reader and two compliant text nodes to a node-length rule.
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
 * The page's controls, as rendered rather than as written.
 *
 * `adminSheets.test.ts` reads the stylesheets, which is the only way to hold a
 * rule vitest never processes. These hold what no sheet can say: which elements
 * are actually on the page, and which class each of them came out carrying. A
 * `<button>` with no class renders as the browser's own control, and the sheet
 * that would have styled it is not the one that is missing — there is no sheet
 * at all.
 */
describe('every control on Admin comes through the same component', () => {
  /** Every button under the article, which is the whole page. */
  const buttonsOf = (container: HTMLElement) =>
    Array.from(page(container).querySelectorAll('button'))

  it('renders every button through Button, and every picker through Select', () => {
    // The assistant section, because it is the one that carries every shape:
    // three pickers, a probe, a listing and a Save.
    const { container } = renderAdmin(effectiveConfig(undefined), '/admin#assistant')
    const buttons = buttonsOf(container)
    // A count, so an exemption cannot quietly become the whole list.
    expect(buttons.length).toBeGreaterThan(4)
    // The one shape that is a `<button>` and is not an action: a Radix Select
    // trigger. It is named rather than skipped, so a bare button cannot hide
    // behind the exemption — each of these has to carry the Select module's
    // own class.
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

  it('carries at most one primary button per section, and none in a group’s head', () => {
    for (const path of ['/admin', '/admin#organization', '/admin#storage', '/admin#assistant']) {
      const { container } = renderAdmin(effectiveConfig(undefined), path)
      const sections = Array.from(page(container).querySelectorAll('section'))
      expect(sections.length, path).toBeGreaterThan(0)
      for (const section of sections) {
        // The section a button belongs to is its *closest* one: a member's Save
        // is the member's, not also its group's.
        const own = Array.from(section.querySelectorAll('button')).filter(
          (button) => button.closest('section') === section
        )
        // A Reload in a stale panel is an alert's one action and keeps its
        // primary, which is what makes "one per section" a rule about the page
        // rather than about every element on it.
        const primaries = own.filter(
          (button) =>
            button.classList.contains(buttonStyles.primary) &&
            button.closest('[role="alert"]') === null
        )
        const title = section.querySelector('h2, h3')?.textContent ?? '(untitled)'
        expect(primaries.length, `${path} ${title}: ${primaries.map((each) => each.textContent).join(', ')}`)
          .toBeLessThanOrEqual(1)
        // A group is the section that holds the rows, and its head is where the
        // file's own facts are stated — not where this page's loudest action
        // belongs. The nomination sits on its row as a secondary.
        if (section.querySelector('ul') !== null) {
          expect(primaries.length, `${path} ${title} is a group head`).toBe(0)
        }
      }
      cleanup()
    }
  })

  it('puts the nomination on its own row, beside the value it changes', () => {
    // Not in the head's action strip below the rows: a control over a value is
    // read where the value is.
    const { container } = renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: false,
        sha256: '',
        chassis: {
          projectDir: '/this/launch',
          projectFile: '/this/launch/jpack-desk.json',
          runtimeBin: 'jpack'
        }
      })
    )
    const nomination = screen.getByRole('button', { name: 'Use this project as the default' })
    const row = screen.getByText('Default project').parentElement!
    expect(row.contains(nomination)).toBe(true)
    expect(row.textContent).toContain('None')
    expect(nomination.classList.contains(buttonStyles.secondary)).toBe(true)
    expect(nomination.classList.contains(buttonStyles.primary)).toBe(false)
    // And the rule line naming the file it writes is still under it.
    expect(row.textContent).toContain('used on the next launch without a directory')
    expect(container.querySelector('article')).toBeTruthy()
  })
})
