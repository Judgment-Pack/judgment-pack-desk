import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { InspectorSlotContext, type InspectorSlot } from '../shell/InspectorSlot'
import { PacksLayout } from '../routes/PacksLayout'
import { PacksIndex } from '../routes/PacksIndex'

afterEach(() => { cleanup(); document.getElementById('preview-test-slot')?.remove() })
function setup() {
  const target = document.createElement('div'); target.id = 'preview-test-slot'; document.body.append(target)
  const reveal = vi.fn()
  const slot: InspectorSlot = { open: true, size: 360, tab: null, setTab: vi.fn(), claim: () => () => {}, reveal, target }
  const stub = stubClient({ list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [
    { id: 'alpha', packVersion: '1.0.0', description: 'Vendor approvals', path: 'packs/alpha.json' },
    { id: 'zeta', packVersion: '', detail: 'The file could not be read.' }
  ] }) }) })
  const queryClient = testQueryClient()
  const router = createMemoryRouter([{ path: '/packs', element: <PacksLayout />, children: [
    { index: true, element: <PacksIndex /> }, { path: ':packId', element: <h1>Pack document</h1> }
  ] }], { initialEntries: ['/packs'] })
  render(<QueryClientProvider client={queryClient}><McpContext.Provider value={connected({ client: stub.client })}>
    <InspectorSlotContext.Provider value={slot}><RouterProvider router={router} /></InspectorSlotContext.Provider>
  </McpContext.Provider></QueryClientProvider>)
  return { router, reveal, target, stub }
}

it('previews inventory metadata without navigation or additional runtime calls', async () => {
  const { router, reveal, target, stub } = setup()
  await screen.findByRole('link', { name: /alpha/ })
  expect(screen.queryByText('Select a pack')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Preview alpha' }))
  await within(target).findByRole('heading', { name: 'alpha' })
  expect(router.state.location.pathname).toBe('/packs')
  expect(reveal).toHaveBeenCalledOnce()
  expect(within(target).getByText('packs/alpha.json')).toBeTruthy()
  expect(stub.calls.map(call => call.name)).toEqual(['list_packs'])
  fireEvent.click(screen.getByRole('button', { name: 'Preview zeta' }))
  await within(target).findByRole('heading', { name: 'zeta' })
  expect(within(target).getByText('Unavailable')).toBeTruthy()
  expect(within(target).getByText('The file could not be read.')).toBeTruthy()
})

it('retains search, ordering and scroll when returning from a pack, and releases its preview while away', async () => {
  const { router, target } = setup()
  await screen.findByRole('link', { name: /alpha/ })
  fireEvent.keyDown(screen.getByLabelText('Sort packs'), { key: 'Enter' })
  fireEvent.click(await screen.findByRole('option', { name: 'Pack ID: Z–A' }))
  fireEvent.change(screen.getByRole('searchbox', { name: 'Search packs' }), { target: { value: 'Vendor' } })
  expect(within(screen.getByRole('navigation', { name: 'Packs' })).getAllByRole('link')).toHaveLength(1)
  const list = document.querySelector('[data-pack-list]') as HTMLElement
  list.scrollTop = 80; fireEvent.scroll(list)
  fireEvent.click(screen.getByRole('button', { name: 'Preview alpha' }))
  fireEvent.click(within(target).getByRole('link', { name: 'Open pack' }))
  await screen.findByRole('heading', { name: 'Pack document' })
  await waitFor(() => expect(target.textContent).toBe(''))
  expect(screen.queryByRole('searchbox')).toBeNull()
  // Retained, hidden content cannot change the active route's page measure.
  expect(document.querySelector('[aria-label="Pack collection"]')?.hasAttribute('data-layout')).toBe(false)
  await act(async () => { await router.navigate(-1) })
  expect(screen.getByRole('article', { name: 'Pack collection' }).getAttribute('data-layout')).toBe('page')
  expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('Vendor')
  expect(screen.getByLabelText('Sort packs').textContent).toContain('Pack ID: Z–A')
  expect((document.querySelector('[data-pack-list]') as HTMLElement).scrollTop).toBe(80)
  await within(target).findByRole('heading', { name: 'alpha' })
})

it('clears obsolete preview metadata when the search excludes that pack', async () => {
  const { target } = setup()
  fireEvent.click(await screen.findByRole('button', { name: 'Preview alpha' }))
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'no-match' } })
  expect(screen.getByRole('heading', { name: 'No matching packs' })).toBeTruthy()
  expect(within(target).queryByRole('heading', { name: 'alpha' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Clear search' }))
  await screen.findByRole('link', { name: /alpha/ })
})
