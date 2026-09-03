/**
 * Create a pack: the three things it asks, the two things it decides, and the
 * exact pair of writes it sends.
 *
 * **The eleven cases this replaces could not be adjusted.** They were written
 * against a native `<select>` and a free-text Path field, and both are gone: a
 * Radix Select's options do not exist until its trigger is opened, and there
 * is no path to type. So this file is a rewrite, and should be read as one.
 *
 * Every write is recorded with its method and its parsed body, and the reads
 * are answered separately — the listing and `jpack.json` are questions this
 * dialog asks, not writes it makes, and counting them would make "exactly two
 * writes" mean nothing.
 */
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig, decodeDeskConfig } from '../config/deskConfig'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { CreatePackDialog } from './CreatePackDialog'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const EXAMPLES = JSON.stringify({
  status: 'valid',
  examples: [
    { name: 'minimal-expense-approval', focus: 'complete minimal pack' },
    { name: 'condition-branches', focus: 'condition shapes' }
  ]
})

const TEMPLATE = JSON.stringify({
  specVersion: '0.2.0-draft',
  id: 'https://served.example/examples/minimal',
  version: '9.9.9',
  title: 'The example’s own title',
  decision: { intent: 'decide', question: 'Approve?' },
  outcomes: [{ id: 'approve' }, { id: 'decline' }],
  rules: [{ id: 'r1' }]
})

const SCHEMA = JSON.stringify({
  required: ['specVersion', 'id', 'version', 'title', 'outcomes', 'rules'],
  properties: {
    specVersion: { const: '0.2.0-draft' },
    id: { type: 'string' },
    version: { type: 'string' },
    title: { $ref: '#/$defs/nonEmptyString' },
    outcomes: { type: 'array', minItems: 2 },
    rules: { type: 'array', minItems: 1 }
  },
  $defs: { nonEmptyString: { type: 'string', minLength: 1 } }
})

const PROJECT = `{
  "configVersion": "2",
  "packs": {
    "sanctions-screening": {
      "path": "sanctions-screening-0.1.0.pack.json"
    }
  },
  "graphs": {
    "onboarding": { "path": "onboarding.graph.json" }
  }
}
`
const PROJECT_SHA = 'ab'.repeat(32)

interface Sent {
  method?: string
  path: string
  body: Record<string, unknown>
}

/**
 * A project the dialog can read, and a record of everything it wrote.
 *
 * `files` is what the listing reports; `project` is what `jpack.json` holds,
 * or `undefined` for a project that has none. `answer` lets one case make a
 * chosen write fail.
 */
type Refusal = { status: number; body: unknown }

function serveProject(
  options: {
    files?: string[]
    listing?: Refusal
    project?: string | undefined
    /**
     * Successive answers to `GET jpack.json`, for the cases where the file is
     * not what this dialog last read. Falls back to `project` once exhausted,
     * which is every other case.
     */
    reads?: (string | Refusal)[]
    answer?: (path: string, index: number) => Refusal | undefined
    /** Awaited before any write answers, so a test can act mid-flight. */
    gate?: () => Promise<void>
    /** What the listing reports as unread, if anything. */
    partial?: string[]
    /** The path the chassis says it actually wrote, where it differs. */
    canonical?: string
    /** Whether the candidate pack path already answers a read. */
    packFileExists?: boolean
  } = {}
) {
  const files = options.files ?? ['jpack.json']
  const sent: Sent[] = []
  let reads = 0
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const text = String(url)
    const ok = (body: unknown) => ({
      ok: true,
      status: 200,
      statusText: '',
      text: async () => JSON.stringify(body)
    })
    const refuse = (refusal: Refusal) => ({
      ok: false,
      status: refusal.status,
      statusText: '',
      text: async () => JSON.stringify(refusal.body)
    })
    if (text.includes('/api/files')) {
      if (options.listing) return refuse(options.listing)
      return ok({
        root: '/p',
        files: files.map((path) => ({ path, bytes: 1, sha256: 'aa' })),
        ...(options.partial ? { partial: options.partial } : {})
      })
    }
    if (init?.method !== 'PUT') {
      // A read. `jpack-desk.json` is asked for by the config provider on every
      // render and is deliberately absent here: the dialog runs on defaults.
      if (text.includes(`path=${encodeURIComponent('jpack.json')}`) || text.includes('path=jpack.json')) {
        const answer = options.reads?.[reads] ?? options.project
        reads += 1
        if (answer === undefined) {
          return { ok: false, status: 404, statusText: 'Not Found', text: async () => '{"error":"no such file in the project: jpack.json"}' }
        }
        if (typeof answer !== 'string') return refuse(answer)
        return ok({
          path: 'jpack.json',
          bytes: answer.length,
          sha256: PROJECT_SHA,
          content: answer
        })
      }
      // The candidate pack path, which the dialog probes directly before it
      // writes. Absent unless a case says otherwise.
      if (options.packFileExists && text.includes('.pack.json')) {
        return ok({ path: 'packs/x.pack.json', bytes: 2, sha256: 'dd', content: '{}' })
      }
      return {
        ok: false,
        status: 404,
        statusText: 'Not Found',
        text: async () => '{"error":"no such file in the project","code":"not-found"}'
      }
    }
    const body = JSON.parse(String(init.body)) as Record<string, unknown>
    sent.push({ method: init.method, path: String(body.path), body })
    if (options.gate) await options.gate()
    const chosen = options.answer?.(String(body.path), sent.length - 1)
    if (chosen) return refuse(chosen)
    return ok({
      // The chassis answers with the path it resolved the request to, which is
      // not always the one that was asked for.
      path: options.canonical !== undefined && String(body.path).includes('.pack.json')
        ? options.canonical
        : body.path,
      bytes: 2,
      sha256: 'cc',
      content: body.content,
      created: true
    })
  })
  return sent
}

