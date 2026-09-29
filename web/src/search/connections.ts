import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { connectionCall } from '../connections/client'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useConnections } from '../connections/catalog'
import { FileRequestError, readFile, writeFile } from '../files/client'
import { useFileListing } from '../files/queries'

export interface SearchConnection { id:string; revision:string; name:string; provider:string; project?:string; location?:string; model?:string; dailyLimit:number; requests?:number; day?:string }
export interface SearchProvider { id:string; name:string; kind:'search-results'|'grounded-answer'; fields:string[]; docs:string }
export interface SearchStatus { version:1; providers:SearchProvider[]; connections:SearchConnection[] }
export interface SearchPreference { version:1; connection:string|null; mode:'auto'|'provided' }
export const SEARCH_CONNECTIONS_KEY=['web-search-connections'] as const
export const SEARCH_PREFERENCE_FILE='jpack-search.json'
const identifier=(v:unknown):v is string=>typeof v==='string'&&/^[a-z][a-z0-9-]{0,47}$/.test(v)
export function decodeSearchStatus(raw:unknown):SearchStatus {
 const v=raw as SearchStatus
 if(!v||v.version!==1||!Array.isArray(v.providers)||v.providers.length>32||!Array.isArray(v.connections)||v.connections.length>32)throw new Error('Invalid search connection response')
 for(const p of v.providers){
  if(!identifier(p.id)||typeof p.name!=='string'||p.name.length>120||!['search-results','grounded-answer'].includes(p.kind)||!Array.isArray(p.fields)||!p.fields.every(f=>['api-key','project','location','model','service-account-json'].includes(f))||typeof p.docs!=='string'||!p.docs.startsWith('https://'))throw new Error('Unsupported search provider')
 }
 for(const c of v.connections){if(!identifier(c.id)||!/^[a-f0-9]{64}$/.test(c.revision)||typeof c.name!=='string'||c.name.length>80||!v.providers.some(p=>p.id===c.provider)||!Number.isSafeInteger(c.dailyLimit)||c.dailyLimit<1||c.dailyLimit>10000||Object.hasOwn(c,'credential'))throw new Error('Invalid search connection')}
 if(new Set(v.connections.map(c=>c.id)).size!==v.connections.length||new Set(v.providers.map(p=>p.id)).size!==v.providers.length)throw new Error('Invalid search connection response')
 return v
}
export const loadSearchStatus=async(signal?:AbortSignal)=>decodeSearchStatus(await connectionCall('status',{},signal,'web-search'))
export function decodeSearchPreference(value:unknown):SearchPreference {
 const v=value as SearchPreference
 if(!v||v.version!==1||!['auto','provided'].includes(v.mode)||v.connection!==null&&!identifier(v.connection)||Object.keys(v).some(k=>!['version','mode','connection'].includes(k)))throw new Error('Search settings could not be read. Reload before making changes.')
 return v
}
export async function loadSearchPreference(signal?:AbortSignal){
 try{const f=await readFile(SEARCH_PREFERENCE_FILE,signal);if(f.bytes>4096)throw new Error('Search settings are too large');return {value:decodeSearchPreference(JSON.parse(f.content)),digest:f.sha256}}
 catch(e){if(e instanceof FileRequestError&&e.status===404&&e.code==='not-found')return {value:{version:1,connection:null,mode:'auto'} as SearchPreference,digest:''};throw e}
}
export function useSearchPreference(){
 const root=useFileListing().data?.root,client=useQueryClient(),key=['web-search-preference',root]
 const query=useQuery({queryKey:key,queryFn:({signal})=>loadSearchPreference(signal),enabled:!!root,retry:false})
 const mutation=useMutation({mutationFn:async(value:SearchPreference)=>{
  if(!query.data||query.isError)throw new Error('Reload search settings before saving.')
  const content=JSON.stringify(decodeSearchPreference(value),null,2)+'\n'
  const saved=await writeFile({path:SEARCH_PREFERENCE_FILE,content,baseSha256:query.data.digest})
  if(saved.content!==content)throw new Error('Search settings could not be verified.')
  return {value,digest:saved.sha256}
 },onMutate:()=>client.cancelQueries({queryKey:key}),onSuccess:data=>{client.setQueryData(key,data);void client.invalidateQueries({queryKey:['desk-files']});void client.invalidateQueries({queryKey:['desk-file',SEARCH_PREFERENCE_FILE]})}})
 return {...query,save:mutation.mutateAsync,saving:mutation.isPending,saveError:mutation.error}
}
export function useSearchConnections(){
 const effective=useEffectiveConfig()
 const local=effective.desk?.localGateway?.status==='ready'&&!effective.desk?.decoded?.values?.research?.gateway
 const catalog=useConnections(local)
 const available=local&&catalog.webSearch
 const query=useQuery({queryKey:SEARCH_CONNECTIONS_KEY,queryFn:({signal})=>loadSearchStatus(signal),enabled:available,retry:false,staleTime:30_000})
 return {...query,available,loading:catalog.loading||available&&query.isPending}
}
