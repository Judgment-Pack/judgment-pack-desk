/**
 * The route, as the page runs it: two answers about one file, one check, and
 * the address space joining the document to the Inspector.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { StrictMode, useState } from 'react'
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
/**
 * An `IntersectionObserver` a test drives.
 *
 * jsdom has none and lays nothing out, so the spy's observing path is invisible
 * to every other case in this file — which is exactly where the outline lost
 * four of its five identity members.
 */
function observable() {
  const callbacks: ((entries: { target: Element; isIntersecting: boolean }[]) => void)[] = []
  class Stub {
    constructor(callback: (entries: { target: Element; isIntersecting: boolean }[]) => void) {
      callbacks.push(callback)
    }
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  vi.stubGlobal('IntersectionObserver', Stub)
  return {
    show(...pointers: string[]) {
      act(() => {
        for (const callback of callbacks) {
          callback(
            pointers.map((pointer) => ({
              target: { getAttribute: () => pointer } as unknown as Element,
              isIntersecting: true
            }))
          )
        }
      })
    }
  }
}

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

/**
 * The route under a slot that is **a new object on every render**.
 *
 * That is what a shell does: `AppShell` memoises its slot on the pane's own
 * state, so opening or closing the Inspector hands the route a different
 * object at the same address. A slot built once and reused would make the
 * arrival effect's dependencies stable for the life of the test, and an effect
 * that reopened the pane on every render would go unnoticed — which is the
 * defect the `location.key` guard exists to prevent.
 */
function Slotted({
  revealed,
  inspector,
  tab
}: {
  revealed: string[]
  inspector: boolean
  tab: string | null
}) {
  // The viewer's own hand on the pane, which is the case that matters: closing
  // it re-renders the shell at the same address and hands the route a slot it
  // has not seen before.
  const [open, setOpen] = useState(inspector)
  return (
    <InspectorSlotContext.Provider value={recordingSlot(revealed, open, tab)}>
      <button type="button" onClick={() => setOpen(false)}>
        close the inspector
      </button>
      <PackView />
    </InspectorSlotContext.Provider>
  )
}

function draw(
  handlers: Record<string, ToolHandler>,
  overrides: Partial<McpConnection> = {},
  path = '/packs/vendor-onboarding',
  pane: { inspector?: boolean; tab?: string | null; strict?: boolean } = {}
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
            <Slotted
              revealed={revealed}
              inspector={pane.inspector === true}
              tab={pane.tab ?? null}
            />
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: [path] }
  )
  const tree = (
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  // **StrictMode on request**, because production runs in it: `main.tsx`
  // wraps the app, and an effect that is not idempotent is a defect only
  // visible there. A test that never renders in StrictMode cannot see it.
  const view = render(pane.strict === true ? <StrictMode>{tree}</StrictMode> : tree)
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

describe('a diagnostic about a member the document omits', () => {
  it('is printed on the strip rather than counted and dropped', async () => {
    // A pack with no `specVersion` is refused at `/specVersion`, and nothing
    // draws a required member that is not there — its absence is a refusal
    // rather than an omission, and a block would take this diagnostic off the
    // strip and put it behind a selection nobody has made. So the nearest
    // rendered ancestor is the document itself, and the strip prints it.
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

  it('is printed on the strip where nothing on the page carries its pointer', async () => {
    // The root-anchoring path, with an address the page genuinely does not
    // render. A diagnostic whose pointer and every ancestor of it are absent
    // from the document has nowhere to land but the strip — and it used to be
    // counted in the layer sentence and printed nowhere at all.
    chassis(PACK_TEXT, DIGEST)
    draw({
      get_pack: () => ({
        text: PACK_TEXT,
        structured: { path: PATH, bytes: PACK_TEXT.length, sha256: DIGEST }
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
              code: 'JPS-STRUCTURE-UNKNOWN-MEMBER',
              codeStability: 'provisional',
              layer: 'structural',
              severity: 'error',
              instancePath: '/nonesuch/0/deeper',
              message: 'This member is not in the schema.'
            }
          ],
          diagnosticsTruncated: false
        })
      })
    })
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(screen.getByText(/JPS-STRUCTURE-UNKNOWN-MEMBER/)).toBeTruthy())
    expect(screen.getByText('This member is not in the schema.')).toBeTruthy()
    // Named at its own pointer, verbatim, because no block on the page carries
    // it and inventing a nearer one would put it on the wrong member.
    expect(screen.getByText('/nonesuch/0/deeper')).toBeTruthy()
    expect(document.getElementById('/nonesuch/0/deeper')).toBeNull()
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

describe('which bytes the page says it checked', () => {
  const PAST = /checked against/
  const PRESENT = /checking against/

  it('says nothing at all where the runtime does not offer validate', async () => {
    chassis(PACK_TEXT, DIGEST)
    draw(SERVED, { validateSupported: false })
    await screen.findByRole('heading', { level: 1 })
    expect(screen.getByText(/does not offer validate/)).toBeTruthy()
    // "checked against the bytes of packs/x.pack.json" under "this document is
    // unchecked" is one of the two lying, and the reader cannot tell which.
    expect(screen.queryByText(PAST)).toBeNull()
    expect(screen.queryByText(PRESENT)).toBeNull()
  })

  it('says nothing where the runtime has not said what it can do', async () => {
    chassis(PACK_TEXT, DIGEST)
    draw(SERVED, { known: false })
    await screen.findByRole('heading', { level: 1 })
    expect(screen.getByText(/has not said what it can do/)).toBeTruthy()
    expect(screen.queryByText(PAST)).toBeNull()
  })

  it('says nothing where there are no bytes to check', async () => {
    chassis('', DIGEST)
    draw(SERVED)
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(screen.getByText(/no bytes to check yet/)).toBeTruthy())
    expect(screen.queryByText(PAST)).toBeNull()
  })

  it('is in the present tense while the check is in flight', async () => {
    chassis(PACK_TEXT, DIGEST)
    draw({ ...SERVED, validate: () => new Promise<never>(() => {}) })
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(screen.getByText('Checking…')).toBeTruthy())
    expect(screen.getByText(PRESENT)).toBeTruthy()
    expect(screen.queryByText(PAST)).toBeNull()
  })

  it('is in the past tense once a report has arrived', async () => {
    chassis(PACK_TEXT, DIGEST)
    draw(SERVED)
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(screen.getByText(PAST)).toBeTruthy())
    expect(screen.getByText(PAST).textContent).toContain(PATH)
  })
})