const FULL = stubClient({
  list_examples: () => ({ text: EXAMPLES }),
  get_example: () => ({ text: TEMPLATE }),
  get_schema: () => ({ text: SCHEMA })
})

const FULL_CAPS = { exampleSupported: true, schemaSupported: true }

/**
 * The dialog, mounted the way the rail mounts it.
 *
 * `open` is state here rather than a constant, because the rail renders
 * `{creating && <CreatePackDialog …/>}` — a dismissal unmounts the component
 * and takes its alert off the screen with it. A harness that pinned `open`
 * open could not tell a dialog that refuses to be dismissed mid-flight from
 * one that is simply never asked.
 */
function Mounted({
  stub,
  overrides,
  deskConfig,
  onClose
}: {
  stub: ReturnType<typeof stubClient>
  overrides: Partial<McpConnection>
  deskConfig: ReturnType<typeof effectiveConfig>
  onClose: (open: boolean) => void
}) {
  const [open, setOpen] = useState(true)
  return (
    <McpContext.Provider value={connected({ client: stub.client, ...overrides })}>
      <DeskConfigFixture value={deskConfig}>
        {open && (
          <CreatePackDialog
            open
            onOpenChange={(next) => {
              onClose(next)
              setOpen(next)
            }}
          />
        )}
      </DeskConfigFixture>
    </McpContext.Provider>
  )
}

function renderDialog(
  stub: ReturnType<typeof stubClient> = FULL,
  overrides: Partial<McpConnection> = FULL_CAPS,
  deskConfig = effectiveConfig(undefined)
) {
  const seen: string[] = []
  const closed: boolean[] = []
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <Mounted
            stub={stub}
            overrides={overrides}
            deskConfig={deskConfig}
            onClose={(next) => closed.push(next)}
          />
        )
      }
    ],
    { initialEntries: ['/'] }
  )
  router.subscribe((state) => seen.push(state.location.pathname))
  const queryClient: QueryClient = testQueryClient()
  const invalidated: unknown[][] = []
  const realInvalidate = queryClient.invalidateQueries.bind(queryClient)
  queryClient.invalidateQueries = ((filters?: { queryKey?: unknown[] }) => {
    if (filters?.queryKey) invalidated.push(filters.queryKey)
    return realInvalidate(filters as never)
  }) as typeof queryClient.invalidateQueries
  const result = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return { ...result, seen, closed, invalidated }
}

const createButton = () => screen.getByRole('button', { name: 'Create pack' }) as HTMLButtonElement

/** Type a name and wait until the dialog is willing to act on it. */
async function nameIt(name: string) {
  fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: name } })
  await waitFor(() => expect(createButton().disabled).toBe(false))
}

