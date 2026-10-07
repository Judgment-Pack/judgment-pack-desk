import { sourceMessage } from '../i18n/source'
import { createContext, useContext, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { answer, deskFetch } from '../files/client'
import { decodeDeskConfig, type AssistantConfig, type EffectiveConfig } from '../config/deskConfig'
import { applyAssistantProfile, type AssistantProfileRead, type ModelPreferences } from '../config/assistantProfile'
import type { AssistantConfigWrite, AssistantConfigWritten } from './client'

export const AI_CONNECTIONS_KEY=['ai-connections'] as const
export const validAIConnectionId=(id:unknown):id is string=>typeof id==='string'&&/^(legacy-api|legacy-codex|ai-[a-f0-9]{24})$/.test(id)
export interface AIConnection { id:string; name:string; enabled:boolean; assistant:AssistantConfig; revision:string }
export interface AIRegistry { version:1; defaultConnection:string; connections:AIConnection[]; sha256:string; path:string }
export interface AIRegistryRead { data?:AIRegistry; loading:boolean; problem?:string }
export interface AITarget { connectionId?:string; connectionRevision?:string; connectionName?:string }
export const aiHeaders=(id?:string,revision?:string):Record<string,string>=>id?{'X-Assistant-Connection':id,...(revision?{'X-Assistant-Revision':revision}:{})}:{}
export function decodeAIRegistry(value:AIRegistry):AIRegistry {
 if(value?.version!==1||!Array.isArray(value.connections)||value.connections.length>32||typeof value.sha256!=='string'||typeof value.path!=='string'||new Set(value.connections.map(c=>c.id)).size!==value.connections.length)throw new Error(sourceMessage('AI connections could not be read.'))
 const connections=value.connections.map(c=>{
  if(!validAIConnectionId(c.id)||typeof c.name!=='string'||!c.name||typeof c.enabled!=='boolean'||!/^\w{64}$/.test(c.revision))throw new Error(sourceMessage('AI connections could not be read.'))
  const decoded=decodeDeskConfig(JSON.stringify({deskConfigVersion:1,assistant:c.assistant}),'desk')
  if(decoded.problems.length||!decoded.values?.assistant)throw new Error(sourceMessage('AI connections could not be read.'))
  return {...c,assistant:decoded.values.assistant}
 })
 if(typeof value.defaultConnection!=='string'||value.defaultConnection&&!connections.some(c=>c.id===value.defaultConnection&&c.enabled))throw new Error(sourceMessage('AI connections could not be read.'))
 return {...value,connections}
}
export function useAIRegistry(){return useQuery({queryKey:AI_CONNECTIONS_KEY,queryFn:async({signal})=>decodeAIRegistry(await answer<AIRegistry>(await deskFetch('/api/ai-connections',{signal}))),retry:false,staleTime:10_000,refetchInterval:15_000})}
export async function writeAIRegistry(base:AIRegistry,connections:AIConnection[],defaultConnection=base.defaultConnection):Promise<AIRegistry>{
 return decodeAIRegistry(await answer<AIRegistry>(await deskFetch('/api/ai-connections',{method:'PUT',headers:{'Content-Type':'application/json','If-Match':base.sha256},body:JSON.stringify({version:1,defaultConnection,connections:connections.map(({revision:_,...c})=>c)})})))
}
export function modelPreferences(read:AssistantProfileRead|undefined,c:AIConnection):ModelPreferences|undefined {
 const p=read?.value
 return p?.profileVersion===2 ? p.inherit?undefined:p.models?.[c.id] : c.id==='legacy-api'?p?.api:c.id==='legacy-codex'?p?.codex:undefined
}
export function connectionAssistant(c:AIConnection,read?:AssistantProfileRead):AssistantConfig {
 const prefs=modelPreferences(read,c)
 return applyAssistantProfile(c.assistant,{present:true,value:{profileVersion:1,...(c.assistant.engine==='codex'?{codex:prefs}:{api:prefs})}})
}
export function deskAIConnections(registry:AIRegistry,read?:AssistantProfileRead):AIConnection[]{
 const p=read?.value
 return registry.connections.filter(c=>c.enabled&&(p?.profileVersion!==2||p.inherit||p.connections?.includes(c.id)))
}
export function resolveAIConnection(effective:EffectiveConfig,id?:string):{connection?:AIConnection;assistant:AssistantConfig;problem?:string}{
 const state=effective.aiConnections
 if(!state)return {assistant:effective.config.assistant}
 const empty:AssistantConfig={engine:'vercel',endpoint:null,thinking:'off'}
 if(state.loading&&!state.data)return {assistant:empty,problem:sourceMessage('Loading AI connections…')}
 if(state.problem||!state.data)return {assistant:empty,problem:sourceMessage('AI connections could not be read. Reload Connections > AI.')}
 if(effective.assistantProfile?.problem)return {assistant:empty,problem:effective.assistantProfile.problem}
 const p=effective.assistantProfile?.value
 const chosen=id??(p?.profileVersion===2&&!p.inherit?p.defaultConnection:state.data.defaultConnection)
 if(!chosen)return {assistant:empty,problem:sourceMessage('Choose a default AI connection in Assistant settings.')}
 const c=deskAIConnections(state.data,effective.assistantProfile).find(c=>c.id===chosen)
 if(!c)return {assistant:empty,problem:sourceMessage('This AI connection is unavailable for this desk. Choose another connection.')}
 return {connection:c,assistant:connectionAssistant(c,effective.assistantProfile)}
}
export interface AIConnectionScopeValue {connection:AIConnection;registry:AIRegistry}
const Scope=createContext<AIConnectionScopeValue|undefined>(undefined)
export function AIConnectionScope({value,children}:{value:AIConnectionScopeValue;children:ReactNode}){return <Scope.Provider value={value}>{children}</Scope.Provider>}
export function useAIConnectionScope(){return useContext(Scope)}
export function useConnectionConfigWriter(){
 const scope=useAIConnectionScope(),client=useQueryClient()
 if(!scope)return undefined
 return async (input:AssistantConfigWrite):Promise<AssistantConfigWritten>=>{
  if(input.ifMatch!==scope.registry.sha256)throw new Error(sourceMessage('AI connections changed. Reload before saving.'))
  const decoded=decodeDeskConfig(JSON.stringify({deskConfigVersion:1,assistant:input.assistant}),'desk')
  if(decoded.problems.length||!decoded.values?.assistant)throw new Error(sourceMessage('Check the connection settings before saving.'))
  const assistant=decoded.values.assistant
  if(assistant.engine!==scope.connection.assistant.engine)throw new Error(sourceMessage('Add another connection to use a different connection method.'))
  const next=await writeAIRegistry(scope.registry,scope.registry.connections.map(c=>c.id===scope.connection.id?{...c,assistant}:c))
  client.setQueryData(AI_CONNECTIONS_KEY,next)
  const row=next.connections.find(c=>c.id===scope.connection.id)!
  return {path:next.path,sha256:next.sha256,assistant:row.assistant,created:false,keyRebindRequired:false,connectionRevision:row.revision}
 }
}