describe('the outline entry an identity member marks', () => {
  const IDENTITY = ['/specVersion', '/id', '/version', '/title', '/description']

  it.each(IDENTITY)('is Identity, for a selection at %s', async (pointer) => {
    // The outline lists `Identity` once and the page draws the five separately,
    // so four of the five equalled no entry and marked nothing at all.
    chassis(PACK_TEXT, DIGEST)
    draw(SERVED, {}, `/packs/vendor-onboarding?at=${encodeURIComponent(pointer)}`)
    await screen.findByRole('heading', { level: 1 })
    const entry = screen.getByRole('link', { name: /Identity/ })
    await waitFor(() => expect(entry.getAttribute('aria-current')).toBe('true'))
    const current = screen
      .getByRole('navigation', { name: 'Members' })
      .querySelectorAll('[aria-current]')
    expect(current).toHaveLength(1)
  })

  it.each(IDENTITY)('is Identity, for an observer reporting %s on screen', async (pointer) => {
    chassis(PACK_TEXT, DIGEST)
    const viewport = observable()
    draw(SERVED)
    await screen.findByRole('heading', { level: 1 })
    viewport.show(pointer)
    const entry = screen.getByRole('link', { name: /Identity/ })
    await waitFor(() => expect(entry.getAttribute('aria-current')).toBe('true'))
  })
})

describe('a check that ran over other bytes', () => {
  // The file on disk moved on after `get_pack` served the page: `rules[0]` is
  // gone, so every `/rules/N` in the check names a different rule from the one
  // the page draws under that pointer.
  const MOVED = (() => {
    const document = JSON.parse(PACK_TEXT) as { rules: unknown[] }
    document.rules = document.rules.slice(1)
    return JSON.stringify(document, null, 2)
  })()
  const MOVED_DIGEST = 'd4d4d4'.padEnd(64, '0')
  const REFUSED = JSON.stringify({
    outputVersion: '2',
    status: 'invalid',
    layers: [
      { name: 'carrier', status: 'passed' },
      { name: 'structural', status: 'passed' },
      { name: 'semantic', status: 'failed' }
    ],
    diagnostics: [
      {
        code: 'JPS-SEMANTIC-UNREACHABLE-RULE',
        codeStability: 'provisional',
        layer: 'semantic',
        severity: 'error',
        instancePath: '/rules/0',
        message: 'This rule can never fire.'
      }
    ],
    diagnosticsTruncated: false
  })

  it('is anchored nowhere, and the strip says which bytes it was about', async () => {
    // **The one this whole comparison exists for.** `/rules/0` resolves on the
    // page, so a report anchored without asking would print a real diagnostic
    // on a rule that is not the rule it is about — which is worse than none,
    // because it looks like an answer.
    chassis(MOVED, MOVED_DIGEST)
    draw(
      {
        get_pack: () => ({
          text: PACK_TEXT,
          structured: { path: PATH, bytes: PACK_TEXT.length, sha256: DIGEST }
        }),
        validate: () => ({ text: REFUSED })
      },
      {},
      '/packs/vendor-onboarding?at=/rules/0',
      { inspector: true, tab: 'checks' }
    )
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() =>
      expect(screen.getByText(/ran over different bytes from the ones shown/)).toBeTruthy()
    )
    // The panel is open on that very rule and says the same thing rather than
    // listing what the check found.
    expect(screen.getByText(/computed against other bytes/)).toBeTruthy()
    expect(screen.queryByText('JPS-SEMANTIC-UNREACHABLE-RULE')).toBeNull()
    expect(screen.queryByText('This rule can never fire.')).toBeNull()
    // And it does not answer "No other diagnostic names this member" either:
    // that is a clean bill drawn from a report about other bytes.
    expect(screen.queryByText(/No other diagnostic names this member/)).toBeNull()
  })

  it('still anchors where the bytes are the same', async () => {
    // The control. Same document from both sources, and the diagnostic reaches
    // the panel for the member it names.
    chassis(PACK_TEXT, DIGEST)
    draw(
      { ...SERVED, validate: () => ({ text: REFUSED }) },
      {},
      '/packs/vendor-onboarding?at=/rules/0',
      { inspector: true, tab: 'checks' }
    )
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(screen.getByText('This rule can never fire.')).toBeTruthy())
    expect(screen.queryByText(/ran over different bytes/)).toBeNull()
  })
})

