import { msg } from '../i18n'
import type { ResearchConfig } from '../config/deskConfig'
import { acquire, newResearchSession, seal } from '../research/gatewayClient'
import {parseJsonText,type JsonNode} from '../research/verify/canon'
import {jobsRequest} from './wire'
import { canonicalize,parseJSON } from '../dialect/canon'
import { readDocumentRecord } from '../documents/record'
import { connectionFailure, type DriveSelection } from '../connections/client'
import type { SourceInput, InputPreview } from './client'
import { jobsAPI } from './client'
import type { InputPlan, ExecutableDialect, ProfileEntry, SourceV2 } from './dialectTypes'
import { RetainedJSON } from './wire'
import {importBindings,sourcePin} from '../dialect/bindings'
import { sourcePaths } from './sourceInputs'

export function parseMappedObject(text: string): Record<string, unknown> {
 const value = parseJSON(text)
 if (value === null || typeof value !== 'object' || Array.isArray(value)) throw Error(msg('Choose one JSON object describing a single input record.'))
 return value
}
/** Case values are general retained JSON, not the dialect's canon domain. */
export function parseMappedCase(text:string):Record<string,unknown> {
 const root=parseJsonText(text)
 if(root.kind!=='object')throw Error(msg('Choose one JSON object describing a single input record.'))
 const value=(node:JsonNode):unknown=>node.kind==='number'?(JSON.stringify(Number(node.literal))===node.literal?Number(node.literal):new RetainedJSON(node.literal)):node.kind==='array'?node.items.map(value):node.kind==='object'?Object.fromEntries(node.members.map(m=>[m.name,value(m.value)])):node.kind==='null'?null:node.value
 return value(root) as Record<string,unknown>
}
export function parseMapping(text: string): ExecutableDialect {
 const value = parseMappedObject(text)
 const sources=value.sources===undefined?[]:value.sources
 const named=value.formatVersion==='2.0.0'||value.formatVersion==='3.0.0'
 if (!named && value.version === 2 && value.sources === undefined) value.sources = []
 const object = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
 const copy = (v: unknown) => object(v) && Array.isArray(v.facts) && Array.isArray(v.evidence) && v.facts.every(f => object(f) && typeof f.target === 'string' && typeof f.source === 'string') && v.evidence.every(e => object(e) && typeof e.requirement === 'string' && typeof e.source === 'string')
 if ((!named && value.version !== 2 && value.version !== 3) || (named && (typeof value.id!=='string'||typeof value.version!=='string')) || (value.case !== undefined && !copy(value.case)) || !Array.isArray(sources) || sources.length > 16 || sources.some(s => !object(s) || typeof s.name !== 'string' || !/^[a-z][a-zA-Z0-9_-]{0,63}$/.test(s.name) || !['operation','selected-file','research','http','sql','web'].includes(String(s.kind)) || !object(s.read) || (s.read.copy !== undefined && !copy(s.read.copy)))) throw Error(msg('The inputs could not be mapped.'))
 return value as unknown as ExecutableDialect // Runner performs authoritative schema validation before acquisition.
}
export function mappedCase(mapping: ExecutableDialect, value: Record<string, unknown>, selections: Record<string, DriveSelection>): Record<string, unknown> {
 const result = parseMappedCase(jobsRequest(value))
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
export class PortableResearchRequired extends Error {
 constructor(readonly name:string,readonly input:RetainedJSON){super(msg('Review research for {{source}} before continuing.',{source:name}))}
}
export interface RetainedResearchPrefix {input:RetainedJSON;name:string;response:RetainedJSON}
// Compare general JSON without passing its numbers through floating point.
function sameJSON(a:unknown,b:unknown):boolean {
 if(a===undefined||b===undefined)return a===b
 const order=(n:JsonNode):unknown=>n.kind==='object'?{kind:n.kind,members:[...n.members].sort((a,b)=>a.name<b.name?-1:a.name>b.name?1:0).map(m=>[m.name,order(m.value)])}:n.kind==='array'?{kind:n.kind,items:n.items.map(order)}:n
 return JSON.stringify(order(parseJsonText(jobsRequest(a))))===JSON.stringify(order(parseJsonText(jobsRequest(b))))
}
export async function prepareMappedInputs(options: {
 imported?:{dialect:ExecutableDialect;binding:unknown}; mapping: ExecutableDialect; caseValue: Record<string, unknown>; files: Record<string, SourceInput['snapshot']>; selections: Record<string, DriveSelection>;
 reviewResearch?:boolean;retainedPrefix?:RetainedResearchPrefix;retainedResearch?:Record<string,RetainedJSON>; config: ResearchConfig; profiles: ProfileEntry[]; signal: AbortSignal; progress: (name: string) => void;
}): Promise<InputPreview> {
 const {mapping, config, profiles, signal} = options
 const prefix=options.retainedPrefix
 const retained=prefix?parseMappedCase(prefix.input.text).source as SourceV2:undefined
 const priorSelections=Object.fromEntries(Object.entries(retained?.sources??{}).flatMap(([name,value])=>value.snapshot?.proof?.drive?[[name,value.snapshot.proof.drive]]:[]))
 const expected:{import?:SourceV2['import'];mapping?:ExecutableDialect;case:Record<string,unknown>}={...(options.imported?{import:options.imported}:{mapping}),case:mappedCase(mapping,options.caseValue,{...priorSelections,...options.selections})}
 if(retained&&(!sameJSON(retained.import,expected.import)||!sameJSON(retained.mapping,expected.mapping)||!sameJSON(retained.case,expected.case)||Object.hasOwn(retained.sources,prefix!.name)))throw Error(msg('Research was reviewed for different inputs. Review research again.'))
 const source: Omit<SourceV2,'mapping'>&{mapping?:ExecutableDialect} = {...expected,sources:retained?{...retained.sources,[prefix!.name]:{response:prefix!.response}}:{}}
 if(options.imported&&canonicalize(mapping as never)!==canonicalize(options.imported.dialect as never))throw Error(msg('The imported definition changed. Review a new local dialect before preparing inputs.'))
 const binding=options.imported?importBindings(options.imported.binding):undefined
 // Check all browser relay pins before any call, not only the first source.
 for (const s of mapping.sources ?? []) {
  const pin=sourcePin(s,binding)
  if (!pin) continue
  const trusted = profiles.find(p => p.profile.id === pin.profile && p.digest === pin.profileDigest)?.profile
  // Offline retained research is verified by Runner under its pinned profiles.
  if (Object.hasOwn(source.sources,s.name)||s.kind==='research'&&options.retainedResearch?.[s.name])continue
  if (!trusted || !config.gateway || trusted.authority !== config.gateway.authority || trusted.publicKey !== config.gateway.signer.public) throw Error(msg('The source profile does not match this Desk gateway.'))
  if (s.provider === 'google-drive' && (!config.documents?.enabled || config.managedLocal !== true)) throw Error(msg('Enable document processing in Admin → Document processing before attaching Drive files.'))
 }
 const sessions:string[]=[]
 try {
  for (let count=0; count<=(mapping.sources ?? []).length; count++) {
   signal.throwIfAborted()
   const plan = await jobsAPI<InputPlan>('inputs/next', {source}, undefined, signal)
   signal.throwIfAborted()
   if (plan.input) {
    // Keep the exact acquired bytes; response.json() would round signed numbers.
    const prepared=plan.input.source
    if (options.imported && !prepared?.mapping)throw Error(msg('The inputs could not be mapped.'))
    return {input:{...plan.input, source:{...source,mapping:options.imported?prepared!.mapping:mapping, mappingDigest:prepared?.mappingDigest} as SourceV2}, factsText:plan.factsText ?? '{}', evidenceText:plan.evidenceText ?? ''}
   }
   const next = plan.next, declared = (mapping.sources ?? []).find(s => s.name === next?.name)
   if (!next || !declared || Object.hasOwn(source.sources,next.name) || next.kind !== declared.kind || next.profile !== sourcePin(declared,binding)?.profile || next.provider !== declared.provider) throw Error(msg('The inputs could not be mapped.'))
   options.progress(next.name)
   if(declared.kind==='research') {
    const retained=options.retainedResearch?.[next.name]
    if(!retained){if(options.reviewResearch)throw new PortableResearchRequired(next.name,new RetainedJSON(jobsRequest({source})));throw Error(msg('Supply retained research responses for this source before preparing inputs.'))}
    source.sources[next.name]={response:retained};continue
   }
   if (declared.provider === 'local-file') {
    const snapshot = options.files[next.name]
    if (!snapshot) throw Error(msg('Choose JSON file'))
    sourcePaths(snapshot); source.sources[next.name] = {snapshot}; continue
   }
   const profile = profiles.find(p => p.profile.id === next.profile)?.profile
   if (!profile || next.source !== profile.source || !next.arguments) throw Error(msg('The inputs could not be mapped.'))
   if (declared.provider === 'google-drive' && (next.arguments.fileId !== options.selections[next.name]?.fileId || next.arguments.grant !== options.selections[next.name]?.grant)) throw Error(msg('The inputs could not be mapped.'))
   const session=newResearchSession();sessions.push(session)
   const response = await acquire(session, profile.source, next.arguments, 1 << 20, signal, declared.provider === 'google-drive' ? 'local-documents' : undefined).catch(cause => {
    if (declared.provider === 'google-drive') {
     const failure = connectionFailure(cause, 'google-drive')
     if (failure.reconnectRequired) throw failure
    }
    throw cause
   })
   signal.throwIfAborted()
   if (declared.kind !== 'selected-file') source.sources[next.name] = {response:new RetainedJSON(response.text)}
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
  await Promise.all(sessions.map(session=>seal(session,AbortSignal.timeout(3000),config.managedLocal?'local-documents':undefined).catch(()=>{})))
 }
}
