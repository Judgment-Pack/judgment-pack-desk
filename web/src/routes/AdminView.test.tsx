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
import {
  PANE_BOUNDS,
  STORAGE_KIND_SAYS,
  decodeDeskConfig,
  effectiveConfig
} from '../config/deskConfig'
import { McpContext } from '../mcp/McpProvider'
import { ShellStateProvider, projectKey, shellStateKey } from '../shell/paneState'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { narrationIn } from '../admin/narration'
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
    // The one kind, on the Select's own trigger.
    expect(screen.getByRole('combobox', { name: 'Kind' }).textContent).toBe('filesystem')
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
    // The Project card's control is a nomination and not a path: the page may
    // name the project it is already running in, or withdraw a default, and
    // nothing else — so there is one button and no field for a path.
    const writes: Record<string, number> = {
      'Reset panes on this machine': 1,
      // The assistant slot's, and one on each of the four cards that write a
      // member of the project's own file.
      Save: 5,
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
    // The assistant's three, then Storage's kind, then Appearance's two — in
    // the order the cards are rendered in.
    expect(triggers).toEqual([
      'OpenAI-compatible',
      'vercel',
      'off',
      'filesystem',
      'system',
      'comfortable'
    ])
    const offered = Array.from(container.querySelectorAll('select')).map(
      (element) => element.textContent
    )
    expect(offered).toEqual([
      'OpenAI-compatibleAnthropicGemini',
      'vercelbuiltin',
      'offonultra',
      'filesystem',
      'systemlightdark',
      'comfortablecompact'
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
    // The Project card's nomination first, then the Assistant form's Save —
    // neither has a digest to state — and then the four project-file cards',
    // which have no bytes to write over and nothing typed to write.
    expect(disabled).toEqual([
      'Use this project as the default',
      'List models',
      'Save',
      'Save',
      'Save',
      'Save',
      'Save'
    ])
    // The desk-level file, on two cards; this project's own file, on four.
    expect(screen.getAllByText(/has not read its own configuration file/).length).toBe(2)
    expect(screen.getAllByText(/has not read this project/).length).toBe(4)
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
    ).toEqual(['List models', 'Save', 'Save', 'Save', 'Save'])
    // The desk-level file has been read, so neither card that writes it says
    // otherwise. The project's own file has not, which is a different file and
    // a different sentence — and the four cards that write it say so.
    expect(screen.queryByText(/has not read its own configuration file/)).toBeNull()
    expect(screen.getAllByText(/has not read this project/).length).toBe(4)
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
    expect(screen.getByText('/usr/local/bin/jpack')).toBeTruthy()
    // And never the project-relative name once the chassis has answered.
    expect(screen.queryByText('jpack-desk.json')).toBeNull()
  })

  it('names the file by the name it reads where the chassis has not answered', () => {
    renderAdmin()
    expect(screen.getAllByText('jpack-desk.json').length).toBeGreaterThan(0)
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
    const { container } = renderAdmin(effectiveConfig(undefined, undefined, CHASSIS_413))
    const project = [...container.querySelectorAll('section')].find(
      (section) => section.querySelector('h2')?.textContent === 'Project file'
    )!
    expect(project.querySelector('details')).toBeNull()
  })

  it('names the file the Project card’s control writes, which is not the one it shows', () => {
    // The card's Location, Status and Content are about the project's own
    // file; its one control writes the desk-level one. The line under the
    // control names that file, from the chassis' answer and never composed.
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
    const project = [...container.querySelectorAll('section')].find(
      (section) => section.querySelector('h2')?.textContent === 'Project file'
    )!
    // Location: the project's own file.
    expect(project.querySelector('dd')!.textContent).toBe('/this/launch/jpack-desk.json')
    // The control's own line: the desk-level file it writes, and this launch.
    const rule = project.querySelector('p')!.textContent ?? ''
    expect(rule).toContain(DESK_PATH)
    expect(rule).toContain('used on the next launch without a directory')
    expect(rule).toContain('/this/launch')
  })

  it('reports the runtime connection rather than a file', async () => {
    renderAdmin()
    // The card that is about a process and not a configuration file: its
    // status is the connection, in the connection's own words.
    await waitFor(() => expect(screen.getByText(/^connected — /)).toBeTruthy())
    expect(screen.getByText('Tool listing')).toBeTruthy()
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
