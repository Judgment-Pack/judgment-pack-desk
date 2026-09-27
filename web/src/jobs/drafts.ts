import { useQuery } from '@tanstack/react-query'
import { listFiles, readFile, writeFile, type FileContent } from '../files/client'
import { msg } from '../i18n'
import type { TriggerConfig } from './triggerTypes'
export interface MappedDraft { text: string; caseText: string }
export interface JobDraftValues {
  name: string; packId: string; inputMode: 'manual' | 'mapped'; facts: string; supplied: boolean; evidence: string
  mapped?: MappedDraft; trigger?: TriggerConfig
}
export interface JobDraft { version: 1; id: string; updatedAt: string; status: 'draft' | 'created'; values: JobDraftValues }
export interface SavedJobDraft { file: FileContent; draft: JobDraft }
const directory = '.desk/job-drafts/'
function path(id: string) {
  if (!/^[a-zA-Z0-9-]{1,80}$/.test(id)) throw Error(msg('Invalid job draft.'))
  return `${directory}${id}.json`
}
function object(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value) }
function strings(value: Record<string, unknown>, keys: string[]) { return keys.every(key => typeof value[key] === 'string') }
function triggerShape(value: unknown): boolean {
  if (!object(value) || !strings(value, ['name', 'kind']) || !['schedule','event','file','cloud'].includes(String(value.kind)) || !['skip','latest'].includes(String(value.missed)) || !['skip','queue'].includes(String(value.overlap)) || typeof value.queueSeconds !== 'number') return false
  if (value.kind === 'schedule' && !value.schedule || value.kind === 'cloud' && !value.cloud || value.kind !== 'event' && !value.input) return false
  if (value.schedule !== undefined && (!object(value.schedule) || !strings(value.schedule,['kind','timezone','startAt']) || !['interval','daily','weekly','once'].includes(String(value.schedule.kind)) || !Number.isFinite(Date.parse(String(value.schedule.startAt))))) return false
  if (value.cloud !== undefined && (!object(value.cloud) || !strings(value.cloud,['connection','subscription','job']))) return false
  if (value.input !== undefined) {
    const input = value.input
    if (!object(input) || !['constant','input-file','mapped-files','mapped-sources'].includes(String(input.kind))) return false
    if (['path','value'].some(key => input[key] !== undefined && typeof input[key] !== 'string')) return false
    if (input.case !== undefined && !object(input.case)) return false
    if (input.files !== undefined && (!object(input.files) || Object.values(input.files).some(path => typeof path !== 'string'))) return false
  }
  return true
}
export function decodeJobDraft(text: string): JobDraft {
  let value: unknown
  try { value = JSON.parse(text) } catch { throw Error(msg('Invalid job draft.')) }
  if (!object(value) || value.version !== 1 || !['draft','created'].includes(String(value.status)) || !strings(value,['id','updatedAt']) || !object(value.values)) throw Error(msg('Invalid job draft.'))
  const v = value.values
  if (!strings(v,['name','packId','facts','evidence']) || typeof v.supplied !== 'boolean' || !['manual','mapped'].includes(String(v.inputMode)) ||
      v.mapped !== undefined && (!object(v.mapped) || !strings(v.mapped,['text','caseText'])) || v.trigger !== undefined && !triggerShape(v.trigger)) throw Error(msg('Invalid job draft.'))
  if (!Number.isFinite(Date.parse(value.updatedAt as string))) throw Error(msg('Invalid job draft.'))
  path(value.id as string)
  return value as unknown as JobDraft
}
export async function loadJobDraft(id: string): Promise<SavedJobDraft> {
  const file = await readFile(path(id)), draft = decodeJobDraft(file.content)
  if (draft.id !== id) throw Error(msg('Invalid job draft.'))
  return { file, draft }
}
/** Explicit configuration only. No picker grants, file snapshots, preview receipts or release approvals. */
export async function saveJobDraft(id: string, values: JobDraftValues, baseSha256 = '', status: JobDraft['status'] = 'draft'): Promise<SavedJobDraft> {
  const draft: JobDraft = { version: 1, id, updatedAt: new Date().toISOString(), status, values: structuredClone({name:values.name,packId:values.packId,inputMode:values.inputMode,facts:values.facts,supplied:values.supplied,evidence:values.evidence,...(values.mapped?{mapped:{text:values.mapped.text,caseText:values.mapped.caseText}}:{}),...(values.trigger?{trigger:values.trigger}:{})}) }
  const content = JSON.stringify(draft, null, 2)
  decodeJobDraft(content)
  const file = await writeFile({ path: path(id), content, baseSha256, createParents: true })
  if (file.content !== content) throw Error(msg('The saved draft could not be verified. Your changes are still here.'))
  return { file, draft }
}
export function useJobDrafts() {
  return useQuery({ queryKey: ['job-drafts'], queryFn: async () => {
    const listing = await listFiles()
    const files = listing.files.filter(file => file.path.startsWith(directory) && file.path.endsWith('.json'))
    const drafts = await Promise.all(files.map(async item => {
      const file = await readFile(item.path), draft = decodeJobDraft(file.content)
      if (path(draft.id) !== item.path) throw Error(msg('Invalid job draft.'))
      return { file, draft }
    }))
    return drafts.filter(item => item.draft.status === 'draft').sort((a, b) => b.draft.updatedAt.localeCompare(a.draft.updatedAt))
  }, retry: false })
}
