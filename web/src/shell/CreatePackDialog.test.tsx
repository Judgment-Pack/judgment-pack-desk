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
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
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
function serveProject(
  options: {
    files?: string[]
    project?: string | undefined
    answer?: (path: string, index: number) => { status: number; body: unknown } | undefined
  } = {}
) {
  const files = options.files ?? ['jpack.json']
  const sent: Sent[] = []
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const text = String(url)
    const ok = (body: unknown) => ({
      ok: true,
      status: 200,
      statusText: '',
      text: async () => JSON.stringify(body)
    })
    if (text.includes('/api/files')) {
      return ok({ root: '/p', files: files.map((path) => ({ path, bytes: 1, sha256: 'aa' })) })
    }
    if (init?.method !== 'PUT') {
      // A read. `jpack-desk.json` is asked for by the config provider on every
      // render and is deliberately absent here: the dialog runs on defaults.
      if (text.includes(`path=${encodeURIComponent('jpack.json')}`) || text.includes('path=jpack.json')) {
        if (options.project === undefined) {
          return { ok: false, status: 404, statusText: 'Not Found', text: async () => '{"error":"no such file in the project: jpack.json"}' }
        }
        return ok({
          path: 'jpack.json',
          bytes: options.project.length,
          sha256: PROJECT_SHA,
          content: options.project
        })
      }
      return { ok: false, status: 404, statusText: 'Not Found', text: async () => '{"error":"no such file"}' }
    }
    const body = JSON.parse(String(init.body)) as Record<string, unknown>
    sent.push({ method: init.method, path: String(body.path), body })
    const chosen = options.answer?.(String(body.path), sent.length - 1)
    if (chosen) {
      return {
        ok: false,
        status: chosen.status,
        statusText: '',
        text: async () => JSON.stringify(chosen.body)
      }
    }
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

function renderDialog(
  stub: ReturnType<typeof stubClient> = FULL,
  overrides: Partial<McpConnection> = FULL_CAPS,
  deskConfig = effectiveConfig(undefined)
) {
  const seen: string[] = []
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: stub.client, ...overrides })}>
            <DeskConfigFixture value={deskConfig}>
              <CreatePackDialog open onOpenChange={() => {}} />
            </DeskConfigFixture>
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: ['/'] }
  )
  router.subscribe((state) => seen.push(state.location.pathname))
  const result = render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return { ...result, seen }
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

  it('offers the empty pack alone, and asks nothing, where neither tool is advertised', async () => {
    serveProject({ project: PROJECT })
    const bare = stubClient({})
    renderDialog(bare, { exampleSupported: false, schemaSupported: false })
    fireEvent.click(screen.getByLabelText('Template'))
    const options = await screen.findAllByRole('option')
    expect(options.map((option) => option.textContent)).toEqual(['Empty pack'])
    expect(bare.calls).toEqual([])
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
