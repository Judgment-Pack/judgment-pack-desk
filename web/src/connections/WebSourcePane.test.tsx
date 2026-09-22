import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ChatStore } from '../chat/store'
import { WebSourcePane } from './WebSourcePane'
const m = vi.hoisted(() => ({ ingest: vi.fn(), close: vi.fn(), attached: vi.fn(), busy: vi.fn(), snapshot: {} as any, config: {} as any, web: true }))
vi.mock('../documents/client', async original => ({ ...await original<typeof import('../documents/client')>(), ingestWeb: m.ingest }))
vi.mock('../chat/ChatProvider', () => ({ useChats: () => m.snapshot }))
vi.mock('../config/DeskConfigProvider', () => ({ useEffectiveConfig: () => m.config }))
vi.mock('./catalog', () => ({ useConnections: () => ({ web: m.web }) }))
let store: ChatStore, id: string
beforeEach(async () => {
 m.web = true
 store = new ChatStore('/synthetic', { read: async () => ({ project:'/synthetic', sha256:'absent', content:{version:1,chats:[]} }), write: vi.fn() })
 await store.load(); id = store.startChat().id
 store.update(id, { composer: 'Keep this draft' })
 m.snapshot = { ...store.getSnapshot(), store }
 m.config = { desk: { localGateway: { status: 'ready' } }, config: { research: { documents: { enabled: true }, gateway: { authority:'fixture', signer:{public:'a'.repeat(64)} } } } }
 m.ingest.mockResolvedValue({ reference:{id:'attachment',digest:'sha256:'+'a'.repeat(64),pages:[1],allowPartial:false}, document:{record:{document:{name:'policy.txt'}}} })
})
afterEach(() => { cleanup(); store.dispose(); sessionStorage.clear(); vi.resetAllMocks() })
function view(target: HTMLElement = document.body) { return <WebSourcePane request={{ source:'web', chatId:id, opener:null }} target={target} onClose={m.close} onAttached={m.attached} onBusy={m.busy} /> }
function choose() { fireEvent.change(screen.getByRole('textbox', {name:'Link'}), {target:{value:'https://example.com/policy'}}); fireEvent.click(screen.getByRole('button',{name:'Attach link'})) }
it('attaches through the gateway without sending, saving an empty session, or replacing the draft', async () => {
 render(view()); choose()
 await waitFor(() => expect(m.attached).toHaveBeenCalledOnce())
 expect(m.ingest).toHaveBeenCalledWith({url:'https://example.com/policy'},m.config.config.research,expect.any(AbortSignal),expect.any(Function))
 const snapshot=store.getSnapshot(); expect(snapshot.chats).toHaveLength(0)
 expect(snapshot.drafts[0]?.composer).toBe('Keep this draft'); expect(snapshot.drafts[0]?.attachments).toHaveLength(1)
})
it.each(['cancel','unmount','disable'] as const)('cancels on %s and discards a late snapshot', async action => {
 let finish!: (value: any) => void
 m.ingest.mockReturnValue(new Promise(resolve => { finish = resolve }))
 const ui = render(view()); choose(); const signal = m.ingest.mock.calls[0]![2] as AbortSignal
 if (action==='cancel') { fireEvent.click(screen.getByRole('button',{name:'Cancel'})); ui.unmount() }
 else if (action==='unmount') ui.unmount()
 else {m.web=false;ui.rerender(view())}
 expect(signal.aborted).toBe(true)
 await act(async () => finish({reference:{id:'late'},document:{record:{document:{name:'late'}}}}))
 expect(store.getSnapshot().drafts[0]?.attachments ?? []).toHaveLength(0); expect(m.attached).not.toHaveBeenCalled()
})
it('retains the URL when the right pane changes between dock and drawer', () => {
 const dock=document.createElement('div'),drawer=document.createElement('div');document.body.append(dock,drawer)
 const ui=render(view(dock));fireEvent.change(screen.getByRole('textbox'),{target:{value:'https://example.com/policy'}})
 ui.rerender(view(drawer));expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('https://example.com/policy')
 expect(document.activeElement).toBe(screen.getByRole('textbox'));ui.unmount();dock.remove();drawer.remove()
})
it('reports failure without closing or losing the URL', async () => {
 m.ingest.mockRejectedValue(new Error('Fixture retrieval failure'));render(view());choose()
 await screen.findByRole('alert');expect(m.attached).not.toHaveBeenCalled();expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('https://example.com/policy')
})
it('rejects an unsupported URL before any acquisition', () => {
 render(view());fireEvent.change(screen.getByRole('textbox'),{target:{value:'http://localhost'}});fireEvent.click(screen.getByRole('button',{name:'Attach link'}))
 expect(screen.getByRole('alert')).toBeTruthy();expect(m.ingest).not.toHaveBeenCalled()
})
