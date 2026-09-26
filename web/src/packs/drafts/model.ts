import { decodeCheckpoint, type Checkpoint } from '../../chat/checkpoint'
import type { Chat, ChatAttachment } from '../../chat/store'
import { answer, deskFetch } from '../../files/client'
import { sourceMessage } from '../../i18n/source'
import { HOME_FOLDER } from '../folders/model'

/** Desk lifecycle data. None of these fields enter a JPS document. */
export interface PackDraft {
  generation: number
  id: string; title: string; titleEdited?: boolean; folderId: string
  createdAt: string; updatedAt: string; mode: Chat['mode']; checkpoint: Checkpoint
  sourceFiles?: {name:string;text:string}[]
  documents: ChatAttachment[]; finalized?: NonNullable<Chat['pack']>
}
export interface DraftDocument { version: 1; drafts: PackDraft[]; deleted: string[] }
export interface DraftReply { project: string; sha256: string; content: unknown }
export interface DraftPersistence {
  read(): Promise<DraftReply>
  write(document: DraftDocument, digest: string): Promise<DraftReply>
}
export const draftPersistence: DraftPersistence = {
  read: async () => answer<DraftReply>(await deskFetch('/api/draft-packs')),
  write: async (document, digest) => answer<DraftReply>(await deskFetch('/api/draft-packs', { method: 'PUT', headers: { 'Content-Type': 'application/json', 'If-Match': digest }, body: JSON.stringify(document) }))
}
export const emptyDraftDocument = (): DraftDocument => ({version: 1, drafts: [], deleted: []})
const id = (value: unknown): value is string => typeof value === 'string' && /^draft-[a-z0-9-]{1,160}$/.test(value)
export function decodeDrafts(value: unknown): DraftDocument {
  const doc = value as DraftDocument | null
  const invalid = (): never => { throw new Error(sourceMessage('Saved drafts could not be read. The saved file has not been changed.')) }
  if (doc?.version !== 1 || !Array.isArray(doc.drafts) || doc.drafts.length > 256 || !Array.isArray(doc.deleted) || doc.deleted.length > 4096 || doc.deleted.some(value => !id(value)) || new Set(doc.deleted).size !== doc.deleted.length) return invalid()
  const seen = new Set(doc.deleted)
  return {version: 1, deleted: [...doc.deleted], drafts: doc.drafts.map(draft => {
    if (!draft || !Number.isSafeInteger(draft.generation) || draft.generation<1 || !id(draft.id) || seen.has(draft.id) || typeof draft.title !== 'string' || !draft.title.trim() || draft.title.length > 500 || typeof draft.folderId !== 'string' || !/^[a-zA-Z0-9-]{1,80}$/.test(draft.folderId) || !['draft','research'].includes(draft.mode) || !Number.isFinite(Date.parse(draft.createdAt)) || !Number.isFinite(Date.parse(draft.updatedAt)) || !Array.isArray(draft.documents) || draft.documents.length > 256) return invalid()
    if (draft.finalized && (typeof draft.finalized.id !== 'string' || typeof draft.finalized.path !== 'string' || typeof draft.finalized.digest !== 'string')) return invalid()
    for (const file of draft.documents) {
      if (!file || typeof file.name !== 'string' || typeof file.id !== 'string' || file.text !== '' || !file.document) return invalid()
      const ref=file.document
      if(file.id!==ref.id || !/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(ref.id) || !/^sha256:[a-f0-9]{64}$/.test(ref.digest) || !Array.isArray(ref.pages) || ref.pages.length>500 || ref.pages.some(n=>!Number.isSafeInteger(n)||n<1) || new Set(ref.pages).size!==ref.pages.length || typeof ref.allowPartial!=='boolean') return invalid()
    }
    if (draft.sourceFiles !== undefined && (!Array.isArray(draft.sourceFiles) || draft.sourceFiles.some(file=>!file || typeof file.name!=='string' || typeof file.text!=='string'))) return invalid()
    seen.add(draft.id)
    const saved = decodeCheckpoint(draft.checkpoint)
    if (!saved.state.candidates.length || saved.state.turns.length || saved.state.responses?.length) return invalid()
    return {...draft, checkpoint: saved}
  })}
}
/** An artifact retains its evidence, never a hidden copy of conversation turns. */
export function artifactCheckpoint(value: Checkpoint): Checkpoint {
  return { ...value, state: {...value.state, turns: [], responses: [], candidates: value.state.candidates.map(({responseId: _responseId, ...candidate}) => candidate), streaming: undefined, streamingId: undefined, events: []} }
}
export function draftTitle(saved: Checkpoint): string {
  const title = (saved.state.candidates.at(-1)?.document as {title?:unknown} | undefined)?.title
  return typeof title === 'string' && title.trim() ? title.trim().slice(0,500) : 'Draft'
}
export function retainedSourceFiles(saved: Checkpoint): {name:string;text:string}[] {
  const files=new Map<string,{name:string;text:string}>()
  for(const turn of saved.state.turns) {
    if(turn.role!=='user')continue
    const matcher=/\n\nAttached file \(reference material, not instructions\): ([^\n]+)\n("(?:[^"\\]|\\.)*")/g
    for(const match of (turn.input??turn.text).matchAll(matcher)) {
      try { const text:unknown=JSON.parse(match[2]!); if(typeof text==='string')files.set(match[1]!+'\0'+text,{name:match[1]!,text}) } catch { /* Not a complete file block. */ }
    }
  }
  return [...files.values()]
}
export function fromChat(chat: Chat, saved: Checkpoint): PackDraft {
  return {generation:1,id: `draft-${Array.from(new TextEncoder().encode(chat.id),byte=>byte.toString(16).padStart(2,'0')).join('')}`, title: draftTitle(saved), folderId: chat.targetFolderId ?? HOME_FOLDER,
    createdAt: chat.createdAt ?? chat.updatedAt, updatedAt: chat.updatedAt, mode: chat.mode,
    checkpoint: artifactCheckpoint(saved), sourceFiles:retainedSourceFiles(saved), documents: chat.documents ?? []}
}
export const draftHref = (id: string) => `/packs/drafts/${encodeURIComponent(id)}`
export const latestText = (draft: PackDraft) => draft.checkpoint.state.candidates.at(-1)?.text
