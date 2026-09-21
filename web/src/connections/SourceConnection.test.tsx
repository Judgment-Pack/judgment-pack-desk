import { createRef } from 'react'
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { SourceConnection } from './SourceConnection'
const calls = vi.hoisted(() => ({ call: vi.fn(), authorize: vi.fn(), status: vi.fn() }))
vi.mock('./client', () => ({ connectionCall: calls.call, authorizeDrive: calls.authorize, useDriveStatus: calls.status, CONNECTIONS_KEY: ['gateway-connections'] }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const items = Array.from({ length: 5 }, (_, i) => ({ id: `note-${i}.md`, title: `Policy ${i}`, url: `obsidian://open?vault=Fixture&file=note-${i}` }))
function view(onSelect = vi.fn(), open = true, provider: 'notion' | 'obsidian' = 'obsidian') {
 return <QueryClientProvider client={testQueryClient()}><SourceConnection provider={provider} available open={open} onOpenChange={vi.fn()} openerRef={createRef()} onSelect={onSelect} /></QueryClientProvider>
}
function connected() { calls.status.mockReturnValue({ data: { state: 'connected', account: { id: 'vault-a', name: 'Fixture' } } }); calls.call.mockResolvedValue({ items, selectionContext: 'epoch', more: false }) }
it('searches without attaching and sends only the selected resource IDs', async () => {
 connected(); calls.call.mockImplementation(async method => method === 'search' ? { items, selectionContext: 'epoch', more: false } : [{ resourceId: items[1]!.id, grant: 'a'.repeat(64) }])
 const selected = vi.fn(); render(view(selected))
 fireEvent.click(screen.getByRole('button', { name: 'Search' }))
 const rows = await screen.findAllByRole('checkbox')
 expect(selected).not.toHaveBeenCalled()
 fireEvent.click(rows[1]!); fireEvent.click(screen.getByRole('button', { name: 'Attach selected' }))
 await waitFor(() => expect(selected).toHaveBeenCalledWith([{ resourceId: 'note-1.md', grant: 'a'.repeat(64) }]))
 expect(calls.call).toHaveBeenCalledWith('select', { resourceIds: ['note-1.md'], selectionContext: 'epoch' }, expect.any(AbortSignal), 'obsidian')
})
it('limits the selection to four and clears stale results on a failed search', async () => {
 connected(); render(view()); fireEvent.click(screen.getByRole('button', { name: 'Search' }))
 const rows = await screen.findAllByRole('checkbox') as HTMLInputElement[]
 rows.slice(0, 4).forEach(row => fireEvent.click(row)); expect(rows[4]!.disabled).toBe(true)
 calls.call.mockRejectedValue(new Error('Fixture search failed')); fireEvent.click(screen.getByRole('button', { name: 'Search' }))
 await screen.findByRole('alert'); expect(screen.queryAllByRole('checkbox')).toHaveLength(0)
 expect((screen.getByRole('button', { name: 'Attach selected' }) as HTMLButtonElement).disabled).toBe(true)
})
it('does not attach a late selection after the user leaves', async () => {
 connected(); let finish!: (value: unknown) => void
 calls.call.mockImplementation(method => method === 'search' ? Promise.resolve({ items, selectionContext: 'epoch', more: false }) : new Promise(resolve => { finish = resolve }))
 const selected = vi.fn(), ui = render(view(selected)); fireEvent.click(screen.getByRole('button', { name: 'Search' }))
 fireEvent.click((await screen.findAllByRole('checkbox'))[0]!); fireEvent.click(screen.getByRole('button', { name: 'Attach selected' }))
 const signal = calls.call.mock.calls.find(c => c[0] === 'select')![2] as AbortSignal
 ui.rerender(view(selected, false)); expect(signal.aborted).toBe(true)
 finish([{ resourceId: 'note-0.md', grant: 'a'.repeat(64) }]); await Promise.resolve(); expect(selected).not.toHaveBeenCalled()
})
it('starts Notion sign-in without requesting a credential file', async () => {
 calls.status.mockReturnValue({ data: { state: 'not-connected' } }); calls.authorize.mockResolvedValue([])
 render(view(vi.fn(), true, 'notion')); fireEvent.click(screen.getByRole('button', { name: 'Continue with Notion' }))
 await waitFor(() => expect(calls.authorize).toHaveBeenCalledWith('connect', expect.any(AbortSignal), 'notion'))
 expect(document.querySelector('input[type=file]')).toBeNull()
})
it('sends vault configuration only to the gateway, never to the chat', async () => {
 calls.status.mockReturnValue({ data: { state: 'not-connected' } }); calls.call.mockResolvedValue({ saved: true })
 const selected = vi.fn(); render(view(selected))
 fireEvent.change(screen.getByRole('textbox', { name: 'Vault folder' }), { target: { value: '/synthetic/vault' } })
 fireEvent.click(screen.getByRole('button', { name: 'Connect vault' }))
 await waitFor(() => expect(calls.call).toHaveBeenCalledWith('configure', { path: '/synthetic/vault' }, expect.any(AbortSignal), 'obsidian'))
 expect(selected).not.toHaveBeenCalled(); expect(calls.authorize).not.toHaveBeenCalled()
})
