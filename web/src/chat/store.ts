import { timestampDate } from './timestamps'
import type { DocumentReference } from '../documents/client'
import { sourceMessage } from '../i18n/source'
import { answer, deskFetch } from '../files/client'
import { checkpoint, decodeCheckpoint, type Checkpoint } from './checkpoint'
import type { ResearchRunBinding } from '../research/useResearchRun'
import { validWebURL } from '../documents/record'

/** The link a web document was read from: the fetched address, and the anchor kept beside it, never sent. */
export interface ChatLink { url: string; anchor?: string }
export interface ChatAttachment { id: string; name: string; text: string; document?: DocumentReference; link?: ChatLink }
const MAX_ANCHOR_BYTES = 4096
function validChatLink(link: unknown): link is ChatLink {
  if (!link || typeof link !== 'object' || Array.isArray(link)) return false
  const { url, anchor } = link as { url?: unknown; anchor?: unknown }
  return validWebURL(url) && (anchor === undefined || typeof anchor === 'string' && new TextEncoder().encode(anchor).length <= MAX_ANCHOR_BYTES && !/[\x00-\x1f\x7f]/.test(anchor))
}
/** Keep pages used by earlier turns available to their citations after reuse. */
export function retainSentDocuments(previous: ChatAttachment[], sent: ChatAttachment[]): ChatAttachment[] {
  const documents = new Map(previous.map(file => [file.id, file]))
  for (const file of sent) {
    if (!file.document) continue
    const before = documents.get(file.id)?.document
    documents.set(file.id, before?.digest === file.document.digest ? {
      ...file, document: { ...file.document, pages: [...new Set([...before.pages, ...file.document.pages])].sort((a, b) => a-b), allowPartial: before.allowPartial || file.document.allowPartial }
    } : file)
  }
  return [...documents.values()]
}
export interface Chat {
  id: string; title: string; pinned: boolean; archived: boolean; updatedAt: string
  /** First accepted submission; absent for history predating this field. */
  createdAt?: string
  composer: string; model: string; mode: 'draft' | 'research'; view: 'chat' | 'draft'
  pack?: { id: string; path: string; digest: string }
  checkpoint?: Checkpoint
  createdCandidateDigest?: string
  attachments?: ChatAttachment[]
  documents?: ChatAttachment[]
  adversarialReview?: boolean
  titleEdited?: boolean
}
interface Document { version: 1; chats: Chat[] }
interface Reply { project: string; sha256: string; content: unknown }
interface Snapshot { chats: Chat[]; drafts: Chat[]; ready: boolean; saving: boolean; error: string; active: string[]; bindings: ReadonlyMap<string, ResearchRunBinding>; dirty: boolean }
export interface ChatPersistence { read(): Promise<Reply>; write(document: Document, digest: string): Promise<Reply> }
const persistence: ChatPersistence = {
  read: async () => answer<Reply>(await deskFetch('/api/conversations')),
  write: async (document, digest) => answer<Reply>(await deskFetch('/api/conversations', {
    method: 'PUT', headers: { 'Content-Type': 'application/json', 'If-Match': digest }, body: JSON.stringify(document)
  }))
}
function decode(value: unknown): Chat[] {
  const doc = value as Partial<Document> | null
  if (doc?.version !== 1 || !Array.isArray(doc.chats) || doc.chats.length > 256) throw new Error(sourceMessage("Unsupported chat history. The saved file has not been changed."))
  const ids = new Set<string>()
  return doc.chats.map(value => {
    if (!value || typeof value !== 'object') throw new Error(sourceMessage("Invalid saved chat"))
    const chat = value as Chat
    if (typeof chat.id !== 'string' || !/^[a-zA-Z0-9-]{1,80}$/.test(chat.id) || ids.has(chat.id)
      || typeof chat.title !== 'string' || typeof chat.composer !== 'string' || typeof chat.model !== 'string'
      || typeof chat.pinned !== 'boolean' || typeof chat.archived !== 'boolean'
      || typeof chat.updatedAt !== 'string' || !Number.isFinite(Date.parse(chat.updatedAt)) || !['draft', 'research'].includes(chat.mode) || !['chat', 'draft'].includes(chat.view)) throw new Error(sourceMessage("Invalid saved chat. History has not been changed."))
    if (chat.createdAt !== undefined && (typeof chat.createdAt !== 'string' || !timestampDate(chat.createdAt))) throw new Error(sourceMessage("Invalid saved chat"))
    ids.add(chat.id)
    if (chat.attachments !== undefined && (!Array.isArray(chat.attachments) || chat.attachments.length > 4
      || chat.attachments.some(file => !file || typeof file.id !== 'string' || typeof file.name !== 'string' || typeof file.text !== 'string' || file.text.length > 200_000))) throw new Error(sourceMessage("Invalid saved attachments"))
    if (chat.documents !== undefined && (!Array.isArray(chat.documents) || chat.documents.length > 256)) throw new Error(sourceMessage("Invalid saved attachments"))
    for (const file of [...(chat.attachments ?? []), ...(chat.documents ?? [])]) {
      if (!file) throw new Error(sourceMessage("Invalid saved attachments"))
      const ref = file.document
      if (ref && file.text !== '') throw new Error(sourceMessage("Invalid saved attachments"))
      if (file.link !== undefined && (!ref || !validChatLink(file.link))) throw new Error(sourceMessage("Invalid saved attachments"))
      if (ref && (file.id !== ref.id || !/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(ref.id) || !/^sha256:[a-f0-9]{64}$/.test(ref.digest) || !Array.isArray(ref.pages) || ref.pages.length > 500 || new Set(ref.pages).size !== ref.pages.length || ref.pages.some(n => !Number.isSafeInteger(n) || n < 1) || typeof ref.allowPartial !== 'boolean')) throw new Error(sourceMessage("Invalid saved attachments"))
    }
    if (chat.documents !== undefined && (!Array.isArray(chat.documents) || chat.documents.length > 256 || chat.documents.some(file => !file?.document || typeof file.name !== 'string' || typeof file.text !== 'string'))) throw new Error(sourceMessage("Invalid saved attachments"))
    if (chat.pack && (typeof chat.pack.id !== 'string' || typeof chat.pack.path !== 'string' || typeof chat.pack.digest !== 'string')) throw new Error(sourceMessage("Invalid saved pack context"))
    return { id: chat.id, title: chat.title, composer: chat.composer, model: chat.model, pinned: chat.pinned, archived: chat.archived,
      updatedAt: chat.updatedAt, ...(chat.createdAt !== undefined ? { createdAt: chat.createdAt } : {}), mode: chat.mode, view: chat.view, attachments: chat.attachments ?? [], documents: chat.documents ?? [], adversarialReview: chat.adversarialReview === true, titleEdited: chat.titleEdited === true, ...(chat.pack ? { pack: chat.pack } : {}),
      ...(chat.checkpoint ? { checkpoint: decodeCheckpoint(chat.checkpoint) } : {}),
      ...(typeof chat.createdCandidateDigest === 'string' ? { createdCandidateDigest: chat.createdCandidateDigest } : {}) }
  })
}

