import type { AssistantEvent } from '../assistant/engine'
import type { ChatAttachment } from './store'
import { validWebURL } from '../documents/record'
import { validWebsiteReference, type WebsiteReference } from '../documents/website'

export interface WorkItem { id: string; name: string; status: 'working' | 'complete' | 'failed' | 'interrupted' }
/** Pair by invocation identity, never by a tool name (parallel calls may repeat). */
export function workItems(events: readonly AssistantEvent[], running: boolean): WorkItem[] {
  const rows: WorkItem[] = []
  const pending = new Map<string, WorkItem>()
  events.forEach((event, index) => {
    if (event.type === 'tool_call' && event.callId) {
      const row: WorkItem = { id: event.callId, name: event.name, status: running ? 'working' : 'interrupted' }
      rows.push(row); pending.set(event.callId, row)
    } else if (event.type === 'tool_result') {
      const row = event.callId ? pending.get(event.callId) : undefined
      const status = event.isError ? 'failed' : 'complete'
      if (row) { row.status = status; pending.delete(event.callId!) }
      else rows.push({ id: `result-${index}`, name: event.name, status })
    }
  })
  return rows
}


export interface WorkRecord { items: WorkItem[]; notices: string[]; critique?: string }
export interface ResponseHistory {
  id: string
  /** The final prose message of this engine response, or its preceding turn if interrupted before prose. */
  messageId?: string
  afterTurnId?: string
  documents: ChatAttachment[]
  websites: WebsiteReference[]
  sourceIds: string[]
  work: WorkRecord
}
export function summarizeWork(events: readonly AssistantEvent[], running: boolean): WorkRecord {
  return { items: workItems(events, running), notices: [...new Set(events.flatMap(event =>
    event.type === 'guardrail' && event.action !== 'narrowed' ? [event.detail]
    : event.type === 'thinking_unavailable' ? [event.detail] : []))],
    critique: [...events].reverse().find(event => event.type === 'critique')?.text }
}
export const attachmentKey = (file: ChatAttachment) => `${file.id}/${file.document?.digest ?? ''}`
export function mergeReferences(previous: ChatAttachment[], next: ChatAttachment[]): ChatAttachment[] {
  const files = new Map(previous.map(file => [attachmentKey(file), file]))
  for (const file of next) {
    const before = files.get(attachmentKey(file))
    files.set(attachmentKey(file), before?.document && file.document ? {...file, document: {...file.document,
      pages: [...new Set([...before.document.pages, ...file.document.pages])].sort((a,b)=>a-b)}} : file)
  }
  return [...files.values()]
}
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
const id = (v: unknown): v is string => typeof v === 'string' && /^[a-zA-Z0-9-]{1,100}$/.test(v)
function invalid(): never { throw new Error('Saved message details are invalid. History was left untouched.') }
export function readMessageId(value: unknown): string | undefined { if (value === undefined) return; return id(value) ? value : invalid() }
export function readAttachments(value: unknown, limit = 256): ChatAttachment[] {
  if (!Array.isArray(value) || value.length > limit) return invalid()
  const files = value.map(file => {
    if (!object(file) || typeof file.id !== 'string' || typeof file.name !== 'string' || typeof file.text !== 'string' || file.text.length > 200_000) return invalid()
    const ref = file.document
    if (ref !== undefined && (!object(ref) || ref.id !== file.id || !/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(file.id) || typeof ref.digest !== 'string' || !/^sha256:[a-f0-9]{64}$/.test(ref.digest) || !Array.isArray(ref.pages) || ref.pages.length > 500 || ref.pages.some(n => !Number.isSafeInteger(n) || n < 1) || new Set(ref.pages).size !== ref.pages.length || typeof ref.allowPartial !== 'boolean' || file.text !== '')) return invalid()
    if (file.link !== undefined && (!ref || !object(file.link) || !validWebURL(file.link.url) || file.link.anchor !== undefined && (typeof file.link.anchor !== 'string' || new TextEncoder().encode(file.link.anchor).length > 4096 || /[\x00-\x1f\x7f]/.test(file.link.anchor)))) return invalid()
    return structuredClone(file) as unknown as ChatAttachment
  })
  if (new Set(files.map(attachmentKey)).size !== files.length) return invalid()
  return files
}
export function readResponseHistory(value: unknown): ResponseHistory[] {
  if (value === undefined) return []
  if (!Array.isArray(value)) return invalid()
  const records = value.map(row => {
    if (!object(row) || !id(row.id) || !Array.isArray(row.websites) || row.websites.length > 16 || !row.websites.every(validWebsiteReference)
      || !Array.isArray(row.sourceIds) || row.sourceIds.some(s => typeof s !== 'string' || !/^src-\d+$/.test(s)) || !object(row.work)
      || !Array.isArray(row.work.items) || !Array.isArray(row.work.notices) || row.work.notices.some(n => typeof n !== 'string')
      || row.work.critique !== undefined && typeof row.work.critique !== 'string') return invalid()
    for (const item of row.work.items) if (!object(item) || typeof item.id !== 'string' || typeof item.name !== 'string' || !['working','complete','failed','interrupted'].includes(String(item.status))) return invalid()
    return {id: row.id, messageId: readMessageId(row.messageId), afterTurnId: readMessageId(row.afterTurnId), documents: readAttachments(row.documents),
      websites: structuredClone(row.websites), sourceIds: [...row.sourceIds], work: {items: structuredClone(row.work.items) as WorkItem[], notices: [...row.work.notices] as string[], ...(typeof row.work.critique === 'string' ? {critique: row.work.critique} : {})}}
  })
  if (new Set(records.map(r => r.id)).size !== records.length) return invalid()
  return records
}
