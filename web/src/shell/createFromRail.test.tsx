/**
 * Creating a pack from the rail, in the composition the rail actually is.
 *
 * `CreatePackDialog.test.tsx` mounts the dialog on its own, which is right for
 * everything the dialog decides — and blind to the two things only the
 * composition can show. Below 900px the rail is a **modal drawer**, and the
 * dialog is mounted inside it: closing the dialog is not closing the drawer,
 * and the drawer is what sits over the page the create navigated to. And the
 * button that opens the dialog lives here, so focus restoration is a fact about
 * these two files together and about neither of them alone.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, Routes, Route, createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { CreatePackPage } from '../routes/CreatePackPage'
import { AppShell } from './AppShell'
import { forgetAuthorBridge } from './authorBridge'
import { forgetConsole } from './consoleLog'

const EXAMPLES = JSON.stringify({
  status: 'valid',
  examples: [{ name: 'minimal-expense-approval' }]
})

const TEMPLATE = JSON.stringify({
  specVersion: '0.2.0-draft',
  id: 'https://served.example/examples/minimal',
  version: '9.9.9',
  title: 'The example’s own title',
  outcomes: [{ id: 'approve' }, { id: 'decline' }],
  rules: [{ id: 'r1' }]
})

const PROJECT = `{
  "configVersion": "2",
  "packs": {}
}
`

let validationStatus = 'valid'
const RUNTIME = stubClient({
  list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [] }) }),
  list_examples: () => ({ text: EXAMPLES }),
  get_example: () => ({ text: TEMPLATE }),
  validate: () => ({ text: JSON.stringify({ status: validationStatus, diagnostics: [] }) })
})

interface Sent {
  path: string
  body: Record<string, unknown>
}

/** The project the chassis serves, and everything the desk wrote to it. */
function serveProject(): Sent[] {
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
      return ok({ root: '/p', files: [{ path: 'jpack.json', bytes: 1, sha256: 'aa' }] })
    }
    if (init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as Record<string, unknown>
      sent.push({ path: String(body.path), body })
      return ok({ path: body.path, bytes: 2, sha256: 'cc', content: body.content, created: true })
    }
    if (text.includes('path=jpack.json')) {
      return ok({ path: 'jpack.json', bytes: PROJECT.length, sha256: 'ab'.repeat(32), content: PROJECT })
    }
    return {
      ok: false,
      status: 404,
      statusText: 'Not Found',
      text: async () => '{"error":"no such file in the project","code":"not-found"}'
    }
  })
  return sent
}

/** Answer every `(max-width: Npx)` query against one viewport width. */
function viewport(width: number) {
  vi.stubGlobal('matchMedia', (query: string) => {
    const limit = /max-width:\s*(\d+)px/.exec(query)
    return {
      media: query,
      matches: limit === null ? false : width <= Number(limit[1]),
      onchange: null,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent: () => false
    }
  })
}

function renderDesk() {
  const seen: string[] = []
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider
            value={connected({ client: RUNTIME.client, exampleSupported: true, schemaSupported: false, validateSupported: true })}
          >
            <AppShell>
              <Routes><Route path="/create-pack" element={<CreatePackPage />} /><Route path="*" element={<h1>a route</h1>} /></Routes>
            </AppShell>
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: ['/'] }
  )
  router.subscribe((state) => seen.push(state.location.pathname))
  return {
    seen,
    router,
    ...render(
      <QueryClientProvider client={testQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
  }
}

/** Open the drawer, then the dialog inside it, and hand back the opener. */
async function openCreate(): Promise<HTMLElement> {
  fireEvent.click(screen.getByRole('button', { name: 'Project navigation' }))
  await screen.findByRole('navigation', { name: 'Project' })
  const opener = screen.getByRole('button', { name: 'Create a pack' })
  fireEvent.click(opener)
  await screen.findByRole('heading', { name: 'Create a pack' })
  return opener
}

let sent: Sent[] = []

beforeEach(() => {
  validationStatus = 'valid'
  sent = serveProject()
  vi.spyOn(window, 'confirm').mockReturnValue(false)
})

afterEach(() => {
  cleanup()
  forgetConsole()
  forgetAuthorBridge()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  window.localStorage.clear()
})


async function nameAndBuild() {
  await waitFor(() => expect(screen.getByLabelText('Starting template').textContent).toContain('minimal'))
  fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: 'Vendor Onboarding' } })
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
  await screen.findByRole('heading', { name: 'Build your decision' })
}
async function review() {
  fireEvent.click(screen.getByRole('button', { name: 'Review pack' }))
  await screen.findByRole('heading', { name: 'Review your pack' })
}