describe('what the Create-pack dialog asks', () => {
  it('asks for a name, a description and a template, and for no path', async () => {
    renderDialog()
    expect(screen.getByLabelText('Name (required)')).toBeTruthy()
    expect(screen.getByLabelText('Description')).toBeTruthy()
    expect(screen.getByLabelText('Template')).toBeTruthy()
    // Where the file goes is configuration, not a question for whoever is
    // creating a pack.
    expect(screen.queryByLabelText('Path')).toBeNull()
    expect(screen.queryByLabelText(/path/i)).toBeNull()
  })

  it('lectures about nothing', async () => {
    const { container } = renderDialog()
    await screen.findByLabelText('Template')
    const text = container.textContent ?? ''
    expect(text).not.toMatch(/bytes|chassis|runtime|directory/i)
    expect(text).not.toMatch(/disclaim|convenience of this dialog|authoring method/i)
  })

  it('derives the id from the name and shows it under the field', async () => {
    renderDialog()
    fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: 'Vendor Onboarding' } })
    const hint = await screen.findByText('id: vendor-onboarding')
    // Announced with the field rather than merely sitting near it.
    expect(screen.getByLabelText('Name (required)').getAttribute('aria-describedby')).toContain(hint.id)
  })

  it('refuses a name whose id would not start with a letter, and stays disabled', async () => {
    renderDialog()
    fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: '2024 review' } })
    expect(await screen.findByText('A name must start with a letter.')).toBeTruthy()
    expect(createButton().disabled).toBe(true)
  })

  it('refuses a name whose id is already a pack in this project', async () => {
    serveProject({ project: PROJECT })
    renderDialog()
    fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: 'Sanctions screening' } })
    expect(
      await screen.findByText('This project already has a pack called sanctions-screening.')
    ).toBeTruthy()
    expect(createButton().disabled).toBe(true)
  })

  it('refuses a name whose file is already there', async () => {
    serveProject({
      files: ['jpack.json', 'packs/vendor-onboarding.pack.json'],
      project: PROJECT
    })
    renderDialog()
    fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: 'Vendor onboarding' } })
    expect(
      await screen.findByText('There is already a file where this pack would be written.')
    ).toBeTruthy()
    expect(createButton().disabled).toBe(true)
  })
})

