import { useCallback, useState } from 'react'
import { ConnectionPaneContext, useConnectionChatLock } from './ConnectionPaneContext'
import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { ConnectionsPane } from './ConnectionsPane'
import { connectionCatalogFixture } from '../testing/connectionCatalog'
import { ConnectionRequestError, type ConnectionProvider } from './client'
const mocks = vi.hoisted(() => ({ call: vi.fn(), authorize: vi.fn(), status: vi.fn(), catalog: vi.fn(), source: vi.fn(), mail: vi.fn(), drive: vi.fn(), cancel: vi.fn(), close: vi.fn(), onBusy: vi.fn(), provider: vi.fn(), config: {} as any, snapshot: {} as any }))
vi.mock('./client', async importOriginal => ({ ...await importOriginal<typeof import('./client')>(), connectionCall: mocks.call, authorizeDrive: mocks.authorize, useDriveStatus: mocks.status, CONNECTIONS_KEY: ['gateway-connections'] }))
vi.mock('./catalog', () => ({ useConnections: mocks.catalog }))
vi.mock('../config/DeskConfigProvider', () => ({ useEffectiveConfig: () => mocks.config }))
vi.mock('../chat/ChatProvider', () => ({ useChats: () => mocks.snapshot }))
vi.mock('../chat/useChatAttachments', () => ({ useChatAttachments: () => ({ reading: false, isReading: () => false, attachSource: mocks.source, attachGmail: mocks.mail, attachDrive: mocks.drive, cancel: mocks.cancel, error: '', clearError: vi.fn() }) }))
const rows = Array.from({ length: 5 }, (_, i) => ({ id: `note-${i}.md`, title: `Policy ${i}`, url: `obsidian://open?vault=Fixture&file=note-${i}` }))
let account: string, state: string
beforeEach(() => {
 account = 'vault-a'; state = 'connected'
 mocks.config = { desk: { localGateway: { status: 'ready' } }, config: { research: { documents: { enabled: true }, gateway: { url: 'http://127.0.0.1:8888' } } } }
 mocks.snapshot = { store: {}, chats: [{ id: 'chat', attachments: [] }], drafts: [], bindings: new Map() }
 mocks.status.mockImplementation(() => ({ data: { state, account: state === 'connected' ? { id: account, name: 'Fixture' } : undefined } }))
 mocks.catalog.mockImplementation(() => ({ entries: connectionCatalogFixture.providers.map(descriptor => ({ descriptor, status: mocks.status() })), loading: false, isError: false, refetch: vi.fn() }))
 mocks.call.mockResolvedValue({ items: rows, selectionContext: 'epoch', more: false })
 mocks.source.mockResolvedValue(true); mocks.mail.mockResolvedValue(true); mocks.drive.mockResolvedValue(true)
})
afterEach(() => { cleanup(); vi.resetAllMocks() })
const client = () => testQueryClient()
function view(provider: ConnectionProvider | undefined = 'obsidian', target: HTMLElement = document.body, chatId: string | undefined = 'chat') {
 return <QueryClientProvider client={client()}><ConnectionsPane request={{ provider, chatId, opener: null }} target={target} onProvider={mocks.provider} onClose={mocks.close} onBusy={mocks.onBusy} /></QueryClientProvider>
}
async function choose(index = 0) {
 fireEvent.click(screen.getByRole('button', { name: 'Search' }))
 fireEvent.click((await screen.findAllByRole('checkbox'))[index]!)
}
it('keeps discovery separate from attachment and attaches selected IDs through the gateway', async () => {
 mocks.call.mockImplementation(async method => method === 'search' ? { items: rows, selectionContext: 'epoch', more: false } : [{ resourceId: rows[1]!.id, grant: 'a'.repeat(64) }])
 render(view()); await choose(1)
 expect(mocks.source).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button', { name: 'Attach 1 item' }))
 await waitFor(() => expect(mocks.source).toHaveBeenCalledWith('obsidian', [{ resourceId: 'note-1.md', grant: 'a'.repeat(64) }]))
 expect(mocks.call).toHaveBeenCalledWith('select', { resourceIds: ['note-1.md'], selectionContext: 'epoch' }, expect.any(AbortSignal), 'obsidian')
 await waitFor(() => expect(mocks.close).toHaveBeenCalledTimes(1))
})
it('accounts for existing attachments and clears stale selections on a failed new search', async () => {
 mocks.snapshot.chats[0].attachments = [{ id: 'existing' }]
 render(view()); await choose()
 const inputs = screen.getAllByRole('checkbox') as HTMLInputElement[]
 fireEvent.click(inputs[1]!); fireEvent.click(inputs[2]!); expect(inputs[3]!.disabled).toBe(true)
 fireEvent.click(inputs[1]!); expect(inputs[3]!.disabled).toBe(false)
 mocks.call.mockRejectedValue(new Error('Fixture failure')); fireEvent.click(screen.getByRole('button', { name: 'Search' }))
 await screen.findByRole('alert'); expect(screen.queryAllByRole('checkbox')).toHaveLength(0)
 expect((screen.getByRole('button', { name: 'Attach 0 items' }) as HTMLButtonElement).disabled).toBe(true)
})
it.each(['leave', 'account', 'configuration'] as const)('cancels a pending selection on %s and ignores a late reply', async change => {
 let finish!: (result: unknown) => void
 mocks.call.mockImplementation(method => method === 'search' ? Promise.resolve({ items: rows, selectionContext: 'epoch', more: false }) : new Promise(resolve => { finish = resolve }))
 const ui = render(view()); await choose(); fireEvent.click(screen.getByRole('button', { name: 'Attach 1 item' }))
 const signal = mocks.call.mock.calls.find(call => call[0] === 'select')![2] as AbortSignal
 if (change === 'leave') ui.unmount()
 else { if (change === 'account') account = 'vault-b'; else mocks.config.config.research.documents.enabled = false; ui.rerender(view()) }
 expect(signal.aborted).toBe(true)
 await act(async () => finish([{ resourceId: 'late', grant: 'a'.repeat(64) }]))
 expect(mocks.source).not.toHaveBeenCalled(); expect(mocks.close).not.toHaveBeenCalled()
})
it('retains the typed query and selected sources when the portal moves to a drawer', async () => {
 const dock = document.createElement('div'), drawer = document.createElement('div'); document.body.append(dock, drawer)
 const ui = render(view('obsidian', dock)); fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Policy' } }); await choose(2)
 ui.rerender(view('obsidian', drawer))
 expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('Policy')
 expect((screen.getAllByRole('checkbox')[2] as HTMLInputElement).checked).toBe(true)
 expect(dock.children).toHaveLength(0); ui.unmount(); dock.remove(); drawer.remove()
})
it('keeps Gmail page selections within the same connection epoch', async () => {
 mocks.call.mockImplementation(async (method, params) => method === 'select' ? params.messageIds.map((id: string) => ({messageId: id, grant: params.selectionContext})) : params.pageToken ? { messages: [{ id: 'b', subject: 'Second' }], selectionContext: 'epoch' } : { messages: [{ id: 'a', subject: 'First' }], selectionContext: 'epoch', nextPageToken: 'next' })
 render(view('gmail')); await choose()
 fireEvent.change(screen.getByRole('textbox'), { target: { value: 'not submitted' } }); fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
 await screen.findByText('Second'); fireEvent.click(screen.getByRole('checkbox')); fireEvent.click(screen.getByRole('button', { name: 'Attach 2 items' }))
 await waitFor(() => expect(mocks.mail).toHaveBeenCalledWith([{ messageId: 'a', grant: 'epoch' }, { messageId: 'b', grant: 'epoch' }]))
 expect(mocks.call).toHaveBeenCalledWith('search', { query: '', pageToken: 'next' }, expect.any(AbortSignal), 'gmail')
})
it('does not close when selected content could not be attached', async () => {
 mocks.call.mockImplementation(async method => method === 'search' ? { items: rows, selectionContext: 'epoch', more: false } : [])
 mocks.source.mockResolvedValue(undefined); render(view()); await choose(); fireEvent.click(screen.getByRole('button', { name: 'Attach 1 item' }))
 await waitFor(() => expect(mocks.source).toHaveBeenCalled()); expect(mocks.close).not.toHaveBeenCalled()
})
it.each(['google-drive', 'gmail', 'notion'] as const)('starts %s consent only from an explicit action, without attaching', async provider => {
 state = 'not-connected'; mocks.authorize.mockResolvedValue([]); render(view(provider))
 expect(mocks.authorize).not.toHaveBeenCalled(); expect(document.querySelector('input[type=file]')).toBeNull()
 fireEvent.click(screen.getByRole('button', { name: provider === 'notion' ? 'Continue with Notion' : 'Continue with Google' }))
 await waitFor(() => expect(mocks.authorize).toHaveBeenCalledWith('connect', expect.any(AbortSignal), provider))
 expect(mocks.source).not.toHaveBeenCalled(); expect(mocks.mail).not.toHaveBeenCalled(); expect(mocks.drive).not.toHaveBeenCalled()
})
it('cancels sign-in without closing the pane or attaching a late result', async () => {
 state = 'not-connected'; let finish!: () => void
 mocks.authorize.mockReturnValue(new Promise<void>(resolve => { finish = resolve }))
 render(view('gmail')); fireEvent.click(screen.getByRole('button', { name: 'Continue with Google' }))
 const signal = mocks.authorize.mock.calls[0]![1] as AbortSignal
 fireEvent.click(screen.getByRole('button', { name: 'Cancel' })); expect(signal.aborted).toBe(true)
 await act(async () => finish()); expect(mocks.close).not.toHaveBeenCalled(); expect(mocks.mail).not.toHaveBeenCalled()
})
it('sends the vault path only to gateway configuration', async () => {
 state = 'not-connected'; mocks.call.mockResolvedValue({ saved: true }); render(view())
 fireEvent.change(screen.getByRole('textbox', { name: 'Vault folder' }), { target: { value: '/synthetic/vault' } })
 fireEvent.click(screen.getByRole('button', { name: 'Connect vault' }))
 await waitFor(() => expect(mocks.call).toHaveBeenCalledWith('configure', { path: '/synthetic/vault' }, expect.any(AbortSignal), 'obsidian'))
 expect(mocks.source).not.toHaveBeenCalled(); expect(mocks.authorize).not.toHaveBeenCalled()
})
it('shows Google setup inline with instructions, without opening a modal or consent', () => {
 state = 'setup-required'; render(view('google-drive'))
 expect(screen.getByText('Setup instructions').closest('details')?.open).toBe(true)
 expect(screen.getByRole('button', { name: 'Choose credentials file' })).toBeTruthy()
 expect(screen.queryByRole('dialog')).toBeNull(); expect(mocks.authorize).not.toHaveBeenCalled()
})
it('offers the registered providers in a searchable catalog', () => {
 render(<QueryClientProvider client={client()}><ConnectionsPane request={{ opener: null }} target={document.body} onProvider={mocks.provider} onClose={mocks.close} onBusy={mocks.onBusy} /></QueryClientProvider>)
 fireEvent.change(screen.getByRole('textbox', { name: 'Search connections…' }), { target: { value: 'notion' } })
 expect(screen.queryByRole('button', { name: /Obsidian/ })).toBeNull()
 fireEvent.click(screen.getByRole('button', { name: /Notion/ })); expect(mocks.provider).toHaveBeenCalledWith('notion')
})