/** One project, multiple chats, one explicit running operation. No framework objects on disk. */
export class ChatStore {
  private state: Snapshot = { chats: [], drafts: [], ready: false, saving: false, error: '', active: [], bindings: new Map(), dirty: false }
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
  private changed(chats: Chat[], patch: Partial<Snapshot> = {}) {
    this.revision++
    this.set({ ...patch, chats, dirty: true })
    clearTimeout(this.timer)
    this.timer = setTimeout(() => { void this.flush() }, 600)
  }
  load(): Promise<void> {
    if (this.loading) return this.loading
    this.loading = (async () => {
      try {
        const reply = await this.io.read()
        if (reply.project !== this.project) throw new Error(sourceMessage("Chat history belongs to a different project"))
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
      if (reply.project !== this.project) throw new Error(sourceMessage("Chat save answered for a different project"))
      this.digest = reply.sha256
      this.savedRevision = revision
      this.set({ saving: false, dirty: this.revision !== revision, error: '' })
      if (this.revision !== revision) this.timer = setTimeout(() => { void this.flush() }, 600)
      return true
    } catch (error) {
      this.set({ saving: false, error: sourceMessage("Chat changes are not saved: {{value0}}", { value0: (error as Error).message }) })
      return false
    }
  }
  retrySave() { this.set({ error: '' }); void this.flush() }
  get canCreate() { return this.state.ready && this.state.chats.length < 256 }
  private saveHomeDraft(chat?: Chat) {
    try {
      const key = `jpack.home-draft:${this.project}`
      if (chat) sessionStorage.setItem(key, JSON.stringify(chat))
      else sessionStorage.removeItem(key)
    } catch { /* A draft still works when browser storage is unavailable. */ }
  }
  /** An unsubmitted composer is not a conversation or a private-history write. */
  startChat(pack?: Chat['pack'], mode?: Chat['mode'], fresh = false): Chat {
    if (!this.state.ready) throw new Error(sourceMessage("Chat history is still loading"))
    const previous = this.state.drafts.find(chat => chat.pack?.id === pack?.id && (mode === undefined || chat.mode === mode))
    if (previous && !fresh) { this.activate(previous.id); return previous }
    if (previous) this.remove(previous.id)
    let cached: Chat | undefined
    if (!pack && !fresh) {
      try {
        const value = JSON.parse(sessionStorage.getItem(`jpack.home-draft:${this.project}`) ?? 'null')
        if (value) {
          const read = decode({ version: 1, chats: [value] })[0]!
          if (!read.pack && !read.checkpoint && !read.archived && !read.pinned && read.title === 'New chat'
            && !this.state.chats.some(chat => chat.id === read.id) && (mode === undefined || read.mode === mode)) cached = read
        }
      } catch { /* A stale browser draft must not block opening home. */ }
    }
    const chat: Chat = cached ?? { id: crypto.randomUUID(), title: 'New chat', pinned: false, archived: false,
      updatedAt: new Date().toISOString(), composer: '', model: '', mode: mode ?? 'draft', view: 'chat', ...(pack ? { pack } : {}) }
    this.set({ drafts: [chat, ...this.state.drafts] })
    if (!pack) this.saveHomeDraft(chat)
    this.activate(chat.id)
    return chat
  }
  create(pack?: Chat['pack'], mode: Chat['mode'] = 'draft'): Chat {
    if (!this.state.ready) throw new Error(sourceMessage("Chat history is still loading"))
    if (this.state.chats.length >= 256) throw new Error(sourceMessage("Chat history is full. Export and delete an older chat first."))
    const chat: Chat = { id: crypto.randomUUID(), title: 'New chat', pinned: false, archived: false, updatedAt: new Date().toISOString(), composer: '', model: '', mode, view: 'chat', ...(pack ? { pack } : {}) }
    this.changed([chat, ...this.state.chats]); this.activate(chat.id)
    return chat
  }
  update(id: string, patch: Partial<Omit<Chat, 'id' | 'updatedAt' | 'createdAt'>>) {
    const draft = this.state.drafts.find(chat => chat.id === id)
    const previous = draft ?? this.state.chats.find(chat => chat.id === id)
    if (previous?.mode === 'research' && patch.mode === 'draft' && (this.state.bindings.get(id)?.state.candidates.length || previous.checkpoint?.state.candidates.length)) return
    if (!previous || Object.entries(patch).every(([key,value]) => previous[key as keyof Chat] === value)) return
    if (draft) {
      const next = { ...draft, ...patch, updatedAt: new Date().toISOString() }
      this.set({ drafts: this.state.drafts.map(chat => chat.id === id ? next : chat) })
      if (!next.pack) this.saveHomeDraft(next)
      return
    }
    this.changed(this.state.chats.map(chat => chat.id === id ? { ...chat, ...patch } : chat))
  }
  activate(id: string) {
    if (this.state.active.includes(id) || ![...this.state.chats, ...this.state.drafts].some(chat => chat.id === id)) return
    this.set({ active: [...this.state.active, id] })
  }
  report(id: string, binding: ResearchRunBinding) {
    const bindings = new Map(this.state.bindings); bindings.set(id,binding)
    this.set({ bindings })
    const previous = this.observed.get(id)
    if (previous?.state === binding.state && previous?.sources === binding.sources) return
    this.observed.set(id, { state: binding.state, sources: binding.sources })
    const prior = previous?.state as ResearchRunBinding['state'] | undefined
    if (prior && previous?.sources === binding.sources && Object.entries(binding.state).every(([key, value]) => key === 'streaming' || key === 'events' || prior[key as keyof typeof prior] === value)) return
    if (binding.state.phase !== 'idle') {
      const chat = this.state.chats.find(item => item.id === id)
      // Opening, restoring, typing, pinning and rendering do not change recency.
      const last = binding.state.turns.at(-1)
      const before = chat?.checkpoint?.state.turns.at(-1)
      const changed = last && (!before || last.at !== before.at || last.text !== before.text)
      this.update(id, { checkpoint: checkpoint(binding.state, binding.sources) })
      if (changed && chat && !binding.state.restored) this.changed(this.state.chats.map(item => item.id === id ? { ...item, updatedAt: last.at } : item))
    }
  }
  problem(message: string) { this.set({ error: message }) }
  get running(): string | undefined { return [...this.state.bindings].find(([,binding]) => binding.run?.running)?.[0] }
  perform(id: string, action: (binding: ResearchRunBinding) => void, needsModel = true): boolean {
    const binding = this.state.bindings.get(id)
    if (!binding || (this.running && this.running !== id) || (needsModel && binding.blocked) || binding.run?.running) return false
    const draft = this.state.drafts.find(chat => chat.id === id)
    if (draft && needsModel) {
      if (!this.canCreate) { this.problem(sourceMessage('Chat history is full. Delete an older chat before sending.')); return false }
      this.changed([{ ...draft, createdAt: new Date().toISOString() }, ...this.state.chats], { drafts: this.state.drafts.filter(chat => chat.id !== id) })
      if (!draft.pack) this.saveHomeDraft()
    }
    action(binding); return true
  }
  remove(id: string) {
    if (this.state.bindings.get(id)?.run?.running) throw new Error(sourceMessage("Stop the running chat before deleting it"))
    const bindings = new Map(this.state.bindings); bindings.delete(id)
    this.observed.delete(id)
    const draft = this.state.drafts.find(chat => chat.id === id)
    this.set({ bindings, active: this.state.active.filter(active => active !== id), drafts: this.state.drafts.filter(chat => chat.id !== id) })
    if (draft && !draft.pack) this.saveHomeDraft()
    if (!draft) this.changed(this.state.chats.filter(chat => chat.id !== id))
  }
  dispose() { clearTimeout(this.timer); this.state.bindings.forEach(binding => binding.run?.stop()) }
}
