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
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
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

const RUNTIME = stubClient({
  list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [] }) }),
  list_examples: () => ({ text: EXAMPLES }),
  get_example: () => ({ text: TEMPLATE })
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
            value={connected({ client: RUNTIME.client, exampleSupported: true, schemaSupported: false })}
          >
            <AppShell>
              <h1>a route</h1>
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
  await screen.findByRole('dialog', { name: 'Create a pack' })
  return opener
}

let sent: Sent[] = []

beforeEach(() => {
  sent = serveProject()
})

afterEach(() => {
  cleanup()
  forgetConsole()
  forgetAuthorBridge()
  vi.unstubAllGlobals()
  window.localStorage.clear()
})

describe('creating a pack from the rail at 800px, where the rail is a drawer', () => {
  it('sends both writes and leaves neither the dialog nor the drawer standing', async () => {
    // The whole sequence, in the composition it runs in. Closing the dialog is
    // not closing the drawer, and the drawer is modal: the page this navigates
    // to was underneath an overlay with `aria-hidden` on it.
    viewport(800)
    const { seen } = renderDesk()
    await openCreate()

    await waitFor(() =>
      expect(screen.getByLabelText('Template').textContent).toContain('minimal')
    )
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    const create = screen.getByRole('button', { name: 'Create pack' }) as HTMLButtonElement
    await waitFor(() => expect(create.disabled).toBe(false))
    fireEvent.click(create)

    // Two writes, the pack then the registration.
    await waitFor(() => expect(sent).toHaveLength(2))
    expect(sent.map((write) => write.path)).toEqual([
      'packs/vendor-onboarding.pack.json',
      'jpack.json'
    ])

    // The route changed, and nothing modal is left over it.
    await waitFor(() => expect(seen).toContain('/packs/vendor-onboarding'))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create a pack' })).toBeNull())
    await waitFor(() =>
      expect(screen.queryByRole('navigation', { name: 'Project' })).toBeNull()
    )
    // The page underneath is back in the accessibility tree, which is the
    // thing a standing modal drawer takes away.
    expect(screen.getAllByRole('main')).toHaveLength(1)
  })

  it('gives focus back to the Create button on Cancel', async () => {
    viewport(800)
    renderDesk()
    const opener = await openCreate()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create a pack' })).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(opener))
  })

  it('gives focus back to the Create button on Escape', async () => {
    viewport(800)
    renderDesk()
    const opener = await openCreate()
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create a pack' })).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(opener))
  })

  it('gives focus back to the Create button after a successful create', async () => {
    // The success path unmounts the dialog through the rail's own state, so
    // this is the exit most likely to drop focus on `<body>`.
    viewport(800)
    renderDesk()
    const opener = await openCreate()
    await waitFor(() =>
      expect(screen.getByLabelText('Template').textContent).toContain('minimal')
    )
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    const create = screen.getByRole('button', { name: 'Create pack' }) as HTMLButtonElement
    await waitFor(() => expect(create.disabled).toBe(false))
    await act(async () => {
      fireEvent.click(create)
    })
    await waitFor(() => expect(sent).toHaveLength(2))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create a pack' })).toBeNull())
    // The opener has gone with the drawer, so what must not happen is focus
    // being left on a detached node — the document keeps it.
    expect(document.body.contains(opener)).toBe(false)
    expect(document.activeElement === document.body || document.body.contains(document.activeElement)).toBe(true)
  })
})

describe('creating a pack from the rail as a column', () => {
  it('closes the dialog, navigates, and leaves the rail exactly where it was', async () => {
    // Wide, the rail is not modal and there is nothing to dismiss: `onCreated`
    // must not close anything, because the rail is the page's own furniture.
    viewport(1400)
    const { seen } = renderDesk()
    const opener = screen.getByRole('button', { name: 'Create a pack' })
    fireEvent.click(opener)
    await screen.findByRole('dialog', { name: 'Create a pack' })
    await waitFor(() =>
      expect(screen.getByLabelText('Template').textContent).toContain('minimal')
    )
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    const create = screen.getByRole('button', { name: 'Create pack' }) as HTMLButtonElement
    await waitFor(() => expect(create.disabled).toBe(false))
    fireEvent.click(create)

    await waitFor(() => expect(sent).toHaveLength(2))
    await waitFor(() => expect(seen).toContain('/packs/vendor-onboarding'))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create a pack' })).toBeNull())
    // Still there, still a column.
    expect(screen.getByRole('navigation', { name: 'Project' })).toBeTruthy()
    await waitFor(() => expect(document.activeElement).toBe(opener))
  })
})
