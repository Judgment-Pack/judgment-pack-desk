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
      return ok({ root: '/p', files: files.map((path) => ({ path, bytes: 1, sha256: 'aa' })) })
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
      return { ok: false, status: 404, statusText: 'Not Found', text: async () => '{"error":"no such file"}' }
    }
    const body = JSON.parse(String(init.body)) as Record<string, unknown>
    sent.push({ method: init.method, path: String(body.path), body })
    if (options.gate) await options.gate()
    const chosen = options.answer?.(String(body.path), sent.length - 1)
    if (chosen) return refuse(chosen)
    return ok({
      path: body.path,
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
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: name } })
  await waitFor(() => expect(createButton().disabled).toBe(false))
}

describe('what the Create-pack dialog asks', () => {
  it('asks for a name, a description and a template, and for no path', async () => {
    renderDialog()
    expect(screen.getByLabelText('Name')).toBeTruthy()
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
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Vendor Onboarding' } })
    const hint = await screen.findByText('id: vendor-onboarding')
    // Announced with the field rather than merely sitting near it.
    expect(screen.getByLabelText('Name').getAttribute('aria-describedby')).toContain(hint.id)
  })

  it('refuses a name whose id would not start with a letter, and stays disabled', async () => {
    renderDialog()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: '2024 review' } })
    expect(await screen.findByText('A name must start with a letter.')).toBeTruthy()
    expect(createButton().disabled).toBe(true)
  })

  it('refuses a name whose id is already a pack in this project', async () => {
    serveProject({ project: PROJECT })
    renderDialog()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Sanctions screening' } })
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
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Vendor onboarding' } })
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
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Vendor Onboarding' } })
    expect(createButton().disabled).toBe(true)
    expect(bare.calls).toEqual([])
  })

  it('says nothing about templates while the runtime has not answered', async () => {
    // A runtime that has not answered advertises nothing, which is not the
    // same fact as one that advertises neither tool. Reporting the first as
    // "there is no template to start from here" makes a slow answer read as a
    // failure — the thing the two template queries are kept apart to avoid.
    serveProject({ project: PROJECT })
    renderDialog(stubClient({}), { status: 'connecting', exampleSupported: false, schemaSupported: false })
    await screen.findByLabelText('Template')
    expect(screen.queryByText('There is no template to start from here.')).toBeNull()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Vendor Onboarding' } })
    expect(createButton().disabled).toBe(true)
  })

  it('offers the empty pack where the schema is advertised and no examples are', async () => {
    serveProject({ project: PROJECT })
    const schemaOnly = stubClient({ get_schema: () => ({ text: SCHEMA }) })
    renderDialog(schemaOnly, { exampleSupported: false, schemaSupported: true })
    fireEvent.click(screen.getByLabelText('Template'))
    const options = await screen.findAllByRole('option')
    expect(options.map((option) => option.textContent)).toEqual(['Empty pack'])
  })

  it('says an empty pack is a start rather than letting it be discovered', async () => {
    // The skeleton carries the schema's required members and nothing else, so
    // `outcomes` is `[]` against `minItems: 2` and `decision` is `{}` against
    // its own required members. The runtime reports it incomplete, correctly,
    // and this line is what stops that being a surprise.
    serveProject({ project: PROJECT })
    const schemaOnly = stubClient({ get_schema: () => ({ text: SCHEMA }) })
    renderDialog(schemaOnly, { exampleSupported: false, schemaSupported: true })
    const hint = await screen.findByText(
      'An empty pack is a start, not a finished one: checks report it incomplete until you fill it in.'
    )
    expect(screen.getByLabelText('Template').getAttribute('aria-describedby')).toContain(hint.id)
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
    fireEvent.submit(screen.getByLabelText('Name').closest('form')!)
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
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Vendor Onboarding' } })
    expect(createButton().disabled).toBe(true)
    expect(sent).toEqual([])
  })

  it('says a name is taken rather than quoting an editor’s override advice', async () => {
    // The chassis answers a 409 with "reload it, or write again with override",
    // which is what it tells an editor. This dialog has no override to offer
    // and the person in front of it has a name to change.
    const sent = serveProject({
      project: PROJECT,
      answer: (path) =>
        path === 'jpack.json'
          ? undefined
          : {
              status: 409,
              body: {
                error: 'the file on disk is not the file this edit started from; reload it, or write again with override',
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
    expect(alert.textContent).toBe(
      'The pack could not be created. Something is already there under that name — try another.'
    )
    expect(alert.textContent).not.toMatch(/override|reload/i)
    expect(sent).toHaveLength(1)
  })

  it('carries the refusal the write answered with, and sends no second write', async () => {
    const sent = serveProject({
      project: PROJECT,
      answer: (path) =>
        path === 'jpack.json'
          ? undefined
          : {
              status: 404,
              body: { error: 'the folder packs does not exist in the project; create it first' }
            }
    })
    renderDialog()
    await waitFor(() => expect(screen.getByLabelText('Template').textContent).toContain('minimal'))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('The pack could not be created.')
    // The chassis' own sentence, carried through rather than reworded.
    expect(
      screen.getByText('the folder packs does not exist in the project; create it first')
    ).toBeTruthy()
    expect(sent).toHaveLength(1)
  })

  it('says exactly what a conflict on jpack.json means', async () => {
    const sent = serveProject({
      project: PROJECT,
      answer: (path) =>
        path === 'jpack.json'
          ? {
              status: 409,
              body: { error: 'stale', path: 'jpack.json', exists: true, actualSha256: 'bb' }
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
