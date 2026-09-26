import { msg } from '../i18n'
import type { ResearchConfig } from '../config/deskConfig'
import { acquire, newResearchSession, seal } from '../research/gatewayClient'
import { canonicalize, parseJsonText } from '../research/verify/canon'
import { readDocumentRecord } from '../documents/record'
import type { DriveSelection } from '../connections/client'
import type { SourceInput, InputPreview } from './client'
import { jobsAPI } from './client'
import type { InputPlan, MappingV2, ProfileEntry, SourceV2 } from './mappingTypes'
import { RetainedJSON } from './wire'
import { sourcePaths } from './sourceInputs'

export function parseMappedObject(text: string): Record<string, unknown> {
 const node = parseJsonText(text)
 // The gateway's canonical domain rejects lossy numbers and duplicate keys.
 canonicalize(node)
 if (node.kind !== 'object') throw Error(msg('Choose one JSON object describing a single input record.'))
 return JSON.parse(text) as Record<string, unknown>
}
export function parseMapping(text: string): MappingV2 {
 const value = parseMappedObject(text)
 if (value.sources === undefined) value.sources = []
 const object = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
 const copy = (v: unknown) => object(v) && Array.isArray(v.facts) && Array.isArray(v.evidence) && v.facts.every(f => object(f) && typeof f.target === 'string' && typeof f.source === 'string') && v.evidence.every(e => object(e) && typeof e.requirement === 'string' && typeof e.source === 'string')
 if (value.version !== 2 || (value.case !== undefined && !copy(value.case)) || !Array.isArray(value.sources) || value.sources.length > 16 || value.sources.some(s => !object(s) || typeof s.name !== 'string' || !/^[a-z][a-zA-Z0-9_-]{0,63}$/.test(s.name) || !['operation','selected-file'].includes(String(s.kind)) || !object(s.read) || (s.read.copy !== undefined && !copy(s.read.copy)))) throw Error(msg('The inputs could not be mapped.'))
 return value as unknown as MappingV2 // Runner performs authoritative schema validation before acquisition.
}
export function mappedCase(mapping: MappingV2, value: Record<string, unknown>, selections: Record<string, DriveSelection>): Record<string, unknown> {
 const result = JSON.parse(JSON.stringify(value)) as Record<string, unknown>
 for (const source of (mapping.sources ?? []).filter(s => s.provider === 'google-drive')) {
  const selection = selections[source.name]
  if (!selection) throw Error(msg('Choose from Google Drive'))
  for (const field of ['fileId','grant'] as const) {
   const argument = source.arguments?.[field] as { $param?: string } | undefined
   const parameter = argument?.$param ? mapping.case?.parameters?.[argument.$param] : undefined
   // A picker grant may only fill an explicit string case parameter. Never splice query text.
   if (!parameter || parameter.from || parameter.type !== 'string' || !/^\/(?:[^~]|~[01])+$/.test(parameter.pointer)) throw Error(msg('The inputs could not be mapped.'))
   const parts = parameter.pointer.slice(1).split('/').map(s => s.replace(/~1/g,'/').replace(/~0/g,'~'))
   if (parts.some(p => ['__proto__','constructor','prototype'].includes(p))) throw Error(msg('The inputs could not be mapped.'))
   let at = result
   for (const part of parts.slice(0,-1)) {
    if (at[part] === undefined) at[part] = {}
    if (!at[part] || Array.isArray(at[part]) || typeof at[part] !== 'object') throw Error(msg('The inputs could not be mapped.'))
    at = at[part] as Record<string, unknown>
   }
   at[parts[parts.length-1]!] = selection[field]
  }
 }
 return result
}
export async function prepareMappedInputs(options: {
 mapping: MappingV2; caseValue: Record<string, unknown>; files: Record<string, SourceInput['snapshot']>; selections: Record<string, DriveSelection>;
 config: ResearchConfig; profiles: ProfileEntry[]; signal: AbortSignal; progress: (name: string) => void;
}): Promise<InputPreview> {
 const {mapping, config, profiles, signal} = options
 const source: SourceV2 = {mapping, case: mappedCase(mapping, options.caseValue, options.selections), sources: {}}
 // Check all browser relay pins before any call, not only the first source.
 for (const s of mapping.sources ?? []) {
  if (!s.profile) continue
  const trusted = profiles.find(p => p.profile.id === s.profile && p.digest === s.profileDigest)?.profile
  if (!trusted || !config.gateway || trusted.authority !== config.gateway.authority || trusted.publicKey !== config.gateway.signer.public) throw Error(msg('The source profile does not match this Desk gateway.'))
  if (s.provider === 'google-drive' && (!config.documents?.enabled || config.managedLocal !== true)) throw Error(msg('Enable document processing in Admin → Storage & data before attaching Drive files.'))
 }
 const session = newResearchSession()
 let calledGateway = false
 try {
  for (let count=0; count<=(mapping.sources ?? []).length; count++) {
   signal.throwIfAborted()
   const plan = await jobsAPI<InputPlan>('inputs/next', {source}, undefined, signal)
   signal.throwIfAborted()
   if (plan.input) {
    // Keep the exact acquired bytes; response.json() would round signed numbers.
    return {input:{...plan.input, source:{...source, mappingDigest:plan.input.source?.mappingDigest}}, factsText:plan.factsText ?? '{}', evidenceText:plan.evidenceText ?? ''}
   }
   const next = plan.next, declared = (mapping.sources ?? []).find(s => s.name === next?.name)
   if (!next || !declared || Object.hasOwn(source.sources,next.name) || next.kind !== declared.kind || next.profile !== declared.profile || next.provider !== declared.provider) throw Error(msg('The inputs could not be mapped.'))
   options.progress(next.name)
   if (declared.provider === 'local-file') {
    const snapshot = options.files[next.name]
    if (!snapshot) throw Error(msg('Choose JSON file'))
    sourcePaths(snapshot); source.sources[next.name] = {snapshot}; continue
   }
   const profile = profiles.find(p => p.profile.id === next.profile)?.profile
   if (!profile || next.source !== profile.source || !next.arguments) throw Error(msg('The inputs could not be mapped.'))
   if (declared.provider === 'google-drive' && (next.arguments.fileId !== options.selections[next.name]?.fileId || next.arguments.grant !== options.selections[next.name]?.grant)) throw Error(msg('The inputs could not be mapped.'))
   calledGateway = true
   const response = await acquire(session, profile.source, next.arguments, 1 << 20, signal, declared.provider === 'google-drive' ? 'local-documents' : undefined)
   signal.throwIfAborted()
   if (declared.kind === 'operation') source.sources[next.name] = {response:new RetainedJSON(response.text)}
   else {
    const record = readDocumentRecord(JSON.parse(response.text).result)
    if (!record.original.bytes || record.provenance.source.kind !== 'google-drive') throw Error(msg('The file could not be read.'))
    const snapshot: SourceInput['snapshot'] = {version:1,original:{name:record.document.name,mediaType:record.document.mediaType,bytes:record.original.bytes,sha256:record.document.id},proof:{session,source:'drive',authority:profile.authority,publicKey:profile.publicKey,response:response.text,registry:'',drive:options.selections[next.name]!}}
    sourcePaths(snapshot); source.sources[next.name] = {snapshot}
   }
  }
  throw Error(msg('The inputs could not be mapped.'))
 } finally {
  // Close the gateway session for housekeeping; Runner verifies receipts, not
  // the seal. A cleanup failure must not replace the actual preparation error.
  if (calledGateway) { const cleanup = AbortSignal.timeout(3000); await seal(session, cleanup, config.managedLocal ? 'local-documents' : undefined).catch(() => {}) }
 }
}