describe('the templates it offers', () => {
  it('lists the runtime’s examples in the runtime’s own order, plus an empty pack', async () => {
    serveProject({ project: PROJECT })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    fireEvent.click(screen.getByLabelText('Template'))
    const options = await screen.findAllByRole('option')
    expect(options.map((option) => option.textContent)).toEqual([
      'minimal-expense-approval',
      'condition-branches',
      'Empty pack'
    ])
  })

  it('offers no template at all where neither tool is advertised, and says so', async () => {
    // The empty pack is the *schema's* skeleton. Without a schema there is
    // nothing to derive one from, and what the dialog used to offer under that
    // name was `{}` with a title, an id and a version written onto it — a file
    // with no `specVersion`, which nothing can read as a pack. It was written,
    // reported as a success, and called invalid the moment anything checked it.
    serveProject({ project: PROJECT })
    const bare = stubClient({})
    renderDialog(bare, { exampleSupported: false, schemaSupported: false })
    expect(await screen.findByText('There is no template to start from here.')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: 'Vendor Onboarding' } })
    expect(createButton().disabled).toBe(true)
    expect(bare.calls).toEqual([])
  })

  it('says nothing about templates while the capability listing has not answered', async () => {
    // A runtime whose tool listing has not answered advertises nothing, which
    // is not the same fact as one that advertises neither tool. The flag that
    // separates them is `known` — *the listing came back* — and not the
    // connection's `status`, which can be `ready` over a listing that failed.
    // Reporting the first as "there is no template to start from here" is the
    // desk making a claim about the runtime out of its own ignorance.
    serveProject({ project: PROJECT })
    renderDialog(stubClient({}), { known: false, exampleSupported: false, schemaSupported: false })
    await screen.findByLabelText('Template')
    expect(screen.queryByText('There is no template to start from here.')).toBeNull()
    fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: 'Vendor Onboarding' } })
    expect(createButton().disabled).toBe(true)
  })

  it('says a refused example listing was refused, rather than showing no examples', async () => {
    // `examples.error` was never read: a listing the runtime refused produced
    // `[]`, which is the same array a runtime carrying no examples produces.
    // One is a refusal and the other is a fact about the runtime.
    serveProject({ project: PROJECT })
    const refuses = stubClient({
      list_examples: () => ({ text: 'the example index could not be read', isError: true }),
      get_schema: () => ({ text: SCHEMA })
    })
    renderDialog(refuses, FULL_CAPS)
    const problem = await screen.findByText(/refused to list its examples/)
    expect(problem.textContent).toContain('the example index could not be read')
    // And it is not reported as "there is no template to start from here",
    // which would be a claim about what this runtime carries.
    expect(screen.queryByText('There is no template to start from here.')).toBeNull()
  })

  it('offers nothing and selects nothing while the templates are still being asked', async () => {
    // **And says nothing to the console while doing it.** A `value` of
    // `undefined` makes Radix's Select uncontrolled, and the first real value
    // switches it to controlled — React warns, and a warning nobody asserts
    // on is a warning that stays. The listener is installed for the whole
    // case, because the transition happens when the listing settles.
    const noise: unknown[][] = []
    const realError = console.error
    const realWarn = console.warn
    console.error = (...args: unknown[]) => void noise.push(args)
    console.warn = (...args: unknown[]) => void noise.push(args)
    try {
      await pendingTemplates(noise)
    } finally {
      console.error = realError
      console.warn = realWarn
    }
  })

  async function pendingTemplates(noise: unknown[][]) {
    // Empty used to be offered on the strength of the capability flag alone
    // and selected as `options[0]` before any listing answered — so the field
    // showed a template that might not resolve, and swapped under the viewer
    // when the examples arrived.
    serveProject({ project: PROJECT })
    let release: () => void = () => {}
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    const slow = stubClient({
      list_examples: async () => {
        await held
        return { text: EXAMPLES }
      },
      get_example: () => ({ text: TEMPLATE }),
      get_schema: () => ({ text: SCHEMA })
    })
    renderDialog(slow, FULL_CAPS)
    await screen.findByLabelText('Template')
    // Nothing selected, nothing claimed, and Create is not offered.
    expect(screen.getByLabelText('Template').textContent).toContain('Asking the runtime')
    expect(screen.queryByText('There is no template to start from here.')).toBeNull()
    fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: 'Vendor Onboarding' } })
    expect(createButton().disabled).toBe(true)

    release()
    await waitFor(() =>
      expect(screen.getByLabelText('Template').textContent).toContain('minimal')
    )
    // Nothing was said to the console at any point in the transition.
    expect(noise.map((args) => String(args[0] ?? ''))).toEqual([])
  }

  it('routes a literal example named empty to get_example, not to the schema', async () => {
    // The sentinel used to be the bare string `empty`, which is a name a
    // runtime may legitimately serve: two options carried one value, and
    // whichever was picked was fetched with `get_schema`.
    serveProject({ project: PROJECT })
    const collides = stubClient({
      list_examples: () => ({ text: JSON.stringify({ status: 'valid', examples: [{ name: 'empty' }] }) }),
      get_example: () => ({ text: TEMPLATE }),
      get_schema: () => ({ text: SCHEMA })
    })
    renderDialog(collides, FULL_CAPS)
    fireEvent.click(await screen.findByLabelText('Template'))
    const options = await screen.findAllByRole('option')
    // Two distinct options, and the labels are what the runtime and the desk
    // each call theirs.
    expect(options.map((option) => option.textContent)).toEqual(['empty', 'Empty pack'])
    fireEvent.click(options[0]!)
    await waitFor(() =>
      expect(collides.calls.some((call) => call.name === 'get_example')).toBe(true)
    )
    expect(collides.calls.find((call) => call.name === 'get_example')!.args).toEqual({ name: 'empty' })
  })

  it('offers the empty pack where the schema is advertised and no examples are', async () => {
    serveProject({ project: PROJECT })
    const schemaOnly = stubClient({ get_schema: () => ({ text: SCHEMA }) })
    renderDialog(schemaOnly, { exampleSupported: false, schemaSupported: true })
    fireEvent.click(screen.getByLabelText('Template'))
    const options = await screen.findAllByRole('option')
    expect(options.map((option) => option.textContent)).toEqual(['Empty pack'])
  })

  it('says nothing about what checks will make of an empty pack', async () => {
    // The line that was here asserted the runtime's verdict without asking it
    // — a disclaimer, and a verdict derived by the shell. Both are things this
    // desk does not do. The runtime reports the document's status on the page
    // this opens, which is the thing entitled to report it.
    serveProject({ project: PROJECT })
    const schemaOnly = stubClient({ get_schema: () => ({ text: SCHEMA }) })
    renderDialog(schemaOnly, { exampleSupported: false, schemaSupported: true })
    await waitFor(() =>
      expect(screen.getByLabelText('Template').textContent).toContain('Empty pack')
    )
    expect(screen.queryByText(/checks report it incomplete/)).toBeNull()
    expect(screen.queryByText(/is a start, not a finished one/)).toBeNull()
  })

  it('offers no empty pack until a schema has actually produced a skeleton', async () => {
    // The option used to appear on the strength of `schemaSupported` — a claim
    // that a *tool* exists, not that a template does. A schema that yields no
    // `specVersion` produces a file nothing can read as a pack.
    serveProject({ project: PROJECT })
    const useless = stubClient({ get_schema: () => ({ text: JSON.stringify({ required: [] }) }) })
    renderDialog(useless, { exampleSupported: false, schemaSupported: true })
    expect(await screen.findByText('There is no template to start from here.')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: 'Vendor Onboarding' } })
    expect(createButton().disabled).toBe(true)
  })

  it('says less when a template is refused with no reason given', async () => {
    // `get_example` answering `isError` with no text used to print "the runtime
    // refused get_example" to whoever was creating a pack.
    serveProject({ project: PROJECT })
    const refusing = stubClient({
      list_examples: () => ({ text: EXAMPLES }),
      get_example: () => ({ text: '', isError: true }),
      get_schema: () => ({ text: SCHEMA })
    })
    const { container } = renderDialog(refusing)
    expect(await screen.findByText('This template could not be read.')).toBeTruthy()
    expect(container.textContent).not.toMatch(/get_example|runtime/i)
    expect(createButton().disabled).toBe(true)
  })

  it('carries the refusal’s own sentence where it gave one', async () => {
    serveProject({ project: PROJECT })
    const refusing = stubClient({
      list_examples: () => ({ text: EXAMPLES }),
      get_example: () => ({ text: 'no example is named minimal-expense-approval', isError: true }),
      get_schema: () => ({ text: SCHEMA })
    })
    renderDialog(refusing)
    expect(
      await screen.findByText(
        'This template could not be read — no example is named minimal-expense-approval'
      )
    ).toBeTruthy()
  })
})

