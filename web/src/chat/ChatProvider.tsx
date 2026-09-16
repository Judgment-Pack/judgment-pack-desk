import { createContext, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { useFileListing } from '../files/queries'
import { useResearchRun } from '../research/useResearchRun'
import { restoreLedger } from './checkpoint'
import { ChatStore, type Chat } from './store'

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
function ChatWorker({ store, chat }: { store: ChatStore; chat: Chat }) {
  const binding = useResearchRun({ model: chat.model, mode: chat.mode })
  const initial = useRef(chat.checkpoint)
  const restoring = useRef(false)
  const [restored, setRestored] = useState(!initial.current)
  useEffect(() => {
    if (restoring.current || !initial.current || !binding.run || !binding.ledger) return
    restoring.current = true
    try {
      restoreLedger(binding.ledger, initial.current.sources)
      void binding.run.restore(initial.current.state).then(() => setRestored(true), error => store.problem(`Chat could not be restored: ${error.message}`))
    } catch (error) { store.problem(`Chat could not be restored: ${(error as Error).message}`) }
  }, [binding.run, binding.ledger, store])
  useEffect(() => {
    if (restored) store.report(chat.id, binding)
  }, [store, chat.id, restored, binding.run, binding.state, binding.ledger, binding.sources, binding.blocked, binding.model, binding.researchConfigured])
  return null
}
