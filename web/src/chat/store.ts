import { answer, deskFetch } from '../files/client'
import { checkpoint, decodeCheckpoint, type Checkpoint } from './checkpoint'
import type { ResearchRunBinding } from '../research/useResearchRun'

export interface Chat {
  id: string; title: string; pinned: boolean; archived: boolean; updatedAt: string
  composer: string; model: string; mode: 'draft' | 'research'; view: 'chat' | 'draft'
  pack?: { id: string; path: string; digest: string }
  checkpoint?: Checkpoint
  createdCandidateDigest?: string
}
interface Document { version: 1; chats: Chat[] }
interface Reply { project: string; sha256: string; content: unknown }
interface Snapshot { chats: Chat[]; ready: boolean; saving: boolean; error: string; active: string[]; bindings: ReadonlyMap<string, ResearchRunBinding>; dirty: boolean }
export interface ChatPersistence { read(): Promise<Reply>; write(document: Document, digest: string): Promise<Reply> }
const persistence: ChatPersistence = {
  read: async () => answer<Reply>(await deskFetch('/api/conversations')),
  write: async (document, digest) => answer<Reply>(await deskFetch('/api/conversations', {
    method: 'PUT', headers: { 'Content-Type': 'application/json', 'If-Match': digest }, body: JSON.stringify(document)
  }))
}
function decode(value: unknown): Chat[] {
  const doc = value as Partial<Document> | null
  if (doc?.version !== 1 || !Array.isArray(doc.chats) || doc.chats.length > 256) throw new Error('Unsupported chat history. The saved file has not been changed.')
  const ids = new Set<string>()
  return doc.chats.map(value => {
    if (!value || typeof value !== 'object') throw new Error('Invalid saved chat')
    const chat = value as Chat
    if (typeof chat.id !== 'string' || !/^[a-zA-Z0-9-]{1,80}$/.test(chat.id) || ids.has(chat.id)
      || typeof chat.title !== 'string' || typeof chat.composer !== 'string' || typeof chat.model !== 'string'
      || typeof chat.pinned !== 'boolean' || typeof chat.archived !== 'boolean'
      || typeof chat.updatedAt !== 'string' || !Number.isFinite(Date.parse(chat.updatedAt)) || !['draft', 'research'].includes(chat.mode) || !['chat', 'draft'].includes(chat.view)) throw new Error('Invalid saved chat. History has not been changed.')
    ids.add(chat.id)
    if (chat.pack && (typeof chat.pack.id !== 'string' || typeof chat.pack.path !== 'string' || typeof chat.pack.digest !== 'string')) throw new Error('Invalid saved pack context')
    return { id: chat.id, title: chat.title, composer: chat.composer, model: chat.model, pinned: chat.pinned, archived: chat.archived,
      updatedAt: chat.updatedAt, mode: chat.mode, view: chat.view, ...(chat.pack ? { pack: chat.pack } : {}),
      ...(chat.checkpoint ? { checkpoint: decodeCheckpoint(chat.checkpoint) } : {}),
      ...(typeof chat.createdCandidateDigest === 'string' ? { createdCandidateDigest: chat.createdCandidateDigest } : {}) }
  })
}

