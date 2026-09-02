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
import { AuthorView } from '../routes/AuthorView'
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

/**
 * Every write the page sent, in order, exactly as the wire carried it.
 *
 * The file listing is answered separately and is **not** recorded: the dialog
 * reads it to decide whether `packs/` is a real directory in this project, and
 * counting that read as a write would make "exactly one write" mean nothing.
 * `files` is what the listing reports, so a case can put the dialog in a
 * project that has a `packs/` directory or one that has not.
 */
function captureWrites(files: { path: string }[] = []) {
  const sent: { method?: string; body: unknown }[] = []
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    if (String(url).includes('/api/files')) {
      return {
        ok: true,
        status: 200,
        statusText: '',
        text: async () =>
          JSON.stringify({
            root: '/p',
            files: files.map((file) => ({ ...file, bytes: 1, sha256: 'aa' }))
          })
      }
    }
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
 * The order matters and is the dialog's own rule: an unnamed file cannot be
 * created, so Create is correctly disabled until the field holds a path that
 * is not empty and does not end in a slash.
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
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      if (String(url).includes('/api/files')) {
        return { ok: true, status: 200, statusText: '', text: async () => '{"root":"/p","files":[]}' }
      }
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
    // The field opens empty in a project with no packs/ directory.
    expect((screen.getByRole('button', { name: 'Create' }) as HTMLButtonElement).disabled).toBe(true)
    await readyToCreate('packs/new.json')
    fireEvent.change(screen.getByLabelText('Path'), { target: { value: 'packs/' } })
    expect((screen.getByRole('button', { name: 'Create' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Path'), { target: { value: '' } })
    expect((screen.getByRole('button', { name: 'Create' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('writes an empty file when Empty file is chosen — it ships no template', async () => {
    // A desk-authored skeleton would be the desk asserting what a pack is,
    // which is exactly the opinion the file API disclaims and the runtime is
    // the only thing entitled to hold.
    const sent = captureWrites()
    renderDialog(FULL, FULL_CAPS)
    fireEvent.change(screen.getByLabelText('Starting bytes'), { target: { value: 'empty' } })
    await readyToCreate('packs/new.json')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(sent).toHaveLength(1))
    expect((sent[0]!.body as { content: string }).content).toBe('')
  })

  it('says the path is this dialog’s convenience and not a rule of the format', () => {
    renderDialog(FULL, FULL_CAPS)
    expect(
      screen.getByText('A convenience of this dialog: nothing in JPS requires this location or this suffix.')
    ).toBeTruthy()
  })

  it('says the parent directory has to be there already, because the chassis makes none', () => {
    renderDialog(FULL, FULL_CAPS)
    expect(screen.getByText(/parent directory has to exist already/)).toBeTruthy()
  })

  it('seeds packs/ only where the project already keeps a file there', async () => {
    // The chassis stats the parent and answers 404 — "the directory packs does
    // not exist in the project; create it first" — and creates no directory,
    // ever. A field seeded with `packs/` in a project with a flat layout is a
    // default that 404s on first use, so the seed is the project's own answer.
    captureWrites([{ path: 'packs/vendor-onboarding.json' }])
    renderDialog(FULL, FULL_CAPS)
    await waitFor(() => expect((screen.getByLabelText('Path') as HTMLInputElement).value).toBe('packs/'))
  })

  it('seeds nothing where the project has no packs directory', async () => {
    captureWrites([{ path: 'vendor-onboarding.json' }])
    renderDialog(FULL, FULL_CAPS)
    await screen.findByRole('option', { name: /minimal-expense-approval/ })
    expect((screen.getByLabelText('Path') as HTMLInputElement).value).toBe('')
  })

  it('relays the chassis’ own refusal when the directory is not there', async () => {
    // Not a sentence invented here: the chassis says which directory and what
    // to do about it, and the dialog carries that through unaltered.
    vi.stubGlobal('fetch', async (url: string) => {
      if (String(url).includes('/api/files')) {
        return { ok: true, status: 200, statusText: '', text: async () => '{"root":"/p","files":[]}' }
      }
      return {
        ok: false,
        status: 404,
        statusText: 'Not Found',
        text: async () =>
          JSON.stringify({ error: 'the directory packs does not exist in the project; create it first' })
      }
    })
    renderDialog(FULL, FULL_CAPS)
    await readyToCreate('packs/new.json')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(
      await screen.findByText(
        /The file could not be created — the directory packs does not exist in the project; create it first/
      )
    ).toBeTruthy()
  })
})

/**
 * Creating a pack while the authoring view is already open.
 *
 * `navigate('/author')` from `/author` matches the same route element, so
 * `AuthorView` does not remount — and a mount-only take of the request never
 * ran again. The editor stayed on whatever it was showing, the dialog's own
 * promise was silently not kept, and the request sat in module state until
 * some later, unrelated mount consumed it, bypassing the dirty-buffer question
 * on the way in.
 */
describe('the Create-pack dialog, with the editor already open', () => {
  function serveProject() {
    const sent: unknown[] = []
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      const text = String(url)
      if (text.includes('/api/files')) {
        return {
          ok: true,
          status: 200,
          statusText: '',
          text: async () =>
            JSON.stringify({
              root: '/p',
              files: [
                { path: 'existing.json', bytes: 2, sha256: 'aa' },
                { path: 'packs/new.json', bytes: 0, sha256: 'bb' }
              ]
            })
        }
      }
      if (init?.method === 'PUT') sent.push(JSON.parse(String(init.body)))
      return {
        ok: true,
        status: 200,
        statusText: '',
        text: async () =>
          JSON.stringify({ path: 'packs/new.json', bytes: 0, sha256: 'bb', content: '', created: true })
      }
    })
    return sent
  }

  function renderBoth() {
    const router = createMemoryRouter(
      [
        {
          path: '*',
          element: (
            <McpContext.Provider value={connected({ client: FULL.client, ...FULL_CAPS })}>
              <CreatePackDialog open onOpenChange={() => {}} />
              <AuthorView />
            </McpContext.Provider>
          )
        }
      ],
      { initialEntries: ['/author'] }
    )
    return render(
      <QueryClientProvider client={testQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
  }

  /**
   * Which file the editor has open, read off the DOM.
   *
   * A role query cannot answer this here: the dialog is a **modal**, so Radix
   * marks the rest of the page `aria-hidden` while it is open and the file
   * list is correctly out of the accessibility tree. `aria-current` is what
   * `AuthorView` writes on the entry it has selected, and it is still there.
   */
  function openedFile(): string | undefined {
    const current = document.querySelector('.file-entry[aria-current="true"] code')
    return current?.textContent ?? undefined
  }

  it('opens the new file in the editor that is already mounted, and consumes the request', async () => {
    serveProject()
    renderBoth()
    await waitFor(() => expect(document.querySelectorAll('.file-entry')).toHaveLength(2))
    expect(openedFile()).toBeUndefined()

    await readyToCreate('packs/new.json')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(openedFile()).toBe('packs/new.json'))
    // Consumed, not left behind for the next mount to pick up.
    expect(takeRequestedOpen()).toBeUndefined()
  })
})
