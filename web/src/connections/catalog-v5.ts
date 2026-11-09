import {parseJsonText} from '../research/verify/canon'

// Owner facts describe the distributed contract; they grant no authority.
export interface CatalogTaxonomy { operation: string; effect: string; checkKind?: string; experimental?: boolean }
export interface CatalogProfile { effect: string; operations: string[] }
export interface SourceOwner { taxonomy?: CatalogTaxonomy; profile?: CatalogProfile; capabilityTaxonomies?: Record<string,CatalogTaxonomy>; dispatchTaxonomies?: Record<string,CatalogTaxonomy> }
export interface ProviderOwner { taxonomy?: CatalogTaxonomy; profile?: CatalogProfile; operationTaxonomies?: Record<string,CatalogTaxonomy>; operationDeclarations?: Record<string,{taxonomy:CatalogTaxonomy;profile:CatalogProfile;components:Record<string,CatalogTaxonomy>}> }
type Row = Record<string,unknown>
const object=(v:unknown):v is Row=>Boolean(v && typeof v==='object' && !Array.isArray(v))
const exact=(v:unknown,keys:string[]):v is Row=>object(v) && Object.keys(v).length===keys.length && keys.every(k=>Object.hasOwn(v,k))
const identifier=(v:unknown):v is string=>typeof v==='string' && /^[a-z][a-z0-9-]{0,47}$/.test(v)
const effects=['pure','read','read-external','propose','act']
const operations=['discover','read','answer','check','transform','evaluate','propose','act','run','own-state','ask']
const external=(operation:string):CatalogTaxonomy=>({operation,effect:'read-external'})
function taxonomy(v:unknown):v is CatalogTaxonomy {
 if(!object(v) || !exact(v,['operation','effect',...(Object.hasOwn(v,'checkKind')?['checkKind']:[]),...(Object.hasOwn(v,'experimental')?['experimental']:[])]) || typeof v.operation!=='string' || typeof v.effect!=='string' || Object.hasOwn(v,'experimental') && typeof v.experimental!=='boolean') return false
 const allowed:Record<string,string[]>={discover:effects.slice(0,3),answer:effects.slice(0,3),check:effects.slice(0,3),read:['read','read-external'],transform:['pure'],ask:['pure'],propose:['propose'],act:['act'],run:effects.slice(1),'own-state':['pure','read']}
 return Object.hasOwn(allowed,v.operation) && allowed[v.operation]!.includes(v.effect) && (v.operation==='check'?typeof v.checkKind==='string' && ['validate','check','verify','test','analyze','advise'].includes(v.checkKind):!Object.hasOwn(v,'checkKind'))
}
const equalTaxonomy=(a:CatalogTaxonomy,b:CatalogTaxonomy)=>a.operation===b.operation && a.effect===b.effect && a.checkKind===b.checkKind && Boolean(a.experimental)===Boolean(b.experimental)
function owners(v:unknown):v is Record<string,CatalogTaxonomy> {return object(v) && Object.keys(v).length<=32 && Object.entries(v).every(([k,t])=>k.length>0 && k.length<=96 && taxonomy(t))}
function equalOwners(a:Record<string,CatalogTaxonomy>,b:Record<string,CatalogTaxonomy>) {return Object.keys(a).length===Object.keys(b).length && Object.entries(b).every(([k,t])=>Object.hasOwn(a,k) && equalTaxonomy(a[k]!,t))}
function summary(t:unknown,p:unknown,parts:Record<string,CatalogTaxonomy>) {
 const values=Object.values(parts); if(!values.length || !taxonomy(t) || !exact(p,['effect','operations']) || !Array.isArray(p.operations))return false
 const effect=effects[Math.max(...values.map(t=>effects.indexOf(t.effect)))]!
 const same=values.every(t=>t.operation===values[0]!.operation && t.checkKind===values[0]!.checkKind)
 const expected:CatalogTaxonomy={operation:same?values[0]!.operation:'run',effect,...(same && values[0]!.checkKind?{checkKind:values[0]!.checkKind}:{}),experimental:values.some(t=>t.experimental)}
 const ops=operations.filter(op=>values.some(t=>t.operation===op))
 return equalTaxonomy(t,expected) && p.effect===effect && p.operations.length===ops.length && p.operations.every((op,i)=>op===ops[i])
}
// Closed dispatch selectors declared by the pinned Gateway owner, including dots.
function sourceOwners(id:unknown):Record<string,CatalogTaxonomy>|undefined {
 switch(id){
 case 'web':return {page:external('read')}
 case 'web-discovery':return {discover:external('discover')}
 case 'web-search':return {answer:external('answer'),search:external('discover')}
 case 'drive':case 'gmail':case 'notion':case 'obsidian':case 'aws-s3':return {read:external('read')}
 case 'databricks':return {statement:{operation:'act',effect:'act'},catalog:external('discover'),mcp:{operation:'run',effect:'act'}}
 case 'azure-devops':return {'workitems.query':external('discover'),'workitems.query-and-get':external('discover'),'workitems.get':external('read'),'repos.list':external('discover'),'repos.items':external('read'),'repos.commits':external('discover'),'pullrequests.list':external('discover'),'pipelines.list':external('discover'),'pipelines.runs':external('discover'),'pipelines.run':external('read'),mcp:{operation:'run',effect:'act'}}
 }
}
function controlOwner(method:string):CatalogTaxonomy|undefined {
 switch(method){
 case 'status':case 'files-status':case 'source-bindings':return {operation:'read',effect:'read'}
 case 'configure':case 'disconnect':case 'select':case 'files-commit':case 'source-bind':case 'source-revoke':return {operation:'act',effect:'act'}
 case 'connect':case 'poll':case 'cancel':return {operation:'run',effect:'act'}
 case 'search':case 'files-list':return external('discover')
 case 'files-read':case 'source-observe':return external('read')
 case 'files-prepare':case 'files-prepare-google-document':return {operation:'propose',effect:'propose'}
 case 'test':return {operation:'check',effect:'read-external',checkKind:'test'}
 }
}
const sourceKeys=['taxonomy','profile','capabilityTaxonomies','dispatchTaxonomies']
function source(s:unknown,provider:boolean) {
 if(!object(s) || !exact(s,[...(provider?['id','shape','record']:['id','input','mediaTypes','maxBytes']),'capabilities',...(Object.hasOwn(s,'adapter')?['adapter']:[]),...sourceKeys]) || !Array.isArray(s.capabilities) || !owners(s.capabilityTaxonomies) || !owners(s.dispatchTaxonomies))return false
 if (!identifier(s.id)) return false
 if (!provider && (!identifier(s.input) || !Number.isSafeInteger(s.maxBytes) || Number(s.maxBytes)<1 || Number(s.maxBytes)>16<<20 || !Array.isArray(s.mediaTypes) || !s.mediaTypes.length || s.mediaTypes.length>8 || new Set(s.mediaTypes).size!==s.mediaTypes.length || !s.mediaTypes.every(m=>typeof m==='string' && /^[a-z][a-z0-9.+-]{0,63}\/[a-z][a-z0-9.+-]{0,63}$/.test(m))))return false
 const expected=sourceOwners(s.id);if(!expected || !equalOwners(s.dispatchTaxonomies,expected) || !summary(s.taxonomy,s.profile,s.dispatchTaxonomies))return false
 const caps:Record<string,CatalogTaxonomy>={}
 for(const name of s.capabilities){if(typeof name!=='string' || !['answer','search','page'].includes(name) || Object.hasOwn(caps,name))return false;caps[name]=external(({answer:'answer',search:'discover',page:'read'} as Record<string,string>)[name]!)}
 return equalOwners(s.capabilityTaxonomies,caps)
}
export function parseCatalogText(raw:string):unknown {
 if(new TextEncoder().encode(raw).length>128<<10)throw Error('Invalid connection catalog')
 const node=parseJsonText(raw)
 const integers=(n:typeof node):boolean=>n.kind==='number'?/^-?(0|[1-9][0-9]*)$/.test(n.literal) && Number.isSafeInteger(Number(n.literal)):n.kind==='array'?n.items.every(integers):n.kind==='object'?n.members.every(m=>integers(m.value)):true
 if(!integers(node))throw Error('Invalid connection catalog')
 return JSON.parse(raw) as unknown
}
export function validCatalogV5(v:unknown):boolean {
 if(!exact(v,['version','providers','sources','discoveryTaxonomy']) || !taxonomy(v.discoveryTaxonomy) || !equalTaxonomy(v.discoveryTaxonomy,{operation:'discover',effect:'pure'}) || !Array.isArray(v.sources) || !Array.isArray(v.providers))return false
 if(!v.sources.every(s=>source(s,false)))return false
 for(const p of v.providers){
  if(!exact(p,['id','auth','registration','selection','queryRequired','operations','protocol','queryMode','presentation','setup','authorizationEndpoints','source','taxonomy','profile','operationTaxonomies','operationDeclarations']) || !source(p.source,true) || !Array.isArray(p.operations) || !owners(p.operationTaxonomies) || !object(p.operationDeclarations) || Object.keys(p.operationTaxonomies).length!==p.operations.length || Object.keys(p.operationDeclarations).length!==p.operations.length)return false
  if(!identifier(p.auth) || !identifier(p.registration) || !identifier(p.selection))return false
  const constituents:Record<string,CatalogTaxonomy>={}
  for(const method of p.operations){
   if(typeof method!=='string' || !Object.hasOwn(p.operationDeclarations,method))return false
   const intrinsic=controlOwner(method),d=p.operationDeclarations[method]
   if(!intrinsic || !exact(d,['taxonomy','profile','components']) || !owners(d.components) || !equalOwners(d.components,{operation:intrinsic,'operator-policy':{operation:'act',effect:'act'}}) || !summary(d.taxonomy,d.profile,d.components) || !Object.hasOwn(p.operationTaxonomies,method) || !taxonomy(d.taxonomy) || !equalTaxonomy(p.operationTaxonomies[method]!,d.taxonomy))return false
   for(const [role,t] of Object.entries(d.components))constituents[`${method}/${role}`]=t
  }
  if(!summary(p.taxonomy,p.profile,constituents))return false
  // V5 retains the exact historical presentation grammar too.
  if(!exact(p.presentation,['name','description','instructions','icon']) || !Array.isArray(p.setup) || !p.setup.every(f=>exact(f,['key','type','label','required'])))return false
 }
 return true
}
