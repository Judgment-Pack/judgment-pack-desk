/**
 * The route, as the page runs it: two answers about one file, one check, and
 * the address space joining the document to the Inspector.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { InspectorSlotContext, type InspectorSlot } from '../shell/InspectorSlot'
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
  slotTarget?.remove()
  slotTarget = null
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

/**
 * A slot that records the one thing this route asks of it.
 *
 * `target` is null by default, so the Inspector's portal renders nowhere and
 * every case about the *document* asserts against the document alone. A case
 * about a panel passes `inspector: true` and gets a real element, torn down
 * with the render.
 */
let slotTarget: HTMLElement | null = null

function recordingSlot(revealed: string[], inspector: boolean, tab: string | null): InspectorSlot {
  if (inspector && slotTarget === null) {
    slotTarget = document.createElement('div')
    document.body.appendChild(slotTarget)
  }
  return {
    open: inspector,
    size: 0,
    tab,
    setTab: () => {},
    target: inspector ? slotTarget : null,
    claim: () => () => {},
    reveal: () => revealed.push('reveal')
  }
}

function draw(
  handlers: Record<string, ToolHandler>,
  overrides: Partial<McpConnection> = {},
  path = '/packs/vendor-onboarding',
  pane: { inspector?: boolean; tab?: string | null } = {}
) {
  const stub = stubClient(handlers)
  const revealed: string[] = []
  const router = createMemoryRouter(
    [
      {
        path: '/packs/:packId',
        element: (
          <McpContext.Provider
            value={connected({ client: stub.client, validateSupported: true, ...overrides })}
          >
            <InspectorSlotContext.Provider
              value={recordingSlot(revealed, pane.inspector === true, pane.tab ?? null)}
            >
              <PackView />
            </InspectorSlotContext.Provider>
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
  return { ...view, router, calls: stub.calls, revealed }
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

describe('the other two views on this pack', () => {
  beforeEach(() => chassis(PACK_TEXT, DIGEST))

  it('offers the what-if view, which nothing else links to any more', async () => {
    // `PackDetail` carried the only link to it and the rail carried a per-pack
    // Evaluate child; both went with this rewrite, and the route was reachable
    // only by typing its URL.
    draw(SERVED)
    const link = await screen.findByRole('link', { name: 'Try it' })
    expect(link.getAttribute('href')).toBe('/packs/vendor-onboarding/evaluate')
  })

  it('offers the matrix only where the listing says the pack declares one', async () => {
    draw(SERVED)
    await screen.findByRole('link', { name: 'Try it' })
    expect(screen.queryByRole('link', { name: 'Test matrix' })).toBeNull()

    cleanup()
    draw({
      ...SERVED,
      list_packs: () => ({
        text: JSON.stringify({
          status: 'valid',
          packs: [{ id: 'vendor-onboarding', matrix: true }]
        })
      })
    })
    expect(await screen.findByRole('link', { name: 'Test matrix' })).toBeTruthy()
  })
})

describe('a diagnostic about a member the page does not render', () => {
  it('is printed on the strip rather than counted and dropped', async () => {
    // A pack with no `specVersion` is refused at `/specVersion`, and the
    // eyebrow renders that member only when it is there — so the diagnostic's
    // nearest rendered ancestor is the document itself. It was counted in the
    // layer sentence and printed nowhere at all.
    const without = JSON.parse(PACK_TEXT) as Record<string, unknown>
    delete without.specVersion
    const text = JSON.stringify(without, null, 2)
    chassis(text, DIGEST)
    draw({
      get_pack: () => ({
        text,
        structured: { path: PATH, bytes: text.length, sha256: DIGEST }
      }),
      validate: () => ({
        text: JSON.stringify({
          outputVersion: '2',
          status: 'invalid',
          layers: [
            { name: 'carrier', status: 'passed' },
            { name: 'structural', status: 'failed' }
          ],
          diagnostics: [
            {
              code: 'JPS-STRUCTURE-REQUIRED-MEMBER',
              codeStability: 'provisional',
              layer: 'structural',
              severity: 'error',
              instancePath: '/specVersion',
              message: 'Required member is missing.'
            }
          ],
          diagnosticsTruncated: false
        })
      })
    })
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(screen.getByText(/JPS-STRUCTURE-REQUIRED-MEMBER/)).toBeTruthy())
    expect(screen.getByText('Required member is missing.')).toBeTruthy()
    // Named at its own pointer, verbatim, because that is the member it is
    // about and no block on the page carries it.
    expect(screen.getByText('/specVersion')).toBeTruthy()
    expect(document.getElementById('/specVersion')).toBeNull()
  })
})

describe('the outline', () => {
  beforeEach(() => chassis(PACK_TEXT, DIGEST))

  it('marks the unit a selection under it belongs to', async () => {
    // `?at` addresses every block; the outline lists twelve units. Returning
    // the selection verbatim marked no entry at all and threw the scroll-spy's
    // answer away for the rest of the visit.
    draw(SERVED, {}, '/packs/vendor-onboarding?at=%2Frules%2F0')
    await screen.findByRole('heading', { level: 1 })
    const outline = screen.getByRole('navigation', { name: 'Members' })
    await waitFor(() =>
      expect(outline.querySelector('[aria-current="true"]')?.textContent).toContain('Rules')
    )
  })

  it('selects without pushing, and keeps the rest of the address', async () => {
    const { router } = draw(SERVED, {}, '/packs/vendor-onboarding?token=abc123')
    await screen.findByRole('heading', { level: 1 })
    const outline = screen.getByRole('navigation', { name: 'Members' })
    fireEvent.click(outline.querySelectorAll('a')[1]!)
    await waitFor(() => expect(router.state.location.search).toContain('at='))
    expect(router.state.location.search).toContain('token=abc123')
    expect(router.state.historyAction).toBe('REPLACE')
  })
})

describe('choosing a member with the pane closed', () => {
  beforeEach(() => chassis(PACK_TEXT, DIGEST))

  it('opens the Inspector, because that is what selecting is for', async () => {
    // Otherwise the panel is filled behind a closed pane and the only thing
    // that changes on screen is the block's own border.
    const { revealed, container } = draw(SERVED)
    await screen.findByRole('heading', { level: 1 })
    expect(revealed).toEqual([])
    fireEvent.click(container.querySelector('[data-pointer="/rules/0"]')!)
    expect(revealed).toEqual(['reveal'])
  })

  it('opens it for a deep link too, which is a selection someone was sent', async () => {
    const { revealed } = draw(SERVED, {}, '/packs/vendor-onboarding#/rules/0')
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(revealed).toEqual(['reveal']))
  })
})

describe('the Checks panel while the check is in flight', () => {
  beforeEach(() => chassis(PACK_TEXT, DIGEST))

  it('says the check has not answered rather than that nothing was found', async () => {
    // The strip says "Checking…" and the panel beside it said "No other
    // diagnostic names this member." — a clean bill from a check that has not
    // spoken, for the whole of every page load.
    draw(
      { ...SERVED, validate: () => new Promise<never>(() => {}) },
      {},
      '/packs/vendor-onboarding?at=%2Frules%2F0',
      { inspector: true, tab: 'checks' }
    )
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(screen.getByText('Checking…')).toBeTruthy())
    const panel = screen.getByRole('tabpanel')
    expect(panel.textContent).toContain('The check has not answered yet.')
    expect(panel.textContent).not.toContain('No other diagnostic names this member.')
  })

  it('leaves the pane’s own empty state standing while the pack is loading', async () => {
    // The route calls the slot hook above its early returns and hands it null
    // until `get_pack` answers. Claiming on the target alone suppressed the
    // pane's empty state for all of that time, and for ever after a refusal.
    draw(
      {
        get_pack: () => {
          throw new Error('the runtime refused the pack')
        }
      },
      {},
      '/packs/vendor-onboarding',
      { inspector: true }
    )
    await screen.findByText(/the runtime refused the pack/)
    expect(slotTarget!.textContent).toBe('')
  })
})