describe('what the Create-pack dialog writes', () => {
  it('sends exactly two writes, in order, with the bodies the wire carries', async () => {
    const sent = serveProject({ project: PROJECT })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    fireEvent.change(screen.getByLabelText('Description'), {
      target: { value: 'Whether a vendor may be onboarded.' }
    })
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent).toHaveLength(2))

    // (1) The pack, into a location it may have to make.
    const pack = sent[0]!
    expect(pack.method).toBe('PUT')
    expect(pack.body).toMatchObject({
      path: 'packs/vendor-onboarding.pack.json',
      baseSha256: '',
      override: false,
      createParents: true
    })
    expect(JSON.parse(String(pack.body.content))).toEqual({
      specVersion: '0.2.0-draft',
      id: 'https://example.invalid/judgment-packs/vendor-onboarding',
      version: '0.1.0',
      title: 'Vendor Onboarding',
      description: 'Whether a vendor may be onboarded.',
      decision: { intent: 'decide', question: 'Approve?' },
      outcomes: [{ id: 'approve' }, { id: 'decline' }],
      rules: [{ id: 'r1' }]
    })

    // (2) The entry, against the digest the read returned.
    const registration = sent[1]!
    expect(registration.body).toMatchObject({ path: 'jpack.json', baseSha256: PROJECT_SHA })
    expect(registration.body.createParents).toBe(false)
    const amended = JSON.parse(String(registration.body.content)) as Record<string, unknown>
    expect(amended.configVersion).toBe('2')
    expect(amended.graphs).toEqual({ onboarding: { path: 'onboarding.graph.json' } })
    expect(Object.keys(amended.packs as object)).toEqual([
      'sanctions-screening',
      'vendor-onboarding'
    ])
    expect((amended.packs as Record<string, unknown>)['vendor-onboarding']).toEqual({
      path: 'packs/vendor-onboarding.pack.json',
      description: 'Whether a vendor may be onboarded.',
      expectedVersion: '0.1.0'
    })
  })

  it('omits a blank description from both documents', async () => {
    const sent = serveProject({ project: PROJECT })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent).toHaveLength(2))
    expect(JSON.parse(String(sent[0]!.body.content))).not.toHaveProperty('description')
    const amended = JSON.parse(String(sent[1]!.body.content)) as { packs: Record<string, unknown> }
    expect(amended.packs['vendor-onboarding']).toEqual({
      path: 'packs/vendor-onboarding.pack.json',
      expectedVersion: '0.1.0'
    })
  })

  it('writes into the configured location under the configured id prefix', async () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({
        deskConfigVersion: 1,
        storage: { packs: { dir: 'decisions', idBase: 'https://acme.example/d' } }
      }),
      'project'
    )
    const sent = serveProject({ project: PROJECT })
    renderDialog(FULL, FULL_CAPS, effectiveConfig(decoded))
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent).toHaveLength(2))
    expect(sent[0]!.body.path).toBe('decisions/vendor-onboarding.pack.json')
    // The decoder normalised the prefix; nothing here decides a separator.
    expect(JSON.parse(String(sent[0]!.body.content)).id).toBe(
      'https://acme.example/d/vendor-onboarding'
    )
  })

  it('opens the new pack once both writes have landed', async () => {
    const sent = serveProject({ project: PROJECT })
    const { seen } = renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    expect(seen).toEqual([])
    fireEvent.click(createButton())
    await waitFor(() => expect(seen).toContain('/packs/vendor-onboarding'))
    expect(sent).toHaveLength(2)
  })

  it('submits on Enter in the name field', async () => {
    const sent = serveProject({ project: PROJECT })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.submit(screen.getByLabelText('Name (required)').closest('form')!)
    await waitFor(() => expect(sent).toHaveLength(2))
    expect(sent[0]!.body.path).toBe('packs/vendor-onboarding.pack.json')
  })
})

