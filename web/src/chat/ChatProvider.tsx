import { graphChatTools } from '../graphs/chatTools'
import { useMcp } from '../mcp/McpProvider'
import { useSearchConnections, useSearchPreference } from '../search/connections'
import { sourceMessage } from '../i18n/source'
import { createContext, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useConnections } from '../connections/catalog'
import { useFileListing } from '../files/queries'
import { useResearchRun } from '../research/useResearchRun'
import { recordActivity } from '../shell/consoleLog'
import { restoreLedger } from './checkpoint'
import { linkReadable, websiteReadable, linkReading } from './linkTools'
import { ChatStore, type Chat, type ChatAttachment } from './store'

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
  useEffect(() => { if (store) void store.load() }, [store])
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
const chatOf = (store: ChatStore, id: string) => { const snapshot = store.getSnapshot(); return [...snapshot.chats, ...snapshot.drafts].find(chat => chat.id === id) }
function ChatWorker({ store, chat }: { store: ChatStore; chat: Chat }) {
  const mcp = useMcp()
  const currentClient = useRef(mcp.client)
  currentClient.current = mcp.client
  const effective = useEffectiveConfig()
  const research = effective.config.research
  // Link reading is offered on the same terms as the composer's Add link: the
  // managed local gateway's catalog advertises the web source, or the desk-level
  // file declares one, and document processing is enabled. Read per turn, so a
  // setting changed mid-chat holds.
  const local = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
  const catalog = useConnections(local)
  const readable = linkReadable(research, { local, catalogWeb: catalog.web })
  const discovery=websiteReadable(research,{local,catalogWeb:catalog.web,catalogDiscovery:catalog.discovery})
  const searchConnections=useSearchConnections(), searchPreference=useSearchPreference()
  const researchEnabled=!!searchPreference.data&&!searchPreference.isError&&(chat.researchMode??searchPreference.data.value.mode)==='auto'
  const connection=searchConnections.available&&!searchConnections.isError&&searchConnections.data?.connections.find(c=>c.id===(chat.searchConnection??searchPreference.data?.value.connection)) || undefined
  const latest = useRef({ research, readable, discovery, researchEnabled, connection, mode: chat.mode })
  latest.current = { research, readable, discovery, researchEnabled, connection, mode: chat.mode }
  const draftTools = useMemo(() => { const links = linkReading({
    available: () => latest.current.readable,
    researchEnabled:()=>latest.current.researchEnabled,
    search:{
      connection:()=>latest.current.connection,
      references:()=>chatOf(store,chat.id)?.searches??[],
      add:ref=>{const current=chatOf(store,chat.id);if(current)store.update(chat.id,{searches:[...(current.searches??[]),ref]})}
    },
    discoveryAvailable:()=>latest.current.discovery,
    websites:()=>chatOf(store,chat.id)?.websites??[],
    addWebsite:ref=>{const current=chatOf(store,chat.id);if(current)store.update(chat.id,{websites:[...(current.websites??[]).filter(w=>w.seed!==ref.seed),ref]})},
    config: () => latest.current.research,
    documents: () => chatOf(store, chat.id)?.documents ?? [],
    addDocument: (attachment: ChatAttachment) => {
      const current = chatOf(store, chat.id)
      if (current) store.update(chat.id, { documents: [...(current.documents ?? []).filter(file => file.id !== attachment.id), attachment] })
    },
    log: text => recordActivity(text, 'research')
  }); const graphs = graphChatTools({client: () => currentClient.current, chatId: chat.id, addDraft: draft => {
    const current = chatOf(store, chat.id)
    if (!current) throw new Error('This conversation is no longer available.')
    if ((current.graphDrafts?.length ?? 0) >= 16) throw new Error('This chat has 16 graph drafts. Start a new chat to create another.')
    store.update(chat.id, {graphDrafts: [...(current.graphDrafts ?? []), draft]})
  }})
  return (context: Parameters<typeof links>[0]) => [...links(context), ...graphs]
  }, [store, chat.id])
  const binding = useResearchRun({ connectionId:chat.aiConnection, apiThinking:chat.apiThinking, model: chat.model, reasoning: chat.reasoning, mode: chat.mode, adversarialReview: chat.adversarialReview, researchPolicy:()=>!latest.current.researchEnabled
    ? 'WEB RESEARCH POLICY: Use only sources supplied in this conversation. Web search and website exploration are disabled for this conversation; do not request configuration as a workaround.'
    : `WEB RESEARCH POLICY: ${latest.current.mode === 'web-research' ? 'Research requested. For substantive research requests, use the available web tools and cite sources actually read. Ask for a source or explain a missing capability when necessary.' : 'Auto. Choose web tools when the request needs research, verification or current sources.'} ${latest.current.connection?'The configured search connection is '+latest.current.connection.provider+'.':'No web-search connection is configured. Supplied-link reading and website exploration may still be available; check your tool list.'}`, draftTools, documents: () => chatOf(store, chat.id)?.documents ?? [] })
  // A newly opened chat mounts its queries at the same time as its model.
  // Do not let that first turn freeze an incomplete tool list or a false
  // 'not configured' policy while saved search settings are still loading.
  const searchBlocked = chat.researchMode === 'provided' ? ''
    : searchPreference.isPending ? sourceMessage('Loading search settings…')
    : searchPreference.isError ? sourceMessage('Search settings could not be read. Reload before sending.')
    : researchEnabled && (catalog.loading || searchConnections.loading) ? sourceMessage('Checking search connections…')
    : researchEnabled && (catalog.isError || searchConnections.isError) ? sourceMessage('Search connections could not be checked. Retry in Web search settings.') : ''
  // A saved chat that predates AI connections names no connection. Its next
  // send waits for an explicit choice rather than taking today's default.
  const legacyTargetMissing=!!effective.aiConnections&&!chat.aiConnection&&!!chat.checkpoint?.state.turns.length
  const blocked = legacyTargetMissing?sourceMessage('Choose an AI connection for this saved chat.'):binding.blocked || searchBlocked
  const initial = useRef(chat.checkpoint)
  const restoring = useRef(false)
  const [restored, setRestored] = useState(!initial.current)
  useEffect(() => {
    if (restoring.current || !initial.current || !binding.run || !binding.ledger) return
    restoring.current = true
    try {
      restoreLedger(binding.ledger, initial.current.sources)
      void binding.run.restore(initial.current.state).then(() => setRestored(true), error => store.problem(sourceMessage('Chat could not be restored: {{reason}}', { reason: error.message })))
    } catch (error) { store.problem(sourceMessage('Chat could not be restored: {{reason}}', { reason: (error as Error).message })) }
  }, [binding.run, binding.ledger, store])
  useEffect(() => {
    if (restored) store.report(chat.id, {...binding, blocked})
  }, [store, chat.id, restored, binding.run, binding.state, binding.ledger, binding.sources, blocked, binding.model, binding.researchConfigured])
  return null
}
