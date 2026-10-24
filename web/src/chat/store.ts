import { conversationWire, encodeWireChat, encodeWireValue, wireField } from './wire'
import { reconcileChats } from './reconcile'
import type { CriticMode, CriticTier } from '@judgment-pack/harness/contract'
import { readCriticSettings } from '@judgment-pack/harness/criticSwitch'
import { savedCriticChoice } from '../assistant/criticSettings'
import {validPolicySources,type PolicyAuthoringSource} from '../policies/authoringSource'
import {validPolicyInterpretationProposal,type PolicyInterpretationProposal} from '../policies/interpretation'
import {validPolicyId,validPolicyRequest,validPolicyProposal,type PolicyRequest,type PolicyProposal} from '../policies/chat'
import {validPlans,type ChatPlan} from '@judgment-pack/harness/planning/model'
import { caseStatements, statementTurns, type Turn } from '../research/run'
import {validDialectChatAction,validDialectId,type DialectChatAction} from '../dialects/chatActions'
import {validDialectRecipeProposal,type DialectRecipeProposal} from '../dialects/proposals'
import { validGraphSnapshot, type GraphDraft } from '../graphs/drafts'
import { ASSISTANT_THINKING, type ThinkingTier } from '../config/deskConfig'
import { validAIConnectionId } from '../assistant/aiConnections'
import { disposeChatContext } from '../assistant/chatContexts'
import { validChatReasoning, type ChatReasoning } from './reasoning'
import type { AuthoringMode } from '../research/mode'
import { validSearchReference, type SearchReference } from '../search/results'
import { migrateMessageOwnership } from './messageOwnership'
import { draftPersistence, decodeDrafts, artifactCheckpoint, retainedSourceFiles, fromChat, draftTitle, type DraftPersistence, type DraftDocument, type PackDraft } from '../packs/drafts/model'
import { validWebsiteReference, type WebsiteReference } from '../documents/website'
import { timestampDate } from './timestamps'
import type { DocumentReference } from '../documents/client'
import { sourceMessage } from '../i18n/source'
import { answer, deskFetch } from '../files/client'
import { checkpoint, decodeCheckpoint, type Checkpoint } from './checkpoint'
import type { ResearchRunBinding } from '../research/useResearchRun'
import { validWebURL } from '../documents/record'