describe('what it says when it cannot', () => {
  it('writes nothing at all in a project that has no jpack.json', async () => {
    // `packs` carries minProperties: 1, so this can only ever amend a
    // configuration and never write one from nothing.
    const sent = serveProject({ files: ['README.md'], project: undefined })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toBe(
      'This project has no jpack.json, so a new pack cannot be registered. Nothing was created.'
    )
    expect(sent).toEqual([])
  })

  it('does not call a listing that failed a project with no jpack.json in it', async () => {
    // `retry: false` means one failed request is the final answer, so the
    // dialog used to enable Create, take the empty listing as fact, and tell
    // the user their project has no jpack.json — a false statement about a
    // project it never read.
    const sent = serveProject({ listing: { status: 503, body: { error: 'the project could not be read' } } })
    renderDialog()
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('This project’s files could not be read, so nothing was created.')
    expect(alert.textContent).toContain('the project could not be read')
    expect(alert.textContent).not.toContain('has no jpack.json')
    fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: 'Vendor Onboarding' } })
    expect(createButton().disabled).toBe(true)
    expect(sent).toEqual([])
  })

  it('says a name is taken rather than quoting an editor’s override advice', async () => {
    // The chassis answers a 409 with "reload it, or write again with override",
    // which is what it tells an editor. This dialog has no override to offer
    // and the person in front of it has a name to change — and it decides that
    // from the `code`, not by reading the sentence.
    const sent = serveProject({
      project: PROJECT,
      answer: (path) =>
        path === 'jpack.json'
          ? undefined
          : {
              status: 409,
              body: {
                error: 'the file on disk is not the file this edit started from; reload it, or write again with override',
                code: 'exists',
                path: 'packs/vendor-onboarding.pack.json',
                exists: true,
                actualSha256: 'bb'
              }
            }
    })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Something is already there under that name — try another.')
    // The editor's advice is kept as detail and never promoted into the
    // sentence: "write again with override" is not an offer this dialog makes.
    // The lead is the alert's own text; the reason is its own element.
    expect(alert.childNodes[0]?.textContent).not.toMatch(/override|reload/i)
    expect(sent).toHaveLength(1)
  })

  it('turns a chassis code into a Create sentence, and keeps its words as detail', async () => {
    // "the directory packs does not exist in the project; create it first" is
    // a good sentence for whoever is editing a file and the wrong one to put
    // in front of somebody who typed a name and pressed Create. The code
    // chooses the sentence; the chassis' own words go underneath.
    const sent = serveProject({
      project: PROJECT,
      answer: (path) =>
        path === 'jpack.json'
          ? undefined
          : {
              status: 404,
              body: {
                error: 'the directory packs does not exist in the project; create it first',
                code: 'directory-missing'
              }
            }
    })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain(
      'The folder configured for packs is not there, and it could not be created.'
    )
    // The chassis' own sentence survives, in the details line.
    expect(
      screen.getByText('the directory packs does not exist in the project; create it first')
    ).toBeTruthy()
    expect(sent).toHaveLength(1)
  })

  it('never promotes an ENOTDIR refusal into a containment claim', async () => {
    // The chassis used to answer "path is not inside the project" for a parent
    // that is a regular file, which sent whoever read it hunting a security
    // problem that is not there. It has its own code now, and its own sentence.
    const sent = serveProject({
      project: PROJECT,
      answer: (path) =>
        path === 'jpack.json'
          ? undefined
          : {
              status: 404,
              body: {
                error: 'packs is a file, not a directory, so nothing can be written inside it',
                code: 'parent-is-a-file'
              }
            }
    })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain(
      'The folder configured for packs is a file, so nothing can be written inside it.'
    )
    expect(alert.textContent).not.toContain('not inside the project')
    expect(sent).toHaveLength(1)
  })

  it('keeps its own general sentence for a code it has never seen', async () => {
    // A code this desk does not know is a refusal it cannot describe.
    // Inventing a specific sentence for it would be worse than the general
    // one, and the chassis' words still reach the details line.
    const sent = serveProject({
      project: PROJECT,
      answer: (path) =>
        path === 'jpack.json'
          ? undefined
          : { status: 500, body: { error: 'the quota daemon said no', code: 'quota-exceeded' } }
    })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('The pack could not be created.')
    expect(screen.getByText('the quota daemon said no')).toBeTruthy()
    expect(sent).toHaveLength(1)
  })

  it('registers the path the chassis says it wrote, not the one it asked for', async () => {
    // The write answers with a read-back from the disk, and its `path` is the
    // canonical spelling the chassis resolved the request to. Discarding it and
    // registering the requested spelling is how an entry ends up naming a file
    // the runtime cleans to something else.
    // A chassis that resolved the request to a different spelling — a
    // case-normalising filesystem answers its read-back this way.
    const sent = serveProject({ project: PROJECT, canonical: 'Packs/vendor-onboarding.pack.json' })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent).toHaveLength(2))
    const registered = JSON.parse(String(sent[1]!.body.content)) as {
      packs: Record<string, { path: string }>
    }
    expect(registered.packs['vendor-onboarding']!.path).toBe('Packs/vendor-onboarding.pack.json')
    // And emphatically not the spelling that was requested.
    expect(String(sent[0]!.body.path)).toBe('packs/vendor-onboarding.pack.json')
  })

  it('refuses a name whose file another entry already claims under another spelling', async () => {
    // `packs/./new.pack.json` and `packs/new.pack.json` are one file to the
    // runtime and were two to this dialog, so the create went through and two
    // ids ended up resolving to one document.
    const aliased = JSON.stringify({
      configVersion: '2',
      packs: { existing: { path: 'packs/./vendor-onboarding.pack.json' } }
    })
    const sent = serveProject({ project: aliased })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    expect(
      await screen.findByText('Another pack in this project already uses that file.')
    ).toBeTruthy()
    expect(createButton().disabled).toBe(true)
    expect(sent).toEqual([])
  })

  it('asks the file itself whether something is already there', async () => {
    // The listing is a snapshot of spellings; the file is the thing with an
    // authoritative answer, so it is asked before either write.
    const sent = serveProject({ project: PROJECT, packFileExists: true })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Something is already there under that name')
    expect(sent).toEqual([])
  })

  it('will not act on an incomplete listing at all', async () => {
    // `partial` means `files` is not all of them, and every question this
    // dialog asks the listing is a question about absence. A project whose
    // packs/ could not be walked used to be told it had no jpack.json.
    const sent = serveProject({ project: PROJECT, partial: ['packs: permission denied'] })
    renderDialog()
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('file listing is incomplete')
    expect(alert.textContent).toContain('packs: permission denied')
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    expect(createButton().disabled).toBe(true)
    expect(sent).toEqual([])
  })

  it('says a project has no jpack.json only when the read itself 404s', async () => {
    // The claim used to come from the cached listing — a snapshot — rather
    // than from asking. A read that was refused for any other reason is "could
    // not be read", which is a different sentence and a different fix.
    const forbidden = { status: 403, body: { error: 'jpack.json cannot be read', code: 'forbidden' } }
    const refused = serveProject({ files: ['jpack.json'], reads: [forbidden, forbidden, forbidden] })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('could not be read')
    expect(alert.textContent).not.toContain('has no jpack.json')
    expect(refused).toEqual([])
  })

  it('says exactly what a conflict on jpack.json means', async () => {
    const sent = serveProject({
      project: PROJECT,
      answer: (path) =>
        path === 'jpack.json'
          ? {
              status: 409,
              body: {
                error: 'the file on disk is not the file this edit started from',
                code: 'stale',
                path: 'jpack.json',
                exists: true,
                actualSha256: 'bb'
              }
            }
          : undefined
    })
    const { seen } = renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    expect(
      await screen.findByText('jpack.json changed while creating — reload and try again')
    ).toBeTruthy()
    expect(sent).toHaveLength(2)
    expect(seen).not.toContain('/packs/vendor-onboarding')
  })

  it('says the pack was created and nothing else changed, rather than implying an unwind', async () => {
    // The file API has no delete verb, so there is no unwind to perform and
    // none to claim.
    const sent = serveProject({
      project: PROJECT,
      answer: (path) =>
        path === 'jpack.json'
          ? { status: 500, body: { error: 'could not stage the write' } }
          : undefined
    })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain(
      'The pack was created but could not be registered. Nothing else was changed.'
    )
    expect(screen.getByText('could not stage the write')).toBeTruthy()
    expect(sent).toHaveLength(2)
  })
})

