/**
 * Create pack: what it writes, what it refuses to write, and what it will not
 * invent.
 *
 * The write is `baseSha256: ''` — the documented "I believe this file does not
 * exist" — with **no `override`**. A 409 is reported and the dialog stops:
 * overwriting a file the user did not know was there is not a convenience.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { CreatePackDialog } from './CreatePackDialog'
import { forgetAuthorBridge, takeRequestedOpen } from './authorBridge'

afterEach(() => {
  cleanup()
  forgetAuthorBridge()
  vi.unstubAllGlobals()
})

const EXAMPLES = JSON.stringify({
  status: 'valid',
  examples: [
    { name: 'minimal-expense-approval', focus: 'complete minimal pack' },
    { name: 'condition-branches', focus: 'condition shapes' }
  ]
})

/** Every write the page sent, in order, exactly as the wire carried it. */
function captureWrites() {
  const sent: { method?: string; body: unknown }[] = []
  vi.stubGlobal('fetch', async (_url: string, init?: RequestInit) => {
    sent.push({
      method: init?.method,
      body: init?.body === undefined ? undefined : JSON.parse(String(init.body))
    })
    return {
      ok: true,
      status: 200,
      statusText: '',
      text: async () =>
        JSON.stringify({ path: 'packs/new.json', bytes: 2, sha256: 'aa', content: '{}', created: true })
    }
  })
  return sent
}

/**
 * Fill the path and wait for the starting bytes to arrive.
 *
 * The order matters and is the dialog's own rule: the field opens on the
 * prefix `packs/`, which names a directory, so Create is correctly disabled
 * until a file is named.
 */
async function readyToCreate(path: string) {
  fireEvent.change(screen.getByLabelText('Path'), { target: { value: path } })
  await waitFor(() =>
    expect((screen.getByRole('button', { name: 'Create' }) as HTMLButtonElement).disabled).toBe(
      false
    )
  )
}

function renderDialog(stub: ReturnType<typeof stubClient>, overrides: Partial<McpConnection> = {}) {
  const seen: string[] = []
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: stub.client, ...overrides })}>
            <CreatePackDialog open onOpenChange={() => {}} />
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
  return { ...result, router, seen }
}

const FULL = stubClient(
  {
    list_examples: () => ({ text: EXAMPLES }),
    get_example: () => ({ text: '{"packId":"example"}' }),
    get_schema: () => ({ text: '{"$schema":"…"}' })
  },
  {}
)

const FULL_CAPS = { exampleSupported: true, schemaSupported: true }

describe('the Create-pack dialog', () => {
  it('sends baseSha256 "" and no override, exactly once', async () => {
    const sent = captureWrites()
    renderDialog(FULL, FULL_CAPS)
    await screen.findByRole('option', { name: /minimal-expense-approval/ })
    await readyToCreate('packs/new.json')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(sent).toHaveLength(1))
    expect(sent[0]!.method).toBe('PUT')
    expect(sent[0]!.body).toMatchObject({
      path: 'packs/new.json',
      baseSha256: '',
      override: false
    })
  })

  it('opens the new file in the authoring view, and only after a successful write', async () => {
    captureWrites()
    const { seen } = renderDialog(FULL, FULL_CAPS)
    await readyToCreate('packs/new.json')
    expect(seen).toEqual([])
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(seen).toContain('/author'))
    expect(takeRequestedOpen()).toBe('packs/new.json')
  })

  it('reports a 409 and sends no second write', async () => {
    const sent: unknown[] = []
    vi.stubGlobal('fetch', async (_url: string, init?: RequestInit) => {
      sent.push(init?.body)
      return {
        ok: false,
        status: 409,
        statusText: 'Conflict',
        text: async () =>
          JSON.stringify({ error: 'stale', path: 'packs/new.json', exists: true, actualSha256: 'bb' })
      }
    })
    const { seen } = renderDialog(FULL, FULL_CAPS)
    await readyToCreate('packs/new.json')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('A file already exists at that path.')).toBeTruthy()
    expect(sent).toHaveLength(1)
    expect(seen).not.toContain('/author')
  })

  it('offers only an empty file, and says why, where the runtime advertises neither tool', async () => {
    const bare = stubClient({})
    renderDialog(bare, { exampleSupported: false, schemaSupported: false })
    expect(screen.getAllByRole('option').map((option) => option.textContent)).toEqual(['Empty file'])
    expect(screen.getByText(/advertises no example and no schema/)).toBeTruthy()
    // Nothing was asked of a runtime that advertises neither.
    expect(bare.calls).toEqual([])
  })

  it('reports the example source as unavailable when only one half of the pair is advertised', () => {
    // `get_example` requires a name and only `list_examples` can supply one,
    // so half the pair is the same as none of it — and the dialog says so
    // rather than presenting a chooser it cannot fill.
    const half = stubClient({ get_example: () => ({ text: '{}' }) })
    renderDialog(half, { exampleSupported: false, schemaSupported: true })
    expect(screen.getByText(/does not advertise the/)).toBeTruthy()
    expect(
      screen.getAllByRole('option').map((option) => option.textContent)
    ).toEqual([expect.stringContaining('JPS schema'), 'Empty file'])
    expect(half.calls.map((call) => call.name)).not.toContain('get_example')
  })

  it('lists the runtime’s examples in the runtime’s own order, with its own focus text', async () => {
    renderDialog(FULL, FULL_CAPS)
    // `findAllByRole` would resolve on the two options that are there from
    // the first paint, before the listing has answered at all.
    await waitFor(() =>
      expect(screen.getAllByRole('option').map((option) => option.textContent)).toEqual([
        'minimal-expense-approval — complete minimal pack',
        'condition-branches — condition shapes',
        expect.stringContaining('JPS schema'),
        'Empty file'
      ])
    )
  })

  it('refuses to create with an empty path, or one that names a directory', async () => {
    renderDialog(FULL, FULL_CAPS)
    // The field opens on the prefix, which names a directory.
    expect((screen.getByRole('button', { name: 'Create' }) as HTMLButtonElement).disabled).toBe(true)
    await readyToCreate('packs/new.json')
    fireEvent.change(screen.getByLabelText('Path'), { target: { value: 'packs/' } })
    expect((screen.getByRole('button', { name: 'Create' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Path'), { target: { value: '' } })
    expect((screen.getByRole('button', { name: 'Create' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('says the path is this dialog’s convenience and not a rule of the format', () => {
    renderDialog(FULL, FULL_CAPS)
    expect(
      screen.getByText('A convenience of this dialog: nothing in JPS requires this location or this suffix.')
    ).toBeTruthy()
  })
})