/** The link a web document was read from: the fetched address, and the anchor kept beside it, never sent. */
export interface ChatLink { url: string; anchor?: string; resolvedUrl?:string }
export interface ChatAttachment { id: string; name: string; text: string; document?: DocumentReference; link?: ChatLink }
const MAX_ANCHOR_BYTES = 4096
function validChatLink(link: unknown): link is ChatLink {
  if (!link || typeof link !== 'object' || Array.isArray(link)) return false
  const { url, anchor, resolvedUrl } = link as { url?: unknown; anchor?: unknown; resolvedUrl?:unknown }
  return validWebURL(url) && (resolvedUrl===undefined||validWebURL(resolvedUrl)) && (anchor === undefined || typeof anchor === 'string' && new TextEncoder().encode(anchor).length <= MAX_ANCHOR_BYTES && !/[\x00-\x1f\x7f]/.test(anchor))
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
 serverRequest?: {requestId:string;admissionId:string}
 policySources?:PolicyAuthoringSource[]
 policyInterpretationKind?:'pack'|'graph'
 policyInterpretations?:PolicyInterpretationProposal[]
 policy?:{id:string}
 policyRequest?:PolicyRequest
 policyProposals?:PolicyProposal[]
 interaction?:'plan'|'work'
 plans?:ChatPlan[]
 input?:{id:string}
 inputActions?:DialectChatAction[]
 inputDrafts?:DialectRecipeProposal[]
  graphDrafts?: GraphDraft[]
  /** A graph workspace is distinct from a pack candidate. */
  graph?: { id: string; path: string; workspace: string }
  apiThinking?:ThinkingTier
  aiConnection?:string
  aiConnectionName?:string
  id: string; title: string; pinned: boolean; archived: boolean; updatedAt: string
  /** First accepted submission; absent for history predating this field. */
  createdAt?: string
  reasoning?: ChatReasoning
  composer: string; model: string; mode: AuthoringMode; view: 'chat' | 'draft'
  pack?: { id: string; path: string; digest: string }
  checkpoint?: Checkpoint
  createdCandidateDigest?: string
  draftId?: string
  draftGeneration?: number
  targetFolderId?: string
  attachments?: ChatAttachment[]
  documents?: ChatAttachment[]
  websites?: WebsiteReference[]
  searches?: SearchReference[]
  researchMode?: 'auto' | 'provided'
  searchConnection?: string
  critic?: CriticMode
  criticReasoning?: CriticTier
  adversarialReview?: boolean
  titleEdited?: boolean
}
interface Document { version: 1; chats: Chat[] }
interface Reply { project: string; sha256: string; content: unknown; chatRevisions?: Record<string,string>; wireChats?:Record<string,string> }
interface Snapshot { packDrafts: PackDraft[]; deletedDrafts: string[]; chats: Chat[]; drafts: Chat[]; ready: boolean; saving: boolean; error: string; active: string[]; bindings: ReadonlyMap<string, ResearchRunBinding>; dirty: boolean }
export interface ChatPersistence { read(): Promise<Reply>; write(document: Document, digest: string, wireChats?:Record<string,string>, editedFields?:ReadonlyMap<string,ReadonlySet<string>>): Promise<Reply> }
const persistence: ChatPersistence = {
  read: async () => readReply(await deskFetch('/api/conversations')),
  write: async (document, digest, wireChats, editedFields) => readReply(await deskFetch('/api/conversations', {
    method: 'PUT', headers: { 'Content-Type': 'application/json', 'If-Match': digest }, body: '{"version":1,"chats":['+document.chats.map(chat=>encodeWireChat(chat,wireChats?.[chat.id],editedFields?.get(chat.id))).join(',')+']}'
  }))
}
async function readReply(response:Response):Promise<Reply> {
 if(!response.ok)return answer<Reply>(response)
 const raw=await response.text(),wireChats=conversationWire(raw)
 return {...JSON.parse(raw) as Reply,wireChats}
}
function decode(value: unknown,wireChats?:Record<string,string>): Chat[] {
  const doc = value as Partial<Document> | null
  if (doc?.version !== 1 || !Array.isArray(doc.chats) || doc.chats.length > 256) throw new Error(sourceMessage("Unsupported chat history. The saved file has not been changed."))
  const ids = new Set<string>()
  return doc.chats.map(value => {
    if (!value || typeof value !== 'object') throw new Error(sourceMessage("Invalid saved chat"))
    const chat = value as Chat
    if (typeof chat.id !== 'string' || !/^[a-zA-Z0-9-]{1,80}$/.test(chat.id) || ids.has(chat.id)
      || typeof chat.title !== 'string' || typeof chat.composer !== 'string' || typeof chat.model !== 'string'
      || typeof chat.pinned !== 'boolean' || typeof chat.archived !== 'boolean'
      || typeof chat.updatedAt !== 'string' || !Number.isFinite(Date.parse(chat.updatedAt)) || !['draft', 'research', 'web-research'].includes(chat.mode) || !['chat', 'draft'].includes(chat.view)) throw new Error(sourceMessage("Invalid saved chat. History has not been changed."))
    if(chat.serverRequest!==undefined&&(!chat.serverRequest||Object.keys(chat.serverRequest).sort().join(',')!=='admissionId,requestId'||![chat.serverRequest.requestId,chat.serverRequest.admissionId].every(v=>typeof v==='string'&&/^[A-Za-z0-9-]{1,100}$/.test(v))))throw Error(sourceMessage('Invalid saved chat'))
    if (chat.apiThinking!==undefined&&!ASSISTANT_THINKING.includes(chat.apiThinking))throw new Error(sourceMessage('Invalid saved chat'))
    if (chat.aiConnection !== undefined && !validAIConnectionId(chat.aiConnection)) throw new Error(sourceMessage('Invalid saved chat'))
    if (chat.aiConnectionName !== undefined && (typeof chat.aiConnectionName!=='string'||chat.aiConnectionName.length>128)) throw new Error(sourceMessage('Invalid saved chat'))
    if (chat.reasoning !== undefined && !validChatReasoning(chat.reasoning)) throw new Error(sourceMessage('Invalid saved chat'))
    if (chat.createdAt !== undefined && (typeof chat.createdAt !== 'string' || !timestampDate(chat.createdAt))) throw new Error(sourceMessage("Invalid saved chat"))
    if (chat.targetFolderId !== undefined && (typeof chat.targetFolderId !== 'string' || !/^[a-zA-Z0-9-]{1,80}$/.test(chat.targetFolderId))) throw new Error(sourceMessage("Invalid saved pack context"))
    if (chat.draftId !== undefined && (typeof chat.draftId !== 'string' || !/^draft-[a-z0-9-]{1,160}$/.test(chat.draftId))) throw new Error(sourceMessage('Invalid saved pack context'))
    if(chat.draftGeneration!==undefined && (!Number.isSafeInteger(chat.draftGeneration)||chat.draftGeneration<1)) throw new Error(sourceMessage('Invalid saved pack context'))
    if(chat.policy!==undefined&&(!chat.policy||Object.keys(chat.policy).length!==1||!validPolicyId(chat.policy.id)||chat.pack||chat.graph||chat.draftId||chat.input))throw Error(sourceMessage('Invalid saved Policy chat scope'))
    if(chat.policyInterpretationKind!==undefined&&!['pack','graph'].includes(chat.policyInterpretationKind))throw Error(sourceMessage('Invalid Policy interpretation kind'))
    if(chat.policySources!==undefined&&!validPolicySources(chat.policySources))throw Error(sourceMessage('Invalid saved Policy authoring sources'))
    if(chat.policyInterpretations!==undefined&&(!chat.policy||!Array.isArray(chat.policyInterpretations)||chat.policyInterpretations.length>16||!chat.policyInterpretations.every(p=>validPolicyInterpretationProposal(p)&&p.policyId===chat.policy!.id)||new Set(chat.policyInterpretations.map(p=>p.id)).size!==chat.policyInterpretations.length))throw Error(sourceMessage('Invalid saved Policy interpretation proposals'))
    if(chat.policyRequest!==undefined&&(!chat.policy||!validPolicyRequest(chat.policyRequest)))throw Error(sourceMessage('Invalid saved Policy request'))
    if(chat.policyProposals!==undefined&&(!chat.policy||!Array.isArray(chat.policyProposals)||chat.policyProposals.length>16||!chat.policyProposals.every(p=>validPolicyProposal(p)&&p.policyId===chat.policy!.id)||new Set(chat.policyProposals.map(p=>p.id)).size!==chat.policyProposals.length))throw Error(sourceMessage('Invalid saved Policy proposals'))
    if(chat.input!==undefined && (!chat.input||typeof chat.input!=='object'||Array.isArray(chat.input)||chat.pack||chat.graph||chat.draftId||Object.keys(chat.input).length!==1||!('id' in chat.input)||chat.input.id!=='list'&&!validDialectId(chat.input.id)))throw new Error(sourceMessage('Invalid saved dialect chat scope'))
    if(chat.inputActions!==undefined&&(!Array.isArray(chat.inputActions)||chat.inputActions.length>32||!chat.inputActions.every(validDialectChatAction)||new Set(chat.inputActions.map(a=>a.id)).size!==chat.inputActions.length))throw new Error(sourceMessage('Invalid saved dialect actions'))
    if(chat.inputDrafts!==undefined&&(!Array.isArray(chat.inputDrafts)||chat.inputDrafts.length>16||!chat.inputDrafts.every(validDialectRecipeProposal)||new Set(chat.inputDrafts.map(d=>d.draftId)).size!==chat.inputDrafts.length))throw new Error(sourceMessage('Invalid saved dialect recipe drafts'))
    if(Object.keys(chat).some(key=>key.toLowerCase()==='graphdrafts'&&key!=='graphDrafts'))throw Error(sourceMessage('Invalid saved graph drafts'))
    if (chat.graphDrafts !== undefined && (!Array.isArray(chat.graphDrafts) || chat.graphDrafts.length > 16 || !chat.graphDrafts.every(validGraphSnapshot) || new Set(chat.graphDrafts.map(d => d.draftId)).size !== chat.graphDrafts.length)) throw new Error(sourceMessage('Invalid saved graph drafts'))
    if (chat.graph && (chat.pack || chat.draftId || typeof chat.graph.id !== 'string' || typeof chat.graph.path !== 'string' || typeof chat.graph.workspace !== 'string' || chat.graph.id.length > 128 || chat.graph.path.length > 1024 || !/^[a-zA-Z0-9-]{1,160}$/.test(chat.graph.workspace))) throw new Error(sourceMessage('Invalid saved graph context'))
    if(chat.interaction!==undefined&&!['plan','work'].includes(chat.interaction)||chat.plans!==undefined&&!validPlans(chat.plans))throw Error(sourceMessage('Invalid saved plan'))
    readCriticSettings({critic:savedCriticChoice(chat),criticReasoning:chat.criticReasoning}, chat.criticReasoning !== undefined && !['off','on','ultra'].includes(chat.criticReasoning))
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
    if(chat.websites!==undefined && (!Array.isArray(chat.websites)||chat.websites.length>16||!chat.websites.every(validWebsiteReference)||new Set(chat.websites.map(w=>w.id)).size!==chat.websites.length))throw new Error(sourceMessage("Invalid saved attachments"))
    if(chat.searches!==undefined && (!Array.isArray(chat.searches)||chat.searches.length>64||!chat.searches.every(validSearchReference)||new Set(chat.searches.map(s=>s.id)).size!==chat.searches.length))throw new Error(sourceMessage('Invalid saved attachments'))
    if(chat.researchMode!==undefined&&!['auto','provided'].includes(chat.researchMode)||chat.searchConnection!==undefined&&(typeof chat.searchConnection!=='string'||!/^([a-z][a-z0-9-]{0,47})$/.test(chat.searchConnection)))throw new Error(sourceMessage('Invalid saved chat'))
    if (chat.pack && (typeof chat.pack.id !== 'string' || typeof chat.pack.path !== 'string' || typeof chat.pack.digest !== 'string')) throw new Error(sourceMessage("Invalid saved pack context"))
    return { serverRequest:chat.serverRequest, id: chat.id, title: chat.title, policySources:chat.policySources,policyInterpretationKind:chat.policyInterpretationKind,policyInterpretations:chat.policyInterpretations,policy:chat.policy,policyRequest:chat.policyRequest,policyProposals:chat.policyProposals, interaction:chat.interaction, plans:chat.plans, ...(chat.input?{input:chat.input}:{}), ...(chat.inputActions?{inputActions:chat.inputActions}:{}), ...(chat.inputDrafts?{inputDrafts:chat.inputDrafts}:{}), ...(chat.graph ? {graph: chat.graph} : {}), ...(chat.graphDrafts ? {graphDrafts: chat.graphDrafts} : {}), composer: chat.composer, model: chat.model, ...(chat.apiThinking?{apiThinking:chat.apiThinking}:{}), ...(chat.aiConnection?{aiConnection:chat.aiConnection,aiConnectionName:chat.aiConnectionName}:{}), ...(chat.reasoning ? {reasoning:{model:chat.reasoning.model,effort:chat.reasoning.effort}} : {}), pinned: chat.pinned, archived: chat.archived,
      updatedAt: chat.updatedAt, ...(chat.createdAt !== undefined ? { createdAt: chat.createdAt } : {}), mode: chat.mode === 'research' && !chat.checkpoint && !chat.draftId ? 'web-research' : chat.mode, view: chat.view, attachments: chat.attachments ?? [], documents: chat.documents ?? [], websites:chat.websites ?? [], searches:chat.searches??[], researchMode:chat.researchMode, searchConnection:chat.searchConnection, ...(chat.adversarialReview === undefined ? {} : { adversarialReview: chat.adversarialReview }), ...(chat.critic === undefined ? {} : { critic: chat.critic }), ...(chat.criticReasoning === undefined ? {} : { criticReasoning: chat.criticReasoning }), titleEdited: chat.titleEdited === true, ...(chat.pack ? { pack: chat.pack } : {}),
      ...(chat.draftId ? { draftId: chat.draftId, draftGeneration:chat.draftGeneration } : {}),
      ...(chat.targetFolderId !== undefined ? { targetFolderId: chat.targetFolderId } : {}),
      ...(chat.checkpoint ? { checkpoint: decodeCheckpoint(chat.checkpoint,wireChats?.[chat.id]&&wireField(wireChats[chat.id]!,'checkpoint')) } : {}),
      ...(typeof chat.createdCandidateDigest === 'string' ? { createdCandidateDigest: chat.createdCandidateDigest } : {}) }
  })
}

