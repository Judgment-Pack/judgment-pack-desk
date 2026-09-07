/**
 * Admin: eight cards, one shape, and no narration.
 *
 * Three assertions carry this file. The **order** case fails if a section is
 * added without being declared, declared without being rendered, or rendered
 * out of order. The **narration sweep** fails on any text node over 140
 * characters that is not quoted material — a path, a decoder's own refusal, a
 * member of the file — which is what "remove the narration" means as a rule
 * rather than as a preference. And the **controls** case is the whole list of
 * what on this page changes anything, so a control cannot be added without
 * appearing here.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { PANE_BOUNDS, decodeDeskConfig, effectiveConfig } from '../config/deskConfig'
import { McpContext } from '../mcp/McpProvider'
import { ShellStateProvider, projectKey, shellStateKey } from '../shell/paneState'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AdminView } from './AdminView'
import { ADMIN_SECTIONS } from './adminSections'

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

/** The chassis' project root, and the key the record therefore lives under. */
const ROOT = '/home/someone/a-project'
const KEY = shellStateKey(projectKey(ROOT))
const DESK_PATH = '/home/someone/.config/jpack-desk/desk.json'

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

/**
 * Every text node the page renders that is **this page's own prose**.
 *
 * Quoted material is exempt and the exemption is the definition: a path, a
 * refusal in the decoder's own sentence, and a member of the file as it is
 * written are not narration, they are the thing the card exists to show. A
 * `<code>` or a `<pre>` is where each of those goes.
 */
function proseNodes(container: HTMLElement): string[] {
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT)
  const found: string[] = []
  for (let node = walker.nextNode(); node !== null; node = walker.nextNode()) {
    const quoted = node.parentElement?.closest('code, pre')
    if (quoted !== null && quoted !== undefined) continue
    const text = node.textContent ?? ''
    if (text.trim() !== '') found.push(text)
  }
  return found
}

