import type { ResearchConfig } from '../config/deskConfig'
import type { MappingV2, ProfileEntry } from './mappingTypes'

export function supportsJobProfile({profile}: ProfileEntry) {
 return profile.shape === 'mcp' && Boolean(profile.tools?.length) || profile.source === 'drive' && profile.shape === 'http' && profile.class === 'record'
}
export function matchesJobGateway({profile}: ProfileEntry, config: ResearchConfig) {
 return Boolean(config.gateway && profile.authority === config.gateway.authority && profile.publicKey === config.gateway.signer.public)
}
/** Change authority explicitly, reset its request, and retain only simple output
 * assignments. Advanced derivations/dependencies must be reviewed as JSON. */
export function canChangeIntegration(mapping: MappingV2, name: string) {
 const source = mapping.sources?.find(s => s.name === name)
 return Boolean(source?.read.copy && !source.read.rule && !source.read.unwrap?.length && !Object.keys(source.parameters ?? {}).length &&
  !mapping.sources?.some(s => Object.values(s.parameters ?? {}).some(p => p.from === name)))
}
export function setSourceIntegration(mapping: MappingV2, entry?: ProfileEntry, replace?: string): MappingV2 {
 if(entry && !supportsJobProfile(entry))throw Error('Unsupported job integration')
 const next=structuredClone(mapping)
 const previous=replace ? next.sources?.find(s=>s.name===replace) : undefined
 if(replace && !canChangeIntegration(next,replace))throw Error('Advanced source requires explicit mapping review')
 next.sources ??= []
 let n=1
 while(next.sources.some(s=>s.name===`source${n}`))n++
 const name=previous?.name ?? `source${n}`
 const read={copy:previous?.read.copy ?? {facts:[],evidence:[]}}
 const source = !entry ? {name,kind:'selected-file' as const,provider:'local-file' as const,read} : entry.profile.source==='drive' && !entry.profile.calculator ? {
  name,kind:'selected-file' as const,provider:'google-drive' as const,profile:entry.profile.id,profileDigest:entry.digest,maxAge:300,arguments:{fileId:{$param:`${name}File`},grant:{$param:`${name}Grant`}},read
 } : {name,kind:'operation' as const,profile:entry.profile.id,profileDigest:entry.digest,maxAge:300,arguments:{tool:entry.profile.tools![0],arguments:{}},read,...(entry.profile.calculator?{calculation:{inputs:{},tables:{}}}:{})}
 if(previous)next.sources=next.sources.map(s=>s.name===name?source:s)
 else next.sources.push(source)
 // Retire only this picker’s generated parameters, never arbitrary user bindings.
 if(previous?.provider==='google-drive' && source.provider!=='google-drive' && next.case?.parameters) {
  for(const field of ['fileId','grant']) {
   const param=(previous.arguments?.[field] as {$param?:string}|undefined)?.$param
   if(param && next.case.parameters[param]?.pointer===`/selections/${name}/${field}` && !JSON.stringify(next.sources).includes(JSON.stringify({$param:param})))delete next.case.parameters[param]
  }
 }
 if(source.provider==='google-drive') {
  next.case ??= {facts:[],evidence:[]}
  next.case.parameters ??= {}
  for(const [suffix,field] of [['File','fileId'],['Grant','grant']]) {
   const key=`${name}${suffix}`, pointer=`/selections/${name}/${field}`
   const existing=next.case.parameters[key]
   if(existing && (existing.pointer!==pointer || existing.type!=='string' || existing.from))throw Error('Source parameter name is already used')
   next.case.parameters[key]={pointer,type:'string'}
  }
 }
 return next
}