/** One project, multiple chats, one explicit running operation. No framework objects on disk. */
export class ChatStore {
  private state: Snapshot = { packDrafts: [], deletedDrafts: [], chats: [], drafts: [], ready: false, saving: false, error: '', active: [], bindings: new Map(), dirty: false }
  private listeners = new Set<() => void>()
  private leases = 0
  retain() { this.leases++; return () => { this.leases--; queueMicrotask(() => { if (!this.leases) this.dispose() }) } }
  private baselineWire:Record<string,string>={}
  private fieldEdits=new Map<string,Map<string,number>>()
  private baseline: Chat[] = []
  private serverOwned = false
  private disposed = false
  private pins = new Set<string>()
  private digest = 'absent'
  private revision = 0
  private savedRevision = 0
  private timer: ReturnType<typeof setTimeout> | undefined
  private loading: Promise<void> | null = null
  private observed = new Map<string, { state: unknown; sources: unknown }>()
  constructor(private project: string, private io: ChatPersistence = persistence, private draftIO: DraftPersistence = draftPersistence) {}
  private draftDigest = 'absent'
  private draftRevision = 0
  private savedDraftRevision = 0
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener) } }
  getSnapshot = () => this.state
  private set(patch: Partial<Snapshot>) { this.state = { ...this.state, ...patch }; this.listeners.forEach(listener => listener()) }
  private changed(chats: Chat[], patch: Partial<Snapshot> = {}) {
    const previous=new Map(this.state.chats.map(c=>[c.id,c]))
    for(const chat of chats){const before=previous.get(chat.id);for(const key of Object.keys(chat))if(!before||!Object.is(before[key as keyof Chat],chat[key as keyof Chat]))this.markField(chat.id,key)}
    this.revision++
    this.set({ ...patch, chats, dirty: true })
    clearTimeout(this.timer)
    this.timer = setTimeout(() => { void this.flush() }, 600)
  }
  private markField(id:string,key:string){const edits=this.fieldEdits.get(id)??new Map<string,number>();edits.set(key,this.revision+1);this.fieldEdits.set(id,edits)}
  private verifyWireConflicts(current:Reply,edits:ReadonlyMap<string,ReadonlySet<string>>){
    for(const [id,keys] of edits){const before=this.baselineWire[id],after=current.wireChats?.[id];if(!before||!after)continue;for(const key of keys)if(wireField(before,key)!==wireField(after,key))throw Error('The same chat field changed on the server. Your local changes are kept.')}
  }
  retryInitialLoad(): Promise<void> {
    if (this.state.ready || !this.state.error || this.state.dirty || this.state.saving || this.running) return Promise.resolve()
    return this.load()
  }
  load(): Promise<void> {
    if (this.loading) return this.loading
    this.loading = (async () => {
      try {
        const reply = await this.io.read()
        if (reply.project !== this.project) throw new Error(sourceMessage("Chat history belongs to a different project"))
        const chats = decode(reply.content,reply.wireChats).map(migrateMessageOwnership)
        const draftReply = await this.draftIO.read()
        if (draftReply.project !== this.project) throw new Error(sourceMessage('Chat history belongs to a different project'))
        const artifacts = decodeDrafts(draftReply.content,draftReply.wireDrafts)
        this.baselineWire=reply.wireChats??{}
        this.baseline = chats
        this.digest = reply.sha256
        this.draftDigest = draftReply.sha256
        this.set({ chats, packDrafts: artifacts.drafts, deletedDrafts: artifacts.deleted, ready: true, error: '' })
        // Idempotent migration includes archived chats. Original checkpoints
        // stay intact until users delete those conversations.
        for (const chat of chats) {
          if (!chat.pack && !chat.draftId && !chat.graph && chat.checkpoint?.state.candidates.length) {
            const candidate = fromChat(chat, chat.checkpoint)
            if (!artifacts.deleted.includes(candidate.id)) {
              if (!this.state.packDrafts.some(item => item.id === candidate.id) && !this.saveDraft(candidate)) break
              this.update(chat.id, {draftId: candidate.id,draftGeneration:this.state.packDrafts.find(item=>item.id===candidate.id)!.generation})
            }
          }
        }
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
    let document: Document = { version: 1, chats: this.state.chats }
    const local = this.state.chats
    let wireChats:Record<string,string>|undefined
    const fieldEdits=new Map([...this.fieldEdits].map(([id,keys])=>[id,new Set(keys.keys())]))
    const draftRevision = this.draftRevision
    const draftDocument: DraftDocument = {version: 1, drafts: this.state.packDrafts, deleted: this.state.deletedDrafts}
    this.set({ saving: true })
    try {
      // Commit artifacts first: a chat delete/link cannot orphan unsaved work.
      if (draftRevision !== this.savedDraftRevision) {
        const saved = await this.draftIO.write(draftDocument, this.draftDigest)
        if (saved.project !== this.project) throw new Error(sourceMessage('Chat save answered for a different project'))
        this.draftDigest = saved.sha256; this.savedDraftRevision = draftRevision
      }
      if(this.serverOwned) {
        const current=await this.io.read()
        if(current.project!==this.project||this.disposed)throw Error('The project changed before saving.')
        this.verifyWireConflicts(current,fieldEdits)
        const remote=decode(current.content,current.wireChats).map(migrateMessageOwnership)
        document={version:1,chats:reconcileChats(this.baseline,local,remote,(current.content as Document).chats)}
        this.digest=current.sha256
        wireChats=current.wireChats
      }
      const reply = await this.io.write(document, this.digest, wireChats, fieldEdits)
      if (reply.project !== this.project) throw new Error(sourceMessage("Chat save answered for a different project"))
      const saved=this.serverOwned?decode(reply.content,reply.wireChats).map(migrateMessageOwnership):local
      // Edits made while the CAS was in flight remain dirty and are merged once.
      const next=this.serverOwned?reconcileChats(local,this.state.chats,saved):this.state.chats
      this.baselineWire=reply.wireChats??{}
      for(const [id,keys] of this.fieldEdits){for(const [key,at] of keys)if(at<=revision)keys.delete(key);if(!keys.size)this.fieldEdits.delete(id)}
      this.baseline=saved
      this.set({chats:next})
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
  /** Await the current checkpoint even when a debounced save is already in flight. */
  async checkpoint(signal:AbortSignal):Promise<void> {
    signal.throwIfAborted()
    if(!this.state.ready || this.state.error)throw Error(this.state.error || 'Chat history is not ready.')
    const revision=this.revision, draftRevision=this.draftRevision
    while(this.savedRevision<revision || this.savedDraftRevision<draftRevision) {
      signal.throwIfAborted()
      if(!this.state.ready || this.state.error)throw Error(this.state.error || 'Chat history is not ready.')
      if(this.state.saving) {
        await new Promise<void>((resolve,reject)=>{
          let unsubscribe=()=>{}
          const done=()=>{if(!this.state.saving){unsubscribe();signal.removeEventListener('abort',abort);resolve()}}
          const abort=()=>{unsubscribe();signal.removeEventListener('abort',abort);reject(new DOMException('Stopped','AbortError'))}
          unsubscribe=this.subscribe(done);signal.addEventListener('abort',abort,{once:true});if(signal.aborted)abort();else done()
        })
      } else if(!await this.flush())throw Error(this.state.error || 'Chat history could not be saved.')
    }
    signal.throwIfAborted()
  }
  retrySave() { this.set({ error: '' }); void this.flush() }
  get projectRoot(){return this.project}
  get canCreate() { return this.state.ready && this.state.chats.length < 256 }
  private saveHomeDraft(chat?: Chat) {
    try {
      const key = `jpack.home-draft:${this.project}`
      if (chat) sessionStorage.setItem(key, JSON.stringify(chat))
      else sessionStorage.removeItem(key)
    } catch { /* A draft still works when browser storage is unavailable. */ }
  }
  /** An unsubmitted composer is not a conversation or a private-history write. */
  startChat(pack?: Chat['pack'], mode?: Chat['mode'], fresh = false, draftId?: string, graph?: Chat['graph'], input?:Chat['input'], policy?:Chat['policy']): Chat {
    if (!this.state.ready) throw new Error(sourceMessage("Chat history is still loading"))
    const previous = this.state.drafts.find(chat => chat.pack?.id === pack?.id && chat.draftId === draftId && chat.graph?.workspace === graph?.workspace && chat.input?.id===input?.id && chat.policy?.id===policy?.id && (mode === undefined || chat.mode === mode))
    if (previous && !fresh) { this.activate(previous.id); return previous }
    if (previous) this.remove(previous.id)
    let cached: Chat | undefined
    if (!pack && !draftId && !graph && !input && !policy && !fresh) {
      try {
        const value = JSON.parse(sessionStorage.getItem(`jpack.home-draft:${this.project}`) ?? 'null')
        if (value) {
          const read = decode({ version: 1, chats: [value] })[0]!
          if (!read.pack && !read.draftId && !read.graph && !read.input && !read.policy && !read.checkpoint && !read.archived && !read.pinned && read.title === 'New chat'
            && !this.state.chats.some(chat => chat.id === read.id) && (mode === undefined || read.mode === mode)) cached = read
        }
      } catch { /* A stale browser draft must not block opening home. */ }
    }
    const chat: Chat = cached ?? { id: crypto.randomUUID(), title: 'New chat', pinned: false, archived: false,
      updatedAt: new Date().toISOString(), composer: '', model: '', mode: mode ?? 'draft', view: 'chat', ...(pack ? { pack } : {}), ...(draftId ? {draftId} : {}), ...(graph ? {graph} : {}), ...(input?{input}:{}), ...(policy?{policy}:{}) }
    const artifact = this.state.packDrafts.find(item => item.id === draftId)
    if (artifact) { chat.draftGeneration=artifact.generation; chat.checkpoint = artifactCheckpoint(artifact.checkpoint); chat.documents = artifact.documents; chat.policySources = artifact.policySources; chat.mode = artifact.mode }
    this.set({ drafts: [chat, ...this.state.drafts] })
    if (!pack && !draftId && !graph && !input && !policy) this.saveHomeDraft(chat)
    this.activate(chat.id)
    return chat
  }
  /** Explicit Keep draft persists an unsubmitted workspace conversation without sending it. */
  retainChat(id: string) {
    const draft = this.state.drafts.find(chat => chat.id === id)
    if (!draft) return
    if (!this.canCreate) throw new Error(sourceMessage('Chat history is full. Export and delete an older chat first.'))
    this.changed([draft, ...this.state.chats], {drafts: this.state.drafts.filter(chat => chat.id !== id)})
  }
  create(pack?: Chat['pack'], mode: Chat['mode'] = 'draft'): Chat {
    if (!this.state.ready) throw new Error(sourceMessage("Chat history is still loading"))
    if (this.state.chats.length >= 256) throw new Error(sourceMessage("Chat history is full. Export and delete an older chat first."))
    const chat: Chat = { id: crypto.randomUUID(), title: 'New chat', pinned: false, archived: false, updatedAt: new Date().toISOString(), composer: '', model: '', mode, view: 'chat', ...(pack ? { pack } : {}) }
    this.changed([chat, ...this.state.chats]); this.activate(chat.id)
    return chat
  }
  update(id: string, patch: Partial<Omit<Chat, 'id' | 'updatedAt' | 'createdAt'>>) {
    if(this.pins.has(id)) { this.problem(sourceMessage('This chat is owned by an active server response. Reconnect or stop it before editing.')); return }
    const draft = this.state.drafts.find(chat => chat.id === id)
    const previous = draft ?? this.state.chats.find(chat => chat.id === id)
    if (previous?.mode === 'research' && patch.mode !== undefined && patch.mode !== 'research' && (this.state.bindings.get(id)?.state.candidates.length || previous.checkpoint?.state.candidates.length)) return
    if(!previous)return
    for(const key of Object.keys(patch))this.markField(id,key)
    if(Object.entries(patch).every(([key,value])=>Object.is(previous[key as keyof Chat],value)))return
    if (draft) {
      const next = { ...draft, ...patch, updatedAt: new Date().toISOString() }
      this.set({ drafts: this.state.drafts.map(chat => chat.id === id ? next : chat) })
      if (!next.pack && !next.draftId && !next.graph && !next.input && !next.policy) this.saveHomeDraft(next)
      return
    }
    this.changed(this.state.chats.map(chat => chat.id === id ? { ...chat, ...patch } : chat))
  }
  /** Uses the live session so later chat checkpoints cannot overwrite task messages. */
  appendConversation(id: string, turns: Turn[]) {
    if(this.pins.has(id))throw Error('Finish or reconcile the active server response before adding messages.')
    const owner = [...this.state.chats, ...this.state.drafts].find(chat => chat.id === id)
    const binding = this.state.bindings.get(id)
    if (!owner || !binding?.run) throw Error('The linked chat is still loading. Try again when it is ready.')
    if (binding.run.running) throw Error('Finish the active chat before adding test-design messages.')
    const previous = binding.run.getSnapshot()
    const next = binding.run.appendConversation(turns)
    if (next === previous && this.state.chats.some(chat=>chat.id===id)) return
    this.retainChat(id)
    const last = next.turns.at(-1)
    const saved = checkpoint(next, binding.sources)
    const bindings = new Map(this.state.bindings); bindings.set(id,{...binding,state:next})
    this.changed(this.state.chats.map(chat => chat.id === id ? {...chat,checkpoint:saved,
      createdAt:chat.createdAt ?? next.turns[0]?.at,updatedAt:last?.at ?? chat.updatedAt,
      title:chat.title === 'New chat' ? (next.turns.find(turn=>turn.role==='user')?.text.slice(0,100) || chat.title) : chat.title} : chat), {bindings})
  }
  activate(id: string) {
    if (this.state.active.includes(id) || ![...this.state.chats, ...this.state.drafts].some(chat => chat.id === id)) return
    this.set({ active: [...this.state.active, id] })
  }
  report(id: string, binding: ResearchRunBinding) {
    // A queued React report must not overwrite a task message appended meanwhile.
    const live = binding.run?.getSnapshot?.()
    if (live && live !== binding.state) return
    const bindings = new Map(this.state.bindings); bindings.set(id,binding)
    this.set({ bindings })
    if(binding.run?.ownership==='server') { this.serverOwned=true; return }
    const previous = this.observed.get(id)
    if (previous?.state === binding.state && previous?.sources === binding.sources) return
    this.observed.set(id, { state: binding.state, sources: binding.sources })
    const prior = previous?.state as ResearchRunBinding['state'] | undefined
    if (prior && previous?.sources === binding.sources && Object.entries(binding.state).every(([key, value]) => key === 'streaming' || key === 'streamingId' || key === 'taskProgress' || key === 'events' || prior[key as keyof typeof prior] === value)) return
    if (binding.state.phase !== 'idle') {
      const owner = this.state.chats.find(item => item.id === id) ?? this.state.drafts.find(item => item.id === id)
      if (owner && !owner.pack && !owner.graph && !owner.input && !owner.policy && binding.state.candidates.length && (!binding.state.restored || binding.state.checkRun === 'running' || binding.state.checkRun === 'failed' || binding.state.checkRun === 'stopped')) {
        const saved = checkpoint(binding.state, binding.sources)
        const current = this.state.packDrafts.find(item => item.id === owner.draftId)
        if (!owner.draftId) {
          const created = fromChat(owner, saved)
          if (!this.state.deletedDrafts.includes(created.id)) { if(this.saveDraft(created)) this.update(id,{draftId:created.id,draftGeneration:created.generation}) }
        } else if (current && !current.finalized && owner.draftGeneration===current.generation) {
          // Never replace a newer artifact with an older conversation branch.
          const accepted = current.checkpoint.state.candidates
          if (accepted.every((candidate,index) => saved.state.candidates[index]?.text === candidate.text)) {
            const next = {...current, title: current.titleEdited ? current.title : draftTitle(saved), checkpoint: artifactCheckpoint(saved), sourceFiles:[...new Map([...(current.sourceFiles??[]),...retainedSourceFiles(saved)].map(file=>[file.name+"\0"+file.text,file])).values()], policySources:owner.policySources??current.policySources, documents: retainSentDocuments(current.documents, owner.documents ?? [])}
            // Conversation-only work and source ownership must not advance the artifact generation.
            if (JSON.stringify(next) !== JSON.stringify(current)) {
              this.saveDraft({...next, generation:current.generation+1, updatedAt: new Date().toISOString()})
              this.update(id,{draftGeneration:current.generation+1})
            }
          }
        }
      }
      const chat = this.state.chats.find(item => item.id === id)
      // Opening, restoring, typing, pinning and rendering do not change recency.
      const last = binding.state.turns.at(-1)
      const before = chat?.checkpoint?.state.turns.at(-1)
      const changed = last && (!before || last.at !== before.at || last.text !== before.text)
      this.update(id, { checkpoint: checkpoint(binding.state, binding.sources) })
      if (changed && chat && !binding.state.restored) this.changed(this.state.chats.map(item => item.id === id ? { ...item, updatedAt: last.at } : item))
    }
  }
  private saveDraft(draft: PackDraft) {
    const previous = this.state.packDrafts.find(item => item.id === draft.id)
    if (!previous && this.state.packDrafts.length >= 256) { this.problem(sourceMessage('Draft storage is full. Delete an older draft first.')); return false }
    this.draftRevision++
    this.changed(this.state.chats,{packDrafts:previous ? this.state.packDrafts.map(item=>item.id===draft.id?draft:item) : [...this.state.packDrafts,draft]})
    return true
  }
  renameDraft(id: string, title: string) {
    const draft=this.state.packDrafts.find(item=>item.id===id)
    if(draft && title.trim()) this.saveDraft({...draft,title:title.trim().slice(0,500),titleEdited:true})
  }
  finalizeDraft(id: string, pack: NonNullable<Chat['pack']>) {
    const draft=this.state.packDrafts.find(item=>item.id===id)
    if(!draft) return
    this.saveDraft({...draft,finalized:pack})
    for(const chat of this.state.chats.filter(item=>item.draftId===id)) this.update(chat.id,{pack,createdCandidateDigest:draft.checkpoint.state.candidates.at(-1)?.digest})
  }
  removeDraft(id: string) {
    if(this.state.chats.some(chat=>chat.draftId===id && this.state.bindings.get(chat.id)?.run?.running)) return false
    if(this.state.deletedDrafts.length>=4096) return false
    this.draftRevision++
    this.changed(this.state.chats,{packDrafts:this.state.packDrafts.filter(item=>item.id!==id),deletedDrafts:[...new Set([...this.state.deletedDrafts,id])]})
    return true
  }
  /** Validate the entire selection before changing either artifact store. */
  removeDraftItems(items: ({kind:'pack';id:string}|{kind:'graph';chatId:string;id:string})[]): boolean {
    if (!this.state.ready || this.running || !items.length) return false
    const packIds = new Set(items.filter(item=>item.kind==='pack').map(item=>item.id))
    const graphItems = items.filter(item=>item.kind==='graph')
    const deleted = [...new Set([...this.state.deletedDrafts,...packIds])]
    if (deleted.length > 4096 || [...packIds].some(id=>!this.state.packDrafts.some(draft=>draft.id===id&&!draft.finalized))
      || graphItems.some(item=>!this.state.chats.some(chat=>chat.id===item.chatId&&chat.graphDrafts?.some(draft=>draft.draftId===item.id&&!draft.saved)))) return false
    const chats = this.state.chats.map(chat=>{
      const ids = new Set(graphItems.filter(item=>item.chatId===chat.id).map(item=>item.id))
      return ids.size ? {...chat,graphDrafts:chat.graphDrafts?.filter(draft=>!ids.has(draft.draftId))} : chat
    })
    if (packIds.size) this.draftRevision++
    this.changed(chats,{packDrafts:this.state.packDrafts.filter(draft=>!packIds.has(draft.id)),deletedDrafts:deleted})
    return true
  }
  problem(message: string) { this.set({ error: message }) }
  get running(): string | undefined { return [...this.state.bindings].find(([,binding]) => binding.run?.running)?.[0] }
  perform(id: string, action: (binding: ResearchRunBinding) => void, needsModel = true): boolean {
    const binding = this.state.bindings.get(id)
    if (!binding || (this.running && this.running !== id) || (needsModel && binding.blocked) || binding.run?.running) return false
    const owner = [...this.state.chats,...this.state.drafts].find(chat=>chat.id===id)
    if (owner?.draftId && !owner.pack) {
      const artifact = this.state.packDrafts.find(item=>item.id===owner.draftId)
      const current = binding.state.candidates.at(-1)?.text
      if (!artifact || owner.draftGeneration!==artifact.generation || artifact.checkpoint.state.candidates.at(-1)?.text !== current) {
        this.problem(sourceMessage('This conversation has an older draft. Open the pack and start a new chat to continue.')); return false
      }
    }
    const draft = this.state.drafts.find(chat => chat.id === id)
    if (draft && needsModel) {
      if (!this.canCreate) { this.problem(sourceMessage('Chat history is full. Delete an older chat before sending.')); return false }
      this.changed([{ ...draft, createdAt: new Date().toISOString() }, ...this.state.chats], { drafts: this.state.drafts.filter(chat => chat.id !== id) })
      if (!draft.pack && !draft.draftId && !draft.graph) this.saveHomeDraft()
    }
    action(binding); return true
  }
  remove(id: string) {
    if (this.state.bindings.get(id)?.run?.running) throw new Error(sourceMessage("Stop the running chat before deleting it"))
    const bindings = new Map(this.state.bindings); bindings.delete(id)
    this.observed.delete(id)
    const draft = this.state.drafts.find(chat => chat.id === id)
    this.set({ bindings, active: this.state.active.filter(active => active !== id), drafts: this.state.drafts.filter(chat => chat.id !== id) })
    if (draft && !draft.pack && !draft.draftId && !draft.graph) this.saveHomeDraft()
    if (!draft) this.changed(this.state.chats.filter(chat => chat.id !== id))
    disposeChatContext(id)
  }
  dispose() { this.disposed=true; clearTimeout(this.timer); this.state.bindings.forEach(binding => binding.run?.ownership==='server' ? binding.run.disconnect?.() : binding.run?.stop()) }
  /** Original user turn and admission identity become durable before any POST. */
  async prepareServerRequest(id:string, state:ResearchRunBinding['state'], requestId:string, admissionId:string, signal:AbortSignal):Promise<string> {
    this.serverOwned=true
    this.retainChat(id)
    if(this.pins.has(id))throw Error('The previous server response has not reconciled.')
	const owner=[...this.state.chats,...this.state.drafts].find(chat=>chat.id===id)
	const retained=checkpoint(state,[])
	retained.sources=owner?.checkpoint?.sources??[]
    this.update(id,{checkpoint:retained,serverRequest:{requestId,admissionId}})
    await this.checkpoint(signal)
    const reply=await this.io.read();signal.throwIfAborted()
    if(reply.project!==this.project||this.disposed)throw Error('The project changed before starting.')
    const revision=reply.chatRevisions?.[id]
    if(!revision||!/^[a-f0-9]{64}$/.test(revision))throw Error('The exact saved chat revision is unavailable.')
    const saved=decode(reply.content,reply.wireChats).find(c=>c.id===id)
    if(saved?.serverRequest?.requestId!==requestId||saved.serverRequest.admissionId!==admissionId||!saved.checkpoint?.state.turns.some(t=>t.id===requestId))throw Error('The saved request changed before starting.')
    this.pins.add(id)
    return revision
  }
  /** Only the controller's proven pre-POST branch may release an unadmitted marker. */
  releaseUnsentServerRequest(id:string,requestId:string,state:ResearchRunBinding['state']) {
    const chat=[...this.state.chats,...this.state.drafts].find(c=>c.id===id)
    if(chat?.serverRequest?.requestId!==requestId||this.disposed)return
    this.pins.delete(id)
    const retained=checkpoint({...state,phase:'conversation',status:'stopped'},[]);retained.sources=chat.checkpoint?.sources??[]
    this.update(id,{serverRequest:undefined,checkpoint:retained})
  }
  pinServerChat(id:string) { this.serverOwned=true;this.pins.add(id) }
  /** Read-only private snapshot reconciliation. Local edits survive both refresh and CAS conflicts. */
  async reconcileServer(id:string, release=false, projection?:{digest:string;responseId:string;requestId:string;admissionId:string}):Promise<Chat|undefined> {
    const reply=await this.io.read()
    if(reply.project!==this.project||this.disposed)throw Error('The project changed during chat reconciliation.')
    this.verifyWireConflicts(reply,new Map([...this.fieldEdits].map(([id,keys])=>[id,new Set(keys.keys())])))
    const remote=decode(reply.content,reply.wireChats).map(migrateMessageOwnership)
    const chats=reconcileChats(this.baseline,this.state.chats,remote)
    this.baseline=remote;this.baselineWire=reply.wireChats??{};this.digest=reply.sha256
    if(release)this.pins.delete(id)
    this.set({chats})
	const selected=chats.find(c=>c.id===id)
	const exactProjection=projection&&reply.chatRevisions?.[id]===projection.digest&&selected?.serverRequest?.requestId===projection.requestId&&selected.serverRequest.admissionId===projection.admissionId&&selected.checkpoint?.state.responses?.some(r=>r.id===projection.responseId)
	if(release&&exactProjection&&selected?.checkpoint?.state.candidates.length&&!selected.pack&&!selected.graph&&!selected.input&&!selected.policy){
		const state=selected.checkpoint.state,statements=caseStatements(state)
        for(const {id:statementId,turn} of statementTurns(state.turns))if(turn.creation)statements.delete(state.inheritedStatements===undefined?statementId:`message:${turn.id}`)
        const saved:Checkpoint={...selected.checkpoint,state:{...state,inheritedStatements:[...statements],phase:'review',status:'needs-input',restored:true,execution:undefined,failure:undefined,events:[],checkRun:undefined,readiness:''}},current=this.state.packDrafts.find(d=>d.id===selected.draftId)
		if(!selected.draftId){
			const created=fromChat(selected,saved)
			if(!this.state.deletedDrafts.includes(created.id)){
				const retained=this.state.packDrafts.find(d=>d.id===created.id)
				if(!retained&&!this.saveDraft(created))throw Error('The retained draft could not be saved.')
				this.update(id,{draftId:created.id,draftGeneration:retained?.generation??created.generation})
			}
		}else if(current&&!current.finalized&&selected.draftGeneration===current.generation){
			const accepted=current.checkpoint.state.candidates
			if(!accepted.every((c,index)=>saved.state.candidates[index]?.text===c.text))throw Error('The retained draft changed before reconciliation.')
			const next={...current,title:current.titleEdited?current.title:draftTitle(saved),checkpoint:artifactCheckpoint(saved),sourceFiles:[...new Map([...(current.sourceFiles??[]),...retainedSourceFiles(saved)].map(file=>[file.name+'\0'+file.text,file])).values()],policySources:selected.policySources??current.policySources,documents:retainSentDocuments(current.documents,selected.documents??[])}
			// Original chat history retains every control and observation. Artifact
			// generation tracks the candidate and independent evidence; a check or
			// visible correction proposal cannot invalidate its own approval token.
			const basis=(draft:PackDraft)=>{const s=draft.checkpoint.state;return JSON.stringify([draft.title,draft.mode,s.candidates.map(c=>c.text),encodeWireValue(s.cases),encodeWireValue(s.expectationIssues),encodeWireValue(s.heldProposal),s.unknowns,s.inheritedStatements,draft.checkpoint.sources,draft.sourceFiles,draft.policySources,draft.documents])}
			if(basis(next)!==basis(current)){
				if(!this.saveDraft({...next,generation:current.generation+1,updatedAt:selected.updatedAt}))throw Error('The retained draft could not be saved.')
				this.update(id,{draftGeneration:current.generation+1})
			}
		}
		if(this.state.dirty&&!await this.flush())throw Error('The retained draft link could not be saved.')
	}
    return this.state.chats.find(c=>c.id===id)
  }

}

/** An explicit control edit replaces the one-release legacy alias, including on inherit. */
export function criticChoice(critic: CriticMode | undefined): Pick<Chat, 'critic' | 'adversarialReview'> {
  return {critic, adversarialReview: undefined}
}
