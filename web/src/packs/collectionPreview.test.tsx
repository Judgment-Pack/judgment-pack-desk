import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { Link, Outlet, RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AppShell } from '../shell/AppShell'
import { projectKey, shellStateKey } from '../shell/paneState'
import { PacksLayout } from '../routes/PacksLayout'
import { inspectorGeometry } from '../shell/inspectorGeometry'

const root = '/preview-project'
const key = shellStateKey(projectKey(root))
beforeEach(() => {
  vi.stubGlobal('fetch', async (url: string) => ({
    ok: String(url).includes('/api/files'), status: String(url).includes('/api/files') ? 200 : 404,
    statusText: '', text: async () => JSON.stringify(String(url).includes('/api/files') ? { root, files: [] } : { error: 'not found' })
  }))
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear() })

function setup(savedOpen = true) {
  localStorage.setItem(key, JSON.stringify({ v: 2, inspector: { open: savedOpen }, inspectorWidth: 600 }))
  const stub = stubClient({ list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [
    { id: 'alpha', description: 'A complete description', packVersion: '1', matrix: true, path: 'packs/alpha.json' },
    { id: 'beta', description: 'A different description', packVersion: '2', matrix: false }
  ] }) }) })
  const router = createMemoryRouter([{ path: '/', element: <AppShell><Outlet /></AppShell>, children: [
    { path: 'packs', element: <PacksLayout />, children: [
      { index: true, element: null },
      { path: ':packId', element: <><h1>Pack document</h1><Link to="/packs">Back to packs</Link></> }
    ] }
  ] }], { initialEntries: ['/packs'] })
  const query = testQueryClient()
  render(<QueryClientProvider client={query}><McpContext.Provider value={connected({ client: stub.client })}>
    <RouterProvider router={router} />
  </McpContext.Provider></QueryClientProvider>)
  return { router, stub, query }
}

it('keeps preview separate from a saved document Inspector and restores each on navigation', async () => {
  const { router, stub } = setup()
  const row = await screen.findByRole('link', { name: /alpha.*A complete/ })
  expect(screen.queryByRole('complementary')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Preview alpha' }))
  let pane = await screen.findByRole('complementary', { name: 'Pack preview' })
  expect(within(pane).getByText('Configured')).toBeTruthy()
  expect(within(pane).getByText('Technical details').closest('details')?.open).toBe(false)
  fireEvent.click(within(pane).getByRole('link', { name: 'Open pack' }))
  await screen.findByRole('heading', { name: 'Pack document' })
  await screen.findByRole('complementary', { name: 'Inspector' })
  await waitFor(() => expect(document.querySelector('.desk')?.getAttribute('style')).toContain('--inspector-w: 600px'))
  await act(async () => { await router.navigate(-1) })
  pane = await screen.findByRole('complementary', { name: 'Pack preview' })
  expect(within(pane).getByRole('heading', { name: 'alpha' })).toBeTruthy()
  expect(document.querySelector('.desk')?.getAttribute('style')).toContain('--inspector-w: 360px')
  expect(row.closest('li')?.dataset.selected).toBe('true')
  fireEvent.click(within(pane).getByRole('button', { name: 'Close pack preview' }))
  await act(async () => { await router.navigate('/packs/alpha') })
  await screen.findByRole('complementary', { name: 'Inspector' })
  expect(JSON.parse(localStorage.getItem(key)!).inspector).toEqual({ open: true })
  expect(stub.calls.map(call => call.name)).toEqual(['list_packs'])
})

it('supports Space, arrow browsing, Escape and filtering without navigation or testing', async () => {
  const { router, stub } = setup(false)
  const row = await screen.findByRole('link', { name: /alpha.*A complete/ })
  row.focus(); fireEvent.keyDown(row, { key: ' ' })
  await screen.findByRole('complementary', { name: 'Pack preview' })
  fireEvent.keyDown(row, { key: 'ArrowDown' })
  expect(document.activeElement).toBe(screen.getByRole('link', { name: /beta.*A different/ }))
  expect(within(screen.getByRole('complementary')).getByRole('heading', { name: 'beta' })).toBeTruthy()
  fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
  await waitFor(() => expect(screen.queryByRole('complementary')).toBeNull())
  fireEvent.click(screen.getByRole('button', { name: 'Preview alpha' }))
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'beta' } })
  await waitFor(() => expect(screen.queryByRole('complementary')).toBeNull())
  expect(screen.getByRole('status').textContent).toBe('1 of 2')
  expect(router.state.location.pathname).toBe('/packs')
  expect(stub.calls.map(call => call.name)).toEqual(['list_packs'])
})

it('bounds a preview independently of a document and uses a drawer before squeezing the list', () => {
  expect(inspectorGeometry(1268, 600, 720, false, 420)).toEqual({ drawer: false, width: 420, min: 320, max: 420 })
  expect(inspectorGeometry(1000, 360, 720, false, 420).drawer).toBe(true)
  expect(inspectorGeometry(1268, 600, 480, false).width).toBeGreaterThan(420)
})
