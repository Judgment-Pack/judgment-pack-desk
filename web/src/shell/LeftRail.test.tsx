/** Navigation never executes suites or fetches graph inventory. */
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { LeftRail } from './LeftRail'
import { HeaderBar } from './HeaderBar'
import { forgetAuthorBridge, publishDirty } from './authorBridge'

afterEach(() => {
  cleanup()
  forgetAuthorBridge()
})

function packs(ids: string[]) {
  return stubClient({
    list_packs: () => ({
      text: JSON.stringify({ status: 'valid', packs: ids.map((id) => ({ id })) })
    })
  })
}

function renderRail(
  stub: ReturnType<typeof stubClient>,
  overrides: Partial<McpConnection> = {},
  path = '/',
  header = false
) {
  const value = connected({ client: stub.client, ...overrides })
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={value}>
            <Tooltip.Provider>
              {header && <HeaderBar inspectorOpen={false} inspectorIsDrawer={false} consoleOpen={false}
                onToggleInspector={() => {}} onToggleConsole={() => {}} railIsDrawer={false} railDrawerOpen={false} onOpenRail={() => {}} />}
              <LeftRail
                mode="expanded"
                onToggle={() => {}}
                asDrawer={false}
                drawerOpen={false}
                onDrawerOpenChange={() => {}}
              />
            </Tooltip.Provider>
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: [path] }
  )
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('the left rail', () => {
  it('never runs the whole-project graph walk, on any runtime', async () => {
    const stub = packs(['intake-triage'])
    // A runtime with no graph tools at all: exactly the case where
    // `useConfiguredGraphs` falls back to running every graph's matrix.
    renderRail(stub, { graphInventorySupported: false })
    await screen.findByRole('link', { name: /^Packs/ })
    expect(stub.calls.map((call) => call.name)).toEqual(['list_packs'])
    expect(stub.calls.every((call) => call.name !== 'experimental_test_graphs')).toBe(true)
  })

  it('leaves starter requests to the creation page', async () => {
    // The Create-pack dialog was mounted unconditionally, so its body ran on
    // every route: `list_examples` on first paint everywhere, and again on
    // every `desk/fileChanged`, because a mounted query is an active one. This
    // is the same objection as the graph walk above, one order of magnitude
    // smaller, and it is held by a runtime that *does* advertise the tools —
    // which is what the earlier rail tests, on `UNKNOWN_CAPABILITIES`, could
    // not see.
    const stub = stubClient({
      list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [] }) }),
      list_examples: () => ({ text: JSON.stringify({ examples: [{ name: 'minimal' }] }) }),
      get_example: () => ({ text: '{}' }),
      get_schema: () => ({ text: '{}' })
    })
    renderRail(stub, { exampleSupported: true, schemaSupported: true })
    await screen.findByRole('button', { name: 'New chat' })
    expect(stub.calls.map((call) => call.name)).toEqual(['list_packs'])

    fireEvent.click(screen.getByRole('button', { name: 'New chat' }))
    expect(stub.calls.map((call) => call.name)).not.toContain('list_examples')
  })

  it('keeps secondary features out of the primary rail', async () => {
    const stub = packs([])
    renderRail(stub, { graphInventorySupported: true })
    await screen.findByRole('link', { name: /^Packs/ })
    for (const name of ['Author', 'Graphs', 'Matrix and coverage']) expect(screen.queryByRole('link', { name })).toBeNull()
    expect(stub.calls.map(call => call.name)).toEqual(['list_packs'])
  })

  it('is one destination with a count, not a list', async () => {
    // The list moved into main's left pane. A project can carry hundreds of
    // packs and a rail cannot: the old entry capped at thirty and handed the
    // rest to the project home, which is a list that stops being one exactly
    // when it would start being useful.
    renderRail(packs(['intake-triage', 'vendor-onboarding']))
    const link = await screen.findByRole('link', { name: /^Packs/ })
    expect(link.getAttribute('href')).toBe('/packs')
    // The **accessible name**, not the markup. Every rail entry carries an
    // `aria-label`, which replaces its contents, so a count that lived only in
    // a child span was a number no screen reader ever reached.
    await waitFor(() => expect(link.getAttribute('aria-label')).toBe('Packs, 2'))
    expect(link.textContent).toContain('2')
    expect(screen.queryByRole('link', { name: 'intake-triage' })).toBeNull()
    expect(screen.queryByRole('link', { name: 'show all →' })).toBeNull()
  })

  it('shows a failed listing as the failure, and claims no count', async () => {
    // "This project declares no packs" and "the listing did not answer" are
    // two different statements, and a `0` here would be the first one said
    // about a project the desk knows nothing about.
    const stub = stubClient({
      list_packs: () => {
        throw new Error('the runtime refused the listing')
      }
    })
    renderRail(stub)
    await screen.findByText(/the runtime refused the listing/)
    const link = screen.getByRole('link', { name: /^Packs/ })
    expect(link.textContent).not.toContain('0')
    // And the name says no number either, which is the same claim in the place
    // assistive technology reads it.
    expect(link.getAttribute('aria-label')).toBe('Packs')
  })

  it.each(['/packs', '/packs/vendor', '/matrix', '/graphs', '/graphs/onboarding'])('marks Packs active at %s', async path => {
    renderRail(packs([]), {}, path)
    await waitFor(() =>
      expect(
        screen.getByRole('link', { name: /^Packs/ }).getAttribute('aria-current')
      ).toBe('page')
    )
  })

  it('shows a dot in the project menu trigger exactly while the buffer is dirty', async () => {
    renderRail(packs([]), {}, '/', true)
    expect(screen.queryByLabelText('unsaved changes')).toBeNull()
    fireEvent.click(document.body)
    publishDirty('jpack.json', true)
    await waitFor(() => expect(screen.getByLabelText('unsaved changes')).toBeTruthy())
    publishDirty('jpack.json', false)
    await waitFor(() => expect(screen.queryByLabelText('unsaved changes')).toBeNull())
  })

  it('keeps the dot while any editor is dirty, and drops it when none is', async () => {
    // **The dot means "something has unsaved bytes", and there are two
    // editors now.** With one module-level flag the last publisher decided for
    // both: a pack editor mounting clean withdrew the dot the authoring view
    // was holding.
    //
    // **Each publish is flushed, and the assertions are synchronous.** Written
    // with `waitFor`, the middle one passed the moment it was called — before
    // React had processed the notification the publish had just queued — so a
    // withdrawal that took the dot away was invisible to it. The mutation
    // harness is what found that: clearing the whole map on every publish left
    // this case green. `act` makes each publish a completed render, and an
    // assertion that has to be true *now* is one a later render cannot repair.
    renderRail(packs([]), {}, '/', true)
    fireEvent.click(document.body)
    await act(async () => {
      publishDirty('jpack.json', true)
    })
    await act(async () => {
      publishDirty('packs/vendor-onboarding.pack.json', true)
    })
    expect(screen.getByLabelText('unsaved changes')).toBeTruthy()
    await act(async () => {
      publishDirty('packs/vendor-onboarding.pack.json', false)
    })
    // One editor is still dirty, so the dot stands. Each publisher withdraws
    // its own key and nobody else's.
    expect(screen.getByLabelText('unsaved changes')).toBeTruthy()
    await act(async () => {
      publishDirty('jpack.json', false)
    })
    expect(screen.queryByLabelText('unsaved changes')).toBeNull()
  })

  it('offers the Admin sections in one menu, in their declared order', async () => {
    renderRail(packs([]))
    fireEvent.keyDown(screen.getByRole('button', { name: 'Admin sections' }), { key: 'Enter' })
    const items = await screen.findAllByRole('menuitem')
    expect(items.map((item) => item.textContent)).toEqual([
      'Project',
      'Organization',
      'Storage',
      'Assistant',
      'Identity provider'
    ])
  })
})
