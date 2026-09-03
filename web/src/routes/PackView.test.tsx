/**
 * The route, as the page runs it: two answers about one file, one check, and
 * the address space joining the document to the Inspector.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient, type ToolHandler } from '../testing/harness'
import { PackView } from './PackView'

const PACK_TEXT = readFileSync(
  join(import.meta.dirname, '..', 'packs', '__fixtures__', 'full.pack.json'),
  'utf8'
)
const PATH = 'packs/vendor-onboarding.pack.json'
const DIGEST = 'a1b2c3'.padEnd(64, '0')

const CLEAN = JSON.stringify({
  outputVersion: '2',
  status: 'valid',
  specVersion: '0.2.0-draft',
  layers: [
    { name: 'carrier', status: 'passed' },
    { name: 'structural', status: 'passed' },
    { name: 'semantic', status: 'passed' }
  ],
  diagnostics: [],
  diagnosticsTruncated: false
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

/** The chassis, answering the one file read this route makes. */
function chassis(content: string | undefined, sha256: string) {
  vi.stubGlobal('fetch', async (url: string) => {
    if (String(url).includes('/api/file?') && content !== undefined) {
      return {
        ok: true,
        status: 200,
        statusText: '',
        text: async () =>
          JSON.stringify({ path: PATH, bytes: content.length, sha256, content })
      }
    }
    return {
      ok: false,
      status: 404,
      statusText: '',
      text: async () => JSON.stringify({ error: 'no such file' })
    }
  })
}

function draw(
  handlers: Record<string, ToolHandler>,
  overrides: Partial<McpConnection> = {},
  path = '/packs/vendor-onboarding'
) {
  const stub = stubClient(handlers)
  const router = createMemoryRouter(
    [
      {
        path: '/packs/:packId',
        element: (
          <McpContext.Provider
            value={connected({ client: stub.client, validateSupported: true, ...overrides })}
          >
            <PackView />
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: [path] }
  )
  const view = render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return { ...view, router, calls: stub.calls }
}

const SERVED: Record<string, ToolHandler> = {
  get_pack: () => ({
    text: PACK_TEXT,
    structured: { path: PATH, bytes: PACK_TEXT.length, sha256: DIGEST }
  }),
  validate: () => ({ text: CLEAN })
}

describe('the document and the check strip', () => {
  beforeEach(() => chassis(PACK_TEXT, DIGEST))

  it('renders the document and the runtime’s own layer sentence', async () => {
    draw(SERVED)
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() =>
      expect(
        screen.getByText(
          'valid — carrier passed, structural passed, semantic passed, 0 diagnostics.'
        )
      ).toBeTruthy()
    )
    expect(screen.getByText(`checked against the bytes of ${PATH}`)).toBeTruthy()
  })

  it('checks with the document alone, keyed on the bytes and the epoch', async () => {
    const { calls } = draw(SERVED)
    await waitFor(() => expect(calls.some((call) => call.name === 'validate')).toBe(true))
    const validate = calls.find((call) => call.name === 'validate')!
    expect(Object.keys(validate.args)).toEqual(['document'])
    expect(validate.args.document).toBe(PACK_TEXT)
  })

  it('says the tool was never offered rather than that the document is fine', async () => {
    draw(SERVED, { validateSupported: false })
    await screen.findByRole('heading', { level: 1 })
    expect(
      screen.getByText('This runtime does not offer validate, so this document is unchecked.')
    ).toBeTruthy()
    expect(screen.queryByText(/valid —/)).toBeNull()
  })

  it('says the listing is what is missing where nothing has answered', async () => {
    draw(SERVED, { known: false, validateSupported: false })
    await screen.findByRole('heading', { level: 1 })
    expect(
      screen.getByText('The runtime has not said what it can do, so this document is unchecked.')
    ).toBeTruthy()
  })
})

describe('the two answers about one file', () => {
  it('says plainly when the digests disagree', async () => {
    chassis(PACK_TEXT, 'f'.repeat(64))
    draw(SERVED)
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() =>
      expect(screen.getByText(/do not describe one revision/)).toBeTruthy()
    )
  })

  it('says nothing where they agree', async () => {
    chassis(PACK_TEXT, DIGEST)
    draw(SERVED)
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(screen.getByText(/checked against the bytes/)).toBeTruthy())
    expect(screen.queryByText(/do not describe one revision/)).toBeNull()
  })

  it('checks the served document, and labels it, where the file did not load', async () => {
    chassis(undefined, DIGEST)
    const { calls } = draw(SERVED)
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() =>
      expect(screen.getByText('checked against the document the runtime served')).toBeTruthy()
    )
    expect(calls.find((call) => call.name === 'validate')!.args.document).toBe(PACK_TEXT)
  })

  it('withholds editing where its own reading of the bytes disagrees with JSON.parse', async () => {
    const duplicated = readFileSync(
      join(import.meta.dirname, '..', 'packs', '__fixtures__', 'duplicate-member.pack.json'),
      'utf8'
    )
    chassis(duplicated, DIGEST)
    draw(SERVED)
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() =>
      expect(screen.getByText(/will not edit around this file/)).toBeTruthy()
    )
  })
})

describe('the deep link', () => {
  beforeEach(() => chassis(PACK_TEXT, DIGEST))

  it('scrolls to, focuses and selects the block, replacing rather than pushing', async () => {
    const scrolled: string[] = []
    Element.prototype.scrollIntoView = function scroll(this: Element) {
      scrolled.push(this.getAttribute('data-pointer') ?? '')
    }
    const { router } = draw(SERVED, {}, '/packs/vendor-onboarding#/rules/0')
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(router.state.location.search).toBe('?at=%2Frules%2F0'))
    expect(scrolled).toContain('/rules/0')
    expect(document.activeElement?.getAttribute('data-pointer')).toBe('/rules/0')
    // Selecting is not navigating: one entry in, one entry still.
    expect(router.state.historyAction).toBe('REPLACE')
  })

  it('changes nothing for a pointer the document does not render', async () => {
    const { router } = draw(SERVED, {}, '/packs/vendor-onboarding#/rules/99')
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(screen.getByText(/checked against/)).toBeTruthy())
    expect(router.state.location.search).toBe('')
  })

  it('ignores a fragment that is not a pointer at all', async () => {
    const { router } = draw(SERVED, {}, '/packs/vendor-onboarding#shortcuts')
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(screen.getByText(/checked against/)).toBeTruthy())
    expect(router.state.location.search).toBe('')
  })
})