describe('a document with no bytes in it', () => {
  it('says there is nothing to check rather than checking for ever', async () => {
    // `useValidate` disables itself for an empty buffer, and a disabled query
    // reports `isPending` for ever — so the strip printed "Checking…" about a
    // check that was never going to start, for the whole of the visit.
    chassis('', DIGEST)
    draw(SERVED)
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() =>
      expect(screen.getByText(/no bytes to check yet/)).toBeTruthy()
    )
    expect(screen.queryByText('Checking…')).toBeNull()
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

  it('opens it on arrival at an address that already carries one', async () => {
    // `?at` is what the desk writes, so it is what a copied link carries.
    const { revealed } = draw(SERVED, {}, '/packs/vendor-onboarding?at=%2Frules%2F0')
    await screen.findByRole('heading', { level: 1 })
    expect(revealed).toEqual(['reveal'])
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

describe('arriving at an address that names a member', () => {
  it('opens the Inspector once, in StrictMode, where production runs', async () => {
    // Reveal was "if closed, toggle" — the same gesture read twice. StrictMode
    // runs an effect twice on purpose, so an arrival made two toggles out of
    // one arrival and left the pane exactly as it found it: closed. The link
    // somebody sent landed on a closed Inspector.
    const { revealed } = draw(SERVED, {}, '/packs/vendor-onboarding?at=/rules/0', {
      strict: true
    })
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(revealed.length).toBeGreaterThan(0))
    // Whatever the count, the pane is asked to *open* and never to flip: a
    // second call is idempotent, which is the property that matters.
    expect(revealed.every((call) => call === 'reveal')).toBe(true)
  })

  it('opens it again for a link to another member of the same pack', async () => {
    // `/packs/a` → `/packs/a?at=/rules/0` reuses this component, so a mount
    // -only effect made *zero* calls: a References link, or any deep link
    // followed from inside the pack, opened nothing at all.
    const { revealed, router } = draw(SERVED, {}, '/packs/vendor-onboarding')
    await screen.findByRole('heading', { level: 1 })
    expect(revealed).toEqual([])

    await act(async () => {
      await router.navigate('/packs/vendor-onboarding?at=/rules/0')
    })
    await waitFor(() => expect(revealed).toEqual(['reveal']))

    // And a second address, in the same pack, is a second arrival.
    await act(async () => {
      await router.navigate('/packs/vendor-onboarding?at=/outcomes/0')
    })
    await waitFor(() => expect(revealed).toEqual(['reveal', 'reveal']))
  })

  it('does not reopen it on a rerender that changes no address', async () => {
    // The other half: this must not fight a viewer who has closed the pane and
    // stayed where they are. The unit is a history entry, not a render.
    const { revealed, rerender, router } = draw(
      SERVED,
      {},
      '/packs/vendor-onboarding?at=/rules/0'
    )
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(revealed).toEqual(['reveal']))
    await act(async () => {
      rerender(
        <QueryClientProvider client={testQueryClient()}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      )
    })
    expect(revealed).toEqual(['reveal'])
  })

  it('opens it again when Back returns to an address that carries one', async () => {
    // **Every arrival is recorded, not only the ones that revealed.** Select a
    // member (entry K), follow a link inside the pack that carries no selection
    // (entry U), close the pane, press Back: U returned before writing anything
    // down, so K still looked like the last entry this had handled and Back —
    // an arrival at an address naming a member — opened nothing.
    const { revealed, router } = draw(SERVED, {}, '/packs/vendor-onboarding?at=/rules/0')
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(revealed).toEqual(['reveal']))

    await act(async () => {
      await router.navigate('/packs/vendor-onboarding')
    })
    expect(revealed).toEqual(['reveal'])

    await act(async () => {
      await router.navigate(-1)
    })
    await waitFor(() => expect(revealed).toEqual(['reveal', 'reveal']))
    expect(router.state.location.search).toContain('at=')
  })

  it('does not reopen the pane the viewer just closed', async () => {
    // **The case the guard is for.** Closing the Inspector re-renders the shell
    // and hands the route a new slot object at the same address; an arrival
    // effect that fires on that runs "open it" the instant the viewer shuts it,
    // and no amount of clicking can close a pane that reopens itself.
    const { revealed } = draw(SERVED, {}, '/packs/vendor-onboarding?at=/rules/0', {
      inspector: true
    })
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(revealed).toEqual(['reveal']))
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'close the inspector' }))
    })
    expect(revealed).toEqual(['reveal'])
  })
})