describe('the Admin page', () => {
  it('renders every card in order, with those exact titles and no others', () => {
    // The whole list, not a slice of it. It fails if a section is added
    // without being declared, declared without being rendered, or rendered out
    // of order — and there is nothing under the heading but the cards now, so
    // no eighth heading can appear without being one.
    renderAdmin()
    const headings = screen
      .getAllByRole('heading', { level: 2 })
      .map((heading) => heading.textContent)
    expect(headings).toEqual(ADMIN_SECTIONS.map((section) => section.title))
    expect(headings[0]).toBe('Project file')
  })

  it('carries no paragraph: every sentence this page writes is one line', () => {
    // **The narration guard.** 140 characters is a line; anything longer is a
    // paragraph, and the page that stood here had thirty of them. Quoted
    // material is exempt — see `proseNodes`.
    const { container } = renderAdmin(
      effectiveConfig(
        decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1 }), 'project'),
        undefined,
        undefined,
        { path: DESK_PATH, present: false, sha256: '' }
      )
    )
    const long = proseNodes(container).filter((text) => text.length > 140)
    expect(long, long.join(' | ')).toEqual([])
  })

  it('sweeps a reintroduced paragraph, wherever on the page it appears', () => {
    // The guard's own instrument, checked against a deliberate failure: a
    // sweep that passed over a clean page would prove nothing about the sweep.
    const { container } = renderAdmin()
    const paragraph = document.createElement('p')
    paragraph.textContent = 'x'.repeat(141)
    container.append(paragraph)
    expect(proseNodes(container).filter((text) => text.length > 140)).toHaveLength(1)
  })

  it('says where each card’s value is written, and what state that file is in', () => {
    const { container } = renderAdmin(
      effectiveConfig(undefined, 'no configuration was read: no such file', undefined, {
        path: DESK_PATH,
        present: false,
        sha256: ''
      })
    )
    const rows = Array.from(container.querySelectorAll('dt')).map((each) => each.textContent)
    // Two per card, in one order, on every one of the eight.
    expect(rows.filter((label) => label === 'Location')).toHaveLength(ADMIN_SECTIONS.length)
    expect(rows.filter((label) => label === 'Status')).toHaveLength(ADMIN_SECTIONS.length)
    expect(rows.slice(0, 2)).toEqual(['Location', 'Status'])
    // The desk-level file is named wherever a card is about it.
    expect(screen.getAllByText(DESK_PATH).length).toBeGreaterThan(0)
    // And an absent file is absent, never "read".
    expect(screen.getAllByText('not present — defaults in use').length).toBeGreaterThan(0)
  })

  it('shows a member of the file as it is written, not as a decode of it', () => {
    // `1e2` is not `100`. A disclosure that re-serialised would show a reader
    // a file that is not on disk, which is the one thing it must not do.
    const text = '{\n  "deskConfigVersion": 1,\n  "panes": {"left": {"width": 2.48e2}}\n}'
    renderAdmin(effectiveConfig(decodeDeskConfig(text, 'project'), undefined, undefined, undefined, text))
    expect(screen.getByText('{"left": {"width": 2.48e2}}')).toBeTruthy()
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
    expect(screen.getByText('filesystem')).toBeTruthy()
    expect(screen.getByText('decisions')).toBeTruthy()
    // The prefix as it will actually be written — normalised at decode.
    expect(screen.getByText('https://acme.example/d/')).toBeTruthy()
  })

  it('shows the built-in location and prefix where the project configured none', () => {
    renderAdmin()
    expect(screen.getByText('packs')).toBeTruthy()
    expect(screen.getByText('https://example.invalid/judgment-packs/')).toBeTruthy()
  })

  it('says a location holds files only where the listing shows one', async () => {
    // The listing reports regular files only, so the page can say a location
    // holds files and can never claim an empty one exists.
    servesListing({ files: [{ path: 'packs/a.pack.json', bytes: 1, sha256: 'aa' }] })
    renderAdmin()
    expect(await screen.findByText('holds files')).toBeTruthy()
  })

  it('says only that no file is under it, which is what the listing can show', async () => {
    servesListing({ files: [{ path: 'jpack.json', bytes: 1, sha256: 'aa' }] })
    renderAdmin()
    expect(
      await screen.findByText('no file is under it — the first pack asks for it to be created')
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
      expect(await screen.findByText(says), says).toBeTruthy()
      cleanup()
    }
  })

  it('says the listing has not answered rather than describing what it has not seen', async () => {
    vi.stubGlobal('fetch', () => new Promise(() => {}))
    renderAdmin()
    expect(await screen.findByText('the file listing has not answered yet')).toBeTruthy()
  })

  it('names the two future kinds as coming soon, as text and not as controls', () => {
    renderAdmin()
    expect(screen.getByText('database — coming soon')).toBeTruthy()
    expect(screen.getByText('cloud storage — coming soon')).toBeTruthy()
    expect(screen.queryByRole('option', { name: /database/ })).toBeNull()
    expect(screen.queryByRole('radio')).toBeNull()
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
    const writes = ['Reset panes on this machine', 'Save', 'Check reachability']
    for (const label of writes) {
      expect(labels.filter((each) => each === label), label).toHaveLength(1)
    }
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
    expect(triggers).toEqual(['OpenAI-compatible', 'vercel', 'off'])
    const offered = Array.from(container.querySelectorAll('select')).map(
      (element) => element.textContent
    )
    expect(offered).toEqual(['OpenAI-compatibleAnthropicGemini', 'vercelbuiltin', 'offonultra'])
    // Every other control is a tool checkbox, which changes nothing until Save.
    const picker = [...triggers, ...offered]
    const others = labels.filter(
      (label) =>
        label !== undefined &&
        !writes.includes(label) &&
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
    const disabled = Array.from(container.querySelectorAll('[disabled]')).map(
      (element) => element.textContent
    )
    expect(disabled).toEqual(['List models', 'Save'])
    expect(screen.getByText(/a write states the bytes it replaces/)).toBeTruthy()
  })

  it('enables the one write once the desk-level file has been read', () => {
    const { container } = renderAdmin(
      effectiveConfig(undefined, undefined, undefined, {
        path: DESK_PATH,
        present: false,
        sha256: ''
      })
    )
    expect(
      Array.from(container.querySelectorAll('[disabled]')).map((element) => element.textContent)
    ).toEqual(['List models'])
    expect(screen.queryByText(/a write states the bytes it replaces/)).toBeNull()
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
    expect(screen.getByText(/Cleared — the panes are back/)).toBeTruthy()
  })

  it('reports a reset it could not make, rather than reporting one it did', () => {
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
    expect(screen.queryByText(/Cleared — the panes/)).toBeNull()
  })

  it('refuses to reset a provisional key, and says which key it is', () => {
    window.localStorage.setItem(shellStateKey('default'), '{"v":1}')
    renderAdmin(effectiveConfig(undefined), '/admin', null)
    expect(screen.getByText(/provisional/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Reset panes on this machine' }))
    expect(screen.getByText(/has not been told which project/)).toBeTruthy()
    expect(window.localStorage.getItem(shellStateKey('default'))).toBe('{"v":1}')
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
      renderAdmin(effectiveConfig(undefined), '/admin#panes')
      expect(scrolled).toContain('panes')
    } finally {
      Element.prototype.scrollIntoView = original
    }
  })

  it('prints every accepted range, inclusive', () => {
    renderAdmin()
    for (const [key, bounds] of Object.entries(PANE_BOUNDS)) {
      expect(screen.getByText(`${key}: ${bounds.min}–${bounds.max}px`)).toBeTruthy()
    }
  })

  it('labels the configured numbers as configured, and measures the rendered ones', () => {
    // The mismatch this fixes: an accepted 720px Inspector renders 440px at
    // 1100px, and an undeclared drawer renders 320px while Admin said 360.
    const { container } = renderAdmin()
    expect(container.textContent).toContain('configured')
    expect(container.textContent).toContain('rendered')
    // No pane is in this document at all — Admin is rendered on its own here —
    // so every rendered figure says so rather than reporting a zero.
    expect(screen.getAllByText('not mounted at this width')).toHaveLength(3)
  })

  it('measures a pane that is there, and calls a mounted-but-collapsed one collapsed', () => {
    // Three answers and not two: absent, collapsed, and a number. `hidden`
    // plus `display: none` is a real element of zero size, and reporting that
    // as `0px` beside a configured 360 reads as a measurement rather than a
    // state.
    const observed: { element: Element; notify: () => void }[] = []
    class Stub {
      private readonly notify: () => void
      constructor(callback: () => void) {
        this.notify = callback
      }
      observe(element: Element) {
        observed.push({ element, notify: this.notify })
      }
      unobserve() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', Stub)

    const rail = document.createElement('nav')
    rail.id = 'desk-rail'
    const inspector = document.createElement('aside')
    inspector.id = 'desk-inspector'
    for (const element of [rail, inspector]) document.body.append(element)
    // 251 rather than 248: the configured rail width is 248 and appears in the
    // same row, so a matching number would not tell a measurement from the
    // configured value it is there to be different from.
    rail.getBoundingClientRect = () =>
      ({ width: 251, height: 600, top: 0, left: 0, right: 251, bottom: 600, x: 0, y: 0 }) as DOMRect

    try {
      renderAdmin()
      expect(screen.getByText('251px')).toBeTruthy()
      expect(screen.getByText('collapsed')).toBeTruthy()
      expect(screen.getAllByText('not mounted at this width')).toHaveLength(1)
    } finally {
      rail.remove()
      inspector.remove()
    }
  })

  it('says the theme is applied and the density is not, rather than claiming both', () => {
    renderAdmin()
    expect(screen.getByText(/Applied. The palette it selects is the light one./)).toBeTruthy()
    expect(screen.getByText(/read by nothing yet/)).toBeTruthy()
  })

  it('reports the runtime connection rather than a file', async () => {
    renderAdmin()
    // The card that is about a process and not a configuration file: its
    // status is the connection, in the connection's own words.
    await waitFor(() => expect(screen.getByText(/^connected — /)).toBeTruthy())
  })
})