it('reports catalog failure once with retry instead of showing guessed providers', () => {
 const refetch = vi.fn()
 mocks.catalog.mockReturnValue({ entries: [], loading: false, isError: true, refetch })
 render(<QueryClientProvider client={client()}><ConnectionsPane request={{ opener: null }} target={document.body} onProvider={mocks.provider} onClose={mocks.close} onBusy={mocks.onBusy} /></QueryClientProvider>)
 expect(screen.getAllByRole('alert')).toHaveLength(1)
 expect(screen.queryByRole('button', { name: /Notion/ })).toBeNull()
 expect(screen.queryByText('No connections found.')).toBeNull()
 fireEvent.click(screen.getByRole('button', { name: 'Retry' })); expect(refetch).toHaveBeenCalledTimes(1)
})

it('cancels a pending selection when its gateway capability disappears', async () => {
 let release!: (result: unknown) => void
 mocks.call.mockImplementation(method => method === 'search' ? Promise.resolve({ items: rows, selectionContext: 'epoch', more: false }) : new Promise(resolve => { release = resolve }))
 const ui = render(view()); await choose(); fireEvent.click(screen.getByRole('button', { name: 'Attach 1 item' }))
 const signal = mocks.call.mock.calls.find(call => call[0] === 'select')![2] as AbortSignal
 mocks.catalog.mockReturnValue({ entries: [], loading: false, isError: false, refetch: vi.fn() })
 ui.rerender(view())
 expect(signal.aborted).toBe(true)
 expect(screen.getByText('This connection is unavailable in the current gateway.')).toBeTruthy()
 await act(async () => release([{ resourceId: 'late', grant: 'a'.repeat(64) }]))
 expect(mocks.source).not.toHaveBeenCalled(); expect(mocks.close).not.toHaveBeenCalled()
})