/** One project, multiple chats, one explicit running operation. No framework objects on disk. */
export class ChatStore {
  private state: Snapshot = { chats: [], ready: false, saving: false, error: '', active: [], bindings: new Map(), dirty: false }
  private listeners = new Set<() => void>()
  private leases = 0
  retain() { this.leases++; return () => { this.leases--; queueMicrotask(() => { if (!this.leases) this.dispose() }) } }
  private digest = 'absent'
  private revision = 0
  private savedRevision = 0
  private timer: ReturnType<typeof setTimeout> | undefined
  private loading: Promise<void> | null = null
  private observed = new Map<string, { state: unknown; sources: unknown }>()
  constructor(private project: string, private io: ChatPersistence = persistence) {}
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener) } }
  getSnapshot = () => this.state
  private set(patch: Partial<Snapshot>) { this.state = { ...this.state, ...patch }; this.listeners.forEach(listener => listener()) }
  private changed(chats: Chat[]) {
    this.revision++
    this.set({ chats, dirty: true })
    clearTimeout(this.timer)
    this.timer = setTimeout(() => { void this.flush() }, 600)
  }
  load(): Promise<void> {
    if (this.loading) return this.loading
    this.loading = (async () => {
      try {
        const reply = await this.io.read()
        if (reply.project !== this.project) throw new Error('Chat history belongs to a different project')
        const chats = decode(reply.content)
        this.digest = reply.sha256
        this.set({ chats, ready: true, error: '' })
      } catch (error) { this.set({ error: (error as Error).message }) }
      finally { this.loading = null }
    })()
    return this.loading
  }
  async flush(): Promise<boolean> {
    clearTimeout(this.timer)
    if (!this.state.ready || this.state.error || this.state.saving) return false
    if (this.savedRevision === this.revision) return true
    const revision = this.revision
    const document: Document = { version: 1, chats: this.state.chats }
    this.set({ saving: true })
    try {
      const reply = await this.io.write(document, this.digest)
      if (reply.project !== this.project) throw new Error('Chat save answered for a different project')
      this.digest = reply.sha256
      this.savedRevision = revision
      this.set({ saving: false, dirty: this.revision !== revision, error: '' })
      if (this.revision !== revision) this.timer = setTimeout(() => { void this.flush() }, 600)
      return true
    } catch (error) {
      this.set({ saving: false, error: `Chat changes are not saved: ${(error as Error).message}` })
      return false
    }
  }
  retrySave() { this.set({ error: '' }); void this.flush() }
  get canCreate() { return this.state.ready && this.state.chats.length < 256 }
  create(pack?: Chat['pack'], mode: Chat['mode'] = 'draft'): Chat {
    if (!this.state.ready) throw new Error('Chat history is still loading')
    if (this.state.chats.length >= 256) throw new Error('Chat history is full. Export and delete an older chat first.')
    const chat: Chat = { id: crypto.randomUUID(), title: 'New chat', pinned: false, archived: false, updatedAt: new Date().toISOString(), composer: '', model: '', mode, view: 'chat', ...(pack ? { pack } : {}) }
    this.changed([chat, ...this.state.chats]); this.activate(chat.id)
    return chat
  }
  update(id: string, patch: Partial<Omit<Chat, 'id' | 'updatedAt'>>) {
    const previous = this.state.chats.find(chat => chat.id === id)
    if (!previous || Object.entries(patch).every(([key,value]) => previous[key as keyof Chat] === value)) return
    this.changed(this.state.chats.map(chat => chat.id === id ? { ...chat, ...patch, updatedAt: new Date().toISOString() } : chat))
  }
  activate(id: string) {
    if (this.state.active.includes(id) || !this.state.chats.some(chat => chat.id === id)) return
    this.set({ active: [...this.state.active, id] })
  }
  report(id: string, binding: ResearchRunBinding) {
    const bindings = new Map(this.state.bindings); bindings.set(id,binding)
    this.set({ bindings })
    const previous = this.observed.get(id)
    if (previous?.state === binding.state && previous?.sources === binding.sources) return
    this.observed.set(id, { state: binding.state, sources: binding.sources })
    if (binding.state.phase !== 'idle') this.update(id, { checkpoint: checkpoint(binding.state, binding.sources) })
  }
  problem(message: string) { this.set({ error: message }) }
  get running(): string | undefined { return [...this.state.bindings].find(([,binding]) => binding.run?.running)?.[0] }
  perform(id: string, action: (binding: ResearchRunBinding) => void, needsModel = true): boolean {
    const binding = this.state.bindings.get(id)
    if (!binding || (this.running && this.running !== id) || (needsModel && binding.blocked) || binding.run?.running) return false
    action(binding); return true
  }
  remove(id: string) {
    if (this.state.bindings.get(id)?.run?.running) throw new Error('Stop the running chat before deleting it')
    const bindings = new Map(this.state.bindings); bindings.delete(id)
    this.observed.delete(id)
    this.set({ bindings, active: this.state.active.filter(active => active !== id) })
    this.changed(this.state.chats.filter(chat => chat.id !== id))
  }
  dispose() { clearTimeout(this.timer); this.state.bindings.forEach(binding => binding.run?.stop()) }
}
