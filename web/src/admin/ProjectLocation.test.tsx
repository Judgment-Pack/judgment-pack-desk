import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { ProjectLocationSettings, ProjectLocationNotice } from './ProjectLocation'
import { DeskNotices, DeskNoticesProvider } from '../shell/DeskNotices'
import { UnsavedChangesProvider } from '../shell/UnsavedChanges'

vi.mock('../config/DeskConfigProvider', () => ({ useEffectiveConfig: () => ({ desk: { chassis: { projectDir: '/managed/research-desk' } } }) }))
vi.mock('../files/client', async importOriginal => ({...await importOriginal<typeof import('../files/client')>(), deskFetch: vi.fn()}))
import { deskFetch } from '../files/client'
const fetchMock = vi.mocked(deskFetch)
afterEach(cleanup)
let moved: boolean
beforeEach(() => {
  moved = false
  fetchMock.mockReset().mockImplementation(async (url, init) => {
    if (url === '/api/project-location') return Response.json({path: '/work/original', defaultDestination: '/data/jpack-desk/projects/original', restartRequired: moved, movedTo: moved ? '/work/new' : undefined})
    if (url.endsWith('/choose-folder')) return Response.json({ destination: '/work/new' })
    if (url.endsWith('/preview')) return Response.json({project: '/work/original', destination: JSON.parse(init!.body as string).destination, token: 'review-token'})
    if (url.endsWith('/move')) { moved = true; return Response.json({path: '/work/original', restartRequired: true, movedTo: '/work/new'}) }
    throw new Error(`Unexpected URL ${url}`)
  })
})
function mount() {
  const client = new QueryClient({defaultOptions: {queries: {retry: false}, mutations: {retry: false}}})
  return render(<QueryClientProvider client={client}><MemoryRouter><UnsavedChangesProvider><DeskNoticesProvider>
    <ProjectLocationSettings/><ProjectLocationNotice/><DeskNotices/>
  </DeskNoticesProvider></UnsavedChangesProvider></MemoryRouter></QueryClientProvider>)
}
async function review() {
  await waitFor(() => expect((screen.getByRole('button', {name: 'Move to…'}) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole('button', {name: 'Move to…'}))
  fireEvent.click(screen.getByRole('button', {name: 'Choose folder…'}))
  await screen.findByText('/work/new')
  fireEvent.click(screen.getByRole('button', {name: 'Review move'}))
  return screen.findByRole('dialog', {name: 'Review project move'})
}
it('reviews exact paths and confirms once; reports restart in the shared status strip', async () => {
  mount(); const dialog = await review()
  expect(within(dialog).getByText('/work/original')).toBeTruthy()
  expect(within(dialog).getByText('/work/new')).toBeTruthy()
  expect(fetchMock.mock.calls.some(([url]) => url.endsWith('/move'))).toBe(false)
  fireEvent.click(within(dialog).getByRole('button', {name: 'Move project'}))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(await screen.findByRole('button', {name: 'Restart required · Project moved'})).toBeTruthy()
  expect(fetchMock.mock.calls.filter(([url]) => url.endsWith('/move'))).toHaveLength(1)
  const submitted = fetchMock.mock.calls.find(([url]) => url.endsWith('/move'))![1]!
  expect(JSON.parse(submitted.body as string)).toEqual({destination: '/work/new', token: 'review-token'})
  expect((screen.getByRole('button', {name: 'Move to…'}) as HTMLButtonElement).disabled).toBe(true)
})
it('shows a conflict beside the actions, preserves the destination and requires a fresh review', async () => {
  mount(); const dialog = await review()
  fetchMock.mockImplementationOnce(async () => Response.json({error: 'The destination already has history.', code: 'stale'}, {status: 409}))
  fireEvent.click(within(dialog).getByRole('button', {name: 'Move project'}))
  expect((await screen.findByRole('alert')).textContent).toContain('The destination already has history.')
  expect(screen.getByText('/work/new')).toBeTruthy()
  expect((screen.getByRole('button', {name: 'Review move'}) as HTMLButtonElement).disabled).toBe(false)
  expect(screen.queryByRole('button', {name: 'Move project'})).toBeNull()
})
it('does not replay an uncertain move; a status read can confirm it', async () => {
  mount(); const dialog = await review()
  fetchMock.mockImplementationOnce(async () => { moved = true; throw new TypeError('network lost') })
  fireEvent.click(within(dialog).getByRole('button', {name: 'Move project'}))
  expect(await screen.findByRole('button', {name: 'Check project location'})).toBeTruthy()
  expect((screen.getByRole('button', {name: 'Review move'}) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(screen.getByRole('button', {name: 'Check project location'}))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(fetchMock.mock.calls.filter(([url]) => url.endsWith('/move'))).toHaveLength(1)
})

it('shows current Desk and startup folders separately; default only fills the destination', async () => {
  mount()
  await screen.findByText('/work/original')
  expect(screen.getByText('Current Desk folder')).toBeTruthy()
  expect(screen.getByText('/managed/research-desk')).toBeTruthy()
  expect(screen.getByText('Startup project folder')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Move to…' }))
  const dialog = await screen.findByRole('dialog', { name: 'Move startup project' })
  const shortcut = within(dialog).getByRole('button', { name: 'Use default location' })
  fireEvent.click(shortcut)
  expect(within(dialog).getByText('/data/jpack-desk/projects/original')).toBeTruthy()
  expect(within(dialog).queryByRole('textbox')).toBeNull()
  expect((shortcut as HTMLButtonElement).disabled).toBe(true)
  expect(fetchMock.mock.calls.filter(([,init]) => init?.method === 'POST')).toHaveLength(0)
  expect(within(dialog).getByText(/Managed Desks and their data remain/)).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Review move' }))
  const review = await screen.findByRole('dialog', { name: 'Review project move' })
  expect(within(review).getByText('/data/jpack-desk/projects/original')).toBeTruthy()
  expect(within(review).queryByText('/managed/research-desk')).toBeNull()
  expect(fetchMock.mock.calls.filter(([url]) => url.endsWith('/move'))).toHaveLength(0)
  fireEvent.click(within(review).getByRole('button', { name: 'Back' }))
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder…' }))
  await screen.findByText('/work/new')
  expect((screen.getByRole('button', { name: 'Use default location' }) as HTMLButtonElement).disabled).toBe(false)
})

it('preserves the destination when the native chooser is canceled or fails', async () => {
  mount()
  await screen.findByText('/work/original')
  fireEvent.click(screen.getByRole('button', { name: 'Move to…' }))
  fireEvent.click(screen.getByRole('button', { name: 'Use default location' }))
  fetchMock.mockImplementationOnce(async () => Response.json({ canceled: true }))
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder…' }))
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Choosing…' })).toBeNull())
  expect(screen.getByText('/data/jpack-desk/projects/original')).toBeTruthy()
  expect(screen.queryByRole('alert')).toBeNull()
  fetchMock.mockImplementationOnce(async () => Response.json({ error: 'The folder chooser could not open.', code: 'internal' }, { status: 503 }))
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder…' }))
  expect((await screen.findByRole('alert')).textContent).toContain('The folder chooser could not open.')
  expect(screen.getByText('/data/jpack-desk/projects/original')).toBeTruthy()
  expect(fetchMock.mock.calls.filter(([url]) => url.endsWith('/move') || url.endsWith('/preview'))).toHaveLength(0)
})
it('aborts native selection when the move dialog closes', async () => {
  mount()
  await screen.findByText('/work/original')
  fireEvent.click(screen.getByRole('button', { name: 'Move to…' }))
  let signal: AbortSignal | undefined
  fetchMock.mockImplementationOnce(async (_url, init) => {
    signal = init?.signal as AbortSignal
    return new Promise((_resolve, reject) => signal!.addEventListener('abort', () => reject(new DOMException('Canceled', 'AbortError'))))
  })
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder…' }))
  expect((screen.getByRole('button', { name: 'Review move' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(signal?.aborted).toBe(true)
})
