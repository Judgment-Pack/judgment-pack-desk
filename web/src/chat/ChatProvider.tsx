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
const EMPTY = { chats: [], drafts: [], ready: false, saving: false, error: '', active: [], bindings: new Map(), dirty: false } as ReturnType<ChatStore['getSnapshot']>
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
  const latest = useRef({ research, readable, discovery })
  latest.current = { research, readable, discovery }
  const draftTools = useMemo(() => linkReading({
    available: () => latest.current.readable,
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
  }), [store, chat.id])
  const binding = useResearchRun({ model: chat.model, mode: chat.mode, adversarialReview: chat.adversarialReview, draftTools })
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
    if (restored) store.report(chat.id, binding)
  }, [store, chat.id, restored, binding.run, binding.state, binding.ledger, binding.sources, binding.blocked, binding.model, binding.researchConfigured])
  return null
}