/**
 * The order of the sequence, which is the whole of what makes it safe.
 *
 * Everything that can refuse a create is asked before the first write. With
 * the project file read *after* the pack was written, three separate defects
 * were reachable: an id that was free when the dialog opened and taken by the
 * time it was used silently replaced the entry that had it; an unreadable or
 * unparseable configuration produced an orphan for a reason known in advance;
 * and none of it was discoverable from the dialog, which reported success.
 */
describe('what it settles before it writes anything', () => {
  const WITH_VENDOR = `{
  "configVersion": "2",
  "packs": {
    "sanctions-screening": {
      "path": "sanctions-screening-0.1.0.pack.json"
    },
    "vendor-onboarding": {
      "path": "vendor-onboarding-0.1.0.pack.json",
      "description": "Whether a vendor may be onboarded."
    }
  }
}
`

  it('refuses an id that was taken while this dialog was open, and writes nothing', async () => {
    // Not contrived: the runtime's own fixture project registers
    // `vendor-onboarding` at `vendor-onboarding-0.1.0.pack.json` — its
    // documented filename convention, which this desk deliberately does not
    // write into. So the *file* does not collide, the write succeeds, and
    // `withPack` replaces the key: the original document stays on disk, named
    // by nothing, and the dialog reports success and navigates to it.
    const sent = serveProject({ project: PROJECT, reads: [PROJECT, WITH_VENDOR] })
    const { seen } = renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toBe('This project already has a pack called vendor-onboarding.')
    expect(sent).toEqual([])
    expect(seen).not.toContain('/packs/vendor-onboarding')
  })

  it('refuses a file another entry already claims, even with nothing on disk', async () => {
    const claimed = `{
  "configVersion": "2",
  "packs": {
    "vendor-onboarding-v2": {
      "path": "packs/vendor-onboarding.pack.json"
    }
  }
}
`
    const sent = serveProject({ project: PROJECT, reads: [PROJECT, claimed] })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toBe('Another pack in this project already uses that file.')
    expect(sent).toEqual([])
  })

  it('leaves no orphan when jpack.json cannot be parsed', async () => {
    const sent = serveProject({ project: PROJECT, reads: [PROJECT, '{ "packs": broken'] })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain(
      'This project’s jpack.json could not be read, so a new pack cannot be registered. Nothing was created.'
    )
    expect(alert.textContent).toContain('not valid JSON')
    expect(sent).toEqual([])
  })

  it('leaves no orphan when jpack.json cannot be read', async () => {
    const sent = serveProject({
      project: PROJECT,
      reads: [PROJECT, { status: 403, body: { error: 'permission denied' } }]
    })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    expect(await screen.findByText('permission denied')).toBeTruthy()
    expect(sent).toEqual([])
  })

  it('says a template it cannot use is a template, and sends nothing', async () => {
    const sent = serveProject({ project: PROJECT })
    const broken = stubClient({
      list_examples: () => ({ text: EXAMPLES }),
      get_example: () => ({ text: 'not a document' }),
      get_schema: () => ({ text: SCHEMA })
    })
    renderDialog(broken)
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('This template could not be used.')
    expect(sent).toEqual([])
  })
})

