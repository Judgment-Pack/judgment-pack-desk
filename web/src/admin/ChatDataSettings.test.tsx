import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { ChatDataSettings } from './ChatDataSettings'
import type { ChatStorageStatus } from './chatStorage'

const mocks = vi.hoisted(() => ({ fetch: vi.fn(), chats: { store: null as null | { running?: string; flush: () => Promise<boolean> }, saving: false, dirty: false, error: '' } }))
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: mocks.fetch }))
vi.mock('../chat/ChatProvider', () => ({ useChats: () => mocks.chats }))
const status: ChatStorageStatus = { path: '/private/settings', recommendedPath: '/private/data', revision: 'original', legacy: true, projectCount: 2, bytes: 5000, projectBytes: 1000, scope: 'personal', maxMoveBytes: 1024 ** 3, maxBackupBytes: 128 * 1024 ** 2 }
function setup() { return render(<QueryClientProvider client={testQueryClient()}><ChatDataSettings /></QueryClientProvider>) }
beforeEach(() => { mocks.fetch.mockReset(); mocks.fetch.mockImplementation(async () => Response.json(status)); mocks.chats.store = null; mocks.chats.dirty = false; mocks.chats.error = ''; mocks.chats.saving = false })
afterEach(cleanup)

it('shows the server location and separates chat storage from keys and project files', async () => {
 setup()
 expect(await screen.findByText('/private/settings')).toBeTruthy()
 expect(screen.getByText(/API keys remain/)).toBeTruthy()
 expect(screen.getByText(/2 projects/)).toBeTruthy()
 expect(mocks.fetch.mock.calls.filter(([url]) => url.endsWith('/move'))).toHaveLength(0)
})
it('moves only after confirmation and keeps the original location as a recovery copy', async () => {
 let release!: (value: Response) => void
 mocks.fetch.mockImplementation(async (url: string) => url.endsWith('/move') ? new Promise<Response>(resolve => { release = resolve }) : Response.json(status))
 setup(); const opener = await screen.findByRole('button', { name: 'Change location…' }); fireEvent.click(opener)
 expect((screen.getByLabelText('New folder') as HTMLInputElement).value).toBe('/private/data')
 fireEvent.click(screen.getByRole('button', { name: 'Move data' }))
 await waitFor(() => expect(release).toBeDefined())
 const [,request] = mocks.fetch.mock.calls.find(([url]) => url.endsWith('/move'))!
 expect(JSON.parse(request.body)).toEqual({ path: '/private/data', revision: 'original' })
 expect((screen.getByRole('button', { name: 'Moving…' }) as HTMLButtonElement).disabled).toBe(true)
 release(Response.json({ ...status, path: '/private/data', previousPath: status.path, revision: 'next', legacy: false }))
 await screen.findByText(/Chat data moved/)
 expect(screen.queryByRole('dialog')).toBeNull()
 expect(screen.getByText(/New changes are saved only/)).toBeTruthy()
 await waitFor(() => expect(document.activeElement).toBe(opener))
})
it('preserves input after refusal and requires an explicit settings reload before a new revision', async () => {
 let current = status
 mocks.fetch.mockImplementation(async (url: string) => {
  if (url.endsWith('/move')) { current = { ...status, revision: 'changed' }; return Response.json({ error: 'The location changed; reload settings.', code: 'stale' }, { status: 409 }) }
  return Response.json(current)
 })
 setup(); fireEvent.click(await screen.findByRole('button', { name: 'Change location…' }))
 fireEvent.change(screen.getByLabelText('New folder'), { target: { value: '/different/data' } })
 fireEvent.click(screen.getByRole('button', { name: 'Move data' }))
 await screen.findByRole('alert')
 expect((screen.getByLabelText('New folder') as HTMLInputElement).value).toBe('/different/data')
 fireEvent.click(screen.getByRole('button', { name: 'Reload settings' }))
 await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
 fireEvent.click(screen.getByRole('button', { name: 'Move data' }))
 await waitFor(() => expect(mocks.fetch.mock.calls.filter(([url]) => url.endsWith('/move'))).toHaveLength(2))
 const calls = mocks.fetch.mock.calls.filter(([url]) => url.endsWith('/move'))
 expect(JSON.parse(calls[1][1].body).revision).toBe('changed')
})
it.each(['running', 'saving', 'dirty', 'error'] as const)('refuses relocation while chats are %s', async state => {
 if (state === 'running') mocks.chats.store = { running: 'one', flush: async () => true }
 else if (state === 'error') mocks.chats.error = 'Unsaved'
 else mocks.chats[state] = true
 setup(); const button = await screen.findByRole('button', { name: 'Change location…' })
 expect((button as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getByText(/Finish or stop active work/)).toBeTruthy()
})
it('does not claim success or open a move after the server refuses storage custody', async () => {
 mocks.fetch.mockResolvedValue(Response.json({ error: 'Private directory is not accessible.' }, { status: 403 }))
 setup(); await screen.findByRole('alert')
 expect(screen.queryByRole('button', { name: 'Change location…' })).toBeNull()
 expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy()
})