it('uses the gateway query requirement instead of assuming a provider can browse', async () => {
 mocks.catalog.mockImplementation(() => ({ entries: [{ descriptor: { ...connectionCatalogFixture.providers[3], queryRequired: true }, status: mocks.status() }], loading: false, isError: false }))
 render(view())
 expect(mocks.call).not.toHaveBeenCalled()
 expect((screen.getByRole('button', { name: 'Search' }) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Policy' } })
 fireEvent.click(screen.getByRole('button', { name: 'Search' }))
 await screen.findByText('Policy 0')
 expect(mocks.call).toHaveBeenCalledWith('search', { query: 'Policy' }, expect.any(AbortSignal), 'obsidian')
})

function LockObserver({ locked, chatId }: { locked: boolean; chatId: string }) {
 useConnectionChatLock(chatId, locked)
 return null
}
function LockFixture({ locked, chatId = 'chat' }: { locked: boolean; chatId?: string }) {
 const [open, setOpen] = useState(true), close = useCallback(() => setOpen(false), [])
 return <ConnectionPaneContext.Provider value={{ open: () => {}, activeChatId: 'chat', close }}>
  <LockObserver locked={locked} chatId={chatId} />{open && view()}
 </ConnectionPaneContext.Provider>
}
it('cancels pending attachment when its retained chat locks, but not when another chat locks', async () => {
 let finish!: (value: unknown) => void
 mocks.call.mockImplementation(method => method === 'search' ? Promise.resolve({ items: rows, selectionContext: 'epoch', more: false }) : new Promise(resolve => { finish = resolve }))
 const ui = render(<LockFixture locked={false} />); await choose(); fireEvent.click(screen.getByRole('button', { name: 'Attach 1 item' }))
 const signal = mocks.call.mock.calls.find(call => call[0] === 'select')![2] as AbortSignal
 ui.rerender(<LockFixture locked chatId="another-chat" />); expect(signal.aborted).toBe(false)
 ui.rerender(<LockFixture locked />); expect(signal.aborted).toBe(true)
 await act(async () => finish([{ resourceId: 'late', grant: 'a'.repeat(64) }]))
 expect(mocks.source).not.toHaveBeenCalled()
})

it('drops earlier Gmail selections when the connection epoch changes between pages', async () => {
 mocks.call.mockImplementation(async (_method, params) => params.pageToken ? {messages:[{id:'b',subject:'Second'}],selectionContext:'new'} : {messages:[{id:'a',subject:'First'}],selectionContext:'old',nextPageToken:'next'})
 render(view('gmail')); await choose(); fireEvent.click(screen.getByRole('button',{name:'Next page'}))
 await screen.findByText('Second'); expect((screen.getByRole('button',{name:'Attach 0 items'}) as HTMLButtonElement).disabled).toBe(true)
})
it('offers explicit reconnect after a revoked connection without opening consent automatically', async () => {
 mocks.call.mockRejectedValue(new ConnectionRequestError('reconnect-required','notion'))
 render(view('notion')); fireEvent.change(screen.getByRole('textbox'),{target:{value:'policy'}})
 fireEvent.click(screen.getByRole('button',{name:'Search'}))
 const reconnect = await screen.findByRole('button',{name:'Reconnect'})
 expect(mocks.authorize).not.toHaveBeenCalled()
 fireEvent.click(reconnect)
 await waitFor(()=>expect(mocks.authorize).toHaveBeenCalledWith('connect',expect.any(AbortSignal),'notion'))
})