describe('what it does while it is working', () => {
  /** A write held open until the test lets it answer. */
  function held() {
    let release!: () => void
    const promise = new Promise<void>((resolve) => {
      release = resolve
    })
    return { gate: () => promise, release }
  }

  it('cannot be dismissed mid-sequence, so the residue is reported to somebody', async () => {
    // The rail unmounts this component when it closes. Escape between the two
    // writes used to take the only place the outcome is stated off the screen,
    // leaving a pack on disk that nothing names and nobody was told about.
    const { gate, release } = held()
    const sent = serveProject({
      project: PROJECT,
      gate,
      answer: (path) =>
        path === 'jpack.json' ? { status: 500, body: { error: 'could not stage the write' } } : undefined
    })
    const { closed } = renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent).toHaveLength(1))

    fireEvent.keyDown(document.body, { key: 'Escape', code: 'Escape' })
    expect(closed).toEqual([])
    expect(screen.getByRole('dialog')).toBeTruthy()

    release()
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain(
      'The pack was created but could not be registered. Nothing else was changed.'
    )
  })

  it('disables Cancel while it works, so the affordance agrees with the behaviour', async () => {
    const { gate, release } = held()
    const sent = serveProject({ project: PROJECT, gate })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    const cancel = screen.getByRole('button', { name: 'Cancel' }) as HTMLButtonElement
    expect(cancel.disabled).toBe(false)
    fireEvent.click(createButton())
    await waitFor(() => expect(cancel.disabled).toBe(true))
    release()
    await waitFor(() => expect(sent).toHaveLength(2))
  })
})

describe('what it invalidates', () => {
  it('invalidates the configuration it just amended, not only the listing', async () => {
    // Without `['desk-file', 'jpack.json']`, the cache the collision check
    // reads stays stale by exactly the entry that was just added.
    const sent = serveProject({ project: PROJECT })
    const { invalidated } = renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent).toHaveLength(2))
    await waitFor(() => expect(invalidated).toContainEqual(['desk-file', 'jpack.json']))
    expect(invalidated).toContainEqual(['desk-files'])
    expect(invalidated).toContainEqual(['list_packs'])
  })

  it('invalidates the listing after a refused write, so a retry is refused in the field', async () => {
    const sent = serveProject({
      project: PROJECT,
      answer: (path) =>
        path === 'jpack.json'
          ? undefined
          : { status: 500, body: { error: 'could not stage the write' } }
    })
    const { invalidated } = renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await screen.findByRole('alert')
    expect(sent).toHaveLength(1)
    expect(invalidated).toContainEqual(['desk-files'])
  })
})
