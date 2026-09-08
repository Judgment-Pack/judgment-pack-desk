/**
 * Admin: two groups, five cards, one shape, and no narration.
 *
 * Four assertions carry this file. The **order** case fails if a group or a
 * section is added without being declared, declared without being rendered, or
 * rendered out of order. The **location** case is the whole point of the
 * grouping: each file's path is stated once, in its group's header, and a card
 * under one states neither it nor a status the header has already given. The
 * **narration sweep** fails on any text node over 140 characters that is not
 * quoted material — a path, a decoder's own refusal, a member of the file —
 * which is what "remove the narration" means as a rule rather than as a
 * preference. And the **controls** case is the whole list of what on this page
 * changes anything, so a control cannot be added without appearing here.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture, DeskConfigProvider } from '../config/DeskConfigProvider'
import { STORAGE_KIND_SAYS, decodeDeskConfig, effectiveConfig } from '../config/deskConfig'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { ShellStateProvider } from '../shell/paneState'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { narrationIn } from '../admin/narration'
import { AdminView } from './AdminView'
import { ADMIN_GROUPS, ADMIN_SECTIONS } from './adminSections'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.localStorage.clear()
})

const QUIET = stubClient({ list_packs: () => ({ text: JSON.stringify({ packs: [] }) }) })

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
 * A desk serving one project file, with the write left in whatever state a
 * case is about.
 *
 * `renderAdmin` above is a *fixture*: the configuration is handed in, so no
 * card has bytes to write over and every Save is disabled. These cases are
 * about what a card says while it is writing, so they need the real provider
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
      // A request that never answers is what "writing" is: the card must say
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
    status,
    statusText: '',
    text: async () => JSON.stringify(body)
  }
}

/** The same shell as `renderAdmin`, over the real configuration provider. */
function renderLiveAdmin() {
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
    { initialEntries: ['/admin'] }
  )
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

/** One card's Status row, or nothing where the card states none. */
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

/** One file listing, or a refusal, for the Storage card to describe. */
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

/** One desk-level read that answered, with whatever the file says. */
function deskRead(file: object) {
  return {
    path: DESK_PATH,
    present: true,
    sha256: 'a'.repeat(64),
    chassis: {
      projectDir: '/this/launch',
      projectFile: '/this/launch/jpack-desk.json',
      runtimeBin: 'jpack'
    },
    decoded: decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1, ...file }), 'desk')
  }
}

