import { createContext, useContext, useEffect, useMemo, useSyncExternalStore, type ReactNode } from 'react'
import { useFileListing } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useConnections } from '../connections/catalog'
import { useSearchConnections, useSearchPreference } from '../search/connections'
import { useSavedChatRun } from '../assistant/serverRuns/useSavedChatRun'
import { privateQuery } from '../data/privateQuery'
import { sourceMessage } from '../i18n/source'
import type { McpToolResult } from '@judgment-pack/harness/engine'
import { useDialectPublicationAutoSave } from './useDialectPublicationAutoSave'
import { usePackAutoSave } from './usePackAutoSave'
import { ChatStore, type Chat } from './store'

const Context = createContext<ChatStore | null>(null)
export const useChatStore = () => useContext(Context)
const EMPTY = { packDrafts: [], deletedDrafts: [], chats: [], drafts: [], ready: false, saving: false, error: '', active: [], bindings: new Map(), dirty: false } as ReturnType<ChatStore['getSnapshot']>
const noop = () => () => {}
export function useChats() {
  const store = useChatStore()
  return { store, ...useSyncExternalStore(store?.subscribe ?? noop, store?.getSnapshot ?? (() => EMPTY), () => EMPTY) }
}
export function ChatProvider({ children }: { children: ReactNode }) {
  const project = useFileListing().data?.root
  const store = useMemo(() => project ? new ChatStore(project) : null, [project])
  useEffect(() => {
    if (!store) return
    void store.load()
    // Retry only initial hydration. Never reload an established transcript over
    // local edits, an active worker, a pending approval, or an uncertain save.
    const recover = () => { if (document.visibilityState !== 'hidden') void store.retryInitialLoad() }
    const timer = window.setInterval(recover, privateQuery.refetchInterval)
    window.addEventListener('focus', recover)
    document.addEventListener('visibilitychange', recover)
    return () => { clearInterval(timer); window.removeEventListener('focus', recover); document.removeEventListener('visibilitychange', recover) }
  }, [store])
  useEffect(() => store?.retain(), [store])
  return <Context.Provider value={store}><ChatWorkers />{children}</Context.Provider>
}
function ChatWorkers() {
  const { store, active, chats, drafts, dirty } = useChats()
  useEffect(() => {
    const leave = (event: BeforeUnloadEvent) => { if (dirty || store?.running) event.preventDefault() }
    const visibility = () => { if (document.visibilityState === 'hidden') void store?.flush() }
    window.addEventListener('beforeunload', leave); document.addEventListener('visibilitychange', visibility)
    return () => { window.removeEventListener('beforeunload', leave); document.removeEventListener('visibilitychange', visibility) }
  }, [store, dirty])
  return <>{active.map(id => { const chat = chats.find(chat => chat.id === id) ?? drafts.find(chat => chat.id === id); return store && chat ? <ChatWorker key={id} store={store} chat={chat} /> : null })}</>
}

function ChatWorker({ store, chat }: { store: ChatStore; chat: Chat }) {
 const project=useFileListing().data?.root,mcp=useMcp(),effective=useEffectiveConfig(),search=useSearchPreference()
 const catalog=useConnections(effective.desk?.localGateway?.status==='ready'&&!effective.desk?.decoded?.values?.research?.gateway),connections=useSearchConnections()
 const researchEnabled=!!search.data&&!search.isError&&(chat.researchMode??search.data.value.mode)==='auto'
 const binding=useSavedChatRun(store,chat)
 const missing=!!effective.aiConnections&&!chat.aiConnection&&!!chat.checkpoint?.state.turns.length
 const blocked=missing?sourceMessage('Choose an AI connection for this saved chat.'):binding.blocked || (chat.researchMode==='provided'?'':search.isPending?sourceMessage('Loading search settings…'):search.isError?sourceMessage('Search settings could not be read. Reload before sending.'):researchEnabled&&(catalog.loading||connections.loading)?sourceMessage('Checking search connections…'):researchEnabled&&(catalog.isError||connections.isError)?sourceMessage('Search connections could not be checked. Retry in Web search settings.'):'')
 useDialectPublicationAutoSave({chat,store,binding,restored:true})
 usePackAutoSave({chat,store,binding,restored:true,project,call:async(name,args)=>{if(!mcp.client)throw Error('Runtime unavailable');return await mcp.client.callTool({name,arguments:args}) as McpToolResult}})
 useEffect(()=>{store.report(chat.id,{...binding,blocked})},[store,chat.id,binding.run,binding.state,binding.model,binding.researchConfigured,blocked])
 return null
}