describe('guided creation from the rail', () => {
  it('opens the creation page and dismisses the mobile navigation drawer', async () => {
    viewport(800)
    const { router } = renderDesk()
    await openCreate()
    expect(router.state.location.pathname).toBe('/create-pack')
    expect(screen.queryByRole('navigation', { name: 'Project' })).toBeNull()
    expect(screen.queryByRole('dialog')).toBeNull()
    expect((screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('radio', { name: 'Draft with AI' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('checks the edited draft and writes those exact bytes only after Review and Create', async () => {
    viewport(800)
    const { router } = renderDesk()
    await openCreate()
    await nameAndBuild()
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'JSON' }), { button: 0 })
    const editor = await screen.findByLabelText('Draft document')
    const draft = JSON.stringify({ ...JSON.parse((editor as HTMLTextAreaElement).value), description: 'Reviewed by the author', extensions: { 'example.keep': { a: 1 } } }, null, 2)
    fireEvent.change(editor, { target: { value: draft } })
    await review()
    expect(sent).toEqual([])
    const create = screen.getByRole('button', { name: 'Create pack' }) as HTMLButtonElement
    await waitFor(() => expect(create.disabled).toBe(false))
    fireEvent.click(create)
    await waitFor(() => expect(sent).toHaveLength(2))
    expect(sent.map((write) => write.path)).toEqual(['packs/vendor-onboarding.pack.json', 'jpack.json'])
    expect(sent[0]!.body.content).toBe(draft)
    await waitFor(() => expect(router.state.location.pathname).toBe('/packs/vendor-onboarding'))
    expect(window.confirm).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('keeps invalid drafts editable and refuses creation until the runtime validates them', async () => {
    validationStatus = 'invalid'
    viewport(800); renderDesk(); await openCreate(); await nameAndBuild(); await review()
    await screen.findByText(/will not call this document a pack/)
    expect((screen.getByRole('button', { name: 'Create pack' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    await screen.findByRole('heading', { name: 'Build your decision' })
    expect(sent).toEqual([])
  })

  it('retains the draft when a dirty navigation is declined and discards it when confirmed', async () => {
    viewport(800)
    const { router } = renderDesk()
    await openCreate(); await nameAndBuild()
    await act(async () => { await router.navigate('/packs') })
    await waitFor(() => expect(window.confirm).toHaveBeenCalled())
    expect(router.state.location.pathname).toBe('/create-pack')
    expect(screen.getByRole('heading', { name: 'Build your decision' })).toBeTruthy()
    vi.mocked(window.confirm).mockReturnValue(true)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/packs'))
    expect(sent).toEqual([])
  })

  it('cancels an untouched creation page without a discard prompt', async () => {
    viewport(800)
    const { router } = renderDesk()
    await openCreate()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/packs'))
    expect(window.confirm).not.toHaveBeenCalled()
  })

  it('keeps the desktop rail in place when opening the creation page', async () => {
    viewport(1400)
    const { router } = renderDesk()
    const rail = screen.getByRole('navigation', { name: 'Project' })
    fireEvent.click(screen.getByRole('button', { name: 'Create a pack' }))
    await screen.findByRole('heading', { name: 'Create a pack' })
    expect(router.state.location.pathname).toBe('/create-pack')
    expect(document.activeElement).toBe(screen.getByLabelText('Name (required)'))
    expect(screen.getByRole('navigation', { name: 'Project' })).toBe(rail)
  })
})