describe('the Admin page', () => {
  it('renders every group and every card in order, with those exact titles', () => {
    // The whole list, not a slice of it. It fails if a group or a section is
    // added without being declared, declared without being rendered, or
    // rendered out of order — and there is nothing under the heading but the
    // status line and the groups, so no sixth card can appear without being
    // declared as one.
    renderAdmin()
    const groups = screen
      .getAllByRole('heading', { level: 2 })
      .map((heading) => heading.textContent)
    expect(groups).toEqual(ADMIN_GROUPS.map((group) => group.title))
    expect(groups).toEqual(['This project', 'This desk'])
    const cards = screen
      .getAllByRole('heading', { level: 3 })
      .map((heading) => heading.textContent)
    expect(cards).toEqual(ADMIN_SECTIONS.map((section) => section.title))
    // A card is a subsection of the file it is a member of, and the outline
    // says so: every one of them is inside its group.
    for (const heading of screen.getAllByRole('heading', { level: 3 })) {
      expect(heading.closest('section')!.parentElement!.closest('section')).toBeTruthy()
    }
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
    // is the file the cards under it write, and the grouping exists so that a
    // file is named once rather than on every card that is a member of it.
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

  it('carries no paragraph while a card is writing, refused, or holding a stale write', async () => {
    const FILE = `{\n  "deskConfigVersion": 1,\n  "organization": { "name": "Unveil", "mark": null },\n  "storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/d/" } }\n}\n`
    const sweep = (container: HTMLElement, where: string) => {
      const long = narrationIn(container)
      expect(long, `${where}: ${long.map((each) => each.says).join(' | ')}`).toEqual([])
    }

    // A write in the air, and a local decode refusal beside it.
    servesAdmin(FILE, 'pending')
    const pending = renderLiveAdmin()
    await waitFor(() => expect(screen.getByDisplayValue('Unveil')).toBeTruthy())
    fireEvent.change(screen.getByLabelText('Packs go to'), { target: { value: '../escape' } })
    fireEvent.click(
      document.getElementById('storage')!.closest('section')!.querySelector('form button')!
    )
    fireEvent.change(screen.getByDisplayValue('Unveil'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('organization')!.closest('section')!.querySelector('form button')!
    )
    await waitFor(() => expect(statusOf('organization')).toContain('writing'))
    sweep(pending.container, 'writing, and a refusal beside it')
    cleanup()
    vi.unstubAllGlobals()

    // The file moved underneath the write, with the digests disclosed.
    servesAdmin(FILE, 'stale')
    const stale = renderLiveAdmin()
    await waitFor(() => expect(screen.getByDisplayValue('Unveil')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('Unveil'), { target: { value: 'Renamed' } })
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


  it('states each file’s location once, in its group, and not on the cards', () => {
    const { container } = renderAdmin(
      effectiveConfig(undefined, 'no configuration was read: no such file', undefined, {
        path: DESK_PATH,
        present: false,
        sha256: ''
      })
    )
    // Inside the cards and groups, so the status line's own pairs — which are
    // not a Location and a Status — are not counted as either.
    const rows = Array.from(container.querySelectorAll('section dt')).map(
      (each) => each.textContent
    )
    // One Location per group, and none at all on the five cards under them.
    expect(rows.filter((label) => label === 'Location')).toHaveLength(ADMIN_GROUPS.length)
    expect(rows.slice(0, 2)).toEqual(['Location', 'Status'])
    for (const group of ADMIN_GROUPS) {
      const header = document.getElementById(group.id)!.closest('section')!
      const locations = Array.from(header.querySelectorAll('dt')).filter(
        (each) => each.textContent === 'Location'
      )
      expect(locations, group.title).toHaveLength(1)
      // And it is the group's own, not a member's: the one Location is above
      // the members rather than inside one of them.
      expect(locations[0]!.closest('section')).toBe(header)
    }
    // The desk-level file is named once inside the group that is about it —
    // the header — and by neither of the two cards under it.
    const deskGroup = document.getElementById('this-desk')!.closest('section')!
    expect(
      Array.from(deskGroup.querySelectorAll('code')).filter(
        (each) => each.textContent === DESK_PATH
      )
    ).toHaveLength(1)
    // And an absent file is absent, never "read".
    expect(screen.getAllByText('not present — defaults in use').length).toBeGreaterThan(0)
  })

  /**
   * **A card's own write is part of what its Status says.**
   *
   * The group header suppresses a card's Status where it says what the group
   * already said, and the group's is the *file's* read state — so before this,
   * a card writing, refused, or holding a stale write showed no Status at all
   * while the group said `read`. Driven on the whole page rather than by
   * handing `SourceCard` two artificial statuses.
   */
  it('says what each card’s own write is doing, while the group says the file was read', async () => {
    const FILE = `{\n  "deskConfigVersion": 1,\n  "organization": { "name": "Unveil", "mark": null },\n  "storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/d/" } }\n}\n`
    servesAdmin(FILE, 'pending')
    renderLiveAdmin()
    // The read has to land first: a Save pressed before it is refused for
    // having no bytes to write over, which is a different state.
    await waitFor(() => expect(screen.getByDisplayValue('Unveil')).toBeTruthy())

    // The group read its file, and while nothing is happening no card repeats it.
    expect(statusOf('this-project')).toBe('read')
    expect(statusOf('organization')).toBeNull()
    expect(statusOf('storage')).toBeNull()

    // Storage: a value this page's own decoder refuses, so nothing is sent.
    fireEvent.change(screen.getByLabelText('Packs go to'), { target: { value: '../escape' } })
    fireEvent.click(
      document.getElementById('storage')!.closest('section')!.querySelector('form button')!
    )
    // Organization: a write that has left and not come back.
    fireEvent.change(screen.getByDisplayValue('Unveil'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('organization')!.closest('section')!.querySelector('form button')!
    )

    await waitFor(() => expect(statusOf('organization')).toContain('writing'))
    expect(statusOf('organization')).toBe('writing — nothing is written until the desk answers')
    expect(statusOf('storage')).toContain('not written:')
    expect(statusOf('storage')).toContain('storage.packs.dir')
    // And the group still says what it read, because that is still true.
    expect(statusOf('this-project')).toBe('read')
    // And no card that did nothing says anything about a write: Admin carries
    // no Appearance card at all any more.
    expect(document.getElementById('appearance')).toBeNull()
  })

  it('says the file moved under a card, on the card the write was refused for', async () => {
    const FILE = `{\n  "deskConfigVersion": 1,\n  "organization": { "name": "Unveil", "mark": null }\n}\n`
    servesAdmin(FILE, 'stale')
    renderLiveAdmin()
    await waitFor(() => expect(screen.getByDisplayValue('Unveil')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('Unveil'), { target: { value: 'Renamed' } })
    fireEvent.click(
      document.getElementById('organization')!.closest('section')!.querySelector('form button')!
    )
    await waitFor(() =>
      expect(statusOf('organization')).toBe('the file changed on disk — nothing was written')
    )
    expect(statusOf('this-project')).toBe('read')
    // The card that did not write says nothing about a write, only about the
    // member this file does not carry.
    expect(statusOf('storage')).toBe('not present — defaults in use')
  })

  it('keeps a card’s own Status where it differs from its group’s, and drops it where it does not', () => {
    // The whole file is absent, so every member says exactly what the group
    // says and none of them says it twice.
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
    cleanup()

    // A member the *other* file supplied is not one this group's header speaks
    // for: it states its own Location and its own Status again.
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
            JSON.stringify({
              deskConfigVersion: 1,
              storage: { packs: { dir: 'elsewhere' } }
            }),
            'desk'
          )
        }
      )
    )
    const storage = document.getElementById('storage')!.closest('section')!
    expect(
      Array.from(storage.querySelectorAll('dt')).map((each) => each.textContent)
    ).toEqual(['Location', 'Status'])
    expect(storage.textContent).toContain(DESK_PATH)
  })

  it('shows a member of the file as it is written, not as a decode of it', () => {
    // The bytes, not a re-serialisation of the decode. `idBase` is normalised
    // at decode — it gains the separator it was missing — and the defaults are
    // applied on top, so a disclosure that re-serialised would show a reader a
    // member that is not the one in the file.
    const text =
      '{\n  "deskConfigVersion": 1,\n  "storage": {"packs": {"idBase": "https://a.example/d"}}\n}'
    renderAdmin(effectiveConfig(decodeDeskConfig(text, 'project'), undefined, undefined, undefined, text))
    expect(screen.getByText('{"packs": {"idBase": "https://a.example/d"}}')).toBeTruthy()
    // The decode is on the field beside it, and says something else.
    expect(screen.getByDisplayValue('https://a.example/d/')).toBeTruthy()
  })

  it('names the storage kind, the location and the id prefix', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({
        deskConfigVersion: 1,
        storage: { packs: { dir: 'decisions', idBase: 'https://acme.example/d' } }
      }),
      'project'
    )
    renderAdmin(effectiveConfig(decoded))
    // The one kind, as the value it is.
    expect(screen.getByText('Kind').parentElement!.textContent).toContain('filesystem')
    expect(screen.getByDisplayValue('decisions')).toBeTruthy()
    // The prefix as it will actually be written — normalised at decode — so a
    // Save that does not touch it writes back what the file already means.
    expect(screen.getByDisplayValue('https://acme.example/d/')).toBeTruthy()
  })

  it('shows the built-in location and prefix where the project configured none', () => {
    renderAdmin()
    expect(screen.getByDisplayValue('packs')).toBeTruthy()
    expect(screen.getByDisplayValue('https://example.invalid/judgment-packs/')).toBeTruthy()
  })

  it('says a location holds files only where the listing shows one', async () => {
    // The listing reports regular files only, so the page can say a location
    // holds files and can never claim an empty one exists.
    servesListing({ files: [{ path: 'packs/a.pack.json', bytes: 1, sha256: 'aa' }] })
    renderAdmin()
    // A substring: the hint states what the listing established and then the
    // decoder's own rule for the field, and both are meant to be there.
    expect(await screen.findByText(/holds files/)).toBeTruthy()
  })

  it('says only that no file is under it, which is what the listing can show', async () => {
    servesListing({ files: [{ path: 'jpack.json', bytes: 1, sha256: 'aa' }] })
    renderAdmin()
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
      renderAdmin()
      expect(await screen.findByText(new RegExp(escaped(says))), says).toBeTruthy()
      cleanup()
    }
  })

  it('says the listing has not answered rather than describing what it has not seen', async () => {
    vi.stubGlobal('fetch', () => new Promise(() => {}))
    renderAdmin()
    expect(await screen.findByText(/the file listing has not answered yet/)).toBeTruthy()
  })

  it('names the two future kinds in the decoder’s own words, and offers neither', () => {
    // The sentence is the one the decoder refuses `"database"` with, exported
    // and quoted rather than written again here: two answers about what is
    // available would be free to disagree, and the mutation table could break
    // one of them while the other went on saying it.
    renderAdmin()
    expect(screen.getByText(STORAGE_KIND_SAYS)).toBeTruthy()
    expect(STORAGE_KIND_SAYS).toContain('database')
    expect(STORAGE_KIND_SAYS).toContain('cloud storage')
  })

  it('renders the one storage kind as a value, and not as a control with one option', () => {
    // **A Select with one option is a control that cannot be operated.** It
    // looks like a choice and offers none, and it appears in every enumeration
    // of what on this page a reader can change. While the union has one member
    // the card says what the file says.
    const { container } = renderAdmin()
    const storage = document.getElementById('storage')!.closest('section')!
    expect(storage.querySelector('[role="combobox"]')).toBeNull()
    expect(storage.querySelector('select')).toBeNull()
    expect(screen.getByText('Kind').parentElement!.textContent).toContain('filesystem')
    // And nothing anywhere on the page offers the kind as an option.
    expect(container.textContent).toContain(STORAGE_KIND_SAYS)
  })

  it('names no user management, roles, invitations or assignment anywhere', () => {
    const { container } = renderAdmin()
    const text = container.textContent ?? ''
    for (const absent of ['Invite', 'Add user', 'Assign', 'Members', 'Permissions']) {
      expect(text).not.toContain(absent)
    }
  })

  it('carries exactly the state-changing controls it names, and no others', () => {
    // The whole list rather than a count, so a control cannot be added without
    // appearing here. The Copy buttons are gone with the paste blocks: the
    // card's Location line says where the file is, and its Content shows what
    // is in it, so a second way to get the same JSON was a second way to do
    // one thing.
    const { container } = renderAdmin()
    const interactive = container.querySelectorAll('button, input, select, textarea')
    const labels = Array.from(interactive).map((element) => element.textContent?.trim())
    // The project group's control is a nomination and not a path: the page may
    // name the project it is already running in, or withdraw a default, and
    // nothing else — so there is one button and no field for a path.
    const writes: Record<string, number> = {
      // The assistant slot's, and one on each of the two cards that write a
      // member of the project's own file.
      Save: 3,
      'Check reachability': 1,
      'Use this project as the default': 1
    }
    for (const [label, count] of Object.entries(writes)) {
      expect(labels.filter((each) => each === label), label).toHaveLength(count)
    }
    expect(labels).not.toContain('Clear the default')
    expect(screen.queryByLabelText('Default project')).toBeNull()
    // Three controls appear only in a state this render is not in, and each is
    // asserted by its absence here and by its own case in the Assistant
    // section's suite.
    for (const conditional of ['Store key', 'Remove key']) {
      expect(labels, conditional).not.toContain(conditional)
    }
    // The form's three pickers, in two shapes, because a Radix Select is a
    // trigger button and a hidden native `<select>`.
    const triggers = Array.from(container.querySelectorAll('[role="combobox"]')).map(
      (element) => element.textContent
    )
    // This project's group offers no picker at all now — Storage's kind is a
    // value and not a control, and the two appearance choices are the user
    // menu's — so every one of these is this desk's, the assistant's three.
    expect(triggers).toEqual(['OpenAI-compatible', 'vercel', 'off'])
    const offered = Array.from(container.querySelectorAll('select')).map(
      (element) => element.textContent
    )
    expect(offered).toEqual([
      'OpenAI-compatibleAnthropicGemini',
      'vercelbuiltin',
      'offonultra'
    ])
    // Every other control is a tool checkbox, which changes nothing until Save.
    const picker = [...triggers, ...offered]
    const others = labels.filter(
      (label) =>
        label !== undefined &&
        !(label in writes) &&
        label !== 'List models' &&
        !picker.includes(label)
    )
    expect(others.every((label) => label === ''), others.join(' | ')).toBe(true)
    // No password field in this render: no endpoint is configured, so there is
    // nothing to bind a key to and the row says so instead of offering one.
    expect(container.querySelectorAll('input[type="password"]')).toHaveLength(0)
    // **One control is disabled here, and it is not a permanent one.** This
    // fixture is the state in which nothing asked for the desk-level file, so
    // this page has never seen the bytes a write would replace.
    const disabled = Array.from(container.querySelectorAll('button[disabled]')).map(
      (element) => element.textContent
    )
    // The project group's nomination first, then its two cards' Saves — which
    // have no bytes to write over and nothing typed to write — and then the
    // Assistant's, which has no digest to state.
    expect(disabled).toEqual([
      'Use this project as the default',
      'Save',
      'Save',
      'List models',
      'Save'
    ])
    // The desk-level file, on two cards; this project's own file, on two.
    expect(screen.getAllByText(/has not read its own configuration file/).length).toBe(2)
    expect(screen.getAllByText(/has not read this project/).length).toBe(2)
  })

  it('enables the two writes once the desk-level file has been read', () => {
    // The Project card's nomination needs one thing more than a digest: the
    // chassis' own path for this project, because that is the only value the
    // route will accept and the page must not compose one.
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
    expect(
      Array.from(container.querySelectorAll('button[disabled]')).map(
        (element) => element.textContent
      )
    ).toEqual(['Save', 'Save', 'List models'])
    // The desk-level file has been read, so neither card that writes it says
    // otherwise. The project's own file has not, which is a different file and
    // a different sentence — and the two cards that write it say so.
    expect(screen.queryByText(/has not read its own configuration file/)).toBeNull()
    expect(screen.getAllByText(/has not read this project/).length).toBe(2)
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
    // And the project file's own cards do not report it.
    expect(screen.getAllByText('read').length).toBeGreaterThan(0)
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

  it('renders no bytes of a file the decoder refused, on either card', () => {
    // **The refusal is about a member, and rendering the file anyway puts that
    // member on the page that reported it.** The desk-level example is the
    // review's own; the project one is worse, because the Project card quotes
    // the whole document rather than one member of it.
    renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: true,
        text: '{"deskConfigVersion":1,"identity":{"apiKey":"sk-live-secret"}}',
        decoded: decodeDeskConfig(
          '{"deskConfigVersion":1,"identity":{"apiKey":"sk-live-secret"}}',
          'desk'
        )
      })
    )
    expect(document.body.textContent).not.toContain('sk-live-secret')
    // And the refusal itself is still said, in the decoder's own words.
    expect(screen.getAllByText(/identity.apiKey: a key is never stored/).length).toBeGreaterThan(0)
    cleanup()

    const project = '{"deskConfigVersion":1,"storage":{"apiKey":"sk-live-secret"}}'
    renderAdmin(effectiveConfig(decodeDeskConfig(project, 'project'), undefined, undefined, undefined, project))
    expect(document.body.textContent).not.toContain('sk-live-secret')
    expect(screen.getAllByText(/storage.apiKey: a key is never stored/).length).toBeGreaterThan(0)
  })

  it('renders no bytes of a file that could not be read at all', () => {
    // There are none to render, and a card that offered a disclosure would be
    // offering the decoded defaults as though they were the file.
    renderAdmin(effectiveConfig(undefined, undefined, CHASSIS_413))
    const project = document.getElementById('this-project')!.closest('section')!
    expect(project.querySelector('details')).toBeNull()
  })

  it('names the file the group’s control writes, which is not the one it shows', () => {
    // The group's Location, Status and Content are about the project's own
    // file; its one control writes the desk-level one. The line under the
    // control names that file, from the chassis' answer and never composed.
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

  it('says None where no identity provider is configured, and its issuer where one is', () => {
    renderAdmin()
    // Off the field row: "None" is also what the Project card says about a
    // default nobody has set.
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
      })
    )
    // Off the field row rather than by text: a text query that matched the
    // Content disclosure as well would pass without the value line existing.
    const provider = screen.getByText('Provider').parentElement!
    expect(provider.textContent).toContain('https://issuer.example')
    expect(provider.textContent).toContain('Acme SSO')
    // And no sentence about what a provider will do later.
    expect(screen.queryByText(/gates nothing/)).toBeNull()
  })
})
